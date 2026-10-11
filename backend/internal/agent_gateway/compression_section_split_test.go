package agent_gateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

func contractMsg(content string) llm.Message {
	return MarkContract(llm.Message{Role: "system", Content: content})
}

func materialMsg(content string) llm.Message {
	return MarkMaterial(llm.Message{Role: "system", Content: content})
}

// TestCompressScored_ContractNeverDropped（M8-2 第 2 轮）：
// 契约块（baseRules + 工具协议）无论预算多紧都必须原样保留 —— 压它会销毁指令、
// 让模型编造证据（v2.10 实测教训）。
func TestCompressScored_ContractNeverDropped(t *testing.T) {
	cfg := SmartCompressionConfig{Enabled: true, KeepRecentForcedN: 3, ScoreThreshold: 2.0}
	msgs := []llm.Message{contractMsg("CONTRACT-RULES-绝不可压")}
	for i := 0; i < 20; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: fmt.Sprintf("turn-%02d", i)})
	}

	out, _ := CompressScored(msgs, BudgetSnapshot{Status: StatusExhausted}, cfg, CompressionInfo{})

	found := false
	for _, m := range out {
		if IsContractSystemMessage(m) && strings.Contains(m.Content, "CONTRACT-RULES-绝不可压") {
			found = true
		}
	}
	if !found {
		t.Fatalf("契约块必须原样保留，got %d 条消息", len(out))
	}
	if len(out) >= len(msgs) {
		t.Errorf("预算极度紧张时应有消息被丢（省 token），got %d/%d", len(out), len(msgs))
	}
}

// TestCompressScored_MaterialFloorKeepsNewestDropsOldest（M8-2 第 2 轮）：
// 素材块可被丢弃以省 token，但"最新 MaterialKeepRecentN 条"必须保留（防丢光历史）。
func TestCompressScored_MaterialFloorKeepsNewestDropsOldest(t *testing.T) {
	cfg := SmartCompressionConfig{
		Enabled:                true,
		KeepRecentForcedN:      3,
		MaterialKeepRecentN:    6,
		ScoreThreshold:         2.0, // 逼 greedy 不额外保留 → 只剩 recent-forced + 素材下限
		SummaryInsertThreshold: 100, // 关掉兜底摘要，避免摘要预览把"被丢内容"又带回输出
	}
	msgs := []llm.Message{contractMsg("CONTRACT")}
	for i := 0; i < 12; i++ {
		msgs = append(msgs, materialMsg(fmt.Sprintf("MATERIAL-%02d", i)))
	}

	out, info := CompressScored(msgs, BudgetSnapshot{Status: StatusExhausted}, cfg, CompressionInfo{})

	if info.MaterialForcedKept == 0 {
		t.Errorf("应记录素材下限强制保留条数")
	}
	// 最新 6 条（06..11）必须在
	for i := 6; i < 12; i++ {
		want := fmt.Sprintf("MATERIAL-%02d", i)
		if !containsContent(out, want) {
			t.Errorf("最新素材 %s 必须保留", want)
		}
	}
	// 最旧 6 条（00..05）应被丢弃
	droppedAny := false
	for i := 0; i < 6; i++ {
		if !containsContent(out, fmt.Sprintf("MATERIAL-%02d", i)) {
			droppedAny = true
		}
	}
	if !droppedAny {
		t.Errorf("最旧素材应可被丢弃以省 token")
	}
	if !containsContent(out, "CONTRACT") {
		t.Errorf("契约块必须保留")
	}
}

// TestCompressLegacy_KeepsAllSystemMessages（M8-2 第 2 轮）：
// 拆分后有多条 system（契约 + 素材），legacy 路径必须全部保留，
// 旧实现只留第一条、把其余当普通消息淘汰 → 会误删素材块。
func TestCompressLegacy_KeepsAllSystemMessages(t *testing.T) {
	msgs := []llm.Message{
		contractMsg("CONTRACT"),
		materialMsg("MATERIAL-A"),
		materialMsg("MATERIAL-B"),
	}
	for i := 0; i < 10; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: fmt.Sprintf("u%d", i)})
	}

	out, _ := CompressLegacy(msgs, CompressionInfo{})

	for _, want := range []string{"CONTRACT", "MATERIAL-A", "MATERIAL-B"} {
		if !containsContent(out, want) {
			t.Errorf("legacy 必须保留全部 system 消息，缺 %s", want)
		}
	}
}

func containsContent(msgs []llm.Message, sub string) bool {
	for _, m := range msgs {
		if strings.Contains(m.Content, sub) {
			return true
		}
	}
	return false
}

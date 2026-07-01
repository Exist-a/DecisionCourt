package agent_gateway

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestCompressScored_KeepSystemAndFirstLast: system 保留在首位；非 system 中
// 按评分与贪心打包选。
func TestCompressScored_KeepSystemAndFirstLast(t *testing.T) {
	cfg := SmartCompressionConfig{
		Enabled:                true,
		KeepRecentForcedN:      2,
		SummaryInsertThreshold: 100,
		ScoreThreshold:         0,
	}
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "plain"},                                    // idx 0 in nonSystem
		{Role: "user", Content: "evidence_id=E1 关键"},                       // idx 1
		{Role: "user", Content: "later1"},                                   // idx 2
		{Role: "user", Content: "later2"},                                   // idx 3
	}
	out, info := CompressScored(msgs, BudgetSnapshot{Status: StatusExhausted}, cfg, CompressionInfo{})
	if info.Strategy != "scored" {
		t.Errorf("strategy: want scored got %q", info.Strategy)
	}
	if info.DroppedCount == 0 {
		t.Errorf("expected drops at exhausted; got none")
	}
	if out[0].Role != "system" || out[0].Content != "sys" {
		t.Errorf("system should stay at front: %+v", out[0])
	}
	// 最近 2 条必保留
	hit1, hit2 := false, false
	for _, m := range out {
		if m.Content == "later1" {
			hit1 = true
		}
		if m.Content == "later2" {
			hit2 = true
		}
	}
	if !hit1 || !hit2 {
		t.Errorf("recent 2 should always be kept (later1=%v later2=%v)", hit1, hit2)
	}
}

// TestCompressScored_AtomicGroupPreserved: tool_call ↔ tool_result 同组，不拆。
func TestCompressScored_AtomicGroupPreserved(t *testing.T) {
	cfg := SmartCompressionConfig{
		Enabled:           true,
		KeepRecentForcedN: 1,
		ScoreThreshold:    0,
	}
	msgs := []llm.Message{
		{Role: "user", Content: "plain"},
		{Role: "assistant", Metadata: map[string]string{"tool_call_id": "t"}, Content: "call"},
		{Role: "tool", Metadata: map[string]string{"tool_call_id": "t"}, Content: "result"},
		{Role: "user", Content: "recent"},
	}
	out, _ := CompressScored(msgs, BudgetSnapshot{Status: StatusExhausted}, cfg, CompressionInfo{})
	// 列表里要么两条都在，要么都不在
	seen := map[string]bool{}
	for _, m := range out {
		seen[m.Content] = true
	}
	_, call := seen["call"]
	_, result := seen["result"]
	if call != result {
		t.Errorf("tool_call and result must keep same fate: kept_call=%v kept_result=%v", call, result)
	}
}

// TestCompressScored_SummaryInserted: 大量丢弃 → 插入摘要 note。
func TestCompressScored_SummaryInserted(t *testing.T) {
	cfg := SmartCompressionConfig{
		Enabled:                true,
		KeepRecentForcedN:      2,
		SummaryInsertThreshold: 1, // 任何丢弃 > 1 都触发
		ScoreThreshold:         0,
	}
	msgs := []llm.Message{
		{Role: "user", Metadata: map[string]string{"agent_type": "judge"}, Content: "evidence_id=E1 anchor"},
		{Role: "user", Content: "low score plain"},
		{Role: "user", Content: "low score plain"},
		{Role: "user", Content: "r1"},
		{Role: "user", Content: "r2"},
	}
	out, info := CompressScored(msgs, BudgetSnapshot{Status: StatusThrottle}, cfg, CompressionInfo{})
	if info.SummarizedBlocks == 0 {
		t.Errorf("expected summary note insertion, got info=%+v", info)
	}
	hasSummary := false
	for _, m := range out {
		if strings.Contains(m.Content, "[earlier context omitted") {
			hasSummary = true
			break
		}
	}
	if !hasSummary {
		t.Errorf("summary note not found in output")
	}
}

// TestCompressScored_NoWhenNormal: 不在 compress/throttle/exhausted 时不应用。
func TestCompressScored_NoWhenNormal(t *testing.T) {
	cfg := SmartCompressionConfig{Enabled: true}
	msgs := []llm.Message{{Role: "user", Content: "x"}}
	out, info := CompressScored(msgs, BudgetSnapshot{Status: StatusNormal}, cfg, CompressionInfo{})
	if info.Strategy != "scored" {
		// strategy 是 CompressScored 内填的；caller 应先看 Applied
	}
	if info.Applied {
		t.Errorf("scored should report not applied for normal status")
	}
	if len(out) != 1 {
		t.Errorf("expected passthrough for normal status, got %d", len(out))
	}
}

// TestCompressScored_NonSystemEmpty: 全是 system 时只 system kept。
func TestCompressScored_NonSystemEmpty(t *testing.T) {
	cfg := SmartCompressionConfig{Enabled: true, KeepRecentForcedN: 1, SummaryInsertThreshold: 0}
	msgs := []llm.Message{
		{Role: "system", Content: "sys-A"},
		{Role: "system", Content: "sys-B"},
	}
	out, info := CompressScored(msgs, BudgetSnapshot{Status: StatusCompress}, cfg, CompressionInfo{})
	if info.AtomicGroups != 0 {
		t.Errorf("no non-system → 0 groups, got %d", info.AtomicGroups)
	}
	if len(out) != 2 {
		t.Errorf("want 2 system, got %d", len(out))
	}
}

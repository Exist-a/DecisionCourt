package agent_gateway

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestTranscriptHistoryMarker_LiteralPinned 钉住区段标记的字面量。
//
// 压缩器靠它把 system 切成 [契约头][历史段]；若有人改动这里却没同步
// agent 包（或反之），截断方向修复会静默失效。agent 包另有一枚"两端一致"
// 的护栏测试。
func TestTranscriptHistoryMarker_LiteralPinned(t *testing.T) {
	const want = "\n\n## 庭审历史（按时间顺序）\n"
	if TranscriptHistoryMarker != want {
		t.Fatalf("marker 漂移：got %q want %q", TranscriptHistoryMarker, want)
	}
}

// buildHistorySystem 造一条超长 system 消息：[契约头] + 标记 + N 条历史行。
func buildHistorySystem(headBytes, lines int) (head, content string) {
	for len(head) < headBytes {
		head += "契约"
	}
	var sb strings.Builder
	sb.WriteString(head)
	sb.WriteString(TranscriptHistoryMarker)
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&sb, "- [speech] 第 %03d 条历史发言\n", i)
	}
	return head, sb.String()
}

// TestTruncateSystemContent_KeepsNewestDropsOldest 是 M8-2 A1 的直接回归钉：
// 超长时保留**最新**发言、丢弃**最旧**的，且契约头一字不动。
func TestTruncateSystemContent_KeepsNewestDropsOldest(t *testing.T) {
	head, content := buildHistorySystem(10_000, 700)
	if len(content) <= compressMaxSystemMsgLen {
		t.Fatalf("测试前置不成立：content=%d 未超过 %d", len(content), compressMaxSystemMsgLen)
	}

	out := truncateSystemContent(content, compressSystemTargetLen)

	if !utf8.ValidString(out) {
		t.Fatalf("结果不是合法 UTF-8")
	}
	if !strings.HasPrefix(out, head) {
		t.Errorf("契约头部必须原样保留（旧实现会砍掉头部）")
	}
	if strings.Contains(out, "第 000 条历史发言") {
		t.Errorf("最旧的历史行应被丢弃")
	}
	if !strings.Contains(out, "第 699 条历史发言") {
		t.Errorf("最新的历史行必须保留（修复的核心）")
	}
	if !strings.Contains(out, "已省略较早") {
		t.Errorf("应插入省略计数，让模型知道历史被裁过")
	}
	if len(out) > compressSystemTargetLen {
		t.Errorf("截断后应落在目标 %d 字节内，got %d", compressSystemTargetLen, len(out))
	}
}

// TestTruncateSystemContent_NoMarkerKeepsHead 无历史标记时退回"契约优先"，
// 保留头部（宁可丢尾部素材也不能丢指令）。
func TestTruncateSystemContent_NoMarkerKeepsHead(t *testing.T) {
	head := strings.Repeat("规则", 6000) // 18000 字节 > 目标
	out := truncateSystemContent(head, compressSystemTargetLen)

	if !strings.HasPrefix(out, "规则") {
		t.Errorf("无标记时应保留头部")
	}
	if len(out) > compressSystemTargetLen {
		t.Errorf("应截到目标以内，got %d", len(out))
	}
	if !strings.HasSuffix(out, compressTruncateMark) {
		t.Errorf("应带压缩标记")
	}
}

// TestTruncateSystemContent_HeadExceedsTarget 头部本身就超目标时只能保头部。
func TestTruncateSystemContent_HeadExceedsTarget(t *testing.T) {
	head := strings.Repeat("契约", 4000) // 24000 字节
	content := head + TranscriptHistoryMarker + "- [speech] x\n"
	out := truncateSystemContent(content, compressSystemTargetLen)

	if !strings.HasPrefix(out, head) {
		t.Errorf("头部本身超目标时仍须原样保留（否则丢契约）")
	}
	if strings.Contains(out, "- [speech]") {
		t.Errorf("头部已超目标，历史行应整体丢弃")
	}
}

// TestTruncateOversizedMessages_RoutesSystemToHistoryAwarePath 确认超长 system
// 消息经 truncateOversizedMessages 走的是"保最近"路径，而非旧的砍尾。
func TestTruncateOversizedMessages_RoutesSystemToHistoryAwarePath(t *testing.T) {
	_, content := buildHistorySystem(10_000, 700)
	msgs := []llm.Message{{Role: "system", Content: content}}

	total := truncateOversizedMessages(msgs)

	if total != len(msgs[0].Content) {
		t.Errorf("返回值应与内容长度一致：want %d got %d", len(msgs[0].Content), total)
	}
	if !strings.Contains(msgs[0].Content, "第 699 条历史发言") {
		t.Errorf("最新的历史行必须保留")
	}
	if strings.Contains(msgs[0].Content, "第 000 条历史发言") {
		t.Errorf("最旧的历史行应被丢弃")
	}
}

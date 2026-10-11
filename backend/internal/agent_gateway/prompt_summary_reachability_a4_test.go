package agent_gateway

import (
	"fmt"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestCompressScored_SummaryReachableAtReactDroppableBound 是 M8-2 A4 的回归钉。
//
// 根因：兜底摘要触发条件 `droppedCount > SummaryInsertThreshold`，原默认阈值 5；
// 而 ReAct 路径 MaxIterations=4 → 非 system ≤ 8，KeepRecentForcedN=3 强制保留最近
// 3 条 → droppable ≤ 5，实测最大 4 → 分支永不触发（死代码）。
//
// 修复：默认阈值下调到 3。本测试用"ReAct 形状 + greedy 一条不额外保留"逼出
// droppable=4，断言摘要确实插入 —— 若将来 MaxIterations / KeepRecentForcedN /
// 阈值再被改到互不匹配，这里会失败。
func TestCompressScored_SummaryReachableAtReactDroppableBound(t *testing.T) {
	pc := NewPromptCompressor(SmartCompressionConfig{
		Enabled:           true,
		KeepRecentForcedN: 3,
		ScoreThreshold:    2.0, // 高于任何可能分值 → greedy 不额外保留一条 → droppable 取 ReAct 上限
	}, nil)
	if got := pc.cfg.SummaryInsertThreshold; got != 3 {
		t.Fatalf("A4：默认 SummaryInsertThreshold 应为 3，got %d", got)
	}

	msgs := []llm.Message{{Role: "system", Content: "sys"}}
	// 4 条可丢（带 evidence_id 锚点）+ 3 条强制保留的最近消息 = ReAct 形状的非 system。
	for i := 0; i < 4; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: fmt.Sprintf("evidence_id=E%d old turn", i+1)})
	}
	for i := 0; i < 3; i++ {
		msgs = append(msgs, llm.Message{Role: "user", Content: fmt.Sprintf("recent-%d", i)})
	}

	out, info := CompressScored(msgs, BudgetSnapshot{Status: StatusCompress}, pc.cfg, CompressionInfo{})
	if info.DroppedCount != 4 {
		t.Fatalf("测试前置：期望 droppable=4（ReAct 上限），got %d", info.DroppedCount)
	}
	if info.SummarizedBlocks == 0 {
		t.Fatalf("droppable=4 > 默认阈值 3，兜底摘要必须触发（A4 修复前阈值 5 永不触发）")
	}
	found := false
	for _, m := range out {
		if strings.Contains(m.Content, "[earlier context omitted") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("输出应含 earlier-context 摘要")
	}
}

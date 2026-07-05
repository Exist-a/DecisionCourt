package agent

// v0.9.1 (ADR 0015) withArgumentSummaryText user_interrupt 注入测试。
//
// 实战场景:用户在 cross-exam 期间用"补充信息"按钮插入"医生让我休息",
// 之前的实现 (orchestrator.go line 286) 用 `if m.ActionType != "speak"` 过滤掉所有
// user_interrupt → 用户补充完全没进 prompt → 下一轮 agent 看不到。
//
// v0.9.1 修复:在摘要最显眼位置显示【用户最新补充】,让 agent 优先看到。

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
)

func TestWithArgumentSummaryText_IncludesUserInterrupt(t *testing.T) {
	t.Parallel()

	messages := []model.Message{
		{ActionType: "speak", Content: "对方发言内容"},
		{ActionType: "user_interrupt", Content: "医生让我休息"},
		{ActionType: "speak", Content: "另一方发言"},
	}

	got := withArgumentSummaryText(messages, model.AgentProsecutor, model.AgentDefender)

	// ✅ 关键断言:用户补充必须出现在摘要里
	if !strings.Contains(got, "用户最新补充") {
		t.Error("摘要应当用【用户最新补充】标注用户输入")
	}
	if !strings.Contains(got, "医生让我休息") {
		t.Error("用户补充内容必须原文出现,不能让 agent 看不到")
	}
	// ✅ 必须明确告诉 agent 这不是对方的论点
	if !strings.Contains(got, "不要把它当作对方的论点") {
		t.Error("摘要必须明确引导 agent 不要把用户补充当作对方论点")
	}
}

func TestWithArgumentSummaryText_NoUserInterrupt_OldBehavior(t *testing.T) {
	t.Parallel()

	messages := []model.Message{
		{ActionType: "speak", Content: "对方发言"},
	}

	got := withArgumentSummaryText(messages, model.AgentProsecutor, model.AgentDefender)

	// 没 user_interrupt 时不应出现【用户最新补充】段落
	if strings.Contains(got, "用户最新补充") {
		t.Error("没有 user_interrupt 时不应插入用户补充段落")
	}
}

func TestWithArgumentSummaryText_PicksLatestUserInterrupt(t *testing.T) {
	t.Parallel()

	messages := []model.Message{
		{ActionType: "user_interrupt", Content: "第一次补充"},
		{ActionType: "speak", Content: "某方发言"},
		{ActionType: "user_interrupt", Content: "最新补充: 医生让我休息"},
	}

	got := withArgumentSummaryText(messages, model.AgentProsecutor, model.AgentDefender)

	// 必须取最新的 user_interrupt(覆盖旧的)
	if !strings.Contains(got, "最新补充: 医生让我休息") {
		t.Error("应当取最新的 user_interrupt,忽略旧的")
	}
	if strings.Contains(got, "第一次补充") {
		t.Error("旧的 user_interrupt 不应再出现,只显示最新")
	}
}
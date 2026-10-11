package agent_gateway

import (
	"context"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/stretchr/testify/require"
)

// 2026-10-10 第 3 轮「成本维度接线」护栏。
//
// 背景（docs/AGENT-GATEWAY-BUDGET-PLAN.md §2）：`cost_usd` 字段与 `limitCostUSD`
// 上限一直存在，但网关**从不赋值**、`classify` **从不看它** → 生产恒为 0、成本上限
// 永不触发。这组测试把"定价 → 算 cost → 参与档位判定 → 上指标"整条链钉住。

func TestParsePricingJSON(t *testing.T) {
	t.Run("空字符串 = 不配定价（合法）", func(t *testing.T) {
		m, err := ParsePricingJSON("")
		require.NoError(t, err)
		require.Nil(t, m)
	})
	t.Run("合法 JSON", func(t *testing.T) {
		m, err := ParsePricingJSON(`{"deepseek-chat":{"input_per_1m":0.27,"output_per_1m":1.10}}`)
		require.NoError(t, err)
		require.InDelta(t, 0.27, m["deepseek-chat"].InputPer1M, 1e-12)
		require.InDelta(t, 1.10, m["deepseek-chat"].OutputPer1M, 1e-12)
	})
	t.Run("非法 JSON → 报错（由调用方 WARN，不静默）", func(t *testing.T) {
		_, err := ParsePricingJSON(`{not json`)
		require.Error(t, err)
	})
	t.Run("负价 → 报错", func(t *testing.T) {
		_, err := ParsePricingJSON(`{"m":{"input_per_1m":-1,"output_per_1m":1}}`)
		require.Error(t, err)
	})
}

func TestPricingTable_CostUSD(t *testing.T) {
	pt := NewPricingTable(map[string]ModelPricing{
		"m": {InputPer1M: 1.0, OutputPer1M: 2.0},
	})
	// 1M input × $1 + 0.5M output × $2 = $2
	require.InDelta(t, 2.0, pt.CostUSD("m", 1_000_000, 500_000), 1e-9)

	t.Run("未知模型记 0（并只 WARN 一次）", func(t *testing.T) {
		require.Equal(t, 0.0, pt.CostUSD("nope", 1_000_000, 1_000_000))
		require.Equal(t, 0.0, pt.CostUSD("nope", 5, 5), "第二次调用仍应为 0，且不应 panic")
	})
	t.Run("空表 / nil 表 = 恒 0（不 panic）", func(t *testing.T) {
		require.Equal(t, 0.0, NewPricingTable(nil).CostUSD("m", 1000, 1000))
		var nilTable *PricingTable
		require.Equal(t, 0.0, nilTable.CostUSD("m", 1000, 1000))
	})
}

// 成本占比必须参与档位判定，且与 token 共用 70/80/100 阈值（决策：另一把同向的尺子）。
func TestMemStore_CostRatioDrivesStatus(t *testing.T) {
	ctx := context.Background()
	// token 上限给得很宽（不会因 token 触发），成本上限 $10 → 70% = $7
	ms := NewMemStore(10_000_000, 10.0, 0.7, 0.8, 5*time.Minute)

	require.NoError(t, ms.AddUsage(ctx, "s1", BudgetUsage{InputTokens: 10, CostUSD: 6.9}))
	snap, err := ms.Check(ctx, "s1")
	require.NoError(t, err)
	require.Equal(t, StatusNormal, snap.Status, "成本 69% < 70% 应仍是 normal")

	require.NoError(t, ms.AddUsage(ctx, "s1", BudgetUsage{InputTokens: 10, CostUSD: 0.2}))
	snap, _ = ms.Check(ctx, "s1")
	require.Equal(t, StatusCompress, snap.Status, "成本 ≥70% 应触发 compress（此前完全被忽略）")
	require.InDelta(t, 0.71, snap.Ratio, 1e-9, "snap.Ratio 应取成本口径（较严）")

	require.NoError(t, ms.AddUsage(ctx, "s1", BudgetUsage{InputTokens: 10, CostUSD: 3.0}))
	snap, _ = ms.Check(ctx, "s1")
	require.Equal(t, StatusExhausted, snap.Status, "成本 ≥100% 应 exhausted")
}

// limitCost=0（未配成本上限）时成本维度必须**完全不参与**，行为与接线前一致。
func TestMemStore_NoCostLimitKeepsTokenOnlyBehavior(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	require.NoError(t, ms.AddUsage(ctx, "s1", BudgetUsage{CostUSD: 99999}))
	snap, err := ms.Check(ctx, "s1")
	require.NoError(t, err)
	require.Equal(t, StatusNormal, snap.Status, "未配成本上限时再高的 cost 也不该影响档位")
	require.Equal(t, 0.0, snap.Ratio)
}

func TestMemStore_MaxRatioAndUsedTokens(t *testing.T) {
	ctx := context.Background()
	ms := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	require.NoError(t, ms.AddUsage(ctx, "s1", BudgetUsage{InputTokens: 100}))
	require.NoError(t, ms.AddUsage(ctx, "s2", BudgetUsage{InputTokens: 500}))

	require.InDelta(t, 0.5, ms.MaxRatio(), 1e-9, "取最高会话水位（s2 = 500/1000）")
	require.Equal(t, 600, ms.UsedTokens())
}

// 端到端：Complete 一次 → 定价参与 → cost 落到预算快照（R19 指标的取值来源）。
func TestGateway_CompleteRecordsCostFromPricing(t *testing.T) {
	inner := &fakeLLM{
		completeContent: `{"action":"speak","content":"hi"}`,
		completeUsage:   llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 500_000},
	}
	g := NewWithConfig(inner, nil, "m", GatewayConfig{
		Enabled:     true,
		TokenBudget: true,
		PricingJSON: `{"m":{"input_per_1m":1.0,"output_per_1m":2.0}}`,
	}, nil)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "sess-cost", AgentType: "prosecutor", TaskType: "speak"})
	_, _, err := g.Complete(ctx, "", nil, llm.CompletionOptions{})
	require.NoError(t, err)

	snap := g.budget.Check(ctx, "sess-cost")
	require.InDelta(t, 2.0, snap.CostUSD, 1e-9,
		"1M input×$1 + 0.5M output×$2 = $2（此前恒为 0）")
	require.InDelta(t, 1.5, float64(snap.TotalTokens)/1e6, 1e-9)
}

// 未配定价时不 panic，cost 记 0（默认行为；同时验证一次性 WARN 路径不炸）。
func TestGateway_CompleteWithoutPricingRecordsZeroCost(t *testing.T) {
	inner := &fakeLLM{completeContent: `{}`, completeUsage: llm.Usage{PromptTokens: 100, CompletionTokens: 50}}
	g := NewWithConfig(inner, nil, "m", GatewayConfig{Enabled: true, TokenBudget: true}, nil)
	ctx := WithTrace(context.Background(), Trace{SessionUUID: "sess-nopricing", AgentType: "judge", TaskType: "assess"})
	_, _, err := g.Complete(ctx, "", nil, llm.CompletionOptions{})
	require.NoError(t, err)
	require.Equal(t, 0.0, g.budget.Check(ctx, "sess-nopricing").CostUSD)
}

package agent_gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
)

// ModelPricing 是某模型每 100 万 token 的美元单价。
//
// 定价**不在代码里硬编码猜测值**：上游价格会变（DeepSeek 已公告调价），写死等于在
// 账单口径上撒谎。价格由运维通过 `AGENT_GATEWAY_PRICING_JSON` 提供；未配置价格的
// 模型记 `cost=0` 并**一次性 WARN**（不是静默 0 —— 静默 0 会让"成本上限"看起来
// 永远没触发，正是 deferred-items-2026-10-09 §B1 的病根）。
type ModelPricing struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
}

// PricingTable 按模型名查单价，并对"未知价格"做一次性告警。
// 零值/空表可用：CostUSD 恒返回 0（不 panic）。
type PricingTable struct {
	mu     sync.Mutex
	prices map[string]ModelPricing
	warned map[string]bool
}

// NewPricingTable 构造定价表（prices 为 nil 时视为无定价）。
func NewPricingTable(prices map[string]ModelPricing) *PricingTable {
	return &PricingTable{prices: prices, warned: map[string]bool{}}
}

// ParsePricingJSON 解析 `AGENT_GATEWAY_PRICING_JSON`。
// 形如：{"deepseek-chat":{"input_per_1m":0.27,"output_per_1m":1.10}}
//
// 空字符串 → 返回空表（合法：表示"不配定价"，cost 记 0）。
// 非法 JSON / 负价 → 返回错误，由调用方 WARN（不 fail-fast：定价缺失不该阻断庭审）。
func ParsePricingJSON(raw string) (map[string]ModelPricing, error) {
	if raw == "" {
		return nil, nil
	}
	var out map[string]ModelPricing
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse pricing json: %w", err)
	}
	for model, p := range out {
		if p.InputPer1M < 0 || p.OutputPer1M < 0 {
			return nil, fmt.Errorf("pricing for %q has negative rate (in=%v out=%v)", model, p.InputPer1M, p.OutputPer1M)
		}
	}
	return out, nil
}

// CostUSD 计算一次调用的美元成本（未知模型返回 0，并只 WARN 一次）。
func (t *PricingTable) CostUSD(model string, promptTokens, completionTokens int) float64 {
	if t == nil || len(t.prices) == 0 {
		return 0
	}
	p, ok := t.prices[model]
	if !ok {
		t.mu.Lock()
		if !t.warned[model] {
			t.warned[model] = true
			slog.Warn("agent_gateway: model has no configured price; cost recorded as 0",
				"model", model,
				"hint", `设置 AGENT_GATEWAY_PRICING_JSON={"<model>":{"input_per_1m":X,"output_per_1m":Y}}`)
		}
		t.mu.Unlock()
		return 0
	}
	return float64(promptTokens)/1e6*p.InputPer1M + float64(completionTokens)/1e6*p.OutputPer1M
}

// HasPrices 报告是否配置了任何单价（供启动期日志使用）。
func (t *PricingTable) HasPrices() bool {
	return t != nil && len(t.prices) > 0
}

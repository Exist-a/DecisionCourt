package search

import (
	"context"
	"fmt"
)

type Result struct {
	Title   string
	URL     string
	Content string
	Score   float64
}

type Provider interface {
	Name() string
	Search(ctx context.Context, query string) ([]Result, error)
}

type mockProvider struct{}

func NewMockProvider() Provider {
	return &mockProvider{}
}

func (m *mockProvider) Name() string {
	return "mock"
}

func (m *mockProvider) Search(_ context.Context, query string) ([]Result, error) {
	// v0.8.3 安全(P3-2 query escape):mock 也走 sanitize,保持测试与生产一致
	cleanQuery, err := SanitizeQuery(query)
	if err != nil {
		return nil, fmt.Errorf("mock: %w", err)
	}
	return []Result{
		{
			Title:   fmt.Sprintf("关于 %s 的搜索结果", cleanQuery),
			URL:     "https://example.com/search",
			Content: fmt.Sprintf("根据行业数据，%s 相关领域在 2026 年保持增长趋势，早期参与者可能获得较高回报。", cleanQuery),
			Score:   0.8,
		},
		{
			Title:   fmt.Sprintf("%s 的风险分析", cleanQuery),
			URL:     "https://example.com/risk",
			Content: fmt.Sprintf("然而，%s 也面临不确定性，包括市场波动和竞争加剧。", cleanQuery),
			Score:   0.6,
		},
	}, nil
}

// NewProvider returns a Provider by name. apiKey is needed by Bocha and
// Tavily; pass "" for providers that don't need one (mock / duckduckgo).
//
// v0.8.3 决定：SearXNG 已弃用,provider 选项只剩 mock / duckduckgo / bocha / tavily。
// 未知 provider 名称:有 apiKey 走 bocha,无 apiKey 走 duckduckgo。
func NewProvider(providerName, apiKey string) (Provider, error) {
	switch providerName {
	case "mock":
		return NewMockProvider(), nil
	case "duckduckgo":
		return NewDuckDuckGoProvider(), nil
	case "bocha":
		if apiKey == "" {
			return nil, fmt.Errorf("bocha provider requires BOCHA_API_KEY")
		}
		return NewBochaProvider(apiKey), nil
	case "tavily":
		// tavily 实现尚未交付;留 case 防止 switch 静默回落到 default
		return nil, fmt.Errorf("tavily provider not implemented yet")
	case "":
		// providerName 为空(没设 SEARCH_PROVIDER)→ 按 apiKey 自动选
		if apiKey != "" {
			return NewBochaProvider(apiKey), nil
		}
		return NewDuckDuckGoProvider(), nil
	default:
		if apiKey != "" {
			return NewBochaProvider(apiKey), nil
		}
		return NewDuckDuckGoProvider(), nil
	}
}

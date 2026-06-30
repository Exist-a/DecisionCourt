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
	return []Result{
		{
			Title:   fmt.Sprintf("关于 %s 的搜索结果", query),
			URL:     "https://example.com/search",
			Content: fmt.Sprintf("根据行业数据，%s 相关领域在 2026 年保持增长趋势，早期参与者可能获得较高回报。", query),
			Score:   0.8,
		},
		{
			Title:   fmt.Sprintf("%s 的风险分析", query),
			URL:     "https://example.com/risk",
			Content: fmt.Sprintf("然而，%s 也面临不确定性，包括市场波动和竞争加剧。", query),
			Score:   0.6,
		},
	}, nil
}

// NewProvider returns a Provider by name. apiKey is needed by Bocha and
// Tavily; pass "" for providers that don't need one (mock / duckduckgo /
// searxng). An unknown provider name falls back to Bocha when an apiKey is
// provided, otherwise to DuckDuckGo.
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
	case "searxng":
		return nil, fmt.Errorf("searxng provider not implemented yet")
	case "tavily":
		return nil, fmt.Errorf("tavily provider not implemented yet")
	default:
		if apiKey != "" {
			return NewBochaProvider(apiKey), nil
		}
		return NewDuckDuckGoProvider(), nil
	}
}

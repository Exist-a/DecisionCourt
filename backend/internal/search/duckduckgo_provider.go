package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const duckDuckGoDefaultBaseURL = "https://api.duckduckgo.com/"

type duckDuckGoProvider struct {
	baseURL string
}

// NewDuckDuckGoProvider creates a provider that hits the public DuckDuckGo API.
func NewDuckDuckGoProvider() Provider {
	return &duckDuckGoProvider{baseURL: duckDuckGoDefaultBaseURL}
}

// NewDuckDuckGoProviderWithBaseURL lets tests inject a mock HTTP server URL.
func NewDuckDuckGoProviderWithBaseURL(baseURL string) Provider {
	return &duckDuckGoProvider{baseURL: baseURL}
}

func (d *duckDuckGoProvider) Name() string {
	return "duckduckgo"
}

func (d *duckDuckGoProvider) Search(ctx context.Context, query string) ([]Result, error) {
	// v0.8.3 安全(P3-2 query escape):user-controlled query 必须先 sanitize
	cleanQuery, err := SanitizeQuery(query)
	if err != nil {
		return nil, fmt.Errorf("duckduckgo: %w", err)
	}
	params := url.Values{}
	params.Set("q", cleanQuery)
	params.Set("format", "json")
	params.Set("pretty", "1")
	params.Set("no_html", "1")
	params.Set("skip_disambig", "1")

	fullURL := fmt.Sprintf("%s?%s", d.baseURL, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 15 * 1000000000}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed with status: %s", resp.Status)
	}

	var results struct {
		Results []struct {
			Title string `json:"Title"`
			URL   string `json:"FirstURL"`
			Text  string `json:"Text"`
		} `json:"Results"`
		RelatedTopics []struct {
			Result string `json:"Result"`
			URL    string `json:"FirstURL"`
			Text   string `json:"Text"`
		} `json:"RelatedTopics"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var searchResults []Result
	for _, r := range results.Results {
		if r.Title != "" && r.URL != "" {
			searchResults = append(searchResults, Result{
				Title:   r.Title,
				URL:     r.URL,
				Content: r.Text,
				Score:   0.8,
			})
		}
	}

	for _, r := range results.RelatedTopics {
		if r.Result != "" && r.URL != "" {
			searchResults = append(searchResults, Result{
				Title:   r.Result,
				URL:     r.URL,
				Content: r.Text,
				Score:   0.6,
			})
		}
	}

	if len(searchResults) == 0 {
		return []Result{
			{
				Title:   fmt.Sprintf("关于 %s 的搜索结果", query),
				URL:     "https://duckduckgo.com",
				Content: fmt.Sprintf("搜索结果未返回相关信息，请尝试更具体的关键词。"),
				Score:   0.5,
			},
		}, nil
	}

	return searchResults, nil
}
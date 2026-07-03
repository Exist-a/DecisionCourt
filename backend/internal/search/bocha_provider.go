package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// bochaDefaultBaseURL is the production endpoint for Bocha AI Web Search API.
// See: https://bocha-ai.feishu.cn/wiki/RXEOw02rFiwzGSkd9mUcqoeAnNK
const bochaDefaultBaseURL = "https://api.bochaai.com"

// bochaDefaultTimeout caps an individual Search request. The Bocha API
// typically responds in <1s; 15s is generous without blocking court flow.
const bochaDefaultTimeout = 15 * time.Second

type bochaProvider struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewBochaProvider creates a provider that hits the public Bocha AI Web
// Search API. apiKey must be set; an empty key makes Search return an error.
func NewBochaProvider(apiKey string) Provider {
	return &bochaProvider{
		apiKey:  apiKey,
		baseURL: bochaDefaultBaseURL,
		http:    &http.Client{Timeout: bochaDefaultTimeout},
	}
}

// NewBochaProviderWithBaseURL lets tests inject a mock HTTP server URL and
// shorten the timeout so a hung server doesn't slow CI.
func NewBochaProviderWithBaseURL(apiKey, baseURL string) Provider {
	return &bochaProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (b *bochaProvider) Name() string { return "bocha" }

// bochaRequest mirrors the documented POST body. Only fields we currently
// send are populated; the API tolerates omitted optional fields.
type bochaRequest struct {
	Query     string `json:"query"`
	Count     int    `json:"count,omitempty"`
	Summary   bool   `json:"summary,omitempty"`
	Freshness string `json:"freshness,omitempty"`
}

// bochaWebPage mirrors the per-result fields we consume.
type bochaWebPage struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Snippet         string `json:"snippet"`
	Summary         string `json:"summary"`
	SiteName        string `json:"siteName"`
	DateLastCrawled string `json:"dateLastCrawled"`
}

// bochaResponse mirrors the outer response. The real API wraps results in
// a top-level `data` object, which we decode here.
type bochaResponse struct {
	Data struct {
		QueryContext struct {
			OriginalQuery string `json:"originalQuery"`
		} `json:"queryContext"`
		WebPages struct {
			WebSearchURL          string         `json:"webSearchUrl"`
			TotalEstimatedMatches int            `json:"totalEstimatedMatches"`
			Value                 []bochaWebPage `json:"value"`
		} `json:"webPages"`
	} `json:"data"`
}

func (b *bochaProvider) Search(ctx context.Context, query string) ([]Result, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("bocha: API key is required")
	}

	// v0.8.3 安全(P3-2 query escape):user-controlled query 必须先 sanitize
	cleanQuery, err := SanitizeQuery(query)
	if err != nil {
		return nil, fmt.Errorf("bocha: %w", err)
	}

	body, err := json.Marshal(bochaRequest{
		Query:     query,
		Count:     10,
		Summary:   true,
		Freshness: "noLimit",
	})
	if err != nil {
		return nil, fmt.Errorf("bocha: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(b.baseURL, "/") + "/v1/web-search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("bocha: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bocha: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bocha: status %d: %s", resp.StatusCode, string(errBody))
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bocha: read body: %w", err)
	}

	var parsed bochaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("bocha: parse response: %w", err)
	}

	results := make([]Result, 0, len(parsed.Data.WebPages.Value))
	for _, page := range parsed.Data.WebPages.Value {
		if page.Name == "" || page.URL == "" {
			continue
		}
		content := page.Summary
		if content == "" {
			content = page.Snippet
		}
		results = append(results, Result{
			Title:   page.Name,
			URL:     page.URL,
			Content: content,
			Score:   0.8,
		})
	}

	if len(results) == 0 {
		results = append(results, Result{
			Title:   fmt.Sprintf("未找到与 %s 相关的高质量结果", cleanQuery),
			URL:     "",
			Content: fmt.Sprintf("博查搜索 %q 未返回相关信息。", cleanQuery),
			Score:   0.5,
		})
	}

	return results, nil
}
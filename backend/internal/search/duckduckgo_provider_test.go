package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDDGResponse 模拟 DuckDuckGo API 返回的 JSON 结构。
type fakeDDGResponse struct {
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

func TestDuckDuckGoProvider_Name(t *testing.T) {
	p := NewDuckDuckGoProvider()
	if got := p.Name(); got != "duckduckgo" {
		t.Fatalf("Name() = %q, want %q", got, "duckduckgo")
	}
}

func TestDuckDuckGoProvider_Search_ParsesResultsAndRelatedTopics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("q"), "跳槽") {
			t.Errorf("expected query to contain 跳槽, got %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("expected format=json, got %q", r.URL.Query().Get("format"))
		}

		resp := fakeDDGResponse{
			Results: []struct {
				Title string `json:"Title"`
				URL   string `json:"FirstURL"`
				Text  string `json:"Text"`
			}{
				{
					Title: "职业发展指南",
					URL:   "https://example.com/career",
					Text:  "跳槽前需评估行业周期",
				},
			},
			RelatedTopics: []struct {
				Result string `json:"Result"`
				URL    string `json:"FirstURL"`
				Text   string `json:"Text"`
			}{
				{
					Result: "跳槽时机",
					URL:    "https://example.com/timing",
					Text:   "经济下行期跳槽需谨慎",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewDuckDuckGoProviderWithBaseURL(srv.URL + "/")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := p.Search(ctx, "是否该跳槽")
	if err != nil {
		t.Fatalf("Search() returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 Results + 1 RelatedTopics), got %d", len(results))
	}

	if results[0].Title != "职业发展指南" {
		t.Errorf("results[0].Title = %q, want %q", results[0].Title, "职业发展指南")
	}
	if results[0].URL != "https://example.com/career" {
		t.Errorf("results[0].URL = %q, want %q", results[0].URL, "https://example.com/career")
	}
	if results[1].Title != "跳槽时机" {
		t.Errorf("results[1].Title = %q, want %q", results[1].Title, "跳槽时机")
	}
}

func TestDuckDuckGoProvider_Search_FallbackOnEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 返回空结果
		_, _ = w.Write([]byte(`{"Results":[],"RelatedTopics":[]}`))
	}))
	defer srv.Close()

	p := NewDuckDuckGoProviderWithBaseURL(srv.URL + "/")

	results, err := p.Search(context.Background(), "极冷门的搜索关键词")
	if err != nil {
		t.Fatalf("Search() returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 fallback result, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "未返回相关信息") {
		t.Errorf("fallback content should mention '未返回相关信息', got %q", results[0].Content)
	}
	if results[0].Score != 0.5 {
		t.Errorf("fallback score = %v, want 0.5", results[0].Score)
	}
}

func TestDuckDuckGoProvider_Search_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := NewDuckDuckGoProviderWithBaseURL(srv.URL + "/")

	_, err := p.Search(context.Background(), "any query")
	if err == nil {
		t.Fatal("expected error on 429 status, got nil")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("error should mention status, got %v", err)
	}
}

func TestDuckDuckGoProvider_Search_NetworkError(t *testing.T) {
	// 使用一个不存在的地址来模拟网络错误
	p := NewDuckDuckGoProviderWithBaseURL("http://127.0.0.1:1/")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := p.Search(ctx, "any query")
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
}

func TestDuckDuckGoProvider_Search_FiltersEmptyFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := fakeDDGResponse{
			Results: []struct {
				Title string `json:"Title"`
				URL   string `json:"FirstURL"`
				Text  string `json:"Text"`
			}{
				{Title: "", URL: "https://example.com/no-title", Text: "无标题"}, // 应被过滤
				{Title: "有效条目", URL: "https://example.com/valid", Text: "有标题有 URL"},
			},
			RelatedTopics: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewDuckDuckGoProviderWithBaseURL(srv.URL + "/")

	results, err := p.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("Search() returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (empty-title filtered out), got %d: %+v", len(results), results)
	}
	if results[0].Title != "有效条目" {
		t.Errorf("results[0].Title = %q, want %q", results[0].Title, "有效条目")
	}
}
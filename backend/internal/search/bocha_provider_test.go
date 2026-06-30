package search

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBochaServer returns an httptest.Server whose handler captures the
// incoming request for later assertion and writes the given response body.
type fakeBochaServer struct {
	server  *httptest.Server
	lastReq *http.Request
	lastBody []byte
	gotAuth string
}

func newFakeBochaServer(t *testing.T, status int, response string) *fakeBochaServer {
	t.Helper()
	f := &fakeBochaServer{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastReq = r
		body, _ := io.ReadAll(r.Body)
		f.lastBody = body
		f.gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func TestBochaProvider_Name(t *testing.T) {
	p := NewBochaProvider("sk-test")
	if got := p.Name(); got != "bocha" {
		t.Fatalf("Name() = %q, want %q", got, "bocha")
	}
}

func TestBochaProvider_Search_RequiresAPIKey(t *testing.T) {
	p := NewBochaProvider("")
	_, err := p.Search(context.Background(), "any query")
	if err == nil {
		t.Fatal("expected error for empty API key")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error should mention API key, got %v", err)
	}
}

func TestBochaProvider_Search_SendsCorrectRequestBodyAndAuth(t *testing.T) {
	f := newFakeBochaServer(t, http.StatusOK, `{"data":{"webPages":{"value":[]}}}`)

	p := NewBochaProviderWithBaseURL("sk-secret-xyz", f.server.URL+"/")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if _, err := p.Search(ctx, "跳槽 是否值得"); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	// Method + path
	if f.lastReq.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", f.lastReq.Method)
	}
	if !strings.HasSuffix(f.lastReq.URL.Path, "/v1/web-search") {
		t.Errorf("expected /v1/web-search suffix, got %s", f.lastReq.URL.Path)
	}

	// Authorization Bearer header
	if f.gotAuth != "Bearer sk-secret-xyz" {
		t.Errorf("expected Bearer auth header, got %q", f.gotAuth)
	}

	// Content-Type
	if ct := f.lastReq.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	// Body
	var sent bochaRequest
	if err := json.Unmarshal(f.lastBody, &sent); err != nil {
		t.Fatalf("could not parse sent body: %v", err)
	}
	if sent.Query != "跳槽 是否值得" {
		t.Errorf("body.query = %q, want %q", sent.Query, "跳槽 是否值得")
	}
	if sent.Count != 10 {
		t.Errorf("body.count = %d, want 10", sent.Count)
	}
	if sent.Summary != true {
		t.Errorf("body.summary should be true to get full snippets")
	}
	if sent.Freshness != "noLimit" {
		t.Errorf("body.freshness = %q, want noLimit", sent.Freshness)
	}
}

func TestBochaProvider_Search_ParsesResultsAndPrefersSummary(t *testing.T) {
	respBody := `{
		"data": {
			"queryContext": {"originalQuery": "AI 大模型"},
			"webPages": {
				"totalEstimatedMatches": 42,
				"value": [
					{
						"id": "1",
						"name": "AI 大模型综述",
						"url": "https://example.com/llm",
						"snippet": "短摘要",
						"summary": "完整长摘要，描述了大模型的发展历程与现状。",
						"siteName": "example.com",
						"dateLastCrawled": "2026-06-01T00:00:00Z"
					},
					{
						"id": "2",
						"name": "大模型应用案例",
						"url": "https://example.com/llm-app",
						"snippet": "案例短摘要",
						"summary": "",
						"siteName": "example.com"
					}
				]
			}
		}
	}`
	f := newFakeBochaServer(t, http.StatusOK, respBody)

	p := NewBochaProviderWithBaseURL("sk-test", f.server.URL+"/")
	results, err := p.Search(context.Background(), "AI 大模型")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Title != "AI 大模型综述" {
		t.Errorf("results[0].Title = %q, want %q", results[0].Title, "AI 大模型综述")
	}
	if results[0].URL != "https://example.com/llm" {
		t.Errorf("results[0].URL = %q", results[0].URL)
	}
	// summary should win over snippet
	if results[0].Content != "完整长摘要，描述了大模型的发展历程与现状。" {
		t.Errorf("results[0].Content = %q, want summary", results[0].Content)
	}

	// Second item only has snippet, should fall back to it
	if results[1].Content != "案例短摘要" {
		t.Errorf("results[1].Content = %q, want snippet fallback", results[1].Content)
	}
}

func TestBochaProvider_Search_FiltersEmptyFields(t *testing.T) {
	respBody := `{
		"data": {
			"webPages": {
				"value": [
					{"name": "", "url": "https://example.com/no-title", "summary": "x"},
					{"name": "无URL条目", "url": "", "summary": "x"},
					{"name": "正常条目", "url": "https://example.com/ok", "summary": "好"}
				]
			}
		}
	}`
	f := newFakeBochaServer(t, http.StatusOK, respBody)

	p := NewBochaProviderWithBaseURL("sk-test", f.server.URL+"/")
	results, err := p.Search(context.Background(), "x")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 valid result (empty name/url filtered out), got %d: %+v", len(results), results)
	}
	if results[0].Title != "正常条目" {
		t.Errorf("results[0].Title = %q", results[0].Title)
	}
}

func TestBochaProvider_Search_FallbackOnEmpty(t *testing.T) {
	f := newFakeBochaServer(t, http.StatusOK, `{"data":{"webPages":{"value":[]}}}`)

	p := NewBochaProviderWithBaseURL("sk-test", f.server.URL+"/")
	results, err := p.Search(context.Background(), "极冷门关键词")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 fallback result, got %d", len(results))
	}
	if !strings.Contains(results[0].Content, "未返回相关信息") {
		t.Errorf("fallback should mention '未返回相关信息', got %q", results[0].Content)
	}
	if results[0].Score != 0.5 {
		t.Errorf("fallback score = %v, want 0.5", results[0].Score)
	}
}

func TestBochaProvider_Search_NonOKStatus(t *testing.T) {
	f := newFakeBochaServer(t, http.StatusUnauthorized, `{"code":401,"message":"invalid api key"}`)

	p := NewBochaProviderWithBaseURL("sk-bad", f.server.URL+"/")
	_, err := p.Search(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error on 401 status")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("error should mention status 401, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("error should surface server message, got %v", err)
	}
}

func TestBochaProvider_Search_NetworkError(t *testing.T) {
	// Use an unreachable address to simulate a network failure.
	p := NewBochaProviderWithBaseURL("sk-test", "http://127.0.0.1:1/")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := p.Search(ctx, "x")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestBochaProvider_Search_InvalidJSON(t *testing.T) {
	f := newFakeBochaServer(t, http.StatusOK, `{not json}`)

	p := NewBochaProviderWithBaseURL("sk-test", f.server.URL+"/")
	_, err := p.Search(context.Background(), "x")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error should mention parse failure, got %v", err)
	}
}

func TestBochaProvider_Search_ContextCancelled(t *testing.T) {
	// Manually-managed TCP listener that accepts but never replies. Using
	// httptest.Server here deadlocks because its Close waits for handler
	// goroutines on keep-alive connections that never exit.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold the connection open; the client will time out / cancel
			// and then we close it during test cleanup.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		wg.Wait()
	})

	p := NewBochaProviderWithBaseURL("sk-test", "http://"+listener.Addr().String()+"/")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = p.Search(ctx, "x")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}
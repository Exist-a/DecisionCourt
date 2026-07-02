package external

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServer_DiscoveryEndpoint 验证 GET /.well-known/agent-card.json 端点。
func TestServer_DiscoveryEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := NewServer(ServerConfig{
		Provider: "decisioncourt",
		Version:  "v0.8.1",
		Cards:    map[string]AgentCard{"prosecutor": {Name: "P", Type: "prosecutor", URL: "http://x/p"}},
	})
	r := gin.New()
	srv.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var doc DiscoveryDocument
	err := json.Unmarshal(rec.Body.Bytes(), &doc)
	require.NoError(t, err)
	assert.Equal(t, "v0.8.1", doc.Version)
	assert.Len(t, doc.Agents, 1)
}

// TestServer_AgentCardEndpoint 验证 GET /a2a/agents/:type/agent-card 端点。
func TestServer_AgentCardEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := NewServer(ServerConfig{
		Provider: "decisioncourt",
		Version:  "v0.8.1",
		Cards: map[string]AgentCard{
			"prosecutor": {Name: "P", Type: "prosecutor", URL: "http://x/p"},
		},
	})
	r := gin.New()
	srv.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/a2a/agents/prosecutor/agent-card", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var card AgentCard
	err := json.Unmarshal(rec.Body.Bytes(), &card)
	require.NoError(t, err)
	assert.Equal(t, "prosecutor", card.Type)

	// 不存在的 type → 404
	req2 := httptest.NewRequest(http.MethodGet, "/a2a/agents/nonexistent/agent-card", nil)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	assert.Equal(t, http.StatusNotFound, rec2.Code)
}

// TestServer_TasksSendEndpoint 验证 POST /a2a/tasks/send 端点。
//
// 最小实现：接收 JSON-RPC 2.0 格式的 task，返回 202 accepted。
func TestServer_TasksSendEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bridge := NewBridge(nil) // 桥接到 nil 内部 bus，noop
	srv := NewServer(ServerConfig{
		Provider: "decisioncourt",
		Version:  "v0.8.1",
		Cards:    map[string]AgentCard{},
		Bridge:   bridge,
	})
	r := gin.New()
	srv.RegisterRoutes(r)

	// 合法 JSON-RPC 请求
	body := `{
		"jsonrpc": "2.0",
		"id": "req-1",
		"method": "tasks/send",
		"params": {
			"id": "task-123",
			"message": {
				"role": "user",
				"parts": [{"type": "text", "text": "hello"}]
			}
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks/send",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
	var resp map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	result := resp["result"].(map[string]interface{})
	assert.Equal(t, "task-123", result["id"])
	assert.Equal(t, "submitted", result["status"].(map[string]interface{})["state"])
}

// TestServer_TasksSendEndpoint_BadJSON 验证格式错误的请求返回 400。
func TestServer_TasksSendEndpoint_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := NewServer(ServerConfig{
		Provider: "decisioncourt",
		Version:  "v0.8.1",
		Cards:    map[string]AgentCard{},
		Bridge:   NewBridge(nil),
	})
	r := gin.New()
	srv.RegisterRoutes(r)

	// 缺 jsonrpc 字段
	body := `{"id":"req-1","method":"tasks/send"}`
	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks/send",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestServer_TasksSendEndpoint_EmptyBody 验证空 body 返回 400。
func TestServer_TasksSendEndpoint_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := NewServer(ServerConfig{
		Provider: "decisioncourt",
		Version:  "v0.8.1",
		Cards:    map[string]AgentCard{},
		Bridge:   NewBridge(nil),
	})
	r := gin.New()
	srv.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/a2a/tasks/send", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

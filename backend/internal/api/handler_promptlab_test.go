package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/promptlab"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fakePromptLabClient 实现了 PromptLabClient 接口,所有方法返回固定结果。
//
// 设计要点:
//   - 字段直接对应 4 个接口方法,测试通过改字段值就能验证 handler 是否正确转发
//   - Err 字段任意方法失败时返回 (handler 应正确处理)
//   - 不调真实 LLM/Store,保证测试 100% 确定性 + 零外部依赖
type fakePromptLabClient struct {
	EvalResult   promptlab.EvalResult
	EvalErr      error
	ABTestResult *promptlab.ABTestResult
	ABTestErr    error
	VersionVal   promptlab.Version
	ReloadResult promptlab.Version
	ReloadErr    error

	// 记录方法被调用的次数,验证 handler 真调了 (而不是短路返回)
	evalCalled   int
	abCalled     int
	versionCalls int
	reloadCalls  int
}

func (f *fakePromptLabClient) Eval(_ context.Context, _ promptlab.EvalRule, _ string) (promptlab.EvalResult, error) {
	f.evalCalled++
	return f.EvalResult, f.EvalErr
}

func (f *fakePromptLabClient) RunABTest(_ context.Context, _, _ string, _ promptlab.EvalRule, _ []string) (*promptlab.ABTestResult, error) {
	f.abCalled++
	return f.ABTestResult, f.ABTestErr
}

func (f *fakePromptLabClient) Version() promptlab.Version {
	f.versionCalls++
	return f.VersionVal
}

func (f *fakePromptLabClient) Reload() (promptlab.Version, error) {
	f.reloadCalls++
	return f.ReloadResult, f.ReloadErr
}

// promptLabEngine wraps Handler + PromptLabClient 装到 gin.Engine,
// 挂一个 fake auth 把 viewer 设为 "test-user",调 RegisterAPIRoutes 注册全部端点。
//
// 不复用 ginEngine helper — 因为 helper 在 handler_investigations_test.go,
// 复用它需要把 promptLab 字段也拼进 Handler,helper 本身不接受 promptLab。
// 这里写一个局部 helper,只用于 Prompt Lab 端点的 4 个测试。
func promptLabEngine(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		c.Set("viewer", "test-user")
		c.Next()
	})
	h.RegisterAPIRoutes(api)
	return r
}

// T1: POST /prompts/eval — length_compliance 端到端。
// 验证: fake client 收到 1 次 Eval 调用,响应 code=0 + EvalResult data。
func TestPromptEval_LengthCompliance(t *testing.T) {
	fake := &fakePromptLabClient{
		EvalResult: promptlab.EvalResult{
			Rule:      promptlab.EvalRuleLength,
			Score:     1.0,
			Pass:      true,
			Reasoning: "length=50/300 chars",
		},
	}
	h := &Handler{promptLab: fake}
	r := promptLabEngine(h)

	body := `{"rule":"length_compliance","output":"some short speech"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/eval", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int                 `json:"code"`
		Data promptlab.EvalResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.Pass)
	require.Equal(t, 1.0, resp.Data.Score)
	require.Equal(t, 1, fake.evalCalled, "fake.Eval 应当被 handler 调 1 次")
}

// T2: POST /prompts/eval — 未知 rule 返回 400。
//
// 验证 handler 在调 PromptLabClient.Eval 前先校验 rule 白名单,
// 减少对 fake client 的不必要调用。
func TestPromptEval_UnknownRule400(t *testing.T) {
	fake := &fakePromptLabClient{}
	h := &Handler{promptLab: fake}
	r := promptLabEngine(h)

	body := `{"rule":"invalid_rule","output":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/eval", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 0, fake.evalCalled, "未知 rule 不应触发 Eval")
}

// T3: GET /prompts/version — 返回 Version 字段 (semver + git_sha + loaded_at + source_path)。
//
// 验证 handler 不需要 body parse,直接调 client.Version()。
func TestPromptVersion(t *testing.T) {
	fake := &fakePromptLabClient{
		VersionVal: promptlab.Version{
			Semver:     "1.0.3-pr1",
			GitSHA:     "abc1234",
			SourcePath: "prompts/base.yaml",
		},
	}
	h := &Handler{promptLab: fake}
	r := promptLabEngine(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/prompts/version", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int              `json:"code"`
		Data promptlab.Version `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "1.0.3-pr1", resp.Data.Semver)
	require.Equal(t, "abc1234", resp.Data.GitSHA)
	require.Equal(t, 1, fake.versionCalls)
}

// T4: POST /prompts/abtest — trial_outputs 长度超限返回 400。
//
// 验证 handler 在调 RunABTest 前先校验 trial_outputs 长度上限,
// 减少 LLM 配额浪费。
func TestPromptABTest_TooManyOutputs400(t *testing.T) {
	fake := &fakePromptLabClient{}
	h := &Handler{promptLab: fake}
	r := promptLabEngine(h)

	// 构造 promptlab.ABTestMaxTrialOutputs+1 条 outputs (default 20, 21 条)
	outputs := make([]string, promptlab.ABTestMaxTrialOutputs+1)
	bodyMap := map[string]interface{}{
		"version_a":     "v1",
		"version_b":     "v2",
		"rule":          "length_compliance",
		"trial_outputs": outputs,
	}
	bodyBytes, _ := json.Marshal(bodyMap)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/abtest", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 0, fake.abCalled, "超长 trial_outputs 不应触发 RunABTest")
}

// T5: promptLab 未注入时 4 条路由都不注册（404）。
//
// 这条覆盖的是 2026-10-09 之前线上一直存在的真实状态：装配漏了
// NewPromptLabAdapter → handler.promptLab == nil → RegisterPromptLabRoutes
// 直接 return → 前端拿 404 而不是 503。必须显式断言，否则"漏接线"这种
// 静默降级又会被放过。
func TestPromptLabRoutes_NotRegisteredWhenNil(t *testing.T) {
	h := &Handler{} // promptLab 故意留 nil
	r := promptLabEngine(h)

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/prompts/eval"},
		{http.MethodPost, "/api/v1/prompts/abtest"},
		{http.MethodGet, "/api/v1/prompts/version"},
		{http.MethodPost, "/api/v1/prompts/reload"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNotFound, rec.Code, "%s %s 应 404", tc.method, tc.path)
	}
}

// T6: eval / abtest 过 LLMRateLimit，version / reload 不过。
//
// 接线时必须带上限流（这两条会真实调 LLM），且不能误伤不烧 token 的两条。
// 用一个"恒定 429"的假限流器区分：被限流的路由必须 429，未被限流的必须 200。
func TestPromptLabRoutes_EvalAndABTestBehindLLMRateLimit(t *testing.T) {
	fake := &fakePromptLabClient{
		EvalResult: promptlab.EvalResult{Rule: promptlab.EvalRuleLength, Pass: true},
	}
	h := &Handler{promptLab: fake}
	h.LLMRateLimit = func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"code": "RATE_LIMITED"})
	}
	r := promptLabEngine(h)

	// eval → 被限流，且不应真的调到 LLM
	req := httptest.NewRequest(http.MethodPost, "/api/v1/prompts/eval",
		strings.NewReader(`{"rule":"length_compliance","output":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, 0, fake.evalCalled, "被限流的请求不应触发 LLM 调用")

	// abtest → 同样被限流
	req = httptest.NewRequest(http.MethodPost, "/api/v1/prompts/abtest",
		strings.NewReader(`{"version_a":"v1","version_b":"v2","rule":"length_compliance","trial_outputs":["a"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, 0, fake.abCalled)

	// version → 不调 LLM，必须放行
	req = httptest.NewRequest(http.MethodGet, "/api/v1/prompts/version", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// reload → 不调 LLM，必须放行
	req = httptest.NewRequest(http.MethodPost, "/api/v1/prompts/reload", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

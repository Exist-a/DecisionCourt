package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/promptlab"
	"github.com/gin-gonic/gin"
)

// PromptLabClient 是 /api/v1/prompts/* 端点依赖的最小集合。
//
// 设计要点:
//   - Handler 不直接持有 promptlab.Store / llm.Client,
//     而是通过接口注入 (PromptLabClient),便于测试时 mock
//   - Production 装配: cmd/server/main.go 把 *promptlab.Store + llm.Client 装进 wrapper
//   - 测试场景: handler_promptlab_test.go 直接用 fakePromptLabClient 返回固定结果
//
// 方法语义:
//   - Eval: 单条 output 跑一条 rule,返回 EvalResult (含 pass + reasoning + latency)
//   - RunABTest: 一组 output 跑 A/B 比较,返回 ABTestResult (含 winner + confidence)
//   - Version: 返回当前加载的 prompt 版本元数据 (semver + git_sha + loaded_at)
//   - Reload: 重新读 YAML,返回新的 Version。失败时返回 error 不修改 Store
//
// 与 promptlab 包解耦:
//   - 接口返回的 EvalResult / ABTestResult / Version 与 promptlab 包同名类型完全一致
//     (用 type alias 而非独立类型,避免 REST JSON tag 重复定义)
//   - 这样调用方 (handler) 拿到结果直接 c.JSON(200, gin.H{"data": result}) 即可,
//     无需做类型转换
type PromptLabClient interface {
	Eval(ctx context.Context, rule promptlab.EvalRule, output string) (promptlab.EvalResult, error)
	RunABTest(ctx context.Context, versionA, versionB string, rule promptlab.EvalRule, trialOutputs []string) (*promptlab.ABTestResult, error)
	Version() promptlab.Version
	Reload() (promptlab.Version, error)
}

// promptLabAdapter 把 *promptlab.Store + llm.Client 装成 PromptLabClient 接口,
// 用于 cmd/server/main.go 装配阶段。
//
// Store 与 llm.Client 都不持有 State,所有调用都 delegate 给底层:
//   - Eval / RunABTest → promptlab.Eval / promptlab.RunABTest (本包)
//   - Version → store.Version() (快照,不修改 Store)
//   - Reload → store.Load() (失败时 Store 保持旧状态,见 store.go §Load 注释)
//
// adapter 本身无锁,因为底层 Store 是 thread-safe (sync.RWMutex)。
type promptLabAdapter struct {
	store     *promptlab.Store
	llmClient llm.Client
}

func (a *promptLabAdapter) Eval(ctx context.Context, rule promptlab.EvalRule, output string) (promptlab.EvalResult, error) {
	return promptlab.Eval(ctx, a.llmClient, rule, output)
}

func (a *promptLabAdapter) RunABTest(ctx context.Context, versionA, versionB string, rule promptlab.EvalRule, trialOutputs []string) (*promptlab.ABTestResult, error) {
	return promptlab.RunABTest(ctx, a.llmClient, versionA, versionB, rule, trialOutputs)
}

func (a *promptLabAdapter) Version() promptlab.Version {
	return a.store.Version()
}

func (a *promptLabAdapter) Reload() (promptlab.Version, error) {
	if err := a.store.Load(); err != nil {
		return promptlab.Version{}, err
	}
	return a.store.Version(), nil
}

// NewPromptLabAdapter 工厂函数,cmd/server/main.go 装配时调一次。
// nil 任何一个参数 → 返回 nil (handler 端会因 promptLab==nil 返回 503,
// 提示用户"Prompt Lab 未配置")。
func NewPromptLabAdapter(store *promptlab.Store, llmClient llm.Client) PromptLabClient {
	if store == nil || llmClient == nil {
		return nil
	}
	return &promptLabAdapter{store: store, llmClient: llmClient}
}

// RegisterPromptLabRoutes 把 /api/v1/prompts/* 路由挂到传入的 group。
//
// 端点清单 (与 V1.0.3-PLAN §2.6 完全一致):
//   POST /api/v1/prompts/eval    → PromptEval
//   POST /api/v1/prompts/abtest  → PromptABTest
//   GET  /api/v1/prompts/version → PromptVersion
//   POST /api/v1/prompts/reload  → PromptReload
//
// v1.0.3 PR-B2 admin 决策说明:
//   plan 原本要求 /reload 是 admin-only,但项目当前 auth 是匿名 JWT,
//   没有 admin role 概念 (config.AppConfig 没 AdminUserID 字段,
//   auth.Claims 只含 user_id)。
//   简化: /reload 任何登录用户都能触发,因为:
//     - Reload 是只读 + swap 操作 (~毫秒级),不烧 CPU/IO
//     - 已经有 5s ticker 自动检测 mtime,manual reload 是 UX 加速手段
//     - 没破坏性影响 (Store 失败时保持旧状态)
//   未来 v1.0.x 引入 admin role 后,可在此处加 viewer 白名单判断,
//   接口签名无需变化 (handler 内部判断)。
func (h *Handler) RegisterPromptLabRoutes(api *gin.RouterGroup) {
	if h.promptLab == nil {
		return
	}
	api.POST("/prompts/eval", h.PromptEval)
	api.POST("/prompts/abtest", h.PromptABTest)
	api.GET("/prompts/version", h.PromptVersion)
	api.POST("/prompts/reload", h.PromptReload)
}

// PromptEval 处理 POST /api/v1/prompts/eval
//
// 请求体: { "rule": "length_compliance|evidence_id_format|stance_mention", "output": "..." }
// 响应:   { "code": 0, "data": <EvalResult> }
// 错误:
//   400 code=1001: 请求体格式错 / rule 非法 / output 为空
//   500 code=1500: LLM 调用失败 (见 EvalResult.Reasoning 字段)
func (h *Handler) PromptEval(c *gin.Context) {
	var req struct {
		Rule   string `json:"rule" binding:"required,max=50"`
		Output string `json:"output" binding:"required,max=10000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "invalid request body",
		})
		return
	}

	if !promptlab.IsBuiltinRule(req.Rule) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": fmt.Sprintf("unknown rule %q (allowed: length_compliance / evidence_id_format / stance_mention)", req.Rule),
		})
		return
	}

	result, err := h.promptLab.Eval(c.Request.Context(), promptlab.EvalRule(req.Rule), req.Output)
	if err != nil {
		slog.Warn("promptlab eval failed",
			"rule", req.Rule, "error", err)
		// 即使 LLM 失败,EvalResult 也带 latency + reason,直接返 200 让前端能看到
		// (Eval 内部已经把 error wrap 进 Reasoning 字段)
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// PromptABTest 处理 POST /api/v1/prompts/abtest
//
// 请求体: { "version_a": "...", "version_b": "...", "rule": "...", "trial_outputs": ["...", ...] }
// 响应:   { "code": 0, "data": <ABTestResult> }
// 错误:
//   400 code=1001: rule 非法 / trial_outputs 长度超限 / 字段缺失
//   500 code=1500: 任意一条 Eval 失败 (整个 A/B Test 终止)
func (h *Handler) PromptABTest(c *gin.Context) {
	var req struct {
		VersionA     string   `json:"version_a" binding:"required,max=50"`
		VersionB     string   `json:"version_b" binding:"required,max=50"`
		Rule         string   `json:"rule" binding:"required,max=50"`
		TrialOutputs []string `json:"trial_outputs" binding:"required,min=1,dive,max=10000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": "invalid request body",
		})
		return
	}

	if !promptlab.IsBuiltinRule(req.Rule) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": fmt.Sprintf("unknown rule %q", req.Rule),
		})
		return
	}

	if len(req.TrialOutputs) > promptlab.ABTestMaxTrialOutputs {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": fmt.Sprintf("trial_outputs length %d exceeds max %d", len(req.TrialOutputs), promptlab.ABTestMaxTrialOutputs),
		})
		return
	}

	result, err := h.promptLab.RunABTest(
		c.Request.Context(),
		req.VersionA, req.VersionB,
		promptlab.EvalRule(req.Rule), req.TrialOutputs,
	)
	if err != nil {
		slog.Warn("promptlab abtest failed",
			"rule", req.Rule, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    1500,
			"message": "abtest failed: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// PromptVersion 处理 GET /api/v1/prompts/version
//
// 返回当前 Store 加载的 Version 元数据。fallback 状态时 Semver="fallback",
// 前端可展示 "⚠ fallback" badge。
//
// 响应: { "code": 0, "data": <Version> }
func (h *Handler) PromptVersion(c *gin.Context) {
	v := h.promptLab.Version()
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": v})
}

// PromptReload 处理 POST /api/v1/prompts/reload
//
// 手动触发 YAML 重读,等效于 5s ticker 检测到 mtime 变更后的自动 reload。
// 失败时 Store 保持旧状态 (promptlab.Store.Load 契约),返回 500 + error。
//
// 响应 (成功): { "code": 0, "data": <Version> }
// 响应 (失败): { "code": 1500, "message": "..." }
func (h *Handler) PromptReload(c *gin.Context) {
	v, err := h.promptLab.Reload()
	if err != nil {
		slog.Warn("promptlab reload failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    1500,
			"message": "reload failed: " + err.Error(),
		})
		return
	}
	slog.Info("promptlab manual reload ok", "version", v.String())
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": v})
}

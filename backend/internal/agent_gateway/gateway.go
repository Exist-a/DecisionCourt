// Package agent_gateway 是 DecisionCourt 项目的 Agent Gateway 装饰器。
// v0.5+ 白盒子集：审计落库 + trace 关联。
// v0.5+ 高级能力：Prompt 压缩、Token 预算、限流、Fallback 退避重试、
// JSON 文件日志。所有能力可通过 GatewayConfig 统一开关。
//
// 不在本范围：跨 provider fallback、Prompt 智能摘要（需要额外 LLM）、
// 持久化预算存储（服务重启清零）。
package agent_gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// Gateway 是装饰后的 llm.Client，所有业务侧通过它调 LLM。
type Gateway struct {
	inner        llm.Client
	recorder     *Recorder
	budget       *TokenBudget
	compressor   *PromptCompressor
	throttler    *Throttler
	retryer      *Retryer
	fileLogger   *FileLogger
	cfg          GatewayConfig
	defaultModel string
}

// NewWithConfig 用完整配置构造 Gateway。
func NewWithConfig(inner llm.Client, rec *Recorder, defaultModel string, cfg GatewayConfig) *Gateway {
	cfg = cfg.Normalize()
	if rec == nil {
		rec = NewRecorder(RecorderConfig{Enabled: false, Provider: "unknown"}, nil)
	}
	if defaultModel == "" {
		defaultModel = "unknown"
	}

	var (
		budget     *TokenBudget
		compressor *PromptCompressor
		throttler  *Throttler
		retryer    *Retryer
		logger     *FileLogger
	)
	if cfg.IsTokenBudgetEnabled() {
		// v2：构造 MemStore 时携带 limit + cost + 阈值 + sliding 时长
		slidingWindow := time.Duration(cfg.BudgetSlidingWindowSec) * time.Second
		store := NewMemStore(cfg.BudgetPerSession, 0, cfg.CompressionThreshold, cfg.ThrottlingThreshold, slidingWindow)
		budget = NewTokenBudgetWithStore(store)
	}
	if cfg.IsPromptCompressionEnabled() {
		compressor = NewPromptCompressor(SmartCompressionConfig{
			Enabled:                cfg.IsSmartCompressionEnabled(),
			KeepRecentForcedN:      cfg.KeepRecentForcedN,
			SummaryInsertThreshold: cfg.SummaryInsertThreshold,
			ScoreThreshold:         cfg.ScoreThreshold,
		})
	}
	if cfg.IsThrottlingEnabled() {
		throttler = NewThrottler()
	}
	if cfg.IsFallbackEnabled() {
		retryer = NewRetryer()
	}
	if cfg.IsFileLoggerEnabled() {
		logger = NewFileLogger(cfg.LogDir)
	}

	return &Gateway{
		inner:        inner,
		recorder:     rec,
		budget:       budget,
		compressor:   compressor,
		throttler:    throttler,
		retryer:      retryer,
		fileLogger:   logger,
		cfg:          cfg,
		defaultModel: defaultModel,
	}
}

// Wrap 保持旧签名：仅启用审计落库，不启用高级能力。用于测试与向后兼容。
func Wrap(inner llm.Client, rec *Recorder, defaultModel string) llm.Client {
	return NewWithConfig(inner, rec, defaultModel, GatewayConfig{})
}

// Complete 实现 llm.Client.Complete 装饰器。
func (g *Gateway) Complete(ctx context.Context, systemPrompt string, messages []llm.Message, opts llm.CompletionOptions) (string, llm.Usage, error) {
	if g.inner == nil {
		return "", llm.Usage{}, errors.New("agent_gateway: inner llm.Client is nil")
	}
	model := g.defaultModel
	if opts.Model != "" {
		model = opts.Model
	}
	tr := FromContext(ctx)

	// 1. 预算检查（同时在 v2 模式下触发 OnWarning hooks）
	bs := g.budgetSnapshot(ctx, tr.SessionUUID)

	// 1b. v2 预算耗尽拒绝（D4 决策）
	if g.cfg.IsRejectWhenExhaustedEnabled() && bs.Status == StatusExhausted {
		err := fmt.Errorf("%w (session=%s ratio=%.2f)", ErrBudgetExhausted, tr.SessionUUID, bs.Ratio)
		// 仍写一条审计日志，让业务可以事后看 budget_exhausted 拒绝记录
		g.recorder.Record(CallInput{
			Trace:   tr,
			Model:   model,
			Usage:   Usage{},
			Latency: 0,
			Status:  StatusError,
			Err:     err,
		})
		return "", llm.Usage{}, err
	}

	// 2. Prompt 压缩
	compInfo := CompressionInfo{BeforeCount: len(messages)}
	if g.compressor != nil && bs.Status != StatusNormal {
		messages, compInfo = g.compressor.Compress(messages, bs)
	}

	// 3. 限流调整 opts
	throttleInfo := ThrottleInfo{MaxTokensBefore: opts.MaxTokens, TemperatureBefore: opts.Temperature}
	if g.throttler != nil {
		opts, throttleInfo = g.throttler.Apply(opts, bs, tr.TaskType)
	}

	// 4. 退避重试调用
	start := time.Now()
	var content string
	var usage llm.Usage
	var err error

	do := func() error {
		content, usage, err = g.inner.Complete(ctx, systemPrompt, messages, opts)
		return err
	}
	if g.retryer != nil {
		err = g.retryer.DoContext(ctx, do)
	} else {
		err = do()
	}
	retryCount := 0
	if g.retryer != nil {
		retryCount = g.retryer.LastCount()
	}
	latency := time.Since(start)

	// 5. 记录使用到预算
	if g.budget != nil && tr.SessionUUID != "" {
		g.budget.AddUsage(ctx, tr.SessionUUID, BudgetUsage{
			InputTokens:  usage.PromptTokens,
			OutputTokens: usage.CompletionTokens,
		})
	}

	// 6. 审计落库
	g.recorder.Record(CallInput{
		Trace:   tr,
		Model:   model,
		Usage:   Usage{PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens, TotalTokens: usage.TotalTokens},
		Latency: latency,
		Status:  toStatus(err),
		Err:     err,
	})

	// 7. 文件日志
	g.writeFileLog(ctx, tr, model, usage, latency, err, compInfo, throttleInfo, retryCount, bs)

	return content, usage, err
}

// StreamComplete 实现 llm.Client.StreamComplete 装饰器。流式失败不重试。
func (g *Gateway) StreamComplete(ctx context.Context, systemPrompt string, messages []llm.Message, opts llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 16)
	if g.inner == nil {
		out <- llm.StreamChunk{Done: true, Err: errors.New("agent_gateway: inner llm.Client is nil")}
		close(out)
		return out
	}
	model := opts.Model
	if model == "" {
		model = g.defaultModel
	}
	tr := FromContext(ctx)

	// 1. 预算检查
	bs := g.budgetSnapshot(ctx, tr.SessionUUID)

	// 1b. v2 预算耗尽拒绝（流式：直接 emit Done+Err 然后关 channel）
	if g.cfg.IsRejectWhenExhaustedEnabled() && bs.Status == StatusExhausted {
		err := fmt.Errorf("%w (session=%s ratio=%.2f)", ErrBudgetExhausted, tr.SessionUUID, bs.Ratio)
		out <- llm.StreamChunk{Done: true, Err: err}
		close(out)
		return out
	}

	// 2. 压缩
	compInfo := CompressionInfo{BeforeCount: len(messages)}
	if g.compressor != nil && bs.Status != StatusNormal {
		messages, compInfo = g.compressor.Compress(messages, bs)
	}

	// 3. 限流
	throttleInfo := ThrottleInfo{MaxTokensBefore: opts.MaxTokens, TemperatureBefore: opts.Temperature}
	if g.throttler != nil {
		opts, throttleInfo = g.throttler.Apply(opts, bs, tr.TaskType)
	}

	start := time.Now()
	src := g.inner.StreamComplete(ctx, systemPrompt, messages, opts)

	go func() {
		defer close(out)
		var firstErr error
		var totalContent string
		for chunk := range src {
			if chunk.Err != nil && firstErr == nil {
				firstErr = chunk.Err
			}
			totalContent += chunk.Content
			out <- chunk
		}
		latency := time.Since(start)

		usage := llm.Usage{}
		if g.budget != nil && tr.SessionUUID != "" {
			// 流式返回没有 token 计数，按输出字符数估算 1 token ≈ 4 字符。
			// v2 多维：把估算值全部记到 OutputTokens（LLM 没办法在流上拿到真实 usage）。
			approx := len(totalContent) / 4
			if approx < 1 {
				approx = 0
			}
			usage.TotalTokens = approx
			usage.CompletionTokens = approx
			g.budget.AddUsage(ctx, tr.SessionUUID, BudgetUsage{OutputTokens: approx})
		}

		g.recorder.Record(CallInput{
			Trace:   tr,
			Model:   model,
			Usage:   Usage{TotalTokens: usage.TotalTokens},
			Latency: latency,
			Status:  toStatus(firstErr),
			Err:     firstErr,
		})
		g.writeFileLog(ctx, tr, model, usage, latency, firstErr, compInfo, throttleInfo, 0, bs)
	}()
	return out
}

// budgetSnapshot 返回当前预算快照；预算未启用时返回 normal 零值。
func (g *Gateway) budgetSnapshot(ctx context.Context, sessionUUID string) BudgetSnapshot {
	if g.budget == nil {
		return BudgetSnapshot{Status: StatusNormal}
	}
	return g.budget.Check(ctx, sessionUUID)
}

// writeFileLog 写入文件日志。失败不阻塞主流程。
func (g *Gateway) writeFileLog(ctx context.Context, tr Trace, model string, usage llm.Usage, latency time.Duration, err error, compInfo CompressionInfo, throttleInfo ThrottleInfo, retryCount int, bs BudgetSnapshot) {
	if g.fileLogger == nil {
		return
	}
	status := StatusSuccess
	errMsg := ""
	if err != nil {
		status = StatusError
		errMsg = err.Error()
		if len(errMsg) > MaxErrorMsgLen {
			errMsg = errMsg[:MaxErrorMsgLen]
		}
	}
	entry := LogEntry{
		RequestID:              tr.RequestID,
		SessionUUID:            tr.SessionUUID,
		AgentType:              tr.AgentType,
		TaskType:               tr.TaskType,
		Model:                  model,
		Provider:               g.recorder.cfg.Provider,
		PromptTokens:           usage.PromptTokens,
		CompletionTokens:       usage.CompletionTokens,
		TotalTokens:            usage.TotalTokens,
		LatencyMs:              int(latency / time.Millisecond),
		Status:                 status,
		ErrorMsg:               errMsg,
		Compressed:             compInfo.Applied,
		CompressionBeforeCount: compInfo.BeforeCount,
		CompressionAfterCount:  compInfo.AfterCount,
		CompressionBeforeLength: compInfo.BeforeLength,
		CompressionAfterLength:  compInfo.AfterLength,
		CompressionStrategy:    compInfo.Strategy,
		CompressionAtomicGroups: compInfo.AtomicGroups,
		CompressionAtomicKept:  compInfo.AtomicGroupsKept,
		CompressionRecentForced: compInfo.RecentForcedKept,
		CompressionSummarized:  compInfo.SummarizedBlocks,
		Throttled:              throttleInfo.Applied,
		ThrottleExempted:       throttleInfo.Exempted,
		ThrottleExemptReason:   throttleInfo.ExemptReason,
		MaxTokensBefore:        throttleInfo.MaxTokensBefore,
		MaxTokensAfter:         throttleInfo.MaxTokensAfter,
		TemperatureBefore:    throttleInfo.TemperatureBefore,
		RetryCount:             retryCount,
		BudgetUsed:             bs.Used,
		BudgetTotal:            bs.Total,
		BudgetRatio:            bs.Ratio,
		BudgetInputTokens:      bs.InputTokens,
		BudgetOutputTokens:     bs.OutputTokens,
		BudgetCostUSD:          bs.CostUSD,
		BudgetSlidingTokens:    bs.SlidingTokens,
		BudgetWarningLevel:     bs.WarningLevel,
	}
	if err := g.fileLogger.Write(entry); err != nil {
		// 仅吞掉错误；避免日志失败拖死主流程
	}
}

func toStatus(err error) string {
	if err != nil {
		return StatusError
	}
	return StatusSuccess
}

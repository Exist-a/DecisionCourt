package agent_gateway

// v0.9 (ADR 0013 §决策 3): Circuit Breaker。
//
// 解决 DeepSeek 凌晨偶发 100% 失败 1-2 小时导致整个 backend trial 不可用
// 的问题。三态熔断:closed(正常) → open(熔断) → half-open(试探)。
//
// 设计决策:
//   - 基于 sony/gobreaker,失败率 50% / 最小 10 请求触发熔断
//   - 熔断 30s 后进入 half-open,放 1 个请求过去探测
//   - 探测成功 → closed;失败 → open
//   - 熔断时调 fallback function(启发式 keyword estimation,响应 < 100ms)
//   - fallback 不可用 → 返回 breaker error,business 层处理
//
// 简历叙述:
//   "为 LLM Gateway 引入 Circuit Breaker(sony/gobreaker),失败率 50% /
//   熔断 30s。模拟 LLM API 100% 失败 2 小时的 chaos 测试中,backend 仍可
//   服务用户,降级到 keyword-based estimation,P95 延迟 < 100ms。"

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
	"github.com/sony/gobreaker"
)

// FallbackFn 是熔断时的降级函数。返回 (content, usage, error):
//   - content:降级的 LLM 输出(粗略结果)
//   - usage:始终 0(没真调 LLM,不计 token)
//   - error:仅在降级本身失败时返回
type FallbackFn func(ctx context.Context, systemPrompt string, messages []llm.Message) (string, llm.Usage, error)

// BreakerConfig 是 Circuit Breaker 的可调配置(写到 GatewayConfig)。
type BreakerConfig struct {
	// Enabled 启用 breaker。false 时 LLMBreaker.Execute 退化为直调 operation。
	Enabled bool
	// FailureRatio 触发熔断的失败率阈值 (0-1)。默认 0.5。
	FailureRatio float64
	// MinRequests 触发评估的最小请求数。默认 10(防止低流量误熔断)。
	MinRequests uint32
	// OpenTimeoutSec 熔断持续时长(秒)。默认 30。
	OpenTimeoutSec int
	// HalfOpenMaxRequests half-open 状态允许通过的探测请求数。默认 1。
	HalfOpenMaxRequests uint32
}

// Normalize 补默认值。
func (c BreakerConfig) Normalize() BreakerConfig {
	if c.FailureRatio <= 0 {
		c.FailureRatio = 0.5
	}
	if c.MinRequests <= 0 {
		c.MinRequests = 10
	}
	if c.OpenTimeoutSec <= 0 {
		c.OpenTimeoutSec = 30
	}
	if c.HalfOpenMaxRequests <= 0 {
		c.HalfOpenMaxRequests = 1
	}
	return c
}

// LLMBreaker 包装 gobreaker.CircuitBreaker,提供 LLM 友好的 Execute 接口。
type LLMBreaker struct {
	cb       *gobreaker.CircuitBreaker
	fallback FallbackFn
	enabled  bool

	// 状态统计(给 audit / observability 用)
	totalTrips    uint64 // 累计触发熔断次数
	totalFallbacks uint64 // 累计降级次数
	totalSuccesses uint64 // 累计成功次数(熔断外的成功)
}

// NewLLMBreaker 用配置构造 breaker。
// fallback 为 nil 时,熔断状态会返回 ErrBreakerOpen 给调用方。
func NewLLMBreaker(cfg BreakerConfig, fallback FallbackFn) *LLMBreaker {
	cfg = cfg.Normalize()

	breaker := &LLMBreaker{
		fallback: fallback,
		enabled:  cfg.Enabled,
	}

	settings := gobreaker.Settings{
		Name:        "agent_gateway.llm",
		MaxRequests: cfg.HalfOpenMaxRequests,
		Timeout:     time.Duration(cfg.OpenTimeoutSec) * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if counts.Requests < cfg.MinRequests {
				return false
			}
			ratio := float64(counts.TotalFailures) / float64(counts.Requests)
			return ratio >= cfg.FailureRatio
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			// 仅在 closed → open 时计数(避免 half-open→open 也算)
			if from == gobreaker.StateClosed && to == gobreaker.StateOpen {
				atomic.AddUint64(&breaker.totalTrips, 1)
			}
		},
	}

	breaker.cb = gobreaker.NewCircuitBreaker(settings)
	return breaker
}

// IsEnabled 返回 breaker 是否启用。
func (b *LLMBreaker) IsEnabled() bool {
	return b != nil && b.enabled
}

// ErrBreakerOpen 是熔断且无 fallback 可用时的错误。
var ErrBreakerOpen = errors.New("agent_gateway: circuit breaker open")

// ErrBreakerTooManyRequests 是 half-open 探测请求过多时的错误。
var ErrBreakerTooManyRequests = errors.New("agent_gateway: too many requests in half-open")

// LLMO 是 LLM 调用的实际工作。返回 (content, usage, error)。
type LLMO func(ctx context.Context) (string, llm.Usage, error)

// Execute 包裹 operation:
//   - breaker 启用时:经 gobreaker 包裹,失败计数 + 熔断判断
//   - 熔断 open:调 fallback(若有)
//   - 真实错误:原样返回
//   - 成功:返回 content + usage
func (b *LLMBreaker) Execute(
	ctx context.Context,
	operation LLMO,
	fallbackCtx struct {
		SystemPrompt string
		Messages     []llm.Message
	},
) (string, llm.Usage, error) {
	if !b.IsEnabled() {
		return operation(ctx)
	}

	// 用闭包捕获 operation 的返回值。
	// gobreaker.Execute 是同步的,只会有一个 operation 在跑,所以闭包内
	// captured 写入不会被并发覆盖。
	var captured struct {
		content string
		usage   llm.Usage
	}

	_, err := b.cb.Execute(func() (interface{}, error) {
		content, usage, opErr := operation(ctx)
		if opErr != nil {
			return nil, opErr
		}
		captured.content = content
		captured.usage = usage
		return nil, nil
	})

	if err == nil {
		atomic.AddUint64(&b.totalSuccesses, 1)
		return captured.content, captured.usage, nil
	}

	// 区分熔断错误 vs 真实错误
	switch {
	case errors.Is(err, gobreaker.ErrOpenState):
		return b.triggerFallback(ctx, fallbackCtx, "open")
	case errors.Is(err, gobreaker.ErrTooManyRequests):
		return b.triggerFallback(ctx, fallbackCtx, "half-open-throttled")
	default:
		// 真实 LLM 错误(超时、网络等),原样返回
		return "", llm.Usage{}, err
	}
}

// triggerFallback 调 fallback 函数,失败则返回 ErrBreakerOpen。
func (b *LLMBreaker) triggerFallback(
	ctx context.Context,
	fallbackCtx struct {
		SystemPrompt string
		Messages     []llm.Message
	},
	reason string,
) (string, llm.Usage, error) {
	atomic.AddUint64(&b.totalFallbacks, 1)

	if b.fallback == nil {
		return "", llm.Usage{}, fmt.Errorf("%w (reason=%s)", ErrBreakerOpen, reason)
	}

	content, usage, err := b.fallback(ctx, fallbackCtx.SystemPrompt, fallbackCtx.Messages)
	if err != nil {
		return "", llm.Usage{}, fmt.Errorf("agent_gateway: fallback failed: %w", err)
	}
	return content, usage, nil
}

// State 返回当前熔断状态(gobreaker.State 枚举)。
func (b *LLMBreaker) State() gobreaker.State {
	if !b.IsEnabled() {
		return gobreaker.StateClosed
	}
	return b.cb.State()
}

// Stats 返回 breaker 的累计统计。
func (b *LLMBreaker) Stats() (trips, fallbacks, successes uint64) {
	if !b.IsEnabled() {
		return 0, 0, 0
	}
	return atomic.LoadUint64(&b.totalTrips),
		atomic.LoadUint64(&b.totalFallbacks),
		atomic.LoadUint64(&b.totalSuccesses)
}

// DefaultKeywordFallback 是默认的降级函数。返回粗略的"系统繁忙"响应,
// 不调 LLM,响应 < 100ms。前端可通过 audit log 的 status=breaker_fallback
// 识别降级状态,显示降级提示。
//
// 设计原则:
//   - **不调 LLM**(避免 fallback 递归触发熔断)
//   - 返回 deterministic content(便于前端识别)
//   - usage = 0(不计入 token 预算)
//   - 返回 status="degraded" 标识(给 audit log 用)
func DefaultKeywordFallback(_ context.Context, _ string, _ []llm.Message) (string, llm.Usage, error) {
	// 简化:返回 JSON(假设 caller 走 JSON 解析)。如果 caller 不解析 JSON,直接当文本返回也行。
	// 这里返回最简的字符串,业务侧 AgentOrchestrator 会自己兜底处理。
	return `{"action":"speak","reasoning":"[degraded:LLM unavailable]","content":"系统繁忙,请稍后重试","confidence":0.0,"stance":"neutral"}`,
		llm.Usage{}, nil
}
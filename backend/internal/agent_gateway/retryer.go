package agent_gateway

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"time"

	"github.com/decisioncourt/backend/internal/observability"
)

// Retryer 对 LLM 调用做退避重试。MVP 仅对 Complete 生效；
// StreamComplete 失败不重试，因为流式重试会破坏 chunk 连续性。
//
// 默认退避：500ms, 1s, 2s，最多 3 次重试。可通过 NewRetryerWithBackoff
// 自定义，方便单测加速。
//
// v2.11 (deferred D8) 两项修正：
//  1. **并发安全**：Retryer 现在是**无状态**的 —— 重试次数通过返回值携带，
//     而不是写进实例字段再事后查询。此前 lastCount 是共享单例上的普通 int，
//     并发调用会互相覆盖（数据竞争 + 把别次调用的重试次数读成自己的）。
//     修法是"从根上消除共享状态"，而不是给字段加锁。
//  2. **错误分类**：只重试值得重试的失败（限流 / 服务端错误 / 网络超时），
//     鉴权与参数错误、用户取消不再重试 —— 避免"重试放大资损"。
const (
	defaultBackoffBase = 500 * time.Millisecond

	// maxBackoffCap 单次退避上限。给退避"设上限"是 jitter 之外的独立要求：
	// 将来把退避列表调大（或自定义传入长退避）时，单次等待不会失控。
	maxBackoffCap = 5 * time.Second
)

// Retryer 执行重试。无状态，可安全并发共享。
type Retryer struct {
	backoffDurations []time.Duration
	// v2.3 (ADR 0037) 注入 metrics；nil 时所有埋点 no-op
	metrics observability.Metrics
}

// NewRetryer 用默认退避（500ms / 1s / 2s）构造。
func NewRetryer(metrics observability.Metrics) *Retryer {
	return NewRetryerWithBackoff([]time.Duration{
		defaultBackoffBase,
		2 * defaultBackoffBase,
		4 * defaultBackoffBase,
	}, metrics)
}

// NewRetryerWithBackoff 用自定义退避构造；空切片表示不重试。
// metrics 传 nil 时所有埋点 no-op（向后兼容）。
func NewRetryerWithBackoff(durations []time.Duration, metrics observability.Metrics) *Retryer {
	return &Retryer{backoffDurations: durations, metrics: metrics}
}

// Do 执行 operation，失败时按退避重试。
//
// 返回 (retries, err)：
//   - retries 是**本次调用**实际发生的重试次数（不含首次尝试），
//     并发调用之间互不干扰（区别于 D8 之前"调完再问 LastCount"的写法）
//   - err 是最终 error；调用方闭包自行接收 operation 成功时的返回值
//
// 语义：operation 某次成功返回 nil；错误不可重试时立即返回，不做无谓等待。
func (r *Retryer) Do(operation func() error) (int, error) {
	return r.do(context.Background(), operation)
}

// DoContext 与 Do 相同，但支持 ctx 取消。本项目中 Complete 使用。
//
// ctx 已取消 / 已超时后不再发起新的尝试（连退避等待都跳过）——否则会白等
// 完退避序列再发现 ctx 早就死了。
func (r *Retryer) DoContext(ctx context.Context, operation func() error) (int, error) {
	return r.do(ctx, operation)
}

func (r *Retryer) do(ctx context.Context, operation func() error) (int, error) {
	err := operation()
	if err == nil {
		return 0, nil
	}
	if !IsRetryable(err) {
		return 0, err
	}

	retries := 0
	for _, d := range r.backoffDurations {
		// ctx 已结束：不再重试（返回最后一次的 error，而不是 ctx.Err()，
		// 保留真实的失败原因给上层分类）。
		if ctx.Err() != nil {
			return retries, err
		}
		timer := time.NewTimer(backoffDuration(d))
		select {
		case <-ctx.Done():
			timer.Stop()
			return retries, ctx.Err()
		case <-timer.C:
		}

		retries++
		if r.metrics != nil {
			r.metrics.IncCounter(observability.MetricLLMRetryAttemptTotal, nil)
		}
		err = operation()
		if err == nil {
			return retries, nil
		}
		if !IsRetryable(err) {
			return retries, err
		}
	}
	return retries, err
}

// backoffDuration 给基础退避加 jitter 并设上限。
//
//   - jitter：取 [d/2, d] 区间的随机值（"equal jitter"）。多个并发调用同时
//     失败时不会在同一毫秒一起重试（thundering herd），也不会让退避完全失效。
//   - cap：超过 maxBackoffCap 时截断。
func backoffDuration(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	half := base / 2
	d := half + time.Duration(rand.Int63n(int64(base-half)+1))
	if d > maxBackoffCap {
		d = maxBackoffCap
	}
	return d
}

// 可重试 / 不可重试的判定依据（表驱动测试见 retryer_classify_test.go）。

// httpStatusCoder 由"带 HTTP 状态码的错误"实现。
//
// 定义成接口是为了让 agent_gateway 不必 import 上游 SDK：llm 包在包装 API 错误时
// 实现它（见 llm.APIStatusError），分类逻辑只依赖这个窄契约。
type httpStatusCoder interface {
	HTTPStatusCode() int
}

// IsRetryable 判定一次失败是否值得重试。
//
// 不可重试（永久性失败，重试只会放大代价）：
//   - context.Canceled：用户 / 上层主动取消
//   - 鉴权与参数错误：HTTP 400 / 401 / 403 / 404 / 405 / 409 / 413 / 422
//
// 可重试（瞬时性失败）：
//   - context.DeadlineExceeded 与 net.Error 超时：网络 / 上游超时
//   - io.EOF / io.ErrUnexpectedEOF：连接中途断开
//   - HTTP 408 / 425 / 429：请求超时 / Too Early / 限流
//   - HTTP 5xx：上游服务端错误
//
// 兜底：无法识别的错误按**可重试**处理。这是有意的保守选择 —— D8 之前的行为是
// "任何非 nil 错误都重试"，若把未知错误改成不可重试，会把"未分类的瞬时故障"
// 变成硬失败（比原缺陷更糟）。本次只收紧**确信是永久性**的那几类。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// 1. 用户 / 上层取消：重试没有意义（且会拖住取消路径）。
	if errors.Is(err, context.Canceled) {
		return false
	}
	// 2. 超时：值得重试（网络抖动 / 上游慢）。
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// 3. 连接中途断开。
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// 4. net 层超时。
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	// 5. HTTP 状态码分类。
	var sc httpStatusCoder
	if errors.As(err, &sc) {
		return isRetryableStatus(sc.HTTPStatusCode())
	}
	// 6. 未知 → 保守可重试（保持 D8 之前的"失败能自动恢复"语义）。
	return true
}

// isRetryableStatus 按 HTTP 状态码判定可重试性。
func isRetryableStatus(code int) bool {
	switch {
	case code == 408 || code == 425 || code == 429: // 超时 / Too Early / 限流
		return true
	case code >= 500 && code <= 599: // 服务端错误
		return true
	case code >= 400 && code <= 499: // 其余客户端错误：参数 / 鉴权等，重试无益
		return false
	default:
		return true
	}
}

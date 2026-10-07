package llm

import (
	"errors"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

// APIStatusError 把上游 LLM API 的错误连同 HTTP 状态码一起包装。
//
// 为什么需要（v2.11 / deferred D8）：重试策略要按状态码分类 —— 429 与 5xx 值得
// 重试，401 / 400 这类参数与鉴权错误重试无益（只会放大代价）。但重试器在
// agent_gateway 包，不该 import 上游 SDK。这里实现一个窄契约
// `HTTPStatusCode() int`，让分类逻辑只依赖方法而不是具体类型
// （见 agent_gateway.IsRetryable）。
//
// Unwrap 保留原始错误链：errors.Is(err, context.Canceled) / 超时判定等仍可用。
type APIStatusError struct {
	StatusCode int
	Status     string
	Err        error
}

func (e *APIStatusError) Error() string {
	if e.Status != "" {
		return fmt.Sprintf("llm api error: status code %d (%s): %v", e.StatusCode, e.Status, e.Err)
	}
	return fmt.Sprintf("llm api error: status code %d: %v", e.StatusCode, e.Err)
}

// Unwrap 暴露原始错误，保持 errors.Is / errors.As 链完整。
func (e *APIStatusError) Unwrap() error { return e.Err }

// HTTPStatusCode 实现 agent_gateway 的重试分类契约（一行方法的窄接口）。
func (e *APIStatusError) HTTPStatusCode() int { return e.StatusCode }

// wrapAPIError 在 error 链里查找带 HTTP 状态码的 SDK 错误并包装。
//
// 找不到状态码时**原样返回**：分类逻辑对未知错误走"保守可重试"的兜底，
// 所以这里不需要为"没有状态码"造一个假值（造了反而会让兜底误判）。
func wrapAPIError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
		return &APIStatusError{StatusCode: apiErr.HTTPStatusCode, Status: apiErr.HTTPStatus, Err: err}
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) && reqErr.HTTPStatusCode > 0 {
		return &APIStatusError{StatusCode: reqErr.HTTPStatusCode, Status: reqErr.HTTPStatus, Err: err}
	}
	return err
}

package llm

// v2.11 (deferred D8): 状态码透传的回归护栏。
//
// 重试分类（agent_gateway.IsRetryable）只有在"上游错误真的带上 HTTP 状态码"
// 时才有用 —— 否则 401 / 400 这类永久性失败会落进"未知 → 可重试"的兜底，
// 分类形同虚设。这组测试钉住 wrapAPIError 的行为。

import (
	"errors"
	"io"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapAPIError_StatusCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"APIError 429", &openai.APIError{HTTPStatusCode: 429, HTTPStatus: "429 Too Many Requests"}, 429},
		{"APIError 401", &openai.APIError{HTTPStatusCode: 401, HTTPStatus: "401 Unauthorized"}, 401},
		{"RequestError 503", &openai.RequestError{HTTPStatusCode: 503, HTTPStatus: "503 Service Unavailable"}, 503},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapAPIError(c.err)
			var sc interface{ HTTPStatusCode() int }
			require.ErrorAs(t, got, &sc, "包装后必须实现 HTTPStatusCode() int")
			assert.Equal(t, c.want, sc.HTTPStatusCode())
		})
	}
}

// TestWrapAPIError_PreservesErrorChain 状态码之外，原始错误链不能断
// （上层还要用 errors.Is 判断取消 / 超时 / io.EOF）。
func TestWrapAPIError_PreservesErrorChain(t *testing.T) {
	inner := &openai.RequestError{HTTPStatusCode: 502, HTTPStatus: "502", Err: io.ErrUnexpectedEOF}
	got := wrapAPIError(inner)
	assert.True(t, errors.Is(got, io.ErrUnexpectedEOF), "Unwrap 必须保留原始错误链")

	// 已经被 fmt.Errorf("%w") 包一层时也要能穿透找到状态码。
	outer := wrapAPIError(io.EOF)
	assert.True(t, errors.Is(outer, io.EOF))
}

// TestWrapAPIError_LeavesUnknownErrorsAlone 没有状态码的错误原样返回 ——
// 不要造一个假状态码（否则会把"未知"错误误判成 4xx 而不再重试）。
func TestWrapAPIError_LeavesUnknownErrorsAlone(t *testing.T) {
	plain := errors.New("no completion choices returned")
	assert.Equal(t, plain, wrapAPIError(plain))

	var sc interface{ HTTPStatusCode() int }
	assert.False(t, errors.As(wrapAPIError(plain), &sc), "无状态码的错误不应被包装成带状态码的错误")

	// 状态码为 0 的 SDK 错误也没有信息量，不应包装。
	zero := &openai.APIError{Message: "boom"}
	assert.Equal(t, error(zero), wrapAPIError(zero))
}

func TestWrapAPIError_NilIsNil(t *testing.T) {
	assert.Nil(t, wrapAPIError(nil))
}

// TestAPIStatusError_ErrorAndAccessors Error() 可读且不泄露额外信息。
func TestAPIStatusError_ErrorAndAccessors(t *testing.T) {
	e := &APIStatusError{StatusCode: 429, Status: "429 Too Many Requests", Err: errors.New("rate limited")}
	assert.Contains(t, e.Error(), "429")
	assert.Contains(t, e.Error(), "rate limited")
	assert.Equal(t, 429, e.HTTPStatusCode())
	assert.True(t, errors.Is(e, e.Err))

	bare := &APIStatusError{StatusCode: 500, Err: errors.New("boom")}
	assert.Contains(t, bare.Error(), "500")
}

package agent_gateway

// v2.11 (deferred D8): 重试错误分类的表驱动测试。
//
// 契约（见 IsRetryable 的文档注释）：
//   不可重试 — 用户取消、鉴权/参数等永久性 4xx
//   可重试   — 超时、连接中断、限流(429)、Too Early(425)、请求超时(408)、5xx
//   兜底     — 无法识别的错误按可重试处理（保守：D8 之前"任何错误都重试"，
//              把未分类故障改成不重试会比原缺陷更糟）

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// statusErr 模拟 llm.APIStatusError 的窄契约（实现 HTTPStatusCode() int）。
type statusErr struct{ code int }

func (e *statusErr) Error() string       { return fmt.Sprintf("api status %d", e.code) }
func (e *statusErr) HTTPStatusCode() int { return e.code }

// wrappedStatusErr 验证 errors.As 能穿透中间包装层。
type wrappedStatusErr struct{ inner error }

func (e *wrappedStatusErr) Error() string { return "wrapped: " + e.inner.Error() }
func (e *wrappedStatusErr) Unwrap() error { return e.inner }

// timeoutNetErr 模拟 net.Error 超时。
type timeoutNetErr struct{}

func (timeoutNetErr) Error() string   { return "i/o timeout" }
func (timeoutNetErr) Timeout() bool   { return true }
func (timeoutNetErr) Temporary() bool { return true }

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// --- 不可重试 ---
		{"nil", nil, false},
		{"用户取消", context.Canceled, false},
		{"包装后的取消", fmt.Errorf("llm completion failed: %w", context.Canceled), false},
		{"鉴权 401", &statusErr{401}, false},
		{"鉴权 403", &statusErr{403}, false},
		{"参数 400", &statusErr{400}, false},
		{"参数 422", &statusErr{422}, false},
		{"不存在 404", &statusErr{404}, false},
		{"冲突 409", &statusErr{409}, false},
		{"穿透包装的 401", &wrappedStatusErr{inner: &statusErr{401}}, false},
		{"模型不存在（deepseek 对错模型名的常见返回）400", &statusErr{400}, false},

		// --- 可重试 ---
		{"超时", context.DeadlineExceeded, true},
		{"包装后的超时", fmt.Errorf("llm completion failed: %w", context.DeadlineExceeded), true},
		{"限流 429", &statusErr{429}, true},
		{"请求超时 408", &statusErr{408}, true},
		{"Too Early 425", &statusErr{425}, true},
		{"服务端 500", &statusErr{500}, true},
		{"网关 502", &statusErr{502}, true},
		{"不可用 503", &statusErr{503}, true},
		{"穿透包装的 503", &wrappedStatusErr{inner: &statusErr{503}}, true},
		{"流中断 EOF", io.EOF, true},
		{"流中断 UnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"net 超时", timeoutNetErr{}, true},
		{"net.Error 判定（标准库）", &net.OpError{Op: "dial", Err: timeoutNetErr{}}, true},

		// --- 兜底：未知 → 保守可重试 ---
		{"未分类错误", errors.New("no completion choices returned"), true},
		{"文件不存在（未分类）", os.ErrNotExist, true},

		// --- 生产错误类型接入（llm 包实际返回的错误形状）---
		// 这一组保证分类器不只对测试自有 fake 生效：llm.APIStatusError 也会被识别。
		{"生产类型 llm.APIStatusError 429", &llm.APIStatusError{StatusCode: 429}, true},
		{"生产类型 llm.APIStatusError 401", &llm.APIStatusError{StatusCode: 401}, false},
		{"生产类型（包装后）500", fmt.Errorf("llm completion failed: %w", &llm.APIStatusError{StatusCode: 500}), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRetryable(c.err); got != c.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// TestIsRetryableStatus_Boundaries 状态码边界：4xx 逐个覆盖"哪些例外"。
func TestIsRetryableStatus_Boundaries(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{399, true}, // 非 4xx/5xx：兜底可重试
		{400, false}, {401, false}, {402, false}, {403, false}, {404, false},
		{405, false}, {406, false}, {407, false}, {408, true}, {409, false},
		{410, false}, {411, false}, {412, false}, {413, false}, {414, false},
		{415, false}, {416, false}, {417, false}, {418, false}, {421, false},
		{422, false}, {423, false}, {424, false}, {425, true}, {426, false},
		{428, false}, {429, true}, {431, false}, {451, false},
		{499, false}, {500, true}, {501, true}, {502, true}, {503, true},
		{504, true}, {507, true}, {599, true}, {600, true}, // 600 非 5xx：兜底可重试
	}
	for _, c := range cases {
		if got := isRetryableStatus(c.code); got != c.want {
			t.Errorf("isRetryableStatus(%d) = %v, want %v", c.code, got, c.want)
		}
	}
}

// TestIsRetryable_TimeoutErrorIsRetryable 真 net timeouts 也要被识别（不只是构造的 fake）。
func TestIsRetryable_TimeoutErrorIsRetryable(t *testing.T) {
	// 一个已过期的 deadline ctx 触发的错误属于超时族
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if !IsRetryable(ctx.Err()) {
		t.Errorf("ctx deadline exceeded 应可重试, got %v", ctx.Err())
	}
}

// TestRetryer_HasNoMutableCounterState 结构护栏（D8 回归防线）。
//
// D8 的原始缺陷是"把重试次数写进共享实例字段，再事后查询"。这条测试钉住
// "重试次数必须通过返回值携带"：Retryer 不得再出现可变计数字段，也不得再提供
// LastCount 方法。否则一旦有人改回去（它不会编译失败），并发缺陷就悄悄回来了。
func TestRetryer_HasNoMutableCounterState(t *testing.T) {
	typ := reflect.TypeOf(Retryer{})
	allowed := map[string]bool{
		"backoffDurations": true, // 构造时固化、运行期只读
		"metrics":          true, // 只读埋点句柄
	}
	forbidden := map[string]bool{"lastcount": true, "retrycount": true, "count": true, "attempts": true}

	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if forbidden[strings.ToLower(f.Name)] {
			t.Errorf("Retryer 不应再有可变计数字段 %s —— 重试次数必须通过返回值携带(D8)", f.Name)
		}
		if !allowed[f.Name] {
			t.Errorf("Retryer 新增了字段 %s —— 若它是可变状态，请改为返回值携带(D8：Retryer 必须无状态)", f.Name)
		}
	}

	if _, ok := reflect.TypeOf(&Retryer{}).MethodByName("LastCount"); ok {
		t.Error("LastCount() 必须保持删除状态 —— 它是并发缺陷的来源(D8)")
	}
	// Do / DoContext 必须携带重试次数（返回两个值）。
	for _, name := range []string{"Do", "DoContext"} {
		m, ok := reflect.TypeOf(&Retryer{}).MethodByName(name)
		if !ok {
			t.Fatalf("Retryer.%s 缺失", name)
		}
		if n := m.Type.NumOut(); n != 2 {
			t.Errorf("Retryer.%s 必须返回 (retries int, err error)，当前有 %d 个返回值", name, n)
		}
	}
}

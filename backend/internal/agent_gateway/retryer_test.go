package agent_gateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// v2.11 (deferred D8): Do / DoContext 改为返回 (retries, err)，删掉 lastCount
// 字段与 LastCount() —— 共享单例上的普通 int 在并发下会互相覆盖，且"调完再问"
// 会把别次调用的重试次数读成自己的。修法是从根上消除共享状态，而不是加锁。

func TestRetryer_SuccessNoRetry(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}, nil)
	calls := 0
	retries, err := r.Do(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls: want 1 got %d", calls)
	}
	if retries != 0 {
		t.Errorf("retries: want 0 got %d", retries)
	}
}

func TestRetryer_RetryOnceThenSuccess(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}, nil)
	calls := 0
	retries, err := r.Do(func() error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls: want 2 got %d", calls)
	}
	if retries != 1 {
		t.Errorf("retries: want 1 got %d", retries)
	}
}

func TestRetryer_FailsAfterMaxRetries(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}, nil)
	calls := 0
	retries, err := r.Do(func() error {
		calls++
		return errors.New("always fail")
	})
	if err == nil {
		t.Fatal("expected err")
	}
	if calls != 4 { // initial + 3 retries
		t.Errorf("calls: want 4 got %d", calls)
	}
	if retries != 3 {
		t.Errorf("retries: want 3 got %d", retries)
	}
}

func TestRetryer_NoRetryWhenDisabled(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{}, nil)
	calls := 0
	retries, err := r.Do(func() error {
		calls++
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected err")
	}
	if calls != 1 {
		t.Errorf("calls: want 1 got %d", calls)
	}
	if retries != 0 {
		t.Errorf("retries: want 0 got %d", retries)
	}
}

// TestRetryer_NonRetryableErrorSkipsRetries 永久性失败不重试（D8 的核心修正）。
// 410 Gone 属 4xx 非 429/408/425 → 不可重试。
func TestRetryer_NonRetryableErrorSkipsRetries(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}, nil)
	calls := 0
	retries, err := r.Do(func() error {
		calls++
		return &statusErr{code: 401}
	})
	if err == nil {
		t.Fatal("expected err")
	}
	if calls != 1 {
		t.Errorf("不可重试错误只应调用 1 次, got %d", calls)
	}
	if retries != 0 {
		t.Errorf("retries: want 0 got %d", retries)
	}
}

// TestRetryer_CanceledContextStopsRetrying ctx 取消后不再发起新尝试。
func TestRetryer_CanceledContextStopsRetrying(t *testing.T) {
	// 用较长退避确保"取消发生在退避等待期间"确实被观察到。
	r := NewRetryerWithBackoff([]time.Duration{500 * time.Millisecond, 500 * time.Millisecond}, nil)
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	retries, err := r.DoContext(ctx, func() error {
		calls++
		return errors.New("transient")
	})
	if err == nil {
		t.Fatal("expected err")
	}
	if calls != 1 {
		t.Errorf("取消后不应再尝试, calls=%d", calls)
	}
	if retries != 0 {
		t.Errorf("retries: want 0 got %d", retries)
	}
}

// TestRetryer_AlreadyCanceledContextSkipsBackoff ctx 一开始就取消 → 不做任何
// 退避等待（否则会白等完整个退避序列）。
func TestRetryer_AlreadyCanceledContextSkipsBackoff(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{2 * time.Second, 2 * time.Second}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := r.DoContext(ctx, func() error { return errors.New("transient") })
	if err == nil {
		t.Fatal("expected err")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("ctx 已取消时不应退避等待, elapsed=%v", elapsed)
	}
}

// TestRetryer_ConcurrentSafe 并发调用必须无数据竞争、且各自拿到自己的重试次数。
//
// 这是 D8 的原始缺陷：lastCount 是共享单例上的普通 int，-race 会直接报竞争。
// 现在 Retryer 无状态，重试次数通过返回值携带。
func TestRetryer_ConcurrentSafe(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{time.Millisecond}, nil)

	const goroutines = 32
	var wg sync.WaitGroup
	var mismatches int64
	var errCount int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// 每个 goroutine 的重试次数不同：i%2==0 → 首次成功(0 次)，
			// 否则失败一次再成功(1 次)。若计数被共享字段串扰，断言会失败。
			calls := 0
			wantRetries := i % 2
			retries, err := r.Do(func() error {
				calls++
				if calls == 1 && i%2 == 1 {
					return errors.New("transient")
				}
				return nil
			})
			if err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			if retries != wantRetries {
				atomic.AddInt64(&mismatches, 1)
			}
		}(i)
	}
	wg.Wait()

	if n := atomic.LoadInt64(&errCount); n != 0 {
		t.Errorf("意外失败 %d 次", n)
	}
	if n := atomic.LoadInt64(&mismatches); n != 0 {
		t.Errorf("重试次数串扰 %d 次（并发不安全）", n)
	}
}

// TestBackoffDuration_JitterAndCap 退避加 jitter（[d/2, d]）且设上限。
func TestBackoffDuration_JitterAndCap(t *testing.T) {
	base := 100 * time.Millisecond
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		d := backoffDuration(base)
		if d < base/2 || d > base {
			t.Fatalf("jitter 越界: base=%v got=%v（应在 [d/2, d]）", base, d)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Error("退避缺少 jitter（200 次采样只有一个值，会形成 thundering herd）")
	}

	// 上限：远超 cap 的基础退避被截断。
	if got := backoffDuration(1 * time.Hour); got > maxBackoffCap {
		t.Errorf("退避未设上限: got=%v cap=%v", got, maxBackoffCap)
	}
	// 0 / 负值不 panic。
	if got := backoffDuration(0); got != 0 {
		t.Errorf("base=0 应返回 0, got %v", got)
	}
	if got := backoffDuration(-time.Second); got != 0 {
		t.Errorf("base<0 应返回 0, got %v", got)
	}
}

// TestRetryer_StatelessAcrossCalls 同一个 Retryer 连续两次调用，
// 第二次的返回值不受第一次影响（无残留状态）。
func TestRetryer_StatelessAcrossCalls(t *testing.T) {
	r := NewRetryerWithBackoff([]time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}, nil)

	first, err := r.Do(func() error { return errors.New("boom") })
	if err == nil || first != 3 {
		t.Fatalf("第一次调用应重试 3 次, got retries=%d err=%v", first, err)
	}

	second, err := r.Do(func() error { return nil })
	if err != nil || second != 0 {
		t.Fatalf("第二次调用应 0 次重试, got retries=%d err=%v", second, err)
	}
}

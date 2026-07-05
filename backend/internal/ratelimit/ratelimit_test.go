package ratelimit

// v0.9 (ADR 0014) 用户级 Trial 限流测试。
//
// 验证 4 个核心契约:
//   1. 前 N 次允许,第 N+1 次拒绝
//   2. 滑动窗口:超窗口的请求可重新允许
//   3. 不同用户隔离:用户 A 满额不影响用户 B
//   4. 并发安全:50 goroutine 并发 Allow,总成功 = limit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestMemoryRateLimiter_BasicAllowDeny 验证:limit=3,前 3 次 allowed=true,
// 第 4 次 allowed=false。这是限流器最基础的契约。
func TestMemoryRateLimiter_BasicAllowDeny(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(3, time.Hour)
	defer r.Stop()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, retryAfter, err := r.Allow(ctx, "user-1")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !allowed {
			t.Errorf("call %d: expected allowed, got denied (retryAfter=%v)", i, retryAfter)
		}
		if retryAfter != 0 {
			t.Errorf("call %d: expected retryAfter=0, got %v", i, retryAfter)
		}
	}

	// 第 4 次应被拒绝
	allowed, retryAfter, err := r.Allow(ctx, "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("call 4: expected denied, got allowed")
	}
	if retryAfter <= 0 || retryAfter > time.Hour {
		t.Errorf("call 4: expected retryAfter in (0, 1h], got %v", retryAfter)
	}
}

// TestMemoryRateLimiter_SlidingWindowExpires 验证:超窗口的 timestamps 被清掉,
// 新请求可以重新允许。这是滑动窗口 vs 固定窗口的关键差异。
func TestMemoryRateLimiter_SlidingWindowExpires(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(2, 100*time.Millisecond)
	defer r.Stop()
	ctx := context.Background()

	// 前 2 次允许
	r.Allow(ctx, "user-1")
	r.Allow(ctx, "user-1")

	// 第 3 次拒绝
	allowed, _, _ := r.Allow(ctx, "user-1")
	if allowed {
		t.Fatal("call 3: expected denied")
	}

	// 等窗口过去
	time.Sleep(150 * time.Millisecond)

	// 窗口外 timestamps 应被清掉,新请求允许
	allowed, _, _ = r.Allow(ctx, "user-1")
	if !allowed {
		t.Error("after window expired: expected allowed, got denied")
	}
}

// TestMemoryRateLimiter_DifferentUsersIndependent 验证:不同用户隔离。
// 用户 A 满额不影响用户 B。这是限流器"按 user 隔离"的契约。
func TestMemoryRateLimiter_DifferentUsersIndependent(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(2, time.Hour)
	defer r.Stop()
	ctx := context.Background()

	// 用户 A 用满
	r.Allow(ctx, "user-A")
	r.Allow(ctx, "user-A")
	allowed, _, _ := r.Allow(ctx, "user-A")
	if allowed {
		t.Fatal("user-A should be denied after 2 trials")
	}

	// 用户 B 仍可调用
	for i := 0; i < 2; i++ {
		allowed, _, _ := r.Allow(ctx, "user-B")
		if !allowed {
			t.Errorf("user-B call %d: expected allowed (different from user-A), got denied", i)
		}
	}
}

// TestMemoryRateLimiter_ConcurrentSafe 验证:并发安全。
// 50 goroutine × 4 calls = 200 次并发请求,limit=10 → 只有 10 次成功。
// 用 -race 跑这个测试,任何 data race 都会被 catch。
func TestMemoryRateLimiter_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(10, time.Hour)
	defer r.Stop()
	ctx := context.Background()

	const goroutines = 50
	const callsPerGoroutine = 4

	var allowedCount int64
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				allowed, _, _ := r.Allow(ctx, "shared-user")
				if allowed {
					atomic.AddInt64(&allowedCount, 1)
				}
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt64(&allowedCount); got != 10 {
		t.Errorf("expected exactly 10 allowed (limit=10), got %d", got)
	}
}

// TestMemoryRateLimiter_Used 验证:Used 返回当前已用次数。
func TestMemoryRateLimiter_Used(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(5, time.Hour)
	defer r.Stop()
	ctx := context.Background()

	if r.Used("user-x") != 0 {
		t.Error("expected Used=0 for new user")
	}
	r.Allow(ctx, "user-x")
	r.Allow(ctx, "user-x")
	if r.Used("user-x") != 2 {
		t.Errorf("expected Used=2, got %d", r.Used("user-x"))
	}
}

// TestMemoryRateLimiter_Stats 验证:Stats 返回 tracked users 和 total requests。
func TestMemoryRateLimiter_Stats(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(10, time.Hour)
	defer r.Stop()
	ctx := context.Background()

	for _, key := range []string{"u1", "u2", "u3"} {
		r.Allow(ctx, key)
		r.Allow(ctx, key)
	}

	users, total := r.Stats()
	if users != 3 {
		t.Errorf("expected trackedUsers=3, got %d", users)
	}
	if total != 6 {
		t.Errorf("expected totalRequests=6, got %d", total)
	}
}

// TestMemoryRateLimiter_Defaults 验证:默认参数生效(limit=0, window=0)。
func TestMemoryRateLimiter_Defaults(t *testing.T) {
	t.Parallel()

	r := NewMemoryRateLimiter(0, 0)
	defer r.Stop()

	if r.limit != 5 {
		t.Errorf("expected default limit=5, got %d", r.limit)
	}
	if r.window != 24*time.Hour {
		t.Errorf("expected default window=24h, got %v", r.window)
	}
}
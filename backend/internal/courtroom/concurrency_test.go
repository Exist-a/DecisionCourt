package courtroom

// v0.10.20 (ADR 0027 §5.2) L0 ConcurrencyLimiter 测试。
//
// 验证 5 个核心契约 (ADR 0027 §5.2):
//   1. BasicAcquireRelease: max=2, 前 2 次 TryAcquire 返回 true, 第 3 次 false;
//      Release 1 次后可再拿到
//   2. ConcurrentSafe: 100 goroutine × 1 acquire/release, 最终 current=0 (无泄漏)
//   3. Stats: current/max 正确
//   4. ZeroMax: max=0 → 用默认 5
//   5. NilSafe: nil limiter 不 panic, TryAcquire 返 true, Release 不阻塞

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyLimiter_BasicAcquireRelease(t *testing.T) {
	t.Parallel()

	// max=2 便于快速测 "第 3 次失败"
	lim := NewConcurrencyLimiter(2)

	// 1. 前 2 次 TryAcquire 返回 true
	if !lim.TryAcquire() {
		t.Error("acquire 1: expected true, got false")
	}
	if !lim.TryAcquire() {
		t.Error("acquire 2: expected true, got false")
	}

	// 2. 第 3 次返回 false (已满)
	if lim.TryAcquire() {
		t.Error("acquire 3: expected false (limit reached), got true")
	}

	// 3. Release 1 次后再 TryAcquire 返回 true
	lim.Release()
	if !lim.TryAcquire() {
		t.Error("acquire after release: expected true, got false")
	}

	// 4. 验证 current 计数正确
	cur, max, available := lim.Stats()
	if cur != 2 {
		t.Errorf("current: expected 2, got %d", cur)
	}
	if max != 2 {
		t.Errorf("max: expected 2, got %d", max)
	}
	if available != 0 {
		t.Errorf("available: expected 0, got %d", available)
	}

	// 5. 全部 Release, current 应该回 0
	lim.Release()
	lim.Release()
	cur, _, _ = lim.Stats()
	if cur != 0 {
		t.Errorf("after release all: expected current=0, got %d", cur)
	}
}

// TestConcurrencyLimiter_ConcurrentSafe 验证: 100 goroutine 并发 TryAcquire
// (不 Release), 成功数必须恰好 = max (50), 验证"slot 真的只够 max 个"。
// 用 -race 跑这个测试, 任何 data race 都会被 catch。
func TestConcurrencyLimiter_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	const goroutines = 100
	lim := NewConcurrencyLimiter(50) // max=50

	var success int64
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lim.TryAcquire() {
				atomic.AddInt64(&success, 1)
				// 不 release, 验证 slot 真的只够 max 个
			}
		}()
	}
	wg.Wait()

	// 期望: 成功 = 50 (max), 因为所有 goroutine 都不 release, slot 满后后续都失败
	if got := atomic.LoadInt64(&success); got != 50 {
		t.Errorf("expected 50 successes (max=50, no release), got %d", got)
	}

	// 验证 current 真的 = 50 (说明 slot 都还在)
	cur, max, available := lim.Stats()
	if cur != 50 {
		t.Errorf("expected current=50 after 50 acquires, got %d", cur)
	}
	if max != 50 {
		t.Errorf("expected max=50, got %d", max)
	}
	if available != 0 {
		t.Errorf("expected available=0 (full), got %d", available)
	}

	// 全部 Release 后, current 必须 = 0 (验证 Release 能彻底清空)
	for i := 0; i < 50; i++ {
		lim.Release()
	}
	cur, _, _ = lim.Stats()
	if cur != 0 {
		t.Errorf("after 50 releases: expected current=0, got %d (slot leak!)", cur)
	}
}

// TestConcurrencyLimiter_Stats 验证: Stats() 返回值正确。
func TestConcurrencyLimiter_Stats(t *testing.T) {
	t.Parallel()

	lim := NewConcurrencyLimiter(3)

	// 初始: 0/3/3
	if cur, max, available := lim.Stats(); cur != 0 || max != 3 || available != 3 {
		t.Errorf("initial stats: got cur=%d max=%d available=%d, want 0/3/3",
			cur, max, available)
	}

	// Acquire 1 次: 1/3/2
	lim.TryAcquire()
	if cur, max, available := lim.Stats(); cur != 1 || max != 3 || available != 2 {
		t.Errorf("after 1 acquire: got cur=%d max=%d available=%d, want 1/3/2",
			cur, max, available)
	}

	// Acquire 3 次: 3/3/0 (满)
	lim.TryAcquire()
	lim.TryAcquire()
	if cur, _, available := lim.Stats(); cur != 3 || available != 0 {
		t.Errorf("when full: got cur=%d available=%d, want 3/0", cur, available)
	}

	// Release 2 次: 1/3/2
	lim.Release()
	lim.Release()
	if cur, _, available := lim.Stats(); cur != 1 || available != 2 {
		t.Errorf("after 2 release: got cur=%d available=%d, want 1/2", cur, available)
	}
}

// TestConcurrencyLimiter_ZeroMax 验证: max <= 0 时用默认 5。
func TestConcurrencyLimiter_ZeroMax(t *testing.T) {
	t.Parallel()

	lim := NewConcurrencyLimiter(0)

	// 验证: max 应该是 5 (默认值)
	if max := lim.Max(); max != 5 {
		t.Errorf("expected default max=5, got %d", max)
	}

	// 验证: 第 6 次 TryAcquire 失败
	for i := 0; i < 5; i++ {
		if !lim.TryAcquire() {
			t.Errorf("acquire %d: expected true, got false", i+1)
		}
	}
	if lim.TryAcquire() {
		t.Error("acquire 6: expected false (default max=5 reached), got true")
	}
}

// TestConcurrencyLimiter_NilSafe 验证: nil limiter 不 panic, 行为合理。
// 这是 nil-injection 模式的支持 (测试代码可不传 limiter)。
func TestConcurrencyLimiter_NilSafe(t *testing.T) {
	t.Parallel()

	var lim *ConcurrencyLimiter // nil

	// 1. TryAcquire 返回 true (不限流, 放行)
	if !lim.TryAcquire() {
		t.Error("nil.TryAcquire: expected true (no limit), got false")
	}

	// 2. Release 不阻塞不 panic
	lim.Release() // 应该立即返回

	// 3. Stats 返回零值
	if cur, max, available := lim.Stats(); cur != 0 || max != 0 || available != 0 {
		t.Errorf("nil.Stats: got cur=%d max=%d available=%d, want 0/0/0",
			cur, max, available)
	}

	// 4. Max 返回 0
	if max := lim.Max(); max != 0 {
		t.Errorf("nil.Max: expected 0, got %d", max)
	}
}

// TestConcurrencyLimiter_ReleaseWithoutAcquire 验证: 没有 TryAcquire 就 Release
// 不会 panic / 阻塞, 只 log warning。这是"宽容释放"契约 ——
// 生产环境优先避免进程卡死。
func TestConcurrencyLimiter_ReleaseWithoutAcquire(t *testing.T) {
	t.Parallel()

	lim := NewConcurrencyLimiter(2)

	// 1. 没 TryAcquire 就 Release: 不 panic, 不阻塞
	done := make(chan struct{})
	go func() {
		lim.Release()
		close(done)
	}()
	select {
	case <-done:
		// 成功: Release 没阻塞
	case <-time.After(1 * time.Second):
		t.Fatal("Release without Acquire blocked (should be non-blocking)")
	}

	// 2. Release 后 current 仍然是 0 (没有负数)
	cur, _, _ := lim.Stats()
	if cur != 0 {
		t.Errorf("after bare Release: expected current=0, got %d", cur)
	}

	// 3. 配对使用: TryAcquire + Release 后 current = 0
	lim.TryAcquire()
	lim.Release()
	cur, _, _ = lim.Stats()
	if cur != 0 {
		t.Errorf("after paired Acquire+Release: expected current=0, got %d", cur)
	}

	// 4. 多 Release: 第一个正常, 后续 Release 不 panic / 不阻塞
	lim.TryAcquire()
	lim.Release()
	lim.Release() // 这次没匹配的 Acquire, 应该宽容处理
	cur, _, _ = lim.Stats()
	if cur != 0 {
		t.Errorf("after extra Release: expected current=0, got %d", cur)
	}
}
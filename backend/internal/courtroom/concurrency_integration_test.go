package courtroom

// v0.10.20 (ADR 0027 §5.3) L0 ConcurrencyLimiter 与 service 集成的集成测试。
//
// 验证:
//   1. nil limiter (未注入): TryAcquire 返 true, withCancel 正常工作, 无副作用
//   2. 注入 limiter: TryAcquire 受限, withCancel 自动 Release slot
//   3. 并发模拟: 5 个 slot 同时被 acquire, 第 6 个 withCancel 失败

import (
	"context"
	"testing"
)

// TestService_NilConcurrencyLimiter_NoLimit 验证: 未注入 limiter 时, L0 不限流,
// 所有 withCancel 调用都成功。
func TestService_NilConcurrencyLimiter_NoLimit(t *testing.T) {
	t.Parallel()

	// 构造一个不带 ConcurrencyLimiter 的 minimal Service
	svc := &Service{
		activeCalls:   make(map[string]context.CancelFunc),
		concurrencyLimiter: nil,
	}

	// TryAcquire 应返 true (不限流)
	if !svc.TryAcquireConcurrencySlot() {
		t.Error("nil limiter TryAcquire should return true (no limit)")
	}

	// withCancel 应正常工作
	ctx, cancel, err := svc.withCancel(context.Background(), "test-session-A")
	if err != nil {
		t.Errorf("withCancel with nil limiter: expected nil error, got %v", err)
	}
	if ctx == nil {
		t.Error("withCancel returned nil ctx")
	}
	if cancel == nil {
		t.Error("withCancel returned nil cancel func")
	}
	cancel() // 应该不 panic, 即使 limiter=nil

	// ConcurrencyStats 应返 0/0/0
	cur, max, available := svc.ConcurrencyStats()
	if cur != 0 || max != 0 || available != 0 {
		t.Errorf("nil limiter stats: expected 0/0/0, got %d/%d/%d", cur, max, available)
	}
}

// TestService_WithConcurrencyLimiter_Limits 验证: 注入 limiter 后,
// service.withCancel 受限; 调一次 withCancel 后 slot 被占, 第 2 次 withCancel 应失败。
//
// 这是 L0 唯一 acquire 点的契约 (handler 层不再直接调 TryAcquireConcurrencySlot,
// 避免双重 acquire 导致同 trial 占 2 slot)。
func TestService_WithConcurrencyLimiter_Limits(t *testing.T) {
	t.Parallel()

	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(2), // max=2
	}

	// 1. service.withCancel × 2 都成功 (模拟 2 个 trial 启动)
	_, cancel1, err := svc.withCancel(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("1st withCancel: expected nil error, got %v", err)
	}
	_, cancel2, err := svc.withCancel(context.Background(), "session-2")
	if err != nil {
		t.Fatalf("2nd withCancel: expected nil error, got %v", err)
	}

	// 2. 第 3 次 withCancel 应该失败 (slot 已满: 2/2)
	_, _, err = svc.withCancel(context.Background(), "session-3")
	if err != ErrConcurrencyLimitExceeded {
		t.Errorf("3rd withCancel: expected ErrConcurrencyLimitExceeded, got %v", err)
	}

	// 3. cancel1() → wrappedCancel → 自动 Release slot
	cancel1()

	// 4. 现在 current = 1 (cancel2 占的还在), available = 1
	cur, max, available := svc.ConcurrencyStats()
	if cur != 1 || max != 2 || available != 1 {
		t.Errorf("after 1 release: stats: expected 1/2/1, got %d/%d/%d", cur, max, available)
	}

	// 5. 现在第 3 次 withCancel 应该成功 (slot 释放了)
	_, cancel3, err := svc.withCancel(context.Background(), "session-3")
	if err != nil {
		t.Errorf("after release, 3rd withCancel: expected nil error, got %v", err)
	}

	// 6. 全部 cancel → current = 0
	cancel2()
	cancel3()
	cur, _, _ = svc.ConcurrencyStats()
	if cur != 0 {
		t.Errorf("after all release: expected current=0, got %d (slot leak!)", cur)
	}
}

// TestService_WithConcurrencyLimiter_FullCycle 验证完整的 acquire-release 生命周期:
// 5 个 withCancel pair (5 trial 启动), 然后全部 cancel, current 必须 = 0。
func TestService_WithConcurrencyLimiter_FullCycle(t *testing.T) {
	t.Parallel()

	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(5),
	}

	// 1. 5 个 withCancel 都成功 (模拟 5 个 trial 启动, 每个 trial 调 1 次 withCancel)
	cancels := make([]context.CancelFunc, 0, 5)
	for i := 0; i < 5; i++ {
		_, cancel, err := svc.withCancel(context.Background(), "session-"+string(rune('a'+i)))
		if err != nil {
			t.Fatalf("withCancel #%d: expected nil error, got %v", i, err)
		}
		cancels = append(cancels, cancel)
	}

	// 2. 第 6 个 withCancel 应该失败 (slot 已满)
	_, _, err := svc.withCancel(context.Background(), "session-fail")
	if err != ErrConcurrencyLimitExceeded {
		t.Errorf("6th withCancel: expected ErrConcurrencyLimitExceeded, got %v", err)
	}

	// 3. 全部 cancel → wrappedCancel 自动 Release → current 必须 = 0
	for _, cancel := range cancels {
		cancel()
	}
	cur, _, _ := svc.ConcurrencyStats()
	if cur != 0 {
		t.Errorf("after full cycle: expected current=0, got %d (slot leak!)", cur)
	}

	// 4. 现在 slot 都释放了, 第 7 个 withCancel 应该成功
	_, cancel, err := svc.withCancel(context.Background(), "session-after-release")
	if err != nil {
		t.Errorf("withCancel after all release: expected nil error, got %v", err)
	}
	cancel()
	cur, _, _ = svc.ConcurrencyStats()
	if cur != 0 {
		t.Errorf("final: expected current=0, got %d", cur)
	}
}

// TestService_ConcurrencyLimiter_DefaultMax 验证未传 max 时, ConcurrencyLimiter
// 用默认 5 (与 ADR 0027 §决策一致)。
func TestService_ConcurrencyLimiter_DefaultMax(t *testing.T) {
	t.Parallel()

	// NewConcurrencyLimiter(0) 应使用默认 5
	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(0),
	}

	_, max, _ := svc.ConcurrencyStats()
	if max != 5 {
		t.Errorf("expected default max=5, got %d", max)
	}
}
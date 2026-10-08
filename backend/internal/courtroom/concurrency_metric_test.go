package courtroom

// v0.10.20 (ADR 0027 §PR 3) L0 ConcurrencyLimiter metric 接入测试。
//
// 验证:
//   1. recordConcurrencyMetric 失败时: MetricGlobalConcurrencyRejectedTotal +1
//   2. recordConcurrencyMetric 成功时: MetricGlobalConcurrencyCurrent gauge 反映正确
//   3. recordConcurrencyMetric 成功时: MetricGlobalConcurrencyMax gauge 反映 max
//   4. nil metrics / nil limiter 时: 不 panic, 静默跳过
//   5. withCancel 链路: 失败时拒绝计数 +1, 成功时 current gauge 反映

import (
	"context"
	"testing"

	"github.com/decisioncourt/backend/internal/observability"
)

// TestService_RecordConcurrencyMetric_Rejected 验证: 拒绝时
// MetricGlobalConcurrencyRejectedTotal counter +1。
func TestService_RecordConcurrencyMetric_Rejected(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(2),
		metrics:            metrics,
	}

	// 1. 第 1 次 withCancel 成功 (acquired=true), 不应该增加 rejected counter
	_, cancel1, err := svc.withCancel(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("1st withCancel: unexpected error %v", err)
	}

	snap := metrics.Snapshot()
	rejectedBefore := counterValue(t, snap, observability.MetricGlobalConcurrencyRejectedTotal)
	if rejectedBefore != 0 {
		t.Errorf("after 1st success: rejected counter should be 0, got %f", rejectedBefore)
	}

	// 2. 第 2 次成功
	_, cancel2, err := svc.withCancel(context.Background(), "session-2")
	if err != nil {
		t.Fatalf("2nd withCancel: unexpected error %v", err)
	}

	// 3. 第 3 次应该失败 (slot 已满)
	_, _, err = svc.withCancel(context.Background(), "session-3")
	if err != ErrConcurrencyLimitExceeded {
		t.Fatalf("3rd withCancel: expected ErrConcurrencyLimitExceeded, got %v", err)
	}

	// 4. 验证 rejected counter = 1
	snap = metrics.Snapshot()
	rejectedAfter := counterValue(t, snap, observability.MetricGlobalConcurrencyRejectedTotal)
	if rejectedAfter != 1 {
		t.Errorf("after 1 rejection: expected rejected counter = 1, got %f", rejectedAfter)
	}

	// 5. 释放 1 个 slot, 再 acquire 应该成功 (slot 被释放了)
	cancel1()
	_, cancel3, err := svc.withCancel(context.Background(), "session-4")
	if err != nil {
		t.Fatalf("4th withCancel after release: expected success, got error %v", err)
	}

	// rejected counter 应该仍是 1 (这次成功, 不增加 rejected)
	snap = metrics.Snapshot()
	rejectedAfterRelease := counterValue(t, snap, observability.MetricGlobalConcurrencyRejectedTotal)
	if rejectedAfterRelease != 1 {
		t.Errorf("after acquire-success post-release: rejected should stay at 1, got %f", rejectedAfterRelease)
	}

	// 6. 再 acquire 1 次 → 再次失败 → rejected = 2
	_, _, err = svc.withCancel(context.Background(), "session-5")
	if err != ErrConcurrencyLimitExceeded {
		t.Fatalf("5th withCancel: expected ErrConcurrencyLimitExceeded, got %v", err)
	}
	snap = metrics.Snapshot()
	rejectedFinal := counterValue(t, snap, observability.MetricGlobalConcurrencyRejectedTotal)
	if rejectedFinal != 2 {
		t.Errorf("after 2 rejections total: expected rejected counter = 2, got %f", rejectedFinal)
	}

	// 7. 清理
	cancel2()
	cancel3()
}

// TestService_RecordConcurrencyMetric_Gauges 验证: 成功时 current / max gauge 反映正确。
func TestService_RecordConcurrencyMetric_Gauges(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(3), // max=3
		metrics:            metrics,
	}

	// 1. 第 1 次 withCancel 成功 → current=1, max=3
	_, cancel1, err := svc.withCancel(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("1st withCancel: unexpected error %v", err)
	}
	snap := metrics.Snapshot()

	if v := gaugeValue(t, snap, observability.MetricGlobalConcurrencyCurrent); v != 1 {
		t.Errorf("after 1 acquire: expected current gauge = 1, got %f", v)
	}
	if v := gaugeValue(t, snap, observability.MetricGlobalConcurrencyMax); v != 3 {
		t.Errorf("expected max gauge = 3, got %f", v)
	}

	// 2. 第 2 次成功 → current=2
	_, cancel2, err := svc.withCancel(context.Background(), "session-2")
	if err != nil {
		t.Fatalf("2nd withCancel: unexpected error %v", err)
	}
	snap = metrics.Snapshot()
	if v := gaugeValue(t, snap, observability.MetricGlobalConcurrencyCurrent); v != 2 {
		t.Errorf("after 2 acquires: expected current gauge = 2, got %f", v)
	}

	// 3. cancel1 → release 1 slot → current gauge 应回落为 1。
	//    v2.13 (deferred D20): wrappedCancel 释放 slot 后也刷新 gauge
	//    (此前只在 acquire 分支写 → gauge 停在最后一次 acquire 的值)。
	cancel1()
	snap = metrics.Snapshot()
	if v := gaugeValue(t, snap, observability.MetricGlobalConcurrencyCurrent); v != 1 {
		t.Errorf("after release: expected current gauge to drop to 1, got %f", v)
	}

	// 4. 第 3 次成功 → cur=2 (cancel1 已释放 1 slot)
	_, cancel3, err := svc.withCancel(context.Background(), "session-3")
	if err != nil {
		t.Fatalf("3rd withCancel: unexpected error %v", err)
	}
	snap = metrics.Snapshot()
	if v := gaugeValue(t, snap, observability.MetricGlobalConcurrencyCurrent); v != 2 {
		t.Errorf("after release+acquire: expected current gauge = 2, got %f", v)
	}

	// 清理
	cancel2()
	cancel3()
}

// TestService_RecordConcurrencyMetric_NilSafe 验证: nil metrics / nil limiter 不 panic。
func TestService_RecordConcurrencyMetric_NilSafe(t *testing.T) {
	t.Parallel()

	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(2),
		metrics:            nil, // 测试 nil metrics
	}

	// 应该不 panic, 静默跳过
	_, cancel, err := svc.withCancel(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("withCancel with nil metrics: unexpected error %v", err)
	}
	cancel()

	// nil limiter 也不 panic
	svc2 := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: nil,
		metrics:            observability.NewMetrics(),
	}
	_, cancel2, err := svc2.withCancel(context.Background(), "session-2")
	if err != nil {
		t.Fatalf("withCancel with nil limiter: unexpected error %v", err)
	}
	cancel2()
}

// TestService_WrappedCancel_ReleasesOnce 验证 v2.13 (deferred D20) 的
// sync.Once 守卫: 同一个 cancel 被重复调用(production 里 direct_verdict
// 的 cancelCall + 调用方 defer cancel 可能各调一次)时, slot 只释放一次。
//
// 反例可观测点: 若重复调用会重复 Release, 会把"后来 acquire 到的 slot"
// 误放 → 第三个 trial 拿到本不该有的 slot。
func TestService_WrappedCancel_ReleasesOnce(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	// cap=1: slot 满时任何 acquire 都会失败, 便于断言"有没有被误放"。
	svc := &Service{
		activeCalls:        make(map[string]context.CancelFunc),
		concurrencyLimiter: NewConcurrencyLimiter(1),
		metrics:            metrics,
	}

	// 1. 占满唯一的 slot。
	_, cancel1, err := svc.withCancel(context.Background(), "s1")
	if err != nil {
		t.Fatalf("1st withCancel: unexpected error %v", err)
	}

	// 2. 正常释放 → slot 空出。
	cancel1()

	// 3. 重新占满 slot (s2)。
	_, cancel2, err := svc.withCancel(context.Background(), "s2")
	if err != nil {
		t.Fatalf("2nd withCancel after release: unexpected error %v", err)
	}

	// 4. 过期/重复的 cancel1() 必须 no-op —— 不能把 s2 的 slot 误放。
	cancel1()

	// 5. slot 仍被 s2 占着 → s3 必须被拒。
	if _, _, err := svc.withCancel(context.Background(), "s3"); err != ErrConcurrencyLimitExceeded {
		t.Fatalf("after duplicate cancel, expected ErrConcurrencyLimitExceeded, got %v", err)
	}

	// 6. 释放 s2 后 gauge 应回落为 0。
	cancel2()
	snap := metrics.Snapshot()
	if v := gaugeValue(t, snap, observability.MetricGlobalConcurrencyCurrent); v != 0 {
		t.Errorf("after all released: expected current gauge = 0, got %f", v)
	}
}

// counterValue 工具函数: 从 MetricSnapshot 拿 counter 值（找不到返回 0）。
// 注意: snap.Counters 是 map[metric_name]→[]samples, 外层 key 就是 metric name。
func counterValue(t *testing.T, snap observability.MetricSnapshot, name string) float64 {
	t.Helper()
	samples, ok := snap.Counters[name]
	if !ok || len(samples) == 0 {
		return 0
	}
	return samples[0].Value
}

// gaugeValue 工具函数: 从 MetricSnapshot 拿 gauge 值（找不到返回 0）。
func gaugeValue(t *testing.T, snap observability.MetricSnapshot, name string) float64 {
	t.Helper()
	samples, ok := snap.Gauges[name]
	if !ok || len(samples) == 0 {
		return 0
	}
	return samples[0].Value
}
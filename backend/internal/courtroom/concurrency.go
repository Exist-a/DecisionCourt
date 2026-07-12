package courtroom

// v0.10.20 (ADR 0027 §决策 3) L0 全局并发 trial 信号量。
//
// 业务背景:
//   - L3/L2/L1 已经分别按 IP / User / Session 维度限流
//   - **L0 全局并发 trial 上限缺失** → 多用户同时跑 trial 时,ECS 2C2G
//     直接 OOM (2026-07-12 已观察到 1 次: 5 个 trial 并发把 2GB 内存撑满)
//
// 解决方案: buffered channel 信号量作为 "全局活跃 trial slot 池"
//   - max = 5 (按用户 2026-07-12 确认)
//   - TryAcquire 非阻塞;失败返 ErrConcurrencyLimitExceeded
//   - Release 释放 slot
//
// 与现有 activeCalls map 的关系 (复用设计):
//   - service.go 现有 activeCalls map[sessionUUID]context.CancelFunc,
//     表达"trial 正在跑 (有 active goroutine)"
//   - L0 信号量**与 activeCalls 配合**: withCancel 前检查信号量
//     (slot 数 = cap(sem) - len(sem)); clearCancel 后 activeCalls 减少
//   - **不重复维护计数** —— 信号量天然反映"当前活跃 trial 数"
//
// 接入位置:
//   - service.go withCancel: 在 activeCalls 写入前检查信号量
//   - handler.go StartTrial: TryAcquire 失败时返 429 + code=1426
//
// 简历叙述:
//   "设计全局并发 trial 信号量 (buffered channel, max=5) 作为
//    activeCalls map 的 size limiter, 防阿里云 2C2G ECS 在多用户同时
//    跑 trial 时 OOM。TryAcquire 非阻塞,Release 由 clearCancel 自动触发。"

import (
	"errors"
	"log/slog"
)

// ErrConcurrencyLimitExceeded 是 L0 信号量拒绝时的标准错误。
// handler 层捕获后返回 HTTP 429 + code=1426 + user_facing_error envelope。
//
// 用户看到的提示: "系统当前庭审数已达上限,请稍后再试"
var ErrConcurrencyLimitExceeded = errors.New("concurrent trial limit reached")

// ConcurrencyLimiter 是 L0 全局并发 trial 信号量。
//
// 设计要点:
//   - buffered channel 容量 = maxConcurrent
//   - TryAcquire 非阻塞 (select default),绝不阻塞 trial 启动
//   - Release 由调用方手动调,或在 service.withCancel 的 wrappedCancel 内自动调
//   - 线程安全 (Go channel 本身是 goroutine-safe)
//
// 算法本质: 信号量 (semaphore),而非限流 (rate limit):
//   - 限流 = 频率 (每秒最多多少次)
//   - 信号量 = 并发 (同时最多多少个)
//   - DecisionCourt 的 trial 是长任务 (5-10 分钟), 用信号量不用限流
//
// 为什么用 buffered channel 而不是 atomic.Int64 + mutex:
//   - buffered channel 是 Go 习惯写法
//   - select default 自然表达 "非阻塞尝试" 语义
//   - atomic.Int64 + mutex 需要手动管理 "拿 slot / 释放 slot" 两步
type ConcurrencyLimiter struct {
	sem chan struct{} // 容量 = maxConcurrent
	max int
}

// NewConcurrencyLimiter 构造信号量。max <= 0 时使用默认值 5。
//
// 默认值 5 来源 (ADR 0027 §6.1):
//   - 阿里云 ECS 2C2G 实测安全值 (5 trial × ~400MB/trial = 2GB,接近 OOM 边界)
//   - 经用户 2026-07-12 确认
func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		max = 5
	}
	slog.Info("concurrency limiter initialized", "max_concurrent_trials", max)
	return &ConcurrencyLimiter{
		sem: make(chan struct{}, max),
		max: max,
	}
}

// TryAcquire 非阻塞尝试拿 slot。
//
//   - 成功: 返回 true,后续必须 Release
//   - 失败 (信号量已满): 返回 false,调用方应返回 ErrConcurrencyLimitExceeded
//
// 非阻塞语义: trial 启动失败应该立刻告诉用户,不能等 (用户期待实时反馈)。
func (l *ConcurrencyLimiter) TryAcquire() bool {
	if l == nil {
		return true // nil limiter = 不限流,放行
	}
	select {
	case l.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release 释放 slot。TryAcquire 成功后必须配对调用,否则 slot 永久泄漏。
//
// 实现: select default 非阻塞接收。
//   - channel 非空 → 正常接收,释放 slot
//   - channel 空 (Release 比 TryAcquire 多, 或没 TryAcquire 就 Release) →
//     log warning + 不阻塞返回
//
// 为什么不是 `<-l.sem` 直接接收 (会阻塞):
//   - 生产环境宽容优先: 一个 bug 不应该把进程卡死 30s 后超时
//   - 阻塞会让 service 整个 goroutine 卡住, 影响其他 trial
//   - log warning 已经足够暴露问题 (监控告警会捕获)
//
// ⚠️ defer Release 必须: 防止 panic 后 slot 不释放。
// ⚠️ 不要在没有 TryAcquire 的情况下 Release: 会 log warning (slot 计数不会变负)。
func (l *ConcurrencyLimiter) Release() {
	if l == nil {
		return
	}
	select {
	case <-l.sem:
		// 正常释放 slot
	default:
		// Release 比 TryAcquire 多 (典型 bug: defer Release 但前面 early return,
		// 或 Release 在多个分支都调用了)。不阻塞,只 log warning。
		slog.Warn("ConcurrencyLimiter.Release without matching TryAcquire")
	}
}

// Stats 返回当前状态 (给 observability 用):
//   - current: 已占用 slot 数
//   - max: 最大 slot 数
//   - available: 剩余可用 slot 数
//
// 注意: len() 在 buffered channel 上是 O(1),无锁。
func (l *ConcurrencyLimiter) Stats() (current, max, available int) {
	if l == nil {
		return 0, 0, 0
	}
	cur := len(l.sem)
	return cur, l.max, l.max - cur
}

// Max 返回最大 slot 数 (配置)。
func (l *ConcurrencyLimiter) Max() int {
	if l == nil {
		return 0
	}
	return l.max
}
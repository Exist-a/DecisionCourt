// Package ratelimit 实现用户级 Trial 限流（ADR 0014）。
//
// 设计目标:
//   - 防止恶意脚本刷 StartTrial,导致 LLM 配额被烧光（P0 风险）
//   - 每用户每天（UTC）最多 N 次 trial,默认 N=5
//   - 单机部署用 sync.Map + 滑动窗口;DAU > 5000 触发 Redis 切换
//
// 关键契约:
//   - 滑动窗口:内存中保留 timestamps,allow 时清掉 window 外的
//   - UTC 日界:每日 00:00:00 重置,简单可预测
//   - 后台清理:每 1 小时 GC 过期 entry,防内存膨胀
//   - 接口预留:RateLimiter interface,DAU 高了切 Redis 实现即可
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// RateLimiter 是限流器抽象。当前只有内存实现,DAU > 5000 时可加 Redis 实现
// (用 INCR + EXPIRE 滑动窗口),Handler 代码不需要改。
type RateLimiter interface {
	// Allow 检查 key(用户 ID)是否允许再发一次 trial。
	//   - allowed=true:允许调用,记录本次 + 1
	//   - allowed=false:超限,retryAfter 告诉调用方还要等多久
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)
	// Used 返回 key 当前已用次数(调试用)
	Used(key string) int
}

// MemoryRateLimiter 是 sync.Map + 滑动窗口实现。
type MemoryRateLimiter struct {
	mu      sync.RWMutex
	limits  map[string]*userCounter // userID → 计数器
	limit   int                     // 窗口内最大次数
	window  time.Duration            // 窗口长度
	maxAge  time.Duration            // 内存保留时长（默认 7 天）
	stopGC  chan struct{}            // 停止后台 GC
	gcEvery time.Duration            // GC 间隔（默认 1 小时）
}

// userCounter 是单个用户的请求时间戳切片（按时间升序）。
type userCounter struct {
	timestamps []int64 // unix nano,按时间升序
}

// NewMemoryRateLimiter 构造限流器。
//   - limit:窗口内最大次数,0 → 默认 5
//   - window:窗口长度,0 → 默认 24 小时
func NewMemoryRateLimiter(limit int, window time.Duration) *MemoryRateLimiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	r := &MemoryRateLimiter{
		limits:  make(map[string]*userCounter),
		limit:   limit,
		window:  window,
		maxAge:  7 * 24 * time.Hour,
		stopGC:  make(chan struct{}),
		gcEvery: 1 * time.Hour,
	}
	go r.gcLoop()
	return r
}

// Allow 实现 RateLimiter.Allow。
// 算法:
//  1. 找到 userCounter
//  2. 清掉 window 外的 timestamps
//  3. 若 len >= limit → 拒绝,返回 retryAfter = (oldest + window - now)
//  4. 否则追加当前时间戳,允许
func (r *MemoryRateLimiter) Allow(_ context.Context, key string) (bool, time.Duration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixNano()
	cutoff := now - r.window.Nanoseconds()

	counter, ok := r.limits[key]
	if !ok {
		counter = &userCounter{}
		r.limits[key] = counter
	}

	// 1. 清掉 window 外的
	i := 0
	for ; i < len(counter.timestamps); i++ {
		if counter.timestamps[i] > cutoff {
			break
		}
	}
	counter.timestamps = counter.timestamps[i:]

	// 2. 检查是否超限
	if len(counter.timestamps) >= r.limit {
		oldest := counter.timestamps[0]
		retryAfter := time.Duration(oldest+r.window.Nanoseconds()-now) * time.Nanosecond
		if retryAfter < 0 {
			retryAfter = 0
		}
		return false, retryAfter, nil
	}

	// 3. 追加本次
	counter.timestamps = append(counter.timestamps, now)
	return true, 0, nil
}

// Used 返回 key 已用次数。
func (r *MemoryRateLimiter) Used(key string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	counter, ok := r.limits[key]
	if !ok {
		return 0
	}
	return len(counter.timestamps)
}

// gcLoop 每小时清理一次过期 entry(全为空 timestamps 或全在 maxAge 外的)。
func (r *MemoryRateLimiter) gcLoop() {
	ticker := time.NewTicker(r.gcEvery)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopGC:
			return
		case <-ticker.C:
			r.gc()
		}
	}
}

// gc 清理过期 entry。
func (r *MemoryRateLimiter) gc() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UnixNano()
	maxAgeCutoff := now - r.maxAge.Nanoseconds()
	windowCutoff := now - r.window.Nanoseconds()

	for key, counter := range r.limits {
		// 清掉 maxAge 外的
		i := 0
		for ; i < len(counter.timestamps); i++ {
			if counter.timestamps[i] > maxAgeCutoff {
				break
			}
		}
		counter.timestamps = counter.timestamps[i:]

		// 空 → 删 entry
		if len(counter.timestamps) == 0 {
			delete(r.limits, key)
			continue
		}

		// 全部在 window 外 → 但还在 maxAge 内 → 等下次 GC
		_ = windowCutoff
	}
}

// Stop 停止后台 GC goroutine（测试清理用）。
func (r *MemoryRateLimiter) Stop() {
	close(r.stopGC)
}

// Stats 返回限流器状态(给 observability 接入用)。
func (r *MemoryRateLimiter) Stats() (trackedUsers, totalRequests int) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	trackedUsers = len(r.limits)
	for _, counter := range r.limits {
		totalRequests += len(counter.timestamps)
	}
	return
}
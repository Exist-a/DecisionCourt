package agent_gateway

import (
	"context"
	"sync"
	"time"
)

// 预算状态常量 — Compressor 与 Throttler 通过这些字符串判断行为。
const (
	StatusNormal    = "normal"
	StatusCompress  = "compress"
	StatusThrottle  = "throttle"
	StatusExhausted = "exhausted"
)

// TokenBudget 是 Agent Gateway 暴露给业务方的预算门面。
// 内部持有一个 BudgetStore（默认 MemStore，可切 Redis）以及一组 OnWarning hook。
// Gateway.Complete / StreamComplete 通过 Check() 读快照；通过 AddUsage() 累加。
type TokenBudget struct {
	store        BudgetStore
	warningFuncs []OnWarningFunc

	mu       sync.Mutex
	warningMux sync.RWMutex // protects warningFuncs
}

// NewTokenBudget 保持 v0.5+ API 不变；构造一个内存实现。这是 GatewayConfig
// 默认行为，向后兼容所有现有测试与调用方。
//
// limitPerSession <= 0 → 20000。
// compressRatio / throttleRatio 为 0 或越界时取 0.7 / 0.8。
func NewTokenBudget(limitPerSession int, compressRatio, throttleRatio float64) *TokenBudget {
	return NewTokenBudgetWithStore(NewMemStore(
		limitPerSession, 0, compressRatio, throttleRatio, 5*time.Minute,
	))
}

// NewTokenBudgetWithStore 接受任意 BudgetStore，便于接入 RedisStore 等。
func NewTokenBudgetWithStore(store BudgetStore) *TokenBudget {
	if store == nil {
		store = NewMemStore(20000, 0, 0.7, 0.8, 5*time.Minute)
	}
	return &TokenBudget{store: store}
}

// AddUsage 记录一次 LLM 调用的用量；调用 Gateway.Complete 内层返回后触发。
//   - 负数与 0 用量被忽略。
//   - 空 sessionUUID 忽略（保持现状；OOM 防护）
func (tb *TokenBudget) AddUsage(ctx context.Context, sessionUUID string, u BudgetUsage) {
	if sessionUUID == "" || u.TotalTokens() <= 0 {
		return
	}
	_ = tb.store.AddUsage(ctx, sessionUUID, u)
}

// RecordUsage 是 v0.5+ 老 API 的兼容入口；将 total 当作 input_tokens 累加，
// 这样旧的 TotalTokens 概念与新多维 input/output 划分兼容（不区分出入 token
// 量并不会破坏既有压缩/限流行为）。
//
// Deprecated: 新代码请直接用 AddUsage(ctx, sessionUUID, BudgetUsage{...})。
func (tb *TokenBudget) RecordUsage(ctx context.Context, sessionUUID string, tokens int) {
	if tokens <= 0 {
		return
	}
	tb.AddUsage(ctx, sessionUUID, BudgetUsage{InputTokens: tokens})
}

// Check 返回 BudgetSnapshot（多维 + sliding + warning level）。
//
// 副作用：若本次读到的 warning level 严格高于该 session 已发的最高 level，
// 会同步触发所有 OnWarningFunc。OnWarningFunc 自身抛出的 panic 不会被吞掉
// （保证不影响主流程的方式是各自实现捕获）。
func (tb *TokenBudget) Check(ctx context.Context, sessionUUID string) BudgetSnapshot {
	snap, err := tb.store.Check(ctx, sessionUUID)
	if err != nil || sessionUUID == "" {
		return snap
	}
	if snap.WarningLevel != "" {
		tb.maybeFireWarning(ctx, sessionUUID, snap)
	}
	return snap
}

// maybeFireWarning 比对 last-emitted vs current rank；只有升级（rank 升高）
// 才触发，避免每次 Check 都重复广播。降级（例如 sliding 滑出）不重置，
// 避免在阈值上下震荡时反复广播。
func (tb *TokenBudget) maybeFireWarning(ctx context.Context, sessionUUID string, snap BudgetSnapshot) {
	tb.warningMux.RLock()
	funcs := append([]OnWarningFunc(nil), tb.warningFuncs...)
	tb.warningMux.RUnlock()

	if len(funcs) == 0 {
		return
	}

	// MemStore 暴露 lastWarning via store 内部；此处通过对比两次 snap 简化：
	// 上层一般不会高频 Check 同一 session；MemStore 自身在 AddUsage 后
	// 也已经按相同规则控制重复触发。这里 fire-only-on-upgrade 由调用方
	// 通过 AddOnWarning 的位置保障（典型的位置是 main.go 装配一次性）。
	//
	// 为避免重复，MemStore 内部 record.LastWarningLevel 已经比 Check 前的
	// 值严格更高才返回 WarningLevel；因此下面的 fire 是 idempotent + safe。
	for _, fn := range funcs {
		fn(ctx, sessionUUID, snap.WarningLevel, snap)
	}
}

// AddOnWarning 注册一个阈值跨越 hook。可以多次调用注册多个；触发时按注册顺序
// 同步执行。
func (tb *TokenBudget) AddOnWarning(fn OnWarningFunc) {
	if fn == nil {
		return
	}
	tb.warningMux.Lock()
	tb.warningFuncs = append(tb.warningFuncs, fn)
	tb.warningMux.Unlock()
}

// Reset 清空某个 session 的所有计数（session-end 时调用）。
func (tb *TokenBudget) Reset(ctx context.Context, sessionUUID string) error {
	return tb.store.Reset(ctx, sessionUUID)
}

// CurrentUsage 兼容旧测试：返回 SessionUUID 对应的累计 input+output tokens。
// 等价于 snap.TotalTokens；当 session 不存在时返回 0。
func (tb *TokenBudget) CurrentUsage(sessionUUID string) int {
	if sessionUUID == "" {
		return 0
	}
	snap, err := tb.store.Check(context.Background(), sessionUUID)
	if err != nil {
		return 0
	}
	return snap.TotalTokens
}

// Close 释放底层 store 资源（Redis 连接等）；无外部依赖时为 noop。
func (tb *TokenBudget) Close() error {
	return tb.store.Close()
}

// limitPerSessionFor 是一个内部辅助：保留 NewTokenBudgetDefault 所需参数范围
// 检查，但不暴露给业务。客户端入口是 NewTokenBudget / NewTokenBudgetWithStore。
func limitPerSessionFor(limit int) int {
	if limit <= 0 {
		return 20000
	}
	return limit
}

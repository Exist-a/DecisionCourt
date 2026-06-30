package agent_gateway

import (
	"context"
	"sync"
	"time"
)

// 预算状态常量。
const (
	StatusNormal    = "normal"
	StatusCompress  = "compress"
	StatusThrottle  = "throttle"
	StatusExhausted = "exhausted"
)

// TokenBudget 按 session_uuid 维护每个庭审的 token 消耗。
// MVP 使用内存存储，庭审重启/服务重启后清零；这是已知限制，但足以
// 支持一次完整庭审内的预算与限流实验。
type TokenBudget struct {
	mu              sync.RWMutex
	sessions        map[string]*budgetState
	limitPerSession int
	compressRatio   float64
	throttleRatio   float64
}

type budgetState struct {
	used      int
	updatedAt time.Time
}

// BudgetSnapshot 是某次 Check 返回的预算快照。
type BudgetSnapshot struct {
	Used   int
	Total  int
	Ratio  float64
	Status string
}

// NewTokenBudget 构造 TokenBudget。
// limitPerSession <= 0 时默认 20000。
// compressRatio / throttleRatio 为 0 时默认 0.7 / 0.8。
func NewTokenBudget(limitPerSession int, compressRatio, throttleRatio float64) *TokenBudget {
	if limitPerSession <= 0 {
		limitPerSession = 20000
	}
	if compressRatio <= 0 || compressRatio >= 1 {
		compressRatio = 0.7
	}
	if throttleRatio <= 0 || throttleRatio >= 1 {
		throttleRatio = 0.8
	}
	return &TokenBudget{
		sessions:        make(map[string]*budgetState),
		limitPerSession: limitPerSession,
		compressRatio:   compressRatio,
		throttleRatio:   throttleRatio,
	}
}

// RecordUsage 累加某 session 的已用 token。ctx 保留给未来扩展（如从
// ctx 读取截止时间）。
func (tb *TokenBudget) RecordUsage(ctx context.Context, sessionUUID string, tokens int) {
	if sessionUUID == "" || tokens <= 0 {
		return
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	state := tb.sessions[sessionUUID]
	if state == nil {
		state = &budgetState{}
		tb.sessions[sessionUUID] = state
	}
	state.used += tokens
	state.updatedAt = time.Now().UTC()
}

// Check 返回某 session 的预算快照。新 session 返回 normal 零值。
func (tb *TokenBudget) Check(ctx context.Context, sessionUUID string) BudgetSnapshot {
	if sessionUUID == "" {
		return BudgetSnapshot{Total: tb.limitPerSession, Status: StatusNormal}
	}
	tb.mu.RLock()
	state := tb.sessions[sessionUUID]
	tb.mu.RUnlock()

	used := 0
	if state != nil {
		used = state.used
	}
	if used < 0 {
		used = 0
	}
	ratio := 0.0
	if tb.limitPerSession > 0 {
		ratio = float64(used) / float64(tb.limitPerSession)
	}
	status := StatusNormal
	switch {
	case ratio >= 1.0:
		status = StatusExhausted
	case ratio >= tb.throttleRatio:
		status = StatusThrottle
	case ratio >= tb.compressRatio:
		status = StatusCompress
	}
	return BudgetSnapshot{
		Used:   used,
		Total:  tb.limitPerSession,
		Ratio:  ratio,
		Status: status,
	}
}

// CurrentUsage 返回某 session 当前已用 token（辅助文件日志）。
func (tb *TokenBudget) CurrentUsage(sessionUUID string) int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if state := tb.sessions[sessionUUID]; state != nil {
		return state.used
	}
	return 0
}

package agent_gateway

import (
	"context"
	"errors"
	"time"
)

// ErrBudgetExhausted is returned by Gateway.Complete / StreamComplete
// when the session's TokenBudget is at 100% and the configured behaviour
// is "reject on exhaust". Business code can use errors.Is to detect it.
var ErrBudgetExhausted = errors.New("agent_gateway: token budget exhausted")

// BudgetStore is the abstraction over per-session token-budget counters.
// Implementations: MemStore (default, in-memory) and a future RedisStore.
//
// Lifecycle: one BudgetStore per Gateway instance, configured at boot via
// GatewayConfig. All methods are safe for concurrent use.
type BudgetStore interface {
	// AddUsage records the usage of one LLM call into the session.
	AddUsage(ctx context.Context, sessionUUID string, u BudgetUsage) error
	// Check returns the current snapshot for the session. Read-only.
	Check(ctx context.Context, sessionUUID string) (BudgetSnapshot, error)
	// Reset clears the session counters (called at session-end).
	Reset(ctx context.Context, sessionUUID string) error
	// Close releases resources (Redis pool etc.).
	Close() error
}

// BudgetUsage is the per-call payload recorded into the store.
// CostUSD is optional (0 if the model price is unknown).
type BudgetUsage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// TotalTokens is the convenience access for input + output.
func (u BudgetUsage) TotalTokens() int { return u.InputTokens + u.OutputTokens }

// BudgetSnapshot is the per-session read result. Returned by Check.
//
// It is the merged view of session totals, sliding-window totals and
// warning state. Status is the worst of total / sliding so that an
// instantaneous burst still surfaces compress/throttle quickly.
type BudgetSnapshot struct {
	SessionUUID string

	// Multi-dim totals since session start (or last Reset).
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	CostUSD      float64

	// Backward-compat aliases: existing CompressionInfo/ThrottleInfo code
	// reads bs.Used / bs.Total for "tokens used vs budget".
	//   Used  == TotalTokens accumulated.
	//   Total == limit (per-session token limit).
	Used  int
	Total int

	// Limits configured at construction.
	LimitTotalTokens int
	LimitCostUSD     float64

	// Sliding-window summary (last 5 min by default).
	SlidingTokens int
	SlidingWindow time.Duration

	// Ratio (0-1+, possibly > 1 when over-budget).
	Ratio float64
	// Status is StatusNormal / StatusCompress / StatusThrottle / StatusExhausted.
	Status string

	// WarningLevel is the last warning threshold crossed in this session.
	// "" if no warning has fired yet. See WarningLevel consts below.
	WarningLevel string
}

// Warning level constants used by OnWarningFunc hooks and BudgetSnapshot.WarningLevel.
const (
	WarningLevelNone      = ""
	WarningLevel70        = "warning_70"
	WarningLevel80        = "warning_80"
	WarningLevelExhausted = "exhausted_100"
)

// OnWarningFunc is the hook signature fired on threshold crossings.
//
// TokenBudget owns a list of these; any number can be registered with
// AddOnWarning. Each hook is called synchronously inside Check; callers
// should keep them short (log + WebSocket broadcast at most).
type OnWarningFunc func(ctx context.Context, sessionUUID string, level string, snap BudgetSnapshot)

// warningSeverity returns a rank: higher number means more severe.
//   0 = no warning
//   1 = warning_70
//   2 = warning_80
//   3 = exhausted_100
func warningSeverity(level string) int {
	switch level {
	case WarningLevel70:
		return 1
	case WarningLevel80:
		return 2
	case WarningLevelExhausted:
		return 3
	}
	return 0
}

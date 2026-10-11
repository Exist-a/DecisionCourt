package agent_gateway

import (
	"context"
	"sync"
	"time"
)

// MemStore 是 BudgetStore 的内存实现。所有数据存在于本进程，重启清零。
//
// 设计要点：
//   - 每个 session 独立持有 input_tokens / output_tokens / cost_usd + sliding 5min history
//   - 检查时按"total 与 sliding"二者取较严状态（更接近业内最差窗口原则）
//   - WarningLevel 只在等级**升级**时返回非空（fire-on-upgrade），避免每次 Check 都广播
//
// 不在本范围：Redis / 多实例共享；持久化；按 user/tenant 维度。
type MemStore struct {
	mu       sync.Mutex
	sessions map[string]*memSession

	// 全部为构造时固化，运行期不变
	limitTotal int
	limitCost  float64
	compressR  float64
	throttleR  float64
	slidingWin time.Duration
}

type memSession struct {
	// 累计
	inputTokens  int
	outputTokens int
	costUSD      float64

	// sliding 5min 历史：每次 AddUsage append 一条；Check 时 drop 过期
	history []memUsageEntry

	// 上次 Check 返回的 WarningLevel，用于"fire on upgrade"判定
	lastWarning string
}

type memUsageEntry struct {
	at        time.Time
	inTokens  int
	outTokens int
	costUSD   float64
}

// NewMemStore 构造内存版 BudgetStore。参数全部允许缺省（取本文件内常量）。
//
//   limitPerSession  <= 0 → DefaultBudgetPerSession
//   limitCostUSD      < 0 → 0（表示不限 USD）
//   compressRatio   默认 0.7
//   throttleRatio   默认 0.8
//   slidingWindow   <= 0 → 5 * time.Minute
func NewMemStore(limitPerSession int, limitCostUSD float64, compressRatio, throttleRatio float64, slidingWindow time.Duration) *MemStore {
	if limitPerSession <= 0 {
		limitPerSession = DefaultBudgetPerSession
	}
	if limitCostUSD < 0 {
		limitCostUSD = 0
	}
	if compressRatio <= 0 || compressRatio >= 1 {
		compressRatio = 0.7
	}
	if throttleRatio <= 0 || throttleRatio >= 1 {
		throttleRatio = 0.8
	}
	if slidingWindow <= 0 {
		slidingWindow = 5 * time.Minute
	}
	return &MemStore{
		sessions:   make(map[string]*memSession),
		limitTotal: limitPerSession,
		limitCost:  limitCostUSD,
		compressR:  compressRatio,
		throttleR:  throttleRatio,
		slidingWin: slidingWindow,
	}
}

// AddUsage 记录一次 LLM 调用的用量。
// 负数 / 0 用量与空 sessionUUID 都直接忽略（保持 v0.5+ 行为）。
func (m *MemStore) AddUsage(ctx context.Context, sessionUUID string, u BudgetUsage) error {
	if sessionUUID == "" || u.TotalTokens() <= 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[sessionUUID]
	if s == nil {
		s = &memSession{}
		m.sessions[sessionUUID] = s
	}
	s.inputTokens += u.InputTokens
	s.outputTokens += u.OutputTokens
	s.costUSD += u.CostUSD
	s.history = append(s.history, memUsageEntry{
		at:        time.Now().UTC(),
		inTokens:  u.InputTokens,
		outTokens: u.OutputTokens,
		costUSD:   u.CostUSD,
	})
	return nil
}

// Check 是核心：计算累积 + sliding 5min，按"较严"原则给 status。
// 同时计算 WarningLevel（fire-on-upgrade：只在升级时返回非空）。
func (m *MemStore) Check(ctx context.Context, sessionUUID string) (BudgetSnapshot, error) {
	snap := BudgetSnapshot{
		SessionUUID:      sessionUUID,
		LimitTotalTokens: m.limitTotal,
		LimitCostUSD:     m.limitCost,
		SlidingWindow:    m.slidingWin,
	}
	if sessionUUID == "" {
		// 旧 API 兼容：空 session 返回"normal"零值快照
		snap.Total = m.limitTotal
		snap.Status = StatusNormal
		return snap, nil
	}
	m.mu.Lock()
	s := m.sessions[sessionUUID]
	m.mu.Unlock()
	if s == nil {
		snap.Total = m.limitTotal
		snap.Status = StatusNormal
		return snap, nil
	}
	snap.InputTokens = s.inputTokens
	snap.OutputTokens = s.outputTokens
	snap.TotalTokens = s.inputTokens + s.outputTokens
	snap.CostUSD = s.costUSD
	snap.Used = snap.TotalTokens
	snap.Total = m.limitTotal

	// 过滤 sliding 5min；过期条目就地 discard（缓存复用，避免内存长期增长）
	now := time.Now().UTC()
	cutoff := now.Add(-m.slidingWin)
	sliding := 0
	kept := s.history[:0]
	for _, e := range s.history {
		if e.at.Before(cutoff) {
			continue
		}
		kept = append(kept, e)
		sliding += e.inTokens + e.outTokens
	}
	if len(kept) != len(s.history) {
		s.history = kept
	}
	snap.SlidingTokens = sliding

	// Ratio：取 total / sliding 中较严一档
	totalRatio := 0.0
	if m.limitTotal > 0 {
		totalRatio = float64(snap.TotalTokens) / float64(m.limitTotal)
	}
	slidingRatio := 0.0
	if m.limitTotal > 0 {
		slidingRatio = float64(sliding) / float64(m.limitTotal)
	}
	// sliding 不能超过 total（sliding 永远是 total 子集）
	if slidingRatio > totalRatio {
		slidingRatio = totalRatio
	}
	// 成本维度（2026-10-10 第 3 轮）：与 token 维度共用 70/80/100 阈值，取较严一档。
	// 只按累计口径（不做成本滑窗），与"成本是另一把同向的尺子"的决策一致。
	costRatio := 0.0
	if m.limitCost > 0 {
		costRatio = s.costUSD / m.limitCost
	}
	snap.Ratio = totalRatio
	if costRatio > snap.Ratio {
		snap.Ratio = costRatio
	}

	status, level := m.classify(totalRatio, slidingRatio, costRatio)
	snap.Status = status

	// WarningLevel fire-on-upgrade：比上次更严才返回新值
	if warningSeverity(level) > warningSeverity(s.lastWarning) {
		snap.WarningLevel = level
		// 升级时即写入 lastWarning（避免下一轮 Check 又因 level 没变而 fire）
		m.mu.Lock()
		s.lastWarning = level
		m.mu.Unlock()
	}
	return snap, nil
}

// classify 给定 total / sliding / cost 三个 ratio，按"较严"给出 status 与 warning level。
// 规则：
//   - 任一 ≥ 1.0          → exhausted_100
//   - 任一 ≥ throttleRatio → throttle  + warning_80
//   - 任一 ≥ compressRatio → compress  + warning_70
//   - 否则 normal
//
// costR 在 limitCost==0（未配成本上限）时恒为 0，等价于"成本维度不参与"。
//
// warning level 与 status 不完全 1-1：当 sliding 触发了 throttle 而 total 还在
// compress 时，status=throttle，level=warning_80（按"较严"原则）。
func (m *MemStore) classify(totalR, slidingR, costR float64) (status string, level string) {
	r := totalR
	if slidingR > r {
		r = slidingR
	}
	if costR > r {
		r = costR
	}
	switch {
	case r >= 1.0:
		return StatusExhausted, WarningLevelExhausted
	case r >= m.throttleR:
		return StatusThrottle, WarningLevel80
	case r >= m.compressR:
		return StatusCompress, WarningLevel70
	default:
		return StatusNormal, WarningLevelNone
	}
}

// MaxRatio 返回所有活跃 session 中"累计口径"水位比的最高值（含成本口径，取较严）。
//
// 用途（2026-10-10 第 3 轮，deferred R19）：让 /metrics 上能看见"预算快烧完"，
// 而不是只能登机器翻文件日志。刻意**不加 session_uuid label**（会话数无上界，会
// 打爆指标基数），所以取"最高水位"这一个有界标量。
//
// 与 Check 的差别：这里不清理 sliding 历史、不触发 warning，只做只读聚合。
func (m *MemStore) MaxRatio() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	max := 0.0
	for _, s := range m.sessions {
		r := 0.0
		if m.limitTotal > 0 {
			r = float64(s.inputTokens+s.outputTokens) / float64(m.limitTotal)
		}
		if m.limitCost > 0 {
			if cr := s.costUSD / m.limitCost; cr > r {
				r = cr
			}
		}
		if r > max {
			max = r
		}
	}
	return max
}

// UsedTokens 返回所有活跃 session 的累计 token 总量（供 counter 指标）。
func (m *MemStore) UsedTokens() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, s := range m.sessions {
		total += s.inputTokens + s.outputTokens
	}
	return total
}

// Reset 清空 session；warning level 同步复位。
func (m *MemStore) Reset(ctx context.Context, sessionUUID string) error {
	if sessionUUID == "" {
		return nil
	}
	m.mu.Lock()
	delete(m.sessions, sessionUUID)
	m.mu.Unlock()
	return nil
}

// Close 是 noop（MemStore 无外部资源）。
func (m *MemStore) Close() error { return nil }

package trace

import (
	"container/list"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store 是 Trace 存储的抽象接口,handler 层只依赖接口 (便于测试 mock)。
//
// 设计要点:
//   - ListBySession(sessionID, date): 拉某 session 在某日期的所有 traces
//   - GetTraceByID(sessionID, traceID): 拉单个 trace 详情 (含 tree)
//   - date 格式: "2006-01-02" (与 agent_gateway.FileLogger dateFormat 一致)
//   - nil receiver 调用应 panic (接口保证非 nil)
//
// 接口分层理由:
//   - 生产: FileTraceStore (从 logs/agent_gateway_YYYY-MM-DD.log 读)
//   - 测试: InMemoryTraceStore (直接传 SessionTrace,无文件 IO)
//   - handler 层不关心存储细节,只调接口
type Store interface {
	// ListBySession 返回某 session 在某 date 的所有 Trace (按 trace_id 分组)。
	//
	// date 空字符串 = 查今天,适合实时 (trial 进行中) 查询。
	// date 非空 = 查历史某天,适合 verdict 页回放。
	//
	// 返回 []*Trace (空数组而非 nil 表示"无数据"),date 格式错返回 error。
	ListBySession(sessionID, date string) ([]*Trace, error)

	// GetTraceByID 返回单个 trace 的完整视图 (含 tree)。
	//
	// traceID 不存在时返回 (nil, nil) — 不是 error,前端按"无数据"渲染。
	// 跨 session 查 traceID (绕过 owner check) 是设计妥协,但生产用 sessionUUID 参数做二次过滤。
	GetTraceByID(sessionID, traceID string) (*Trace, error)
}

// InMemoryTraceStore 是 Store 接口的内存实现,供测试 + 单 trial 实时模式用。
//
// 数据来源:
//   - 测试: 直接构造 SessionTrace 注入
//   - 生产: 可以从 FileTraceStore 解析后转储 (但当前生产用 FileTraceStore 读文件)
//
// 并发模型:
//   - 读多写少 (Write 仅测试场景),用 RWMutex
//   - session → SessionTrace 映射
type InMemoryTraceStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionTrace // key = SessionID
}

// NewInMemoryTraceStore 构造空 store。
func NewInMemoryTraceStore() *InMemoryTraceStore {
	return &InMemoryTraceStore{
		sessions: make(map[string]*SessionTrace),
	}
}

// Put 注入 SessionTrace (测试用)。
//
// sessionID 空 / st 为 nil → panic (明显的调用错误,不让静默失败)。
func (s *InMemoryTraceStore) Put(sessionID string, st *SessionTrace) {
	if sessionID == "" {
		panic("trace.InMemoryTraceStore.Put: sessionID is empty")
	}
	if st == nil {
		panic("trace.InMemoryTraceStore.Put: SessionTrace is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = st
}

// ListBySession 实现 Store 接口。
func (s *InMemoryTraceStore) ListBySession(sessionID, _ string) ([]*Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, ok := s.sessions[sessionID]
	if !ok {
		return []*Trace{}, nil
	}
	return Aggregate(st, nil), nil
}

// GetTraceByID 实现 Store 接口。
func (s *InMemoryTraceStore) GetTraceByID(sessionID, traceID string) (*Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	traces := Aggregate(st, []string{traceID})
	if len(traces) == 0 {
		return nil, nil
	}
	return traces[0], nil
}

// FileTraceStore 是 Store 接口的文件实现,生产场景用。
//
// 行为:
//   - 每次 ListBySession / GetTraceByID 都读 1 次文件
//   - 解析结果进 LRU 缓存 (默认 100 entries),后续同 session 查询走缓存
//   - date 空 → 用今天 (time.Now().Local().Format("2006-01-02"))
//
// 性能:
//   - 单文件解析 ~50KB / 100 runs,实测 < 5ms (Go JSON Lines 解析 + map 聚合)
//   - LRU 100 entries ≈ 5MB 内存,2C2G ECS 够用
//   - 缓存 key = sessionID + ":" + date,跨 session 不互相污染
//
// 为什么不直接用 agent_gateway FileLogger 的运行时数据?
//   - FileLogger 是 hot path 上的"追加写",加同步读会拖慢 LLM 调用
//   - 读端聚合由本 store 在 REST 请求时懒加载,符合"读不影响写"原则
type FileTraceStore struct {
	logDir string

	// LRU 缓存 (sessionID:date → *SessionTrace)
	mu       sync.Mutex
	cache    *lruCache
	dateNow  func() time.Time // 可注入便于测试 (test 用固定时间)
}

// NewFileTraceStore 构造 FileTraceStore。
//
// logDir 与 agent_gateway.GatewayConfig.LogDir 一致 (默认 "logs")。
// maxCacheEntries 默认 100,可通过 SetMaxCache 调整。
func NewFileTraceStore(logDir string) *FileTraceStore {
	return &FileTraceStore{
		logDir:  logDir,
		cache:   newLRUCache(100),
		dateNow: func() time.Time { return time.Now().Local() },
	}
}

// SetMaxCache 调整 LRU 缓存大小 (测试场景用,生产 100 已够)。
func (s *FileTraceStore) SetMaxCache(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = newLRUCache(n)
}

// logFilePath 拼 agent_gateway 日志文件路径。
//
// 文件名规则 (与 agent_gateway.FileLogger 写入路径对齐):
//   - "logs/agent_gateway_YYYY-MM-DD.log"
func (s *FileTraceStore) logFilePath(date string) string {
	return filepath.Join(s.logDir, "agent_gateway_"+date+".log")
}

// parseForSession 读文件 + 解析 + 过滤 sessionID。
//
// 设计要点:
//   - 缓存以 (sessionID, date) 为 key,但实际缓存整个 SessionTrace 文件 (跨 session 复用解析结果)
//   - LRU key = "file:" + date (文件级缓存),过滤 sessionID 在内存里做
//   - 这样多 session 共一天文件时,只解析 1 次
func (s *FileTraceStore) parseForSession(sessionID, date string) (*SessionTrace, error) {
	if date == "" {
		date = s.dateNow().Format("2006-01-02")
	}

	// 文件级缓存 (sessionID 空时也走缓存,通用复用)
	cacheKey := "file:" + date
	s.mu.Lock()
	if cached, ok := s.cache.get(cacheKey); ok {
		s.mu.Unlock()
		return s.filterSession(cached, sessionID), nil
	}
	s.mu.Unlock()

	// 缓存未命中,读文件
	path := s.logFilePath(date)
	st, _, err := ParseFile(path)
	if err != nil {
		// 文件不存在 (当天还没生成) → 返空,不算错
		if errors.Is(err, os.ErrNotExist) {
			return &SessionTrace{SessionID: sessionID, Traces: make(map[string][]Run)}, nil
		}
		return nil, fmt.Errorf("trace.FileTraceStore: parse %s: %w", path, err)
	}

	// 写缓存
	s.mu.Lock()
	s.cache.put(cacheKey, st)
	s.mu.Unlock()

	return s.filterSession(st, sessionID), nil
}

// filterSession 从完整 SessionTrace 过滤出指定 session 的 runs。
//
// 日志文件可能含多 session 数据 (虽然概率低 — agent_gateway 单 session 通常跑 1 天),
// 这里按 SessionID 过滤保证返回数据是请求者 owner 的 session。
func (s *FileTraceStore) filterSession(full *SessionTrace, sessionID string) *SessionTrace {
	if full == nil || sessionID == "" || full.SessionID == sessionID {
		return full
	}
	filtered := &SessionTrace{
		SessionID: sessionID,
		Traces:    make(map[string][]Run),
	}
	for traceID, runs := range full.Traces {
		for _, r := range runs {
			if r.SessionID == sessionID {
				filtered.Traces[traceID] = append(filtered.Traces[traceID], r)
			}
		}
	}
	return filtered
}

// ListBySession 实现 Store 接口。
func (s *FileTraceStore) ListBySession(sessionID, date string) ([]*Trace, error) {
	st, err := s.parseForSession(sessionID, date)
	if err != nil {
		return nil, err
	}
	return Aggregate(st, nil), nil
}

// GetTraceByID 实现 Store 接口。
func (s *FileTraceStore) GetTraceByID(sessionID, traceID string) (*Trace, error) {
	st, err := s.parseForSession(sessionID, "")
	if err != nil {
		return nil, err
	}
	traces := Aggregate(st, []string{traceID})
	if len(traces) == 0 {
		return nil, nil
	}
	return traces[0], nil
}

// lruCache 是简单的 LRU 缓存 (container/list + map),不复用 agent_gateway.ResponseCache
// 因为后者 CacheKey 是 SHA256 hash,本场景需要 string key + 任意 value。
type lruCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List               // 双向链表,front=最新,back=最旧
	items    map[string]*list.Element // key → list element
}

type lruEntry struct {
	key   string
	value *SessionTrace
}

func newLRUCache(capacity int) *lruCache {
	if capacity <= 0 {
		capacity = 1
	}
	return &lruCache{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element, capacity),
	}
}

func (c *lruCache) get(key string) (*SessionTrace, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(elem)
	return elem.Value.(*lruEntry).value, true
}

func (c *lruCache) put(key string, value *SessionTrace) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.ll.MoveToFront(elem)
		elem.Value.(*lruEntry).value = value
		return
	}

	elem := c.ll.PushFront(&lruEntry{key: key, value: value})
	c.items[key] = elem

	// 超容淘汰最旧
	if c.ll.Len() > c.capacity {
		oldest := c.ll.Back()
		if oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEntry).key)
		}
	}
}

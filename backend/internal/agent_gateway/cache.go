package agent_gateway

// v0.9 (ADR 0013 §决策 2): Response Cache。
//
// 同一 trial 内多个 agent 看同一份 evidence 时,prompt 高度重复(只差
// role = prosecutor/defender),调 LLM 是浪费。本文件实现 in-memory LRU +
// TTL 缓存,key 由 model + system_prompt hash + messages hash + temperature
// 构成,触发场景:
//
//   - trial 同一阶段多 agent 看同一份 evidence → 命中率 30-40%
//   - 同 trial 不同 round 复用历史 context → 命中率 10-20%
//   - 跨 trial(不同 session)不命中(因为 cache key 不含 session 字段,
//     但每个 entry 记录 sessionID 用于 EvictSession 主动清理)
//
// 设计决策(详见 ADR 0013):
//   - 不引 Redis:单机 sync.Map + LRU 已够,DAU > 5000 trial/天才考虑
//   - 不缓存 MaxTokens:同 prompt 不同 max_tokens 共享 cache(LLM 实际
//     输出不受 max_tokens 影响,调用方自己截断)
//   - TTL 5min:trial 不会跨 5min 复用同一 prompt(超过就当新思路)
//   - LRU 10000 entries:~200KB / entry,总 ~2GB,2C2G ECS 够用
//   - EvictSession:trial 结束时主动清空该 session 的 cache,防止内存膨胀

import (
	"container/list"
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// CachedResponse 缓存的 LLM 响应。
type CachedResponse struct {
	Content string
	Usage   llm.Usage
	// CachedAt 用于调试和埋点,不是 TTL 的依据(TTL 用 expiresAt)。
	CachedAt time.Time
}

// CacheKey 缓存键,model + sys_hash + msg_hash + temperature。
// 注意:**不含 max_tokens** —— 同 prompt 不同 max_tokens 应共享缓存,
// LLM 输出由 prompt 决定,跟 max_tokens 无关,调用方自己截断。
type CacheKey struct {
	Model       string
	SysHash     [32]byte
	MsgHash     [32]byte
	Temperature float32 // 跟 llm.CompletionOptions.Temperature 类型一致
}

// MakeCacheKey 从 LLM 调用参数计算 cache key。
// systemPrompt 用 SHA256 哈希(避免在 key 里存大字符串),messages 同理。
func MakeCacheKey(model, systemPrompt string, messages []llm.Message, temperature float32) CacheKey {
	return CacheKey{
		Model:       model,
		SysHash:     hashString(systemPrompt),
		MsgHash:     hashMessages(messages),
		Temperature: temperature,
	}
}

func hashString(s string) [32]byte {
	h := sha256.Sum256([]byte(s))
	return h
}

func hashMessages(messages []llm.Message) [32]byte {
	h := sha256.New()
	for _, m := range messages {
		// 用 '\x00' 作为分隔符,避免 role="ab" content="c" 跟 role="a" content="bc" 撞。
		h.Write([]byte(m.Role))
		h.Write([]byte{0})
		h.Write([]byte(m.Content))
		h.Write([]byte{0})
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// cacheEntry 是 LRU 双向链表中的一个节点。
type cacheEntry struct {
	key       CacheKey
	value     *CachedResponse
	expiresAt time.Time
	sessionID string // 用于 EvictSession 主动清理
}

// ResponseCache 是 in-memory LRU + TTL 缓存。
// 线程安全:用 sync.RWMutex 保护(读多写少);hits/misses 用 atomic。
type ResponseCache struct {
	mu      sync.RWMutex
	entries map[CacheKey]*list.Element
	ll      *list.List
	ttl     time.Duration
	max     int

	hits   uint64
	misses uint64
}

// NewResponseCache 构造一个 ResponseCache。
//   - ttl:entry 过期时间,0 → 默认 5min
//   - max:LRU 上限 entry 数,0 → 默认 10000
func NewResponseCache(ttl time.Duration, max int) *ResponseCache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if max <= 0 {
		max = 10000
	}
	return &ResponseCache{
		entries: make(map[CacheKey]*list.Element, max),
		ll:      list.New(),
		ttl:     ttl,
		max:     max,
	}
}

// Get 查 cache。命中返回 (value, true),未命中返回 (nil, false)。
// 命中时把 entry 移到 LRU 头部(最近使用)。
// sessionID 用于日志/调试(可为空)。
func (c *ResponseCache) Get(key CacheKey, sessionID string) (*CachedResponse, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	el, ok := c.entries[key]
	if !ok {
		c.mu.RUnlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}
	entry := el.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		// 过期,移到写锁清理
		c.mu.RUnlock()
		c.mu.Lock()
		// double-check:可能在我们等锁时被其他 goroutine 删了
		if el, ok := c.entries[key]; ok {
			c.ll.Remove(el)
			delete(c.entries, key)
		}
		c.mu.Unlock()
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}
	c.mu.RUnlock()
	c.mu.Lock()
	// 移到 LRU 头部
	c.ll.MoveToFront(el)
	c.mu.Unlock()
	atomic.AddUint64(&c.hits, 1)
	return entry.value, true
}

// Put 写入 cache。已存在则更新值与过期时间;超上限则淘汰最旧的 entry。
func (c *ResponseCache) Put(key CacheKey, sessionID string, value *CachedResponse) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// 已存在 → 更新
	if el, ok := c.entries[key]; ok {
		entry := el.Value.(*cacheEntry)
		entry.value = value
		entry.expiresAt = now.Add(c.ttl)
		entry.sessionID = sessionID
		c.ll.MoveToFront(el)
		return
	}

	// 超上限 → 淘汰最旧(尾部)
	for c.ll.Len() >= c.max {
		oldest := c.ll.Back()
		if oldest == nil {
			break
		}
		oldEntry := oldest.Value.(*cacheEntry)
		c.ll.Remove(oldest)
		delete(c.entries, oldEntry.key)
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: now.Add(c.ttl),
		sessionID: sessionID,
	}
	el := c.ll.PushFront(entry)
	c.entries[key] = el
}

// EvictSession 删除指定 session 的所有 entry。
// 在 trial 结束 / session 被 cleanup 时调用,防止内存膨胀。
// 返回被删除的 entry 数。
func (c *ResponseCache) EvictSession(sessionID string) int {
	if c == nil || sessionID == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	evicted := 0
	var toRemove []*list.Element
	for el := c.ll.Front(); el != nil; el = el.Next() {
		entry := el.Value.(*cacheEntry)
		if entry.sessionID == sessionID {
			toRemove = append(toRemove, el)
		}
	}
	for _, el := range toRemove {
		entry := el.Value.(*cacheEntry)
		c.ll.Remove(el)
		delete(c.entries, entry.key)
		evicted++
	}
	return evicted
}

// Stats 返回命中统计。
//   - hits:命中次数
//   - misses:未命中次数
//   - size:当前 entry 数
func (c *ResponseCache) Stats() (hits, misses uint64, size int) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return atomic.LoadUint64(&c.hits), atomic.LoadUint64(&c.misses), c.ll.Len()
}

// HitRatio 返回命中率(0-1)。无访问时返回 0。
func (c *ResponseCache) HitRatio() float64 {
	hits, misses, _ := c.Stats()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// CacheHit 用于给 Recorder 标记 status=cache_hit。
const CacheHit = "cache_hit"
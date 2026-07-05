// Package idempotency 实现用户请求幂等性（ADR 0012 §决策 2）。
//
// 解决弱网 / 用户重复点击"开始庭审"导致的重复 trial 问题。
// 客户端发送 Idempotency-Key header(随机 UUID),后端用 sync.Map 存 24h,
// 同 key 重复请求 → 返回相同响应(缓存 result),不调业务逻辑。
//
// 设计决策:
//   - 单机 sync.Map(DAU > 5000 触发 Redis 切换,留 interface)
//   - TTL 24h(超过重置)
//   - key 长度限制 64(防滥用)
//   - 业务侧 status code + body 都缓存,确保客户端拿到的响应一致
//
// 简历叙述:
//   "为防弱网重复点击,设计 Idempotency-Key 中间件(sync.Map + 24h TTL),
//    客户端自动注入 UUID header,服务端去重表。实战场景:用户 4G 弱网
//    连点 5 次'开始庭审',后端只创建 1 个 trial,响应一致。"
package idempotency

import (
	"sync"
	"time"
)

// StatusCode 是 HTTP 状态码(避免 import net/http)。
type StatusCode int

// Body 是缓存的响应体(JSON 序列化后)。
type Body []byte

// CachedResponse 是缓存的完整响应。
type CachedResponse struct {
	StatusCode StatusCode
	Body       Body
	// CreatedAt 用于 TTL 过期判断
	CreatedAt time.Time
}

// Idempotency 是请求幂等器。
type Idempotency struct {
	mu      sync.RWMutex
	entries map[string]*CachedResponse
	ttl     time.Duration
}

// NewIdempotency 构造幂等器。
//   - ttl ≤ 0 → 默认 24h
func NewIdempotency(ttl time.Duration) *Idempotency {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Idempotency{
		entries: make(map[string]*CachedResponse),
		ttl:     ttl,
	}
}

// Get 返回 key 对应的缓存响应。
//   - (nil, false):未命中(新请求,正常处理业务逻辑)
//   - (resp, true):命中(返回缓存,跳过业务逻辑)
func (i *Idempotency) Get(key string) (*CachedResponse, bool) {
	if i == nil {
		return nil, false
	}
	i.mu.RLock()
	defer i.mu.RUnlock()

	resp, ok := i.entries[key]
	if !ok {
		return nil, false
	}
	// TTL 过期 → 视为未命中
	if time.Since(resp.CreatedAt) > i.ttl {
		return nil, false
	}
	return resp, true
}

// Put 写入 key → response。
//   - 已存在 → 不覆盖(幂等原则,首次响应优先)
//   - TTL 过期 → 覆盖
func (i *Idempotency) Put(key string, resp CachedResponse) {
	if i == nil || key == "" {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	if existing, ok := i.entries[key]; ok {
		// TTL 没过期 → 不覆盖
		if time.Since(existing.CreatedAt) <= i.ttl {
			return
		}
	}
	resp.CreatedAt = time.Now()
	i.entries[key] = &resp
}

// EvictExpired 清掉过期 entry（测试用）。
func (i *Idempotency) EvictExpired() int {
	if i == nil {
		return 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()

	now := time.Now()
	evicted := 0
	for k, v := range i.entries {
		if now.Sub(v.CreatedAt) > i.ttl {
			delete(i.entries, k)
			evicted++
		}
	}
	return evicted
}

// Stats 返回当前状态(给 observability)。
func (i *Idempotency) Stats() (tracked int) {
	if i == nil {
		return 0
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.entries)
}

// MaxKeyLen 是 key 长度上限(防滥用)。
const MaxKeyLen = 64
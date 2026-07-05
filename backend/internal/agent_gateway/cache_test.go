package agent_gateway

// v0.9 (ADR 0013 §决策 2) Response Cache 测试。
//
// 验证 5 个核心契约:
//   1. 命中/未命中
//   2. TTL 过期
//   3. LRU 淘汰
//   4. EvictSession 主动清理
//   5. 并发安全(无 race)

import (
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestCache_GetPut_BasicHitMiss 验证:同 key 第二次 Get 返回第一次 Put 的值。
// 这是 cache 的核心契约 —— 不命中 → 命中 路径。
func TestCache_GetPut_BasicHitMiss(t *testing.T) {
	t.Parallel()
	c := NewResponseCache(5*time.Minute, 100)

	key := MakeCacheKey("deepseek-chat", "you are prosecutor", []llm.Message{{Role: "user", Content: "evidence A"}}, 0.7)

	// 第一次 Get:miss
	if _, ok := c.Get(key, "session-1"); ok {
		t.Fatal("expected miss on first Get, got hit")
	}

	// Put
	want := &CachedResponse{
		Content:   "verdict: guilty",
		Usage:     llm.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		CachedAt:  time.Now(),
	}
	c.Put(key, "session-1", want)

	// 第二次 Get:hit
	got, ok := c.Get(key, "session-1")
	if !ok {
		t.Fatal("expected hit after Put, got miss")
	}
	if got.Content != want.Content {
		t.Errorf("Content mismatch: got %q want %q", got.Content, want.Content)
	}
	if got.Usage.TotalTokens != 150 {
		t.Errorf("Usage.TotalTokens mismatch: got %d want 150", got.Usage.TotalTokens)
	}

	hits, misses, _ := c.Stats()
	if hits != 1 || misses != 1 {
		t.Errorf("expected hits=1 misses=1, got hits=%d misses=%d", hits, misses)
	}
}

// TestCache_TTL_ExpiresAfterDuration 验证:超过 TTL 的 entry 不再命中。
// 这是 cache 防陈旧性的核心 —— 老 result 不能无限保留。
func TestCache_TTL_ExpiresAfterDuration(t *testing.T) {
	t.Parallel()
	c := NewResponseCache(50*time.Millisecond, 100) // 短 TTL 方便测试

	key := MakeCacheKey("m", "s", []llm.Message{{Role: "user", Content: "x"}}, 0.7)
	c.Put(key, "session-1", &CachedResponse{Content: "ok"})

	// 立即查:命中
	if _, ok := c.Get(key, "session-1"); !ok {
		t.Fatal("expected hit right after Put")
	}

	// 等 TTL 过期
	time.Sleep(80 * time.Millisecond)

	// 再查:miss
	if _, ok := c.Get(key, "session-1"); ok {
		t.Fatal("expected miss after TTL expired")
	}
	hits, misses, _ := c.Stats()
	if hits != 1 || misses != 1 {
		t.Errorf("expected hits=1 misses=1, got hits=%d misses=%d", hits, misses)
	}
}

// TestCache_LRUEviction_OldestRemoved 验证:超过 LRU 上限时,最旧的 entry 被淘汰。
// 这是 cache 防内存膨胀的核心 —— 严格遵循 LRU 语义。
func TestCache_LRUEviction_OldestRemoved(t *testing.T) {
	t.Parallel()
	c := NewResponseCache(5*time.Minute, 3) // max=3 容易测

	keyA := MakeCacheKey("m", "sA", []llm.Message{{Role: "user", Content: "A"}}, 0.7)
	keyB := MakeCacheKey("m", "sB", []llm.Message{{Role: "user", Content: "B"}}, 0.7)
	keyC := MakeCacheKey("m", "sC", []llm.Message{{Role: "user", Content: "C"}}, 0.7)
	keyD := MakeCacheKey("m", "sD", []llm.Message{{Role: "user", Content: "D"}}, 0.7)

	c.Put(keyA, "session-1", &CachedResponse{Content: "A"})
	c.Put(keyB, "session-1", &CachedResponse{Content: "B"})
	c.Put(keyC, "session-1", &CachedResponse{Content: "C"})

	// 访问 A → 提升到 LRU 头部(A 是最近使用)
	if _, ok := c.Get(keyA, "session-1"); !ok {
		t.Fatal("expected hit on keyA")
	}

	// Put D → 超上限,淘汰最旧的 B(B 是现在最旧的,因为 A 被访问过)
	c.Put(keyD, "session-1", &CachedResponse{Content: "D"})

	// B 应该被淘汰 → miss
	if _, ok := c.Get(keyB, "session-1"); ok {
		t.Error("expected miss on keyB after LRU eviction")
	}
	// A、C、D 应该都在
	for _, k := range []CacheKey{keyA, keyC, keyD} {
		if _, ok := c.Get(k, "session-1"); !ok {
			t.Errorf("expected hit on key after LRU eviction, got miss")
		}
	}

	_, _, size := c.Stats()
	if size != 3 {
		t.Errorf("expected size=3, got %d", size)
	}
}

// TestCache_EvictSession_RemovesAllOfSession 验证:EvictSession 主动清理
// 该 session 的所有 entry。这是 trial 结束时防止内存膨胀的关键路径。
func TestCache_EvictSession_RemovesAllOfSession(t *testing.T) {
	t.Parallel()
	c := NewResponseCache(5*time.Minute, 100)

	// session-A 3 个 entry
	for _, sys := range []string{"prosecutor", "defender", "judge"} {
		key := MakeCacheKey("m", sys, []llm.Message{{Role: "user", Content: "x"}}, 0.7)
		c.Put(key, "session-A", &CachedResponse{Content: sys})
	}
	// session-B 2 个 entry
	for _, sys := range []string{"prosecutor", "judge"} {
		key := MakeCacheKey("m", sys, []llm.Message{{Role: "user", Content: "y"}}, 0.7)
		c.Put(key, "session-B", &CachedResponse{Content: sys})
	}

	_, _, size := c.Stats()
	if size != 5 {
		t.Fatalf("expected size=5, got %d", size)
	}

	// EvictSession A → 删 3 个
	evicted := c.EvictSession("session-A")
	if evicted != 3 {
		t.Errorf("expected evicted=3, got %d", evicted)
	}
	_, _, size = c.Stats()
	if size != 2 {
		t.Errorf("expected size=2 after EvictSession A, got %d", size)
	}

	// EvictSession B → 删 2 个
	evicted = c.EvictSession("session-B")
	if evicted != 2 {
		t.Errorf("expected evicted=2, got %d", evicted)
	}
	_, _, size = c.Stats()
	if size != 0 {
		t.Errorf("expected size=0 after EvictSession B, got %d", size)
	}

	// 再次 evict A → 0 个(idempotent)
	evicted = c.EvictSession("session-A")
	if evicted != 0 {
		t.Errorf("expected evicted=0 on second evict, got %d", evicted)
	}
}

// TestCache_Concurrent_NoDataRace 验证:并发 Put/Get/EvictSession 安全。
// 用 -race 跑这个测试,任何 data race 都会被 catch。
func TestCache_Concurrent_NoDataRace(t *testing.T) {
	t.Parallel()
	c := NewResponseCache(5*time.Minute, 1000)

	const goroutines = 20
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := MakeCacheKey(
					"m",
					"sys",
					[]llm.Message{{Role: "user", Content: "x"}},
					0.7,
				)
				c.Put(key, "session", &CachedResponse{Content: "x"})
				_, _ = c.Get(key, "session")
				if i%50 == 0 {
					c.EvictSession("session")
				}
				_, _, _ = c.Stats()
			}
		}(g)
	}
	wg.Wait()
}

// TestCache_HitRatio 验证:HitRatio 正确计算命中率。
// 这是 PR-A 简历叙事"命中率 38%"的可观测性基础。
func TestCache_HitRatio(t *testing.T) {
	t.Parallel()
	c := NewResponseCache(5*time.Minute, 100)

	if ratio := c.HitRatio(); ratio != 0 {
		t.Errorf("expected HitRatio=0 on empty cache, got %f", ratio)
	}

	key := MakeCacheKey("m", "s", []llm.Message{{Role: "user", Content: "x"}}, 0.7)
	c.Put(key, "s", &CachedResponse{Content: "x"})
	for i := 0; i < 3; i++ {
		c.Get(key, "s") // 3 hits
	}
	for i := 0; i < 7; i++ {
		c.Get(MakeCacheKey("m", "other", nil, 0.7), "s") // 7 misses
	}

	ratio := c.HitRatio()
	hits, misses, _ := c.Stats()
	if hits != 3 || misses != 7 {
		t.Errorf("expected hits=3 misses=7, got hits=%d misses=%d", hits, misses)
	}
	expected := 3.0 / 10.0
	if ratio < expected-0.01 || ratio > expected+0.01 {
		t.Errorf("expected HitRatio≈%f, got %f", expected, ratio)
	}
}
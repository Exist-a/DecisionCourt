package idempotency

// v0.9 (ADR 0012 §决策 2) Idempotency 测试。
//
// 验证 4 个核心契约:
//   1. 同 key 第二次 Get 返回第一次 Put 的响应
//   2. TTL 过期后 Get miss
//   3. 已存在 Put 不覆盖(幂等原则)
//   4. 并发安全

import (
	"sync"
	"testing"
	"time"
)

func TestIdempotency_PutGet_BasicHitMiss(t *testing.T) {
	t.Parallel()

	i := NewIdempotency(1 * time.Hour)

	// 未命中
	if _, ok := i.Get("key-1"); ok {
		t.Fatal("expected miss on first Get")
	}

	// Put
	i.Put("key-1", CachedResponse{StatusCode: 200, Body: []byte(`{"code":0}`)})

	// 命中
	resp, ok := i.Get("key-1")
	if !ok {
		t.Fatal("expected hit after Put")
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected StatusCode=200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"code":0}` {
		t.Errorf("expected Body='{\"code\":0}', got %q", resp.Body)
	}
}

func TestIdempotency_TTLExpires(t *testing.T) {
	t.Parallel()

	i := NewIdempotency(50 * time.Millisecond)
	i.Put("key-1", CachedResponse{StatusCode: 200})

	if _, ok := i.Get("key-1"); !ok {
		t.Fatal("expected hit right after Put")
	}

	time.Sleep(80 * time.Millisecond)

	if _, ok := i.Get("key-1"); ok {
		t.Fatal("expected miss after TTL expired")
	}
}

func TestIdempotency_PutDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	i := NewIdempotency(1 * time.Hour)

	// 首次 Put
	i.Put("key-1", CachedResponse{StatusCode: 200, Body: []byte("first")})

	// 第二次 Put (不同响应) → 应该不覆盖
	i.Put("key-1", CachedResponse{StatusCode: 500, Body: []byte("second")})

	resp, ok := i.Get("key-1")
	if !ok {
		t.Fatal("expected hit")
	}
	if string(resp.Body) != "first" {
		t.Errorf("expected Body='first' (no overwrite), got %q", resp.Body)
	}
}

func TestIdempotency_NilSafe(t *testing.T) {
	t.Parallel()

	var i *Idempotency
	// nil Idempotency 应该不 panic
	if _, ok := i.Get("any"); ok {
		t.Error("nil Idempotency should not return hit")
	}
	i.Put("any", CachedResponse{StatusCode: 200})
	if got := i.Stats(); got != 0 {
		t.Errorf("nil Idempotency Stats should return 0, got %d", got)
	}
}

func TestIdempotency_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	i := NewIdempotency(1 * time.Hour)
	const goroutines = 50
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for n := 0; n < opsPerGoroutine; n++ {
				key := "shared-key"
				if n%10 == 0 {
					key = "different-key"
				}
				i.Put(key, CachedResponse{StatusCode: 200})
				_, _ = i.Get(key)
			}
		}(g)
	}
	wg.Wait()

	stats := i.Stats()
	if stats < 1 || stats > 2 {
		t.Errorf("expected Stats in [1,2], got %d", stats)
	}
}

func TestIdempotency_Defaults(t *testing.T) {
	t.Parallel()

	i := NewIdempotency(0)
	if i.ttl != 24*time.Hour {
		t.Errorf("expected default ttl=24h, got %v", i.ttl)
	}
}

func TestIdempotency_EvictExpired(t *testing.T) {
	t.Parallel()

	i := NewIdempotency(50 * time.Millisecond)
	i.Put("a", CachedResponse{StatusCode: 200})
	i.Put("b", CachedResponse{StatusCode: 200})
	i.Put("c", CachedResponse{StatusCode: 200})

	time.Sleep(80 * time.Millisecond)

	evicted := i.EvictExpired()
	if evicted != 3 {
		t.Errorf("expected evicted=3, got %d", evicted)
	}
	if stats := i.Stats(); stats != 0 {
		t.Errorf("expected Stats=0 after evict, got %d", stats)
	}
}
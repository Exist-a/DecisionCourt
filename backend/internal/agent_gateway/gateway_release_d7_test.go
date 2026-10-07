package agent_gateway

// v2.11 (deferred D7): Gateway.Release 的回归护栏。
//
// 背景：TokenBudget.Reset 与 ResponseCache.EvictSession 早已实现，但包外
// **没有任何调用点**（Gateway 只暴露 Complete/StreamComplete），于是按 session
// 累积的预算滑动窗口与响应缓存映射随进程存活一直增长。Release 是包的公开出口。
//
// 验收（deferred D7）：清理后该 session 的预算记录与缓存条目均归零，
// 且**不影响其他 session**。

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReleaseTestGateway 构造一个预算 + 缓存都启用的 Gateway（不跑 LLM）。
func newReleaseTestGateway(t *testing.T) *Gateway {
	t.Helper()
	g := NewWithConfig(nil, nil, "test-model", GatewayConfig{
		Enabled:          true,
		TokenBudget:      true,
		CacheEnabled:     true,
		CacheTTLSec:      300,
		CacheMaxEntries:  16,
		BudgetPerSession: 20000,
	}, nil)
	require.NotNil(t, g.budget, "预算必须启用才能测 Release")
	require.NotNil(t, g.cache, "缓存必须启用才能测 Release")
	return g
}

func cacheKeyFor(sys string) CacheKey {
	return MakeCacheKey("test-model", sys, nil, 0.7)
}

// TestGateway_Release_ClearsSessionResources 核心契约：清本 session、不动别人。
func TestGateway_Release_ClearsSessionResources(t *testing.T) {
	g := newReleaseTestGateway(t)
	ctx := context.Background()

	g.budget.AddUsage(ctx, "s1", BudgetUsage{InputTokens: 100})
	g.budget.AddUsage(ctx, "s2", BudgetUsage{InputTokens: 200})
	require.Equal(t, 100, g.budget.CurrentUsage("s1"))

	k1 := cacheKeyFor("sys-1")
	k2 := cacheKeyFor("sys-2")
	g.cache.Put(k1, "s1", &CachedResponse{Content: "c1"})
	g.cache.Put(k2, "s2", &CachedResponse{Content: "c2"})
	_, _, size := g.cache.Stats()
	require.Equal(t, 2, size)

	g.Release("s1")

	// 1. 预算：本 session 归零
	assert.Equal(t, 0, g.budget.CurrentUsage("s1"), "清理后按会话维度的预算应归零")
	// 2. 预算：其他 session 不受影响
	assert.Equal(t, 200, g.budget.CurrentUsage("s2"), "不得影响其他 session 的预算")
	// 3. 缓存：只删本 session 的条目
	_, _, size = g.cache.Stats()
	assert.Equal(t, 1, size, "只应清掉 s1 的缓存条目")
	if _, ok := g.cache.Get(k1, "s1"); ok {
		t.Error("s1 的缓存条目应已被 EvictSession 删除")
	}
	if _, ok := g.cache.Get(k2, "s2"); !ok {
		t.Error("s2 的缓存条目必须存活")
	}
}

// TestGateway_Release_Idempotent Release 可重复调用，二次调用不 panic 也不破坏状态。
func TestGateway_Release_Idempotent(t *testing.T) {
	g := newReleaseTestGateway(t)
	g.budget.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 5})

	g.Release("s1")
	g.Release("s1")
	assert.Equal(t, 0, g.budget.CurrentUsage("s1"))
}

// TestGateway_Release_NilAndEmptySafe nil Gateway / 空 sessionUUID 都必须 no-op。
func TestGateway_Release_NilAndEmptySafe(t *testing.T) {
	var nilGateway *Gateway
	assert.NotPanics(t, func() { nilGateway.Release("s1") }, "nil Gateway 必须安全")

	g := newReleaseTestGateway(t)
	g.budget.AddUsage(context.Background(), "", BudgetUsage{InputTokens: 5})
	assert.NotPanics(t, func() { g.Release("") }, "空 sessionUUID 必须安全")
}

// TestGateway_Release_PartialCapabilities 只启用预算 / 只启用缓存时也要安全。
func TestGateway_Release_PartialCapabilities(t *testing.T) {
	t.Run("仅预算", func(t *testing.T) {
		g := NewWithConfig(nil, nil, "m", GatewayConfig{Enabled: true, TokenBudget: true}, nil)
		require.NotNil(t, g.budget)
		require.Nil(t, g.cache)
		g.budget.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 9})
		assert.NotPanics(t, func() { g.Release("s1") })
		assert.Equal(t, 0, g.budget.CurrentUsage("s1"))
	})

	t.Run("仅缓存", func(t *testing.T) {
		// 显式打开 Throttling 以打破 isChildDefault()（否则"没有任何子开关被显式
		// 设置"会被当成"子能力全开"，预算也会被一并建出来）。
		g := NewWithConfig(nil, nil, "m", GatewayConfig{
			Enabled: true, CacheEnabled: true, CacheTTLSec: 60, Throttling: true,
		}, nil)
		require.Nil(t, g.budget, "本用例覆盖「只有缓存」时的装配")
		require.NotNil(t, g.cache)
		k := cacheKeyFor("sys")
		g.cache.Put(k, "s1", &CachedResponse{Content: "c"})
		assert.NotPanics(t, func() { g.Release("s1") })
		_, _, size := g.cache.Stats()
		assert.Equal(t, 0, size)
	})

	t.Run("全关", func(t *testing.T) {
		g := NewWithConfig(nil, nil, "m", GatewayConfig{}, nil)
		assert.NotPanics(t, func() { g.Release("s1") })
	})
}

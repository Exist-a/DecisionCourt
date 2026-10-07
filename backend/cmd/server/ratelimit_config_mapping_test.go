package main

import (
	"reflect"
	"testing"

	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/middleware"
)

// v2.11 (deferred D10) 回归护栏：限流阈值从 config 到运行时的映射必须完整。
//
// 背景：ADR 0027 §4.3 提议 RATE_LIMIT_SESSION_ACTION_RPS / _BURST /
// RATE_LIMIT_MAX_CONCURRENT_TRIALS 可配，但实现时漏了接线——常量与文档在、
// 没人读，"看起来可配、实际改不动"。与 v2.10 的 buildGatewayConfig 漏搬同源
// （ADR 0044 §3.5 Problem B）。
//
// 手法与 gateway_config_mapping_test.go 一致：源配置填非零值，断言目标结构
// 的**每个**非函数字段都非零。漏搬的字段必然是零值 → 测试失败；新增阈值字段
// 无需改测试，会自动被纳入。

// TestBuildSessionRateLimitConfig_MapsEveryField 反射遍历
// middleware.SessionConfig 的数值字段，断言全部被映射（非零）。
func TestBuildSessionRateLimitConfig_MapsEveryField(t *testing.T) {
	// 刻意用互不相同的值，避免"错搬成相邻字段"也能通过。
	src := config.Config{SessionActionRPS: 7.5, SessionActionBurst: 11}
	got := buildSessionRateLimitConfig(src, func() {})

	vt := reflect.TypeOf(got)
	vv := reflect.ValueOf(got)
	for i := 0; i < vt.NumField(); i++ {
		f := vt.Field(i)
		// OnReject 由装配层注入(绑 metrics)，不属于 config 映射范围。
		if !f.IsExported() || f.Type.Kind() == reflect.Func {
			continue
		}
		if vv.Field(i).IsZero() {
			t.Errorf("SessionConfig.%s 未被 buildSessionRateLimitConfig 映射 —— "+
				"该阈值会在运行时静默取零值(退回中间件默认)", f.Name)
		}
	}

	if got.RPS != src.SessionActionRPS {
		t.Errorf("RPS 映射错误: want %v got %v", src.SessionActionRPS, got.RPS)
	}
	if got.Burst != src.SessionActionBurst {
		t.Errorf("Burst 映射错误: want %v got %v", src.SessionActionBurst, got.Burst)
	}
}

// TestBuildSessionRateLimitConfig_InjectsOnReject 确认回调被透传，
// 否则限流拒绝的 metric 会静默丢失。
func TestBuildSessionRateLimitConfig_InjectsOnReject(t *testing.T) {
	called := 0
	got := buildSessionRateLimitConfig(
		config.Config{SessionActionRPS: 2, SessionActionBurst: 5},
		func() { called++ },
	)
	got.OnReject()
	if called != 1 {
		t.Fatalf("OnReject 未被透传: called=%d", called)
	}
}

// TestConcurrencyLimiterMax_MapsConfig 钉住 L0 上限的 config → limiter 通道。
func TestConcurrencyLimiterMax_MapsConfig(t *testing.T) {
	if got := concurrencyLimiterMax(config.Config{MaxConcurrentTrials: 9}); got != 9 {
		t.Errorf("MaxConcurrentTrials 未被映射: want 9 got %d", got)
	}
	// 未配置(0)时原样透传，由 NewConcurrencyLimiter 兜底为 5。
	if got := concurrencyLimiterMax(config.Config{}); got != 0 {
		t.Errorf("未配置应透传 0 交由 limiter 兜底, got %d", got)
	}
}

// TestHistoricalRateLimitDefaultsUnchanged 钉住"只做可配、不改行为"：
// 中间件内置默认值仍是历史值，且 limiter 非正数兜底仍是 5。
func TestHistoricalRateLimitDefaultsUnchanged(t *testing.T) {
	if got := middleware.DefaultSessionConfig.RPS; got != 2 {
		t.Errorf("middleware.DefaultSessionConfig.RPS 应保持 2(历史值), got %v", got)
	}
	if got := middleware.DefaultSessionConfig.Burst; got != 5 {
		t.Errorf("middleware.DefaultSessionConfig.Burst 应保持 5(历史值), got %d", got)
	}
	if got := middleware.DefaultConfig.RPS; got != 20 {
		t.Errorf("middleware.DefaultConfig.RPS(per-IP) 应保持 20, got %v", got)
	}
	if got := middleware.DefaultConfig.Burst; got != 50 {
		t.Errorf("middleware.DefaultConfig.Burst(per-IP) 应保持 50, got %d", got)
	}
}

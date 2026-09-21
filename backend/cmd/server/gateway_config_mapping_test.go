package main

import (
	"reflect"
	"testing"

	"github.com/decisioncourt/backend/internal/config"
)

// v2.10 (ADR 0044) 回归护栏：buildGatewayConfig 必须搬完 GatewayConfig 的每个字段。
//
// 背景：这个映射是字段对字段的手工拷贝，漏一个不会编译报错，只会让对应功能
// **静默失效**。v2.10 开发中真实踩到两次：
//  1. AGENT_GATEWAY_SMART_COMPRESSION_ABSTRACTIVE_SUMMARY 在 config 里读到了、
//     main.go 没搬 → 压缩照跑但永远走 extractive，没有任何日志。
//  2. 更早的存量缺口（本次才被发现）：LLMTimeoutSec / CacheEnabled / Cache* /
//     Breaker 从来没被搬过 → ADR 0013 的 Response Cache 与 Circuit Breaker
//     事实上从未生效，ADR 0037 为它们加的 metric 恒为 0。
//
// 做法：把源配置的每个字段都填成非零值，然后断言目标配置的每个字段也非零。
// 漏搬的字段必然是零值 → 测试失败。新增字段无需改测试。
func TestBuildGatewayConfig_MapsEveryField(t *testing.T) {
	src := config.AgentGatewayConfig{}
	fillAllNonZero(reflect.ValueOf(&src).Elem())

	got := buildGatewayConfig(src)
	assertAllNonZero(t, reflect.ValueOf(got), "")
}

// fillAllNonZero 把结构体（含嵌套 struct）所有导出字段填成非零值。
func fillAllNonZero(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Bool:
			f.SetBool(true)
		case reflect.String:
			f.SetString("x")
		case reflect.Int, reflect.Int32, reflect.Int64:
			f.SetInt(7)
		case reflect.Uint32:
			f.SetUint(7)
		case reflect.Float64:
			f.SetFloat(0.42)
		case reflect.Struct:
			fillAllNonZero(f)
		}
	}
}

// assertAllNonZero 递归断言所有导出字段非零，prefix 用于报出嵌套路径。
func assertAllNonZero(t *testing.T, v reflect.Value, prefix string) {
	t.Helper()
	vt := v.Type()
	for i := 0; i < vt.NumField(); i++ {
		field := vt.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Name
		if prefix != "" {
			name = prefix + "." + name
		}
		f := v.Field(i)
		if f.Kind() == reflect.Struct {
			assertAllNonZero(t, f, name)
			continue
		}
		if f.IsZero() {
			t.Errorf("GatewayConfig.%s 未被 buildGatewayConfig 映射 —— "+
				"该配置项会在运行时静默失效（源配置已填非零值，映射后仍为零）", name)
		}
	}
}

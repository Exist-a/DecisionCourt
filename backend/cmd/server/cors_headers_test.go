package main

import (
	"strings"
	"testing"
)

// v2.11 (deferred D19 第二半) 护栏：CORS 白名单必须覆盖前端实际会发的自定义头。
//
// 教训：这份列表曾漏掉 `X-XSRF-TOKEN`。只修 CSRF cookie Path 还不够 —— 浏览器对
// 带该 header 的写请求会先发 CORS preflight，服务端 Allow-Headers 没列出它 →
// preflight 失败 → 前端 `fetch` 直接 "Failed to fetch"（实测），
// 即"浏览器写路径整体不可用"的另一个入口。
//
// 与 `frontend/lib/api.ts` 的注入点一一对应，改动任何一侧都必须同步。
func TestCORSAllowedHeaders_CoversFrontendCustomHeaders(t *testing.T) {
	// 前端 fetchJson 会注入的自定义头（见 frontend/lib/api.ts）
	required := []string{
		"Authorization",   // 非浏览器 client / 迁移期兼容
		"X-Request-ID",    // 白盒化 trace
		"Idempotency-Key", // 启动庭审去重（ADR 0012）
		"X-XSRF-TOKEN",    // CSRF double-submit（ADR 0039 / v2.5）
	}
	have := map[string]bool{}
	for _, h := range corsAllowedHeaders {
		have[strings.ToLower(h)] = true
	}
	for _, h := range required {
		if !have[strings.ToLower(h)] {
			t.Errorf("corsAllowedHeaders 缺少 %s —— 浏览器发该头时 CORS preflight 会失败，"+
				"前端 fetch 直接报 Failed to fetch（v2.11 D19 实测）", h)
		}
	}
	// 基础头也不能丢
	for _, h := range []string{"Origin", "Content-Type", "Accept"} {
		if !have[strings.ToLower(h)] {
			t.Errorf("corsAllowedHeaders 缺少基础头 %s", h)
		}
	}
}

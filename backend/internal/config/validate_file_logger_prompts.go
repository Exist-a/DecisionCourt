package config

import "fmt"

// ValidateFileLoggerPrompts 校验 v2.8 PR-3 (ADR 0042) 引入的
// AGENT_GATEWAY_FILE_LOGGER_PROMPTS env 值。合法值:
//   - "off"      → 完全不写 LogEntry (合规场景)
//   - "metadata" → 只写 metadata (v2.7 baseline 行为, 默认)
//   - "full"     → 写 system prompt + input messages + output content (opt-in)
//
// 启动 fail-fast (cmd/server/main.go 调用), 不让无效 env 静默进入 writeFileLog
// 导致 runtime 行为偏离用户预期. 跟 ValidateAppEnv (v2.4 P1-7) 同级 fail-fast.
//
// 历史: v0.10.19 之前的 feature flag 偶有 "garbage value silently default"
// 教训 — 用户设错环境变量比写错默认值更常见.
func ValidateFileLoggerPrompts(v string) error {
	switch v {
	case "off", "metadata", "full":
		return nil
	default:
		return fmt.Errorf("AGENT_GATEWAY_FILE_LOGGER_PROMPTS=%q invalid; must be one of off|metadata|full (ADR 0042)", v)
	}
}

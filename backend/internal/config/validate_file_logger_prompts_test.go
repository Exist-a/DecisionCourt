package config

import (
	"strings"
	"testing"
)

// v2.8 PR-3 (ADR 0042) — ValidateFileLoggerPrompts 启动 fail-fast 校验测试.
//
// 行为契约:
//   - "off" / "metadata" / "full" 三态合法 (与 APP_ENV dev/staging/prod tri-state 类似)
//   - 其它拼写错误 (大小写 / 多余空格 / 错词) → 返回 error 含三态 hint
//   - 空字符串: 通过 (Normalize() 后变成 "metadata" 默认值, Validate 在 Load() 末尾对最终值调用)
func TestValidateFileLoggerPrompts_LegalValues(t *testing.T) {
	for _, v := range []string{"off", "metadata", "full"} {
		t.Run(v, func(t *testing.T) {
			if err := ValidateFileLoggerPrompts(v); err != nil {
				t.Errorf("legal value %q should not error, got %v", v, err)
			}
		})
	}
}

func TestValidateFileLoggerPrompts_IllegalValues(t *testing.T) {
	for _, v := range []string{"OFF", "OFF_METADATA", "FULLLL", "prompts", "", "  full  "} {
		if v == "" {
			continue // 空字符串正常, Load() 后的 Normalize 把它变成默认 "metadata"
		}
		t.Run(v, func(t *testing.T) {
			err := ValidateFileLoggerPrompts(v)
			if err == nil {
				t.Errorf("illegal value %q should error", v)
				return
			}
			// 错误信息必须告诉用户合法的三态 (避免 "garbage value silently default" 教训).
			msg := err.Error()
			for _, want := range []string{"off", "metadata", "full"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error message must mention legal value %q for error recovery; got %q", want, msg)
				}
			}
		})
	}
}

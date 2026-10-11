package agent_gateway

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// TestThrottler_JSONOutputExemptFromMaxTokensCut 是 M8-2 A2 的回归钉：
// JSON 输出（决策步 react_think / react_reflect）不得被限流砍 max_tokens。
//
// 旧行为：500 → max(100, 500*remaining*2)，耗尽档砍到 100 —— 一整份决策 JSON
// 在 100 token 下必被截断 → 解析失败 → 重试（同样被砍）→ return error 打断整轮。
// 截断的 JSON 是不可用产物，与"关键输出"同类，故保留 max_tokens、仅降 temperature。
func TestThrottler_JSONOutputExemptFromMaxTokensCut(t *testing.T) {
	th := NewThrottler(nil)
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7, JSONMode: true}

	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "react_think")

	if !info.Exempted {
		t.Fatalf("JSON 输出应豁免限流，info=%+v", info)
	}
	if !strings.Contains(info.ExemptReason, "json_output") {
		t.Errorf("豁免原因应标明 json_output，got %q", info.ExemptReason)
	}
	if out.MaxTokens != 500 {
		t.Errorf("JSON 输出 max_tokens 必须保留 500，got %d", out.MaxTokens)
	}
	if out.Temperature != 0.2 {
		t.Errorf("temperature 仍应降到 0.2，got %f", out.Temperature)
	}
	if info.Applied {
		t.Errorf("豁免时 Applied 应为 false")
	}
}

// TestThrottler_NonJSONStillThrottled 确认豁免只针对 JSON 输出，普通文本仍被限流。
func TestThrottler_NonJSONStillThrottled(t *testing.T) {
	th := NewThrottler(nil)
	opts := llm.CompletionOptions{MaxTokens: 500, Temperature: 0.7} // JSONMode 默认 false

	out, info := th.Apply(opts, BudgetSnapshot{Status: StatusExhausted, Ratio: 1.0}, "react_speak_stream")

	if !info.Applied {
		t.Fatalf("非 JSON 输出仍应被限流，info=%+v", info)
	}
	if out.MaxTokens != 100 {
		t.Errorf("耗尽档非 JSON 应砍到 100，got %d", out.MaxTokens)
	}
}

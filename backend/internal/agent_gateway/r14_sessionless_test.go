package agent_gateway

// R14: 显式"无会话"调用（Prompt Lab 的 eval / abtest）的回归护栏。
//
// 语义要点：审计表 session_id 上的空值有**两种**成因 —— 有意（Prompt Lab 不属于任何
// 庭审）与漏填（bug）。以前一律当 bug 拦下，代价是无会话的调用永远进不了 llm_calls
// （看不到成本、无法按 prompt 版本归因）。现在用显式标记区分：声明了才放行。
//
// 所以这里要同时钉住**两侧**：标记能穿透到 Record（放行路径可用），
// 以及默认不标记（护栏路径不被无意打开）。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecorder_PropagatesSessionless Trace 上的标记必须穿透到 Record。
func TestRecorder_PropagatesSessionless(t *testing.T) {
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, nil)

	rec := r.buildRecord(CallInput{
		Trace:  Trace{Sessionless: true, AgentType: "promptlab", TaskType: "prompt_eval"},
		Model:  "deepseek-v4-flash",
		Status: StatusSuccess,
	})
	assert.True(t, rec.Sessionless, "Trace.Sessionless 必须传到 Record")
	assert.Equal(t, "promptlab", rec.AgentType)
	assert.Equal(t, "prompt_eval", rec.TaskType)
}

// TestRecorder_SessionlessDefaultsFalse 普通调用（庭审内）默认**不**带标记。
//
// 这一条是护栏的一半：如果默认值被改成 true（或标记判断写反），所有"漏填
// session_uuid"的 bug 会静默写进 llm_calls，而不是变成 llm_audit_fk_violation。
func TestRecorder_SessionlessDefaultsFalse(t *testing.T) {
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, nil)

	rec := r.buildRecord(CallInput{
		Trace:   Trace{SessionUUID: "sess-1", AgentType: "judge"},
		Model:   "deepseek-v4-flash",
		Status:  StatusSuccess,
		Latency: 5 * time.Millisecond,
	})
	assert.False(t, rec.Sessionless, "庭审内调用不得被标成无会话，否则漏填 session 不再被拦")
}

// TestRecorder_SessionlessReachesStore 端到端到 Store：Store 拿到的 Record 带标记。
// （Insert 侧的分支行为由 gorm_store_d9_test 的 buildLLMCallRow(nil, ...) 覆盖。）
func TestRecorder_SessionlessReachesStore(t *testing.T) {
	store := &captureStore{}
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)

	r.Record(CallInput{
		Trace:  Trace{Sessionless: true, AgentType: "promptlab", TaskType: "prompt_abtest"},
		Model:  "deepseek-v4-flash",
		Status: StatusSuccess,
	})

	require.Len(t, store.records, 1)
	assert.True(t, store.records[0].Sessionless)
	assert.Equal(t, "prompt_abtest", store.records[0].TaskType)
}

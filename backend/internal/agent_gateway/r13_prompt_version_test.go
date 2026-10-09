package agent_gateway

// R13: prompt 版本归因（llm_calls.prompt_version）的回归护栏。
//
// 缺口本质与 D9 同族：列建了、写入路径没接上 → 列恒为 NULL 而无人发现。
// 这里钉住三件事：
//   1. Recorder 接线前不伪造版本（留空而不是写 "unknown"）；
//   2. 接线后每次 buildRecord 都现取版本（热加载换 prompt 后新行带新值）；
//   3. 超长版本串被截断到 varchar(120) 以内，不会撑爆列。

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecorder_PromptVersionEmptyWhenUnwired 未接线时留空。
//
// 刻意不写 "unknown" —— 伪造的版本号比空值更危险：它会让人以为
// "归因数据是有的"，从而把空值当成真实的版本去做对比。
func TestRecorder_PromptVersionEmptyWhenUnwired(t *testing.T) {
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, nil)
	rec := r.buildRecord(CallInput{Status: StatusSuccess})
	assert.Equal(t, "", rec.PromptVersion, "未接线时必须留空，不得伪造版本")
	assert.Equal(t, "", r.PromptVersion())
}

// TestRecorder_PromptVersionProviderIsEvaluatedPerCall 每次调用现取版本。
//
// 这是 R13 的关键语义：promptlab 支持 YAML 热加载，如果把版本在启动时
// 取成值快照，热加载之后写入的行会一直带着旧版本 —— 归因就错了。
func TestRecorder_PromptVersionProviderIsEvaluatedPerCall(t *testing.T) {
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, nil)
	r.SetPromptVersionProvider(func() string { return "1.0.3-pr1@dev#aaaaaaaa" })

	first := r.buildRecord(CallInput{Status: StatusSuccess})
	require.Equal(t, "1.0.3-pr1@dev#aaaaaaaa", first.PromptVersion)

	// 模拟热加载后 prompt 内容变化（版本提供者换了取值）
	r.SetPromptVersionProvider(func() string { return "1.0.3-pr1@dev#bbbbbbbb" })
	second := r.buildRecord(CallInput{Status: StatusSuccess})
	assert.Equal(t, "1.0.3-pr1@dev#bbbbbbbb", second.PromptVersion,
		"换了 prompt 之后新写入的行必须带新归因键（不能是启动快照）")
}

// TestRecorder_PromptVersionTruncated 超长版本串被截断到列宽以内。
func TestRecorder_PromptVersionTruncated(t *testing.T) {
	r := NewRecorder(RecorderConfig{Enabled: true}, nil)
	long := strings.Repeat("x", MaxPromptVersionLen+50)
	r.SetPromptVersionProvider(func() string { return long })

	got := r.buildRecord(CallInput{Status: StatusSuccess}).PromptVersion
	assert.Len(t, got, MaxPromptVersionLen, "必须截断到 varchar(120) 的宽度")
}

// TestRecorder_PromptVersionNilRecorderSafe nil receiver 不 panic。
// （NewWithConfig 传 nil recorder 时会走这条路。）
func TestRecorder_PromptVersionNilRecorderSafe(t *testing.T) {
	var r *Recorder
	assert.NotPanics(t, func() { r.SetPromptVersionProvider(func() string { return "x" }) })
	assert.Equal(t, "", r.PromptVersion())
}

// TestRecorder_RecordWritesPromptVersionToStore 端到端：Record → Store 收到的
// Record.PromptVersion 非空（而不是只有 buildRecord 层面的正确）。
func TestRecorder_RecordWritesPromptVersionToStore(t *testing.T) {
	store := &captureStore{}
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)
	r.SetPromptVersionProvider(func() string { return "1.0.5-pr2@abc1234#deadbeef" })

	r.Record(CallInput{
		Trace:   Trace{SessionUUID: "sess-1", AgentType: "judge", RequestID: "req-1"},
		Model:   "deepseek-v4-flash",
		Status:  StatusSuccess,
		Latency: 12 * time.Millisecond,
	})

	require.Len(t, store.records, 1)
	assert.Equal(t, "1.0.5-pr2@abc1234#deadbeef", store.records[0].PromptVersion)
}

// captureStore 是 Store 的内存替身。
type captureStore struct{ records []Record }

func (s *captureStore) Insert(r Record) error {
	s.records = append(s.records, r)
	return nil
}

package agent_gateway

// v2.11 (deferred D9): llm_calls 补 request_id + agent_type 的回归护栏。
//
// 这两列的缺口本质是"字段建了但写入路径没理会"——漏映射不会编译报错，
// 只会让列恒为 NULL/零值。所以真正的护栏不是"测一次写入"，而是
// **反射断言每个可映射字段都被写入**：新增审计字段时若忘了映射，测试直接失败。
//
// 用纯函数 buildLLMCallRow 而不是真写 Postgres：单测不该依赖 DB，而映射逻辑
// 才是缺口所在。

import (
	"reflect"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildLLMCallRow_WritesAuditFields 直接钉住 D9 的两个新字段 + R13 的 PromptVersion。
func TestBuildLLMCallRow_WritesAuditFields(t *testing.T) {
	sessionID := uuid.New()
	row := buildLLMCallRow(&sessionID, Record{
		SessionUUID:   "sess-1",
		AgentType:     "prosecutor",
		RequestID:     "req-abc123",
		PromptVersion: "1.0.3-pr1@3fc2ae8#ab12cd34",
		TaskType:      "opening",
		Model:         "deepseek-v4-flash",
		LatencyMs:     321,
		Status:        StatusSuccess,
		CreatedAt:     time.Now().UTC(),
	})

	require.NotNil(t, row.SessionID)
	require.Equal(t, sessionID, *row.SessionID, "session_id 必须是 lookup 到的 DB 主键")
	assert.Equal(t, "prosecutor", row.AgentType, "agent_type 必须写入(D9: 此前恒为空)")
	assert.Equal(t, "req-abc123", row.RequestID, "request_id 必须写入(D9: 此前列不存在)")
	assert.Equal(t, "1.0.3-pr1@3fc2ae8#ab12cd34", row.PromptVersion,
		"prompt_version 必须写入(R13: 此前列不存在，prompt 改动无法归因)")
	assert.Equal(t, "opening", row.TaskType)
	assert.Equal(t, 321, row.LatencyMs)
	assert.NotEqual(t, uuid.Nil, row.ID, "每行必须有新主键")
}

// TestBuildLLMCallRow_MapsEveryRecordField 反射护栏：Record 里每个能在
// LLMCall 找到同名同类型字段的，都必须被写入（非零）。
//
// 跳过规则（有意为之，非同名字段）：
//   - AgentID / CostUSD / CostCNY：LLMCall 独有，Record 没有对应来源
//     （AgentID 是 UUID 外键，调用点只有类型字符串 —— 见 D9 决策：存 AgentType）
//   - ID / SessionID：类型不同或名字不同（Record.ID 是 string 行键，
//     LLMCall.SessionID 来自 lookup 的 DB 主键；R14 起是 *uuid.UUID 可空）
func TestBuildLLMCallRow_MapsEveryRecordField(t *testing.T) {
	src := Record{
		ID:               "row-1",
		SessionUUID:      "sess-1",
		AgentType:        "defender",
		TaskType:         "rebuttal",
		RequestID:        "req-1",
		Model:            "deepseek-v4-pro",
		Provider:         "deepseek",
		PromptVersion:    "1.0.4-pr1@deadbee#0011aabb",
		Sessionless:      true,
		PromptTokens:     11,
		CompletionTokens: 22,
		TotalTokens:      33,
		LatencyMs:        44,
		Status:           StatusError,
		ErrorMsg:         "boom",
		CreatedAt:        time.Now().UTC(),
	}
	sid := uuid.New()
	row := buildLLMCallRow(&sid, src)

	rt := reflect.TypeOf(src)
	rv := reflect.ValueOf(row)
	lt := rv.Type()

	mapped := 0
	for i := 0; i < lt.NumField(); i++ {
		lf := lt.Field(i)
		if !lf.IsExported() {
			continue
		}
		sf, ok := rt.FieldByName(lf.Name)
		if !ok {
			continue // LLMCall 独有字段（AgentID / CostUSD / CostCNY ...）
		}
		if sf.Type.Kind() != lf.Type.Kind() {
			continue // 同名但不同类型（ID: string vs uuid.UUID）
		}
		mapped++
		if rv.Field(i).IsZero() {
			t.Errorf("LLMCall.%s 未被 buildLLMCallRow 映射 —— "+
				"Record 里有同名同类型字段，写入后该列会是零值/NULL", lf.Name)
		}
	}

	// 防止"跳过规则"意外吞掉全部字段：至少应覆盖到 12 个字段
	// （AgentType / RequestID / PromptVersion / TaskType / Model / Prompt* /
	//  Completion* / Total* / LatencyMs / Status / ErrorMsg / CreatedAt）。
	require.GreaterOrEqual(t, mapped, 12,
		"反射护栏覆盖的字段数异常偏少，检查名称/类型匹配是否被改坏")
}

// TestBuildLLMCallRow_ZeroAgentTypeStillWritesEmptyString 明确语义：
// agent_type 允许为空（用户级 / 非 agent 调用），但"字段被映射"这件事
// 由上面的反射护栏保证 —— 空值是合法数据，不是漏映射。
func TestBuildLLMCallRow_ZeroAgentTypeStillWritesEmptyString(t *testing.T) {
	sid := uuid.New()
	row := buildLLMCallRow(&sid, Record{TaskType: "user_action"})
	assert.Equal(t, "", row.AgentType, "无 agent 的调用 agent_type 为空是合法的")
	assert.Nil(t, row.AgentID, "AgentID 保持不写(有意决策，见 D9)")
}

// TestBuildLLMCallRow_NilSessionForSessionlessCall R14: 显式无会话的调用
// 以 NULL session_id 落库（而不是被丢掉）。
func TestBuildLLMCallRow_NilSessionForSessionlessCall(t *testing.T) {
	row := buildLLMCallRow(nil, Record{
		Sessionless: true,
		AgentType:   "promptlab",
		TaskType:    "prompt_eval",
		Status:      StatusSuccess,
	})
	assert.Nil(t, row.SessionID, "Sessionless 调用必须允许 session_id 为 NULL")
	assert.Equal(t, "promptlab", row.AgentType, "其余审计字段照常写入")
	assert.Equal(t, "prompt_eval", row.TaskType)
}

// TestLLMCallSchemaHasAuditColumns 钉住模型层的列定义，防止有人把字段删掉
// （删掉后写入不会报错，DB 层也只是少一列，同样静默）。
func TestLLMCallSchemaHasAuditColumns(t *testing.T) {
	typ := reflect.TypeOf(model.LLMCall{})
	for _, name := range []string{"RequestID", "AgentType", "PromptVersion"} {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("model.LLMCall 缺少 %s —— D9/R13 引入的审计字段被删了", name)
		}
		if tag := f.Tag.Get("gorm"); tag == "" {
			t.Errorf("model.LLMCall.%s 缺少 gorm tag", name)
		}
	}
}

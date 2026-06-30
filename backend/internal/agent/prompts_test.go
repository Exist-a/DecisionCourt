package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

// fakeTool 仅用于构造 toolBlockForPrompt 的输入。
type fakeTool struct{ name, desc string }

func (f *fakeTool) Name() string        { return f.name }
func (f *fakeTool) Description() string { return f.desc }
func (f *fakeTool) Execute(_ context.Context, _ map[string]interface{}) (string, error) {
	return "", nil
}

// T1: 无 tool 时 toolBlockForPrompt 返回空字符串
func TestToolBlockForPrompt_EmptyWhenNoTools(t *testing.T) {
	require.Equal(t, "", toolBlockForPrompt(map[string]Tool{}))
}

// T2: 有 tool 时输出包含工具说明 + 调用建议
func TestToolBlockForPrompt_ContainsToolAndGuidance(t *testing.T) {
	block := toolBlockForPrompt(map[string]Tool{
		"investigator_search": &fakeTool{
			name: "investigator_search",
			desc: "派遣调查员进行 web 搜索",
		},
	})
	require.Contains(t, block, "## 工具调用协议")
	require.Contains(t, block, "**investigator_search**")
	require.Contains(t, block, "### 调用建议")
	require.Contains(t, block, "reflect")
	require.Contains(t, block, "tool_call")
	require.Contains(t, block, "speak")
}

// T3: 多个 tool 时按字典序稳定排列
func TestToolBlockForPrompt_StableOrdering(t *testing.T) {
	block := toolBlockForPrompt(map[string]Tool{
		"zeta":  &fakeTool{name: "zeta", desc: "z"},
		"alpha": &fakeTool{name: "alpha", desc: "a"},
		"mu":    &fakeTool{name: "mu", desc: "m"},
	})
	idxAlpha := strings.Index(block, "**alpha**")
	idxMu := strings.Index(block, "**mu**")
	idxZeta := strings.Index(block, "**zeta**")
	require.True(t, idxAlpha >= 0 && idxMu >= 0 && idxZeta >= 0, "所有工具名都应出现")
	require.True(t, idxAlpha < idxMu && idxMu < idxZeta, "应按字典序输出: %d < %d < %d", idxAlpha, idxMu, idxZeta)
}

// T4: baseRules 接受 toolsBlock 后，输出格式段落包含 action/tool/tool_input 字段
func TestBaseRules_ActionFieldInSchema(t *testing.T) {
	out := baseRules("")
	require.Contains(t, out, "\"action\": \"speak\" 或 \"tool_call\" 或 \"reflect\"")
	require.Contains(t, out, "\"tool\":")
	require.Contains(t, out, "\"tool_input\":")
	require.Contains(t, out, "evidence_refs") // 必填字段保留
}

// T4b: v0.5 策略笔记字段必须在 schema 里出现 —— LLM 看不到字段名就不会
// 在 reflect 时填 memory_type / memory_note，MemoryHook 就永远不被触发。
func TestBaseRules_MemoryFieldsInSchema(t *testing.T) {
	out := baseRules("")
	require.Contains(t, out, "memory_type", "baseRules 必须告诉 LLM 可以填 memory_type")
	require.Contains(t, out, "memory_note", "baseRules 必须告诉 LLM 可以填 memory_note")
	require.Contains(t, out, "strategy_note", "必须列出 4 个合法 memory_type 之一")
	require.Contains(t, out, "opponent_weakness")
	require.Contains(t, out, "self_correction")
	require.Contains(t, out, "evidence_eval")
	require.Contains(t, out, "策略笔记", "策略笔记段落必须出现以引导 LLM 主动记笔记")
}

// T5: toolsBlock 非空时被插入到 ## 输出格式 之前
func TestBaseRules_ToolBlockBeforeOutputFormat(t *testing.T) {
	tools := `## 工具调用协议
[X_TOOL_BLOCK_MARKER]
`
	out := baseRules(tools)
	idxMarker := strings.Index(out, "[X_TOOL_BLOCK_MARKER]")
	idxOutput := strings.Index(out, "## 输出格式")
	require.True(t, idxMarker >= 0, "toolsBlock 应被嵌入")
	require.True(t, idxOutput >= 0, "输出格式段落应保留")
	require.True(t, idxMarker < idxOutput, "工具说明必须在「输出格式」之前: marker=%d output=%d", idxMarker, idxOutput)
}

// T6: ProsecutorPrompt / DefenderPrompt 接收 toolsBlock 后将其传给 baseRules
func TestLawyerPrompt_InjectToolsBlock(t *testing.T) {
	session := model.CourtSession{
		Title:   "X 是否更好",
		OptionA: "选 A",
		OptionB: "选 B",
		Context: "背景",
	}
	agent := model.Agent{BeliefA: 0.75, BeliefB: 0.25}

	t.Run("prosecutor 注入工具说明", func(t *testing.T) {
		p := ProsecutorPrompt(agent, session, nil, "## 工具调用协议\n[X_PRO_TOOL]\n")
		require.Contains(t, p, "[X_PRO_TOOL]")
		idx := strings.Index(p, "[X_PRO_TOOL]")
		idxOutput := strings.Index(p, "## 输出格式")
		require.True(t, idx < idxOutput)
	})

	t.Run("defender 注入工具说明", func(t *testing.T) {
		p := DefenderPrompt(agent, session, nil, "## 工具调用协议\n[X_DEF_TOOL]\n")
		require.Contains(t, p, "[X_DEF_TOOL]")
		idx := strings.Index(p, "[X_DEF_TOOL]")
		idxOutput := strings.Index(p, "## 输出格式")
		require.True(t, idx < idxOutput)
	})

	t.Run("toolsBlock 空时不插入工具段落", func(t *testing.T) {
		p := ProsecutorPrompt(agent, session, nil, "")
		require.NotContains(t, p, "## 工具调用协议")
	})

	t.Run("策略段落包含主动搜证提示", func(t *testing.T) {
		p := ProsecutorPrompt(agent, session, nil, "")
		require.Contains(t, p, "investigator_search")
		require.Contains(t, p, "reflect")
	})
}
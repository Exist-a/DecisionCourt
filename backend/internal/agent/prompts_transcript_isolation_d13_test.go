package agent

// v2.11 (deferred D13-A) 回归护栏：庭审转写 → 提示词的**信息隔离不变量**。
//
// 背景（deferred D13 现象 A）："控辩双方互不可见对方推理链"此前有两重保险，但其中
// 一重是**结构性巧合** —— 转写路径恰好只渲染 `Content`（推理链存在
// `model.Message.Metadata` 里），而不是"被设计保证不渲染推理"。另一重
// （a2a.BuildContextView 的剥离投影）在生产代码里根本没有消费者。
//
// 这组测试把"只渲染 Content"从巧合变成显式契约：
//   1. 注入式断言 —— 往 Metadata / A2A payload 里塞**虚构的敏感字段**，断言它
//      不出现在为对手/法官组装的提示词里。（旧测法只能断言"已知字段被删"，
//      测不出**新增**字段泄漏。）
//   2. 等价性断言 —— 只改 Metadata 不影响提示词文本（证明"加固没改行为"）。

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

// 虚构的"未来敏感字段"。真实字段名故意不用（reasoning 是已知的）——
// 重点在于**没人记得把它们加进剥离逻辑**时也不会泄漏。
const (
	fakeSensitiveKey1 = "planned_next_move"
	fakeSensitiveKey2 = "internal_confidence"
	fakeSensitiveVal1 = "下一轮我要突袭汇率风险"
	fakeSensitiveVal2 = "0.93"
)

// messagesWithSensitiveMetadata 造一条普通庭审消息，但 Metadata 里塞满虚构敏感字段
// （模拟"未来有人往 metadata 加了内部字段"）。
func messagesWithSensitiveMetadata() []model.Message {
	return []model.Message{
		{
			Round:      1,
			Phase:      "cross_exam",
			ActionType: "speak",
			Content:    "控方陈述：创业公司期权价值被高估。",
			Metadata: `{"reasoning":"我的真实推理链","` + fakeSensitiveKey1 + `":"` + fakeSensitiveVal1 +
				`","` + fakeSensitiveKey2 + `":` + fakeSensitiveVal2 + `}`,
		},
		{
			Round:      1,
			Phase:      "cross_exam",
			ActionType: "speak",
			Content:    "辩方回应：大厂稳定性同样有价格。",
			Metadata:   `{"reasoning":"对方没算股权稀释"}`,
		},
	}
}

// assertNoSensitiveLeak 断言文本里不含任何虚构敏感字段（键名或值）。
func assertNoSensitiveLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, bad := range []string{fakeSensitiveKey1, fakeSensitiveKey2, fakeSensitiveVal1, fakeSensitiveVal2, "我的真实推理链", "对方没算股权稀释"} {
		if strings.Contains(text, bad) {
			t.Errorf("%s 泄漏了敏感内容 %q —— 转写渲染必须只取 Content/ActionType，"+
				"绝不允许 Metadata 里的字段进入提示词（D13-A）", where, bad)
		}
	}
	// 正文本身必须还在（否则"没泄漏"是因为什么都没渲染）
	if !strings.Contains(text, "控方陈述") {
		t.Errorf("%s 没渲染出正文 —— 隔离不该以丢失合法内容为代价", where)
	}
}

// TestTranscriptIsolation_NoMetadataLeakInPromptBuilders 覆盖所有"吃 messages 的
// 提示词构造器"：法官 / 书记员 / 判决书路径。
func TestTranscriptIsolation_NoMetadataLeakInPromptBuilders(t *testing.T) {
	session := model.CourtSession{
		SessionUUID: "sess-d13",
		Title:       "D13 隔离验证",
		OptionA:     "接受 offer",
		OptionB:     "留在大厂",
		Context:     "背景",
	}
	evidences := []model.Evidence{
		{EvidenceID: "E001", Type: "fact", Content: "offer 文本"},
	}
	msgs := messagesWithSensitiveMetadata()
	jd := JudgeDecision{BeliefA: 0.6, BeliefB: 0.4, Preferred: "option_a", Reasoning: "理由", Recommendation: "建议"}

	t.Run("ClerkPrompt", func(t *testing.T) {
		p, err := ClerkPrompt(session, evidences, msgs)
		require.NoError(t, err)
		assertNoSensitiveLeak(t, "ClerkPrompt", p)
	})
	t.Run("ClerkSummaryPrompt", func(t *testing.T) {
		p, err := ClerkSummaryPrompt(session, evidences, msgs, 1)
		require.NoError(t, err)
		assertNoSensitiveLeak(t, "ClerkSummaryPrompt", p)
	})
	t.Run("JudgePrompt", func(t *testing.T) {
		p, err := JudgePrompt(session, evidences, msgs, 0.5, 0.5)
		require.NoError(t, err)
		assertNoSensitiveLeak(t, "JudgePrompt", p)
	})
	t.Run("JudgeFinalPrompt", func(t *testing.T) {
		p, err := JudgeFinalPrompt(session, evidences, msgs, 0.5, 0.5, "")
		require.NoError(t, err)
		assertNoSensitiveLeak(t, "JudgeFinalPrompt", p)
	})
	t.Run("ClerkPromptWithJudgeDecision", func(t *testing.T) {
		p, err := ClerkPromptWithJudgeDecision(session, evidences, msgs, jd, "")
		require.NoError(t, err)
		assertNoSensitiveLeak(t, "ClerkPromptWithJudgeDecision", p)
	})
}

// TestTranscriptIsolation_MetadataChangeDoesNotAlterPrompt 等价性证明：
// 只改 Metadata（塞敏感字段）不得改变提示词文本 —— 即"加固没有改行为"。
//
// 这是 D13 授权时要求的"改造前后逐字节相同"的可执行版本：与其存 golden 文本，
// 不如断言**唯一的变量（Metadata）不影响输出**，这样它对未来的改动也持续有效。
func TestTranscriptIsolation_MetadataChangeDoesNotAlterPrompt(t *testing.T) {
	session := model.CourtSession{SessionUUID: "s", Title: "t", OptionA: "A", OptionB: "B", Context: "c"}
	evidences := []model.Evidence{{EvidenceID: "E001", Type: "fact", Content: "x"}}

	clean := messagesWithSensitiveMetadata()
	for i := range clean {
		clean[i].Metadata = `{}` // 清空 Metadata，其余字段完全一致
	}
	dirty := messagesWithSensitiveMetadata()

	pClean, err := JudgePrompt(session, evidences, clean, 0.5, 0.5)
	require.NoError(t, err)
	pDirty, err := JudgePrompt(session, evidences, dirty, 0.5, 0.5)
	require.NoError(t, err)

	require.Equal(t, pClean, pDirty,
		"只改 Metadata 不应改变提示词文本 —— 若不同，说明渲染读了 Metadata（隔离被破坏）")
}

// TestTranscriptIsolation_SpeakHistoryOmitsDBMetadata 覆盖**对手 LLM 实际看到的
// 对话历史**（orchestrator.speak 里的 model.Message → llm.Message 转换）。
//
// 该转换会自己构造 Metadata（agent_type / evidence_id 两个标签），绝不能把
// DB 里的 m.Metadata 拷过去（那里有 reasoning）。
func TestTranscriptIsolation_SpeakHistoryOmitsDBMetadata(t *testing.T) {
	msgs := messagesWithSensitiveMetadata()
	llmMsgs := buildLLMHistory(msgs, nil)

	require.Len(t, llmMsgs, len(msgs))
	for i, m := range llmMsgs {
		require.Equal(t, msgs[i].Content, m.Content, "正文必须原样传递")
		for k, v := range m.Metadata {
			if k != "agent_type" && k != "evidence_id" {
				t.Errorf("llm.Message.Metadata 出现了不该有的键 %q=%q —— "+
					"它只能含 agent_type / evidence_id 两个网关标签（D13-A）", k, v)
			}
			for _, bad := range []string{fakeSensitiveKey1, fakeSensitiveKey2, fakeSensitiveVal1, fakeSensitiveVal2} {
				if strings.Contains(v, bad) {
					t.Errorf("Metadata 值泄漏了 %q", bad)
				}
			}
		}
	}
}

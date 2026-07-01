package agent_gateway

import (
	"regexp"
	"strings"

	"github.com/decisioncourt/backend/internal/llm"
)

// MessageScore 给非 system 消息打的"重要性分"。
//
// 设计依据见 .trae/documents/prompt-compression-courtscenario.md §3.3。
// 公式（均贡献权重到 final_score；final = role + position + reference + type_boost）：
//
//	role_weight   按 AgentType 配置（system 2.0 / judge 1.5 / clerk 1.3 / investigator 1.2 / 律师 1.0）
//	position_boost idx 0 / last 各 +0.3，中间 0（"开场陈词" + "最近事实"加权）
//	reference_boost 含 "evidence_" / "@prosecutor" / "@defender" / "@judge" 等 +0.3
//	type_boost    含 tool_call_id 的 assistant +0.5（标记原子组起点）
//
// 注意：分数不乘以 recency decay（与 legacy 的"靠位置淘汰"区分）；
// 我们用"强制保留最近 N 条"来实现"防丢光"。
type MessageScore struct {
	Message        llm.Message
	Index          int
	Score          float64
	RoleWeight     float64
	PositionBoost  float64
	ReferenceBoost float64
	TypeBoost      float64
	KeepReason     string
}

var (
	// evidenceRef 匹配形如 "证据 #3" / "evidence_id=abc" / "evidence_id:xxx"
	evidenceRef = regexp.MustCompile(`(?i)evidence[_ -]?id|证据\s*#`)
	// reference 匹配庭辩中常见的指代
	referenceMarker = regexp.MustCompile(`(?i)@?(prosecutor|defender|judge|clerk|investigator)|刚才|先前|前面(?:提到|说过)|earlier|previously`)
	// toolCallID 匹配 llm.Message.Metadata["tool_call_id"]（通过结构判定即可，无需 regex）
)

// DefaultRoleWeights 是默认角色权重。系统消息在 CompressScored 中被独立保留，
// 此处仍给出默认值便于单元测试。
var DefaultRoleWeights = map[string]float64{
	"system":       2.0,
	"judge":        1.5,
	"clerk":        1.3,
	"investigator": 1.2,
	"prosecutor":   1.0,
	"defender":     1.0,
	// 其他角色或未指定：1.0
}

// ScoreMessages 给一组非 system 消息打分，按 OriginalIdx 升序返回。
//
// bs 暂时未直接参与分数（BudgetSnapshot 已通过外部 "KeepRecentForcedN" 兜底）；
// 保留 bs 参数是为了未来根据 bs.Status 动态调权重（throttle 时更激进保留
// judge 的输出等）。
func ScoreMessages(messages []llm.Message, bs BudgetSnapshot) []MessageScore {
	out := make([]MessageScore, len(messages))
	last := len(messages) - 1
	for i, m := range messages {
		s := MessageScore{Message: m, Index: i}

		s.RoleWeight = roleWeightFor(m)

		// position_boost：首末各 +0.3
		if i == 0 || i == last {
			s.PositionBoost = 0.3
		}

		// reference_boost：含 evidence_id / "@prosecutor" / "@defender" / "刚才" 等
		if evidenceRef.MatchString(m.Content) || referenceMarker.MatchString(m.Content) {
			s.ReferenceBoost = 0.3
		}

		// type_boost：含 tool_call_id 的 assistant
		if _, ok := m.Metadata["tool_call_id"]; ok && m.Role == "assistant" {
			s.TypeBoost = 0.5
		}

		s.Score = s.RoleWeight + s.PositionBoost + s.ReferenceBoost + s.TypeBoost
		out[i] = s
	}
	return out
}

// roleWeightFor 按 role / metadata.agent_type 推导权重。
// 规则：先看 m.Metadata["agent_type"]（更精确），否则按 m.Role 推断。
func roleWeightFor(m llm.Message) float64 {
	if m.Role == "system" {
		return 2.0
	}
	if t, ok := m.Metadata["agent_type"]; ok {
		if w, ok := DefaultRoleWeights[strings.ToLower(t)]; ok {
			return w
		}
	}
	switch m.Role {
	case "assistant":
		return 1.2 // 默认助手回答
	case "user":
		return 1.0 // 默认律师 / 用户
	case "tool":
		return 1.2 // 工具响应
	}
	return 1.0
}

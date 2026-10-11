package agent_gateway

import "github.com/decisioncourt/backend/internal/llm"

// v2.12 (M8-2 第 2 轮)：system 消息的"区块"标记。
//
// 背景：压缩器要么对 system 整条不动（0%），要么超标时从末尾硬截断。根因是
// 一条 system 消息里混了两类语义完全不同的内容：
//
//	[契约] baseRules + 工具协议 + 输出格式  ← 绝不能压（压它 → 模型编造证据）
//	[素材] 庭审历史 / 证据 / 投影区块        ← 应该可压（token 大头）
//
// 于是 buildInitialMessages 把 system 拆成多条，各自用 Metadata[MessageSectionKey]
// 标记。压缩器据此：契约块**永不评分、永不压缩**；素材块**正常参与评分/原子组/贪心**
// （保留最近 N 条素材，旧素材可被丢弃）—— 见 docs/COMPRESSION-REDESIGN-PROPOSAL.md §2.2。
//
// 标记放在 llm.Message.Metadata（不序列化到上游，见 llm.Message 注释）。
// 兼容：未标记的 system 消息一律视作**契约**（保持旧行为：system 强制保留）。
const (
	// MessageSectionKey 是 Metadata 里的区块键。
	MessageSectionKey = "section"
	// MessageSectionContract 标记"契约块"：baseRules + 工具协议，永不压缩。
	MessageSectionContract = "contract"
	// MessageSectionMaterial 标记"素材块"：庭审历史/证据/投影，可参与压缩。
	MessageSectionMaterial = "material"
)

// MarkContract 给契约块消息打标记。
func MarkContract(m llm.Message) llm.Message {
	if m.Metadata == nil {
		m.Metadata = map[string]string{}
	}
	m.Metadata[MessageSectionKey] = MessageSectionContract
	return m
}

// MarkMaterial 给素材块消息打标记。
func MarkMaterial(m llm.Message) llm.Message {
	if m.Metadata == nil {
		m.Metadata = map[string]string{}
	}
	m.Metadata[MessageSectionKey] = MessageSectionMaterial
	return m
}

// IsMaterialMessage 报告 m 是否是"素材块"（可压缩）。
func IsMaterialMessage(m llm.Message) bool {
	return m.Metadata != nil && m.Metadata[MessageSectionKey] == MessageSectionMaterial
}

// IsContractSystemMessage 报告 m 是否是"契约块"——即 system 且**未**标记为素材。
// 未标记的旧式单条 system 也归入此类，从而保持"system 强制保留"的旧行为。
func IsContractSystemMessage(m llm.Message) bool {
	return m.Role == "system" && !IsMaterialMessage(m)
}

// IsSystemMessage 报告 m 是否是 system 角色（契约或素材都算）。
func IsSystemMessage(m llm.Message) bool {
	return m.Role == "system"
}

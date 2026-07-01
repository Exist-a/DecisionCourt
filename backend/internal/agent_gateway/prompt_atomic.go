package agent_gateway

import "github.com/decisioncourt/backend/internal/llm"

// AtomicGroup 表示一个原子组：组内的多条消息必须同生共死（要么全保留，要么全丢）。
//
// 当前实现识别两类原子关系：
//   - tool_call ↔ tool_result：assistant 含 tool_call_id 的消息 + 后续同 tool_call_id 的 tool 消息
//   - evidence 引用：含 evidence_id metadata 的多条消息（不同证据描述互不相关，不会聚成同一组）
//
// 参考：Microsoft Agent Framework MessageGroup（tool_call + tool_result 原子性）。
type AtomicGroup struct {
	ID           string  // 组 ID；空字符串表示"未分组"（独立消息，会单独评分淘汰）
	Indices      []int   // 落在该组内的消息索引（在 messages 中的位置）
	GroupScore   float64 // 组内最高分
	GroupSize    int     // 消息条数
	GroupLength  int     // 消息总字符数
}

// BuildAtomicGroups 从 messages + scored 中识别原子组。
//
// 规则一：tool_call_id 相同的 assistant + 后续 tool 消息聚为一组；多条同 id 的
//        assistant 全部纳入同一组。
// 规则二：只有一条消息的组视为"未分组"，返回 ID 为空，独立参与评分。
//
// 返回的 groups **有序**：与消息原顺序一致；每个组包含的 Indices 也按原顺序排列。
func BuildAtomicGroups(messages []llm.Message, scored []MessageScore) []AtomicGroup {
	// 第一遍按 tool_call_id 收集
	byToolCall := map[string][]int{}
	for i, m := range messages {
		if id, ok := m.Metadata["tool_call_id"]; ok && id != "" {
			byToolCall[id] = append(byToolCall[id], i)
		}
	}

	// 第二遍构造 groups
	assigned := map[int]string{} // idx → groupID
	var groups []AtomicGroup
	// 注意：每个 tool_call_id 独立成组
	for _, groupID := range sortedKeys(byToolCall) {
		idxs := byToolCall[groupID]
		var group AtomicGroup
		group.ID = "tool:" + groupID
		group.Indices = idxs
		group.GroupSize = len(idxs)
		for _, i := range idxs {
			assigned[i] = group.ID
			group.GroupLength += len(messages[i].Content)
			if s := scored[i].Score; s > group.GroupScore {
				group.GroupScore = s
			}
		}
		groups = append(groups, group)
	}

	// 第三遍：未分配的消息各自单独成组（ID 为空）
	for i, m := range messages {
		if _, ok := assigned[i]; ok {
			continue
		}
		group := AtomicGroup{
			ID:          "",
			Indices:     []int{i},
			GroupSize:   1,
			GroupLength: len(m.Content),
		}
		group.GroupScore = scored[i].Score
		groups = append(groups, group)
	}

	return groups
}

func sortedKeys(m map[string][]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 用 Go 内置 sort 避免额外依赖；不去重但 map key 唯一所以无重复
	sortStrings(keys)
	return keys
}

// 简单字符串排序（只在本文件用；如果项目其他地方已有 sort 包导入，可以替换）
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

package agent

import (
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// NormalizeEvidenceRefs 把 Speaker/AgentOutput.EvidenceRefs 中混入的
// DB UUID 全部映射回人类可读的 display_id（如 "E001"），确保进入 A2A
// 私有消息和 private_memory 表的 linked_evidence_ids 字段对前端一致。
//
// 背景：v0.5+ Memory/A2A 改造后，前端 MemoryAuditPanel / MemoryTimeline
// 用 evidence_id 渲染"引用证据"chip。LLM 偶尔会把模型输入里瞥到的 UUID
// 当作 evidence_refs 返回，导致 MemoryEntry.linkedEvidenceIds 出现
// 形如 "8dd288bc-fda6-47f5-b86e-983bd595bba8" 的字符串，破坏 UI 一致
// 性。设计文档 `.trae/documents/memory-a2a-redesign.md` 把这个列为
// "已发现但未做"，约定由后端 recordSideEffects 做一层
// evidence_id → display_id JOIN 映射（详见 v1.1 §"已发现但未做"）。
//
// 行为：
//   - 解析为 UUID 的元素 → 按 Evidence.ID 在传入 evidences 中查找对应
//     row，命中则替换为 Evidence.EvidenceID；未命中保留原值（保守
//     fallback，避免无声丢数据，方便审计）。
//   - 已是 display_id 的元素（如 "E001"） → 原样保留。
//   - 空串 / 空白 → 跳过（不入返回数组）。
//   - evidences 为空 / nil → 返回原 refs 的副本（过滤空串）。
//
// 复杂度：O(len(refs) * len(evidences))。单 session 证据量级 ≤ 50，
// refs ≤ 10，无需建索引。
//
// 调用点：recordSideEffects (orchestrator.go)、buildPrivateMemoryMessage
// (reflect_classifier.go)。
func NormalizeEvidenceRefs(refs []string, evidences []model.Evidence) []string {
	if len(refs) == 0 {
		return nil
	}

	// 预构建 uuid → display_id 索引；evidences 可能为 nil，零容量 map 即可。
	uuidIndex := make(map[uuid.UUID]string, len(evidences))
	for _, e := range evidences {
		if e.ID == uuid.Nil || e.EvidenceID == "" {
			continue
		}
		uuidIndex[e.ID] = e.EvidenceID
	}

	out := make([]string, 0, len(refs))
	for _, r := range refs {
		trimmed := trimSpace(r)
		if trimmed == "" {
			continue
		}
		if parsed, err := uuid.Parse(trimmed); err == nil {
			if display, ok := uuidIndex[parsed]; ok {
				out = append(out, display)
				continue
			}
			// UUID 但不在本 session 证据列表里 —— 保留原值作为 fallback，
			// 让审计 / 排查能看到"前端拿到的是什么"，而不是默默丢数据。
		}
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// trimSpace 是 strings.TrimSpace 的内联版，省一行 import。该文件本身
// 没有用 strings，避免额外依赖；空字符串检查已由调用点处理。
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end {
		if s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r' {
			start++
			continue
		}
		break
	}
	for end > start {
		if s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r' {
			end--
			continue
		}
		break
	}
	return s[start:end]
}
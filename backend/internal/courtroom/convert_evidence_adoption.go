package courtroom

import (
	"fmt"

	"github.com/decisioncourt/backend/internal/model"
)

// v2.9 PR-4 (ADR 0043) §2.1 — LLM "evidence_adoption" 字段 → model.EvidenceAdoptionEntry[]
//
// ClerkAgent 在 verdict 时输出 evidence_adoption 数组 (per "原则 5"). 我们:
//
//   1. 校验每个 entry 必填字段 (evidence_id / display_id / status)
//   2. 转 model.EvidenceAdoptionEntry
//   3. 失败 (LLM 输出坏) 不 panic, 让 caller 落 BuildAdoptionSummary 兜底值
//
// 这是有限信任 LLM 输出, 不存任何对系统状态有副作用的副作用.
func convertToEvidenceAdoptionEntries(raw []interface{}) ([]model.EvidenceAdoptionEntry, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty array")
	}
	out := make([]model.EvidenceAdoptionEntry, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("entry %d is not a JSON object (got %T)", i, item)
		}
		entry := model.EvidenceAdoptionEntry{}
		if v, ok := m["evidence_id"].(string); ok {
			entry.EvidenceID = v
		}
		if v, ok := m["display_id"].(string); ok {
			entry.DisplayID = v
		}
		if v, ok := m["status"].(string); ok {
			entry.Status = v
		}
		// weight_applied 兼容 number / string (LLM 偶尔输出 "1.0" 而非 1.0)
		switch w := m["weight_applied"].(type) {
		case float64:
			entry.WeightApplied = w
		case string:
			var f float64
			if _, err := fmt.Sscanf(w, "%f", &f); err == nil {
				entry.WeightApplied = f
			}
		}
		if v, ok := m["reason"].(string); ok {
			entry.Reason = v
		}
		// 必填校验
		if entry.EvidenceID == "" || entry.DisplayID == "" || entry.Status == "" {
			return nil, fmt.Errorf("entry %d missing required fields", i)
		}
		out = append(out, entry)
	}
	return out, nil
}

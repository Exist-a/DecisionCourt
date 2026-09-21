package courtroom

import (
	"fmt"
	"regexp"

	"github.com/decisioncourt/backend/internal/model"
)

// v2.10 ADR 0044 #7: 压缩后判决质量回环度量。
//
// 背景：v2.3 (ADR 0037) 给 agent_gateway 加的埋点全是操作型指标（压缩比 /
// 耗时 / token 用量），回答不了"压缩是否影响了判决质量"。本文件补一个质量
// 指标：判决书正文引用到的证据 display id，有多少能在本次庭审记录里找到源头。
//
// 语义：
//   - 1.0  → 判决书引用的每个证据都真实存在（无凭空引用）
//   - <1.0 → 出现判决书引用但庭审里不存在的证据，说明压缩丢了证据链，
//     或 LLM 产生了幻觉（ADR 0015 / 0021 的同类风险）
//
// 只在"判决书确实引用了证据"时才计算；纯 prose 判决书返回 ok=false，避免把
// "没引用"误记为"准确率 0"。

// evidenceDisplayID 匹配判决书里的证据 display id。
//
// 证据在 prompt / 判决书里以 "E001" 形式出现（见 evidence/service.go 的
// fmt.Sprintf("E%03d", count+1)），LLM 也可能写成 "E1" / "E-001" 等变体。
// 前缀 \b 且限定最多 3 位数字，避免把 "E1234"、"3e5" 这类串误判成证据引用
// （"3e5" 的 e 前面是数字，不构成词边界）。
var evidenceDisplayID = regexp.MustCompile(`(?i)\bE-?0*(\d{1,3})\b`)

// ExtractEvidenceDisplayIDs 从判决书文本中提取所有引用的证据 display id，
// 归一化为 "E%03d" 形式并按首次出现顺序去重返回。
//
// 纯函数（无 I/O），便于单测。
func ExtractEvidenceDisplayIDs(content string) []string {
	matches := evidenceDisplayID.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
			continue
		}
		id := fmt.Sprintf("E%03d", n)
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// ComputeVerdictEvidenceAccuracy 计算判决书证据引用准确率。
//
// 返回 (ratio, ok)：
//   - ok=false → 判决书没有引用任何证据，或庭审本身没有证据，指标不适用
//   - ok=true  → ratio ∈ [0,1]，引用命中数 / 引用总数
//
// 纯函数（无 I/O），便于单测。
func ComputeVerdictEvidenceAccuracy(verdictContent string, evidences []model.Evidence) (float64, bool) {
	referenced := ExtractEvidenceDisplayIDs(verdictContent)
	if len(referenced) == 0 {
		return 0, false
	}
	if len(evidences) == 0 {
		// 判决书引用了证据，但庭审一条证据都没有 —— 全部属于凭空引用。
		return 0, true
	}

	known := make(map[string]bool, len(evidences))
	for _, e := range evidences {
		known[normalizeDisplayID(e.EvidenceID)] = true
	}

	hits := 0
	for _, id := range referenced {
		if known[id] {
			hits++
		}
	}
	return float64(hits) / float64(len(referenced)), true
}

// normalizeDisplayID 把库里的 evidence_id 归一化成与 ExtractEvidenceDisplayIDs
// 同构的 "E%03d"，保证两边比较不会因为零填充差异误判。
// 无法解析的（例如库里的值本身就是 UUID）原样返回，此时不会命中引用。
func normalizeDisplayID(raw string) string {
	m := evidenceDisplayID.FindStringSubmatch(raw)
	if len(m) < 2 {
		return raw
	}
	var n int
	if _, err := fmt.Sscanf(m[1], "%d", &n); err != nil {
		return raw
	}
	return fmt.Sprintf("E%03d", n)
}

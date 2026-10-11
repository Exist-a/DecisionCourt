package agent

import (
	"regexp"
	"sort"
	"strings"

	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
)

// 主张级接地检查（docs/AGENT-GROUNDING-ANALYSIS.md §3 P1）。
//
// 现状（P1 之前）：四道输出护栏（反幻觉正则 / 新意度 Jaccard / 立场 judge /
// 已反驳证据）全部是**模式级**，没有任何一道在判断"这句话有没有依据"。本文件补
// 上第五道：把发言里的事实性主张抽出来，逐条检查能否在**提供给模型的素材**里找到
// 支撑。
//
// 纪律（关键）：**只标注、不硬拒**。文档明确要求"先标记 + 度量，再决定是否硬拒"
// —— 避免重演"护栏把输出推向模糊"的副作用（见 §2.3 恶性循环）。

// ClaimKind* 是主张类别标签（作为 metric 的 label 值）。
const (
	ClaimKindNumber   = "number"      // 数字 / 百分比 / 金额
	ClaimKindCaseNo   = "case_number" // 案号
	ClaimKindEvidence = "evidence_id" // 证据编号（E001 等）
)

// GroundingClaim 是一条被抽取出来的事实性主张。
type GroundingClaim struct {
	Text      string `json:"text"`
	Kind      string `json:"kind"`
	Supported bool   `json:"supported"`
}

var (
	gEvidenceRe  = regexp.MustCompile(`\bE[0-9]{3}\b`)
	gPercentRe   = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?%`)
	gMoneyRe     = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?\s*(?:万元|亿元|万美元|万|亿|元|美元)`)
	gCaseNoRe    = regexp.MustCompile(`[0-9]{4}[\p{Han}]{1,6}[0-9]{1,6}号`)
	gBigNumberRe = regexp.MustCompile(`[0-9][0-9,]{2,}(?:\.[0-9]+)?`)
)

// ExtractFactualClaims 从发言正文抽取可判定的事实性主张：数字 / 百分比 / 金额 /
// 案号 / 证据编号。重叠匹配按"起点靠前、同起点更长者优先"去重，避免同一处被重复
// 计数（如 "41%" 不再同时算成 "41"）。
func ExtractFactualClaims(content string) []GroundingClaim {
	if content == "" {
		return nil
	}
	type span struct {
		start, end int
		kind       string
	}
	var spans []span
	add := func(re *regexp.Regexp, kind string) {
		for _, m := range re.FindAllStringIndex(content, -1) {
			spans = append(spans, span{m[0], m[1], kind})
		}
	}
	// 添加顺序 = 优先级（证据编号最具体，先抽）。
	add(gEvidenceRe, ClaimKindEvidence)
	add(gPercentRe, ClaimKindNumber)
	add(gMoneyRe, ClaimKindNumber)
	add(gCaseNoRe, ClaimKindCaseNo)
	add(gBigNumberRe, ClaimKindNumber)

	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return (spans[i].end - spans[i].start) > (spans[j].end - spans[j].start)
	})

	var claims []GroundingClaim
	lastEnd := -1
	for _, s := range spans {
		if s.start < lastEnd {
			continue // 与前一条重叠（被更长者覆盖）
		}
		claims = append(claims, GroundingClaim{Text: content[s.start:s.end], Kind: s.kind})
		lastEnd = s.end
	}
	return claims
}

// CheckGrounding 逐条判定主张能否在 materials 中找到支撑，返回未获支撑的主张与
// 主张总数。materials 应是**实际喂给该次生成的全部素材**（证据正文 / 调查发现 /
// 庭审历史 / 案件背景）。
func CheckGrounding(content, materials string) (unsupported []GroundingClaim, total int) {
	claims := ExtractFactualClaims(content)
	if len(claims) == 0 {
		return nil, 0
	}
	runs := digitRuns(materials)
	for i := range claims {
		c := &claims[i]
		var supported bool
		if c.Kind == ClaimKindEvidence {
			// 证据编号按字面量精确匹配（E001）。
			supported = strings.Contains(materials, c.Text)
		} else {
			// 数字类：只认"独立数字 run"。**不**回退到子串包含 ——
			// "150" 是 "4150" 的子串，字面包含会把跨边界拼接误判为有据。
			// 带单位的写法（"45 万" vs 素材 "45万"）由 run 匹配覆盖。
			key := digitsOnly(c.Text)
			supported = key != "" && runs[key]
		}
		c.Supported = supported
		if !supported {
			unsupported = append(unsupported, *c)
		}
	}
	return unsupported, len(claims)
}

// BuildGroundingMaterials 把"本次生成可见的全部素材"拼成一个可检索的语料，供
// CheckGrounding 使用。与 buildSpeakMaterials 同源（顺序略有不同，不影响判定）。
func BuildGroundingMaterials(
	session model.CourtSession,
	evidences []model.Evidence,
	findings []investigation.Finding,
	messages []model.Message,
) string {
	var b strings.Builder
	if ctx, err := buildContextSafe(session, evidences); err == nil {
		b.WriteString(ctx)
	}
	b.WriteString(buildInvestigationContext(session, findings))
	for _, m := range messages {
		if line, err := renderTranscriptLine(m, historyLineMaxRunes); err == nil {
			b.WriteString(line)
		}
	}
	return b.String()
}

// digitsOnly 保留数字与小数点，去掉千分位/单位/空白等。
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// digitRuns 把文本切成"独立数字 run"集合。按 run 而不是整串子串匹配，避免
// "150" 被素材里的 "4150" 误判为有据（跨边界数字拼接会造假命中）。
func digitRuns(s string) map[string]bool {
	out := map[string]bool{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out[cur.String()] = true
			cur.Reset()
		}
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' || r == ',' {
			if r == ',' {
				continue // 千分位分隔符不切断 run（1,000 → 1000）
			}
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

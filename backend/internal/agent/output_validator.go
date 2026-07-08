package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// v0.10.1 (ADR 0021) 输出幻觉硬编码验证器。
//
// 问题背景:
//   baseRules 第 4/5/13/14 条虽然禁止编造证据细节 / 引用不存在的证据 / 虚构对方论点,
//   但实测 LLM 在 stress 下会违反(典型 hallucination 模式)。仅靠 prompt 约束不够,
//   必须有 post-processing 层的硬性 reject + retry。
//
// 设计原则:
//   - **evidence_refs 为空时**: 禁止任何"具体证据引用"模式,因为没有合法证据可引
//   - **evidence_refs 非空时**: 引用的 E00X 编号必须能在 allowlist 里找到
//   - 数字 / 案号 / 金额模式: 在没有 evidence 支撑时一律 reject(LLM 倾向"凑具体"
//     让论证看起来实,但这些是幻觉高发模式)
//   - 不强制 100% reject(避免误伤),只硬拒"明显是幻觉"的模式
//
// 接入位置:
//   validateSpeak() 现有逻辑后追加(react_runner.go:657)。Reject 触发
//   react_runner.go:385 的 retry 机制,LLM 看到 error 后重新生成。
//
// 测试覆盖:
//   output_validator_test.go 包含所有模式的单元测试。
//   跑 N 次真实庭审(cross_exam 阶段)对比修复前/后 hallucination 概率。

// HallucinationMode 标识一类幻觉模式,便于日志聚合分析
type HallucinationMode string

const (
	ModeEmptyEvidenceRef   HallucinationMode = "evidence_ref_empty_with_citation"  // evidence_refs 空但文本引用"证据N"
	ModeEmptyEvidenceStats HallucinationMode = "evidence_ref_empty_with_stats"     // evidence_refs 空但文本含百分比/金额
	ModeEmptyEvidenceCase  HallucinationMode = "evidence_ref_empty_with_case_num"  // evidence_refs 空但文本含案号
	ModeInvalidEvidenceID  HallucinationMode = "evidence_ref_invalid_id"           // evidence_refs 含 E00X 但不在 allowlist
	// v0.10.1 第二轮修复:
	ModeCaseNumAlways  HallucinationMode = "case_num_always_forbidden"  // 不管 evidence_refs,任何案号模式一律 reject(LLM 倾向编造)
	ModeUnverifiedIDs  HallucinationMode = "evidence_ref_unverified"   // allowedIDs 未知但 evidence_refs 非空(无法验证,保守 reject)
)

// 精确数字百分比: 15.3% / 20% / 0.5%
// 注意: 不匹配 "100%" 这种 case context 里可能有的(由 evidence 提供的合法数据)
var percentRegex = regexp.MustCompile(`\d+(?:\.\d+)?\s*%`)

// 中国法院案号: (2022)京01民终1234号 / (2021)最高法民终5678号
var caseNumberRegex = regexp.MustCompile(`[（(]\d{4}[）)]\S{0,15}\d+号`)

// 证据引用: 证据三 / 证据五 / 证据7 / 附件3 / 附件 12
var evidenceRefRegex = regexp.MustCompile(`证据\s*[一二三四五六七八九十\d]+|附件\s*\d+`)

// 金额: 月薪8000元 / 损失48000元 / 2.3亿元 / 营收1200万
// 匹配"X元 / X万元 / X亿元 / X块钱"
var moneyRegex = regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:元|万元|亿元|块钱|百万元)`)

// evidence_id 引用模式: E00X 或 E00X,E00Y 格式
var evidenceIDRegex = regexp.MustCompile(`\bE\d{3,4}\b`)

// ValidationResult 描述一次输出验证的结果
type ValidationResult struct {
	OK       bool               // true 表示无问题
	Issues   []ValidationIssue  // 严重问题(必须 reject)
	Warnings []ValidationIssue  // 警告(只记 audit,不 reject)
}

// ValidationIssue 单个问题
type ValidationIssue struct {
	Mode    HallucinationMode
	Pattern string  // 匹配到的具体内容(便于 debug)
	Reason  string  // 给 LLM retry 时看的解释
}

// ValidateAgainstHallucination 验证 LLM 输出是否有 hallucination 模式。
//
// 参数:
//   - content:        LLM 输出的发言文本
//   - evidenceRefs:   LLM 声明引用的证据 ID 列表 (如 ["E001","E002"])
//   - allowedIDs:     当前案件实际存在的证据 ID 集合 (从数据库读)
//
// 返回 ValidationResult;Issues 非空时 caller 应该 reject 让 LLM 重生成。
func ValidateAgainstHallucination(content string, evidenceRefs []string, allowedIDs []string) ValidationResult {
	var issues []ValidationIssue

	// 预处理
	content = strings.TrimSpace(content)
	if content == "" {
		// 空 content 由 validateSpeak 单独处理,这里不重复
		return ValidationResult{OK: true}
	}

	hasEvidence := len(evidenceRefs) > 0
	hasAllowedEvidence := len(allowedIDs) > 0

	// ========== Layer A: evidence_refs 空时的硬约束 ==========
	if !hasEvidence {
		// A1: 禁止引用"证据N" / "附件N"(没证据可引)
		if matches := evidenceRefRegex.FindAllString(content, -1); len(matches) > 0 {
			issues = append(issues, ValidationIssue{
				Mode:    ModeEmptyEvidenceRef,
				Pattern: strings.Join(matches, ","),
				Reason:  "evidence_refs 为空数组,但发言中引用了具体证据编号。请删除证据引用或提供 evidence_refs。",
			})
		}

		// A3: 禁止含百分比数字(没证据支撑的具体百分比通常是 LLM 凑的)
		if matches := percentRegex.FindAllString(content, -1); len(matches) > 0 {
			issues = append(issues, ValidationIssue{
				Mode:    ModeEmptyEvidenceStats,
				Pattern: strings.Join(matches, ","),
				Reason:  "evidence_refs 为空数组,但发言中包含具体百分比。如果没有证据支撑,请改用'行业数据'等定性描述,不要虚构精确数字。",
			})
		}

		// A4: 禁止具体金额(月薪/损失/赔偿等)
		if matches := moneyRegex.FindAllString(content, -1); len(matches) > 0 {
			issues = append(issues, ValidationIssue{
				Mode:    ModeEmptyEvidenceStats,
				Pattern: strings.Join(matches, ","),
				Reason:  "evidence_refs 为空数组,但发言中包含具体金额。请改用'损失金额较大'等定性描述,不要虚构精确数字。",
			})
		}
	}

	// ========== Layer C: 无条件硬约束(不管 evidence_refs 状态) ==========
	// v0.10.1 第二轮修复:实测发现即使 evidence_refs 非空,LLM 仍会在 content 里
	// 编造"最高人民法院第17号指导案例" / "（2022）京03民终4567号"等具体案号。
	// 案号是 hallucination 高发模式,一律 reject。
	if matches := caseNumberRegex.FindAllString(content, -1); len(matches) > 0 {
		issues = append(issues, ValidationIssue{
			Mode:    ModeCaseNumAlways,
			Pattern: strings.Join(matches, ","),
			Reason:  "发言中包含法院案号,LLM 倾向于编造具体案号让论证'看起来实'。请改用'参考类似判例'等模糊表述,或让用户提供具体案号。",
		})
	}

	// ========== Layer B: evidence_refs 非空时的硬约束 ==========
	if hasEvidence && !hasAllowedEvidence {
		// v0.10.1 第二轮修复:Runner struct 还没存 session evidence 列表,
		// 没法验证 evidence_refs 是否真实存在。保守起见拒绝所有,
		// 防止 LLM 编造 UUID / E00X ID。等 v0.11 RunnerConfig 加
		// AllowedEvidenceIDs 字段后做正向校验。
		issues = append(issues, ValidationIssue{
			Mode:    ModeUnverifiedIDs,
			Pattern: strings.Join(evidenceRefs, ","),
			Reason:  "evidence_refs 非空但后端无法验证其真实性(尚未接入 session evidence 列表),保守拒绝。请改用定性引用或撤回 evidence_refs。",
		})
	} else if hasEvidence && hasAllowedEvidence {
		// B1: evidence_refs 里所有 ID 必须在 allowedIDs 出现
		allowedSet := make(map[string]struct{}, len(allowedIDs))
		for _, id := range allowedIDs {
			allowedSet[id] = struct{}{}
		}
		for _, ref := range evidenceRefs {
			if _, ok := allowedSet[ref]; !ok {
				issues = append(issues, ValidationIssue{
					Mode:    ModeInvalidEvidenceID,
					Pattern: ref,
					Reason:  fmt.Sprintf("evidence_refs 包含 %q,但当前证据列表中没有该 ID(只允许 %v)", ref, allowedIDs),
				})
			}
		}
	}

	// 构建结果
	res := ValidationResult{
		OK:     len(issues) == 0,
		Issues: issues,
	}
	if !res.OK {
		res.Warnings = append(res.Warnings, issues...)
	}
	return res
}

// FormatValidationIssuesForRetry 把 issues 格式化成给 LLM 看的 prompt 补充文本。
// 当 retry 时,把这段话附在用户消息里,告诉 LLM "上一轮为什么被 reject"。
func FormatValidationIssuesForRetry(issues []ValidationIssue) string {
	if len(issues) == 0 {
		return ""
	}
	parts := make([]string, 0, len(issues)+1)
	parts = append(parts, "你上一轮输出包含幻觉(编造内容),必须修正:")
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("- 模式: %s, 匹配: [%s], 原因: %s", issue.Mode, issue.Pattern, issue.Reason))
	}
	parts = append(parts, "请删除所有虚构的数字/案号/证据引用,只基于用户提供的案情上下文 + 真实证据(如有)发言。")
	return strings.Join(parts, "\n")
}
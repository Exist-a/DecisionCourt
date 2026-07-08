package agent

import (
	"strings"
	"testing"
)

// TestValidateAgainstHallucination_Cases 覆盖三类高频幻觉模式。
// TDD: 先写测试,确保 validator 按预期 reject / accept。
func TestValidateAgainstHallucination_Cases(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		evidenceRefs []string
		allowedIDs   []string
		wantOK       bool
		wantModes    []HallucinationMode
	}{
		// ============== 修复前 prosecutor 实际输出(用户庭审) ==============
		{
			name: "evidence_refs 空但引用证据三/五 + 案号 + 百分比(经典用户 bug)",
			content: "我方提交的证据三显示,选项A的实施方案在过去三年内为企业带来年均15%的复合增长率," +
				"远高于行业平均水平的8%。同时,证据五的专家评估报告指出,未来五年可降低运营成本约20%。" +
				"参考(2022)京01民终1234号判决。",
			evidenceRefs: []string{},
			wantOK:       false,
			wantModes:    []HallucinationMode{ModeEmptyEvidenceRef, ModeEmptyEvidenceStats, ModeCaseNumAlways},
		},

		// ============== v0.10.1 第二轮:案号模式无条件 reject ==============
		{
			name: "evidence_refs 非空 + 仍含案号 → 仍要 reject(v0.10.1 加严)",
			content: "我方证据显示,被告构成虚假宣传。参考最高人民法院第17号指导案例" +
				"及(2022)京03民终4567号判决,法院支持全额退款。",
			evidenceRefs: []string{"some-real-evidence-id"},
			wantOK:       false,
			wantModes:    []HallucinationMode{ModeCaseNumAlways, ModeUnverifiedIDs},
		},

		// ============== v0.10.1 第二轮:evidence_refs 非空但无验证 → reject ==============
		{
			name: "evidence_refs 非空 + 无 allowedEvidenceIDs(默认 nil) → reject",
			content: "根据相关证据,被告存在违约行为。",
			evidenceRefs: []string{"fake-uuid-xxx"},
			wantOK:       false,
			wantModes:    []HallucinationMode{ModeUnverifiedIDs},
		},

		// ============== 修复前 defender 实际输出 ==============
		{
			name: "evidence_refs 空但引用附件3 + 月薪8000元 + 百分比",
			content: "我方提交的附件3中,第三方审计报告指出选项B的违约概率低于0.5%," +
				"远低于行业基准5%。对方月薪8000元,机会成本48000元。",
			evidenceRefs: []string{},
			wantOK:       false,
			wantModes:    []HallucinationMode{ModeEmptyEvidenceRef, ModeEmptyEvidenceStats},
		},

		// ============== evidence_refs 空但内容"合格"(只定性) ==============
		{
			name:         "evidence_refs 空但只有定性表述 → 应该 pass",
			content:      "对方主张继续教育有普遍价值,但忽略了本案具体情境。我方认为,应在个案基础上评估培训课程的适用性,而非依赖宏观数据。",
			evidenceRefs: []string{},
			wantOK:       true,
		},

		// ============== evidence_refs 非空时引用真实 E00X(合法,需提供 allowedIDs) ==============
		{
			name:         "evidence_refs 非空 + allowedIDs 包含 → pass",
			content:      "根据证据显示,原告月薪为 8000 元,这是 user 在案情中明确提到的数字。",
			evidenceRefs: []string{"E001"},
			allowedIDs:   []string{"E001"},
			wantOK:       true,
		},

		// ============== content 含"证据"但是否定句("无证据"/"未提交证据")→ 应 pass ==============
		{
			// 注释: 严格匹配 "证据N/附件N" 编号,不匹配"证据"单字。"未提交证据"这类文字会被误判吗?
			// 答: 不会。我们的 regex 是 `证据[一二三四五六七八九十\d]+|附件\s*\d+`,
			// "未提交证据" 后面没数字/中文数字,不命中。
			name:         "否定句'无证据可提交' → 应该 pass(无数字触发)",
			content:      "目前无证据可提交,只能基于案情本身进行客观分析。",
			evidenceRefs: []string{},
			wantOK:       true,
		},

		// ============== 边界: 数字 0 / 100 是否被误判 ==============
		{
			// "0.5%" 含 "0.5" + "%" 触发 percentRegex → 应该 reject
			// 这是预期行为: 0.5% 也是"具体百分比",LLM 不应自己造
			name:         "evidence_refs 空 + 0.5% 也算具体百分比",
			content:      "我方观点的可信度低于 0.5% 的误差。",
			evidenceRefs: []string{},
			wantOK:       false,
			wantModes:    []HallucinationMode{ModeEmptyEvidenceStats},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateAgainstHallucination(tt.content, tt.evidenceRefs, tt.allowedIDs)
			if got.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v; issues = %+v", got.OK, tt.wantOK, got.Issues)
			}
			if !tt.wantOK {
				// 验证至少匹配了预期的 mode
				gotModes := make(map[HallucinationMode]bool)
				for _, issue := range got.Issues {
					gotModes[issue.Mode] = true
				}
				for _, wantMode := range tt.wantModes {
					if !gotModes[wantMode] {
						t.Errorf("missing mode %s in issues: %+v", wantMode, got.Issues)
					}
				}
			}
		})
	}
}

// TestFormatValidationIssuesForRetry 验证 retry message 包含模式 + 解释
func TestFormatValidationIssuesForRetry(t *testing.T) {
	issues := []ValidationIssue{
		{Mode: ModeEmptyEvidenceRef, Pattern: "证据三,证据五", Reason: "test reason"},
	}
	got := FormatValidationIssuesForRetry(issues)

	if !strings.Contains(got, "证据三") {
		t.Errorf("retry message should contain pattern: %s", got)
	}
	if !strings.Contains(got, "test reason") {
		t.Errorf("retry message should contain reason: %s", got)
	}
	if !strings.Contains(got, "证据引用") && !strings.Contains(got, "evidence_refs") {
		t.Errorf("retry message should mention evidence references: %s", got)
	}
}

// TestValidateAgainstHallucination_EmptyContent 当 content 为空时不应触发任何规则
// (空 content 由 validateSpeak 单独处理)
func TestValidateAgainstHallucination_EmptyContent(t *testing.T) {
	got := ValidateAgainstHallucination("", []string{}, nil)
	if !got.OK {
		t.Errorf("empty content should pass hallucination check: %+v", got.Issues)
	}
}
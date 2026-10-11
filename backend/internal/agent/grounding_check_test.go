package agent

import (
	"testing"

	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/stretchr/testify/require"
)

func TestExtractFactualClaims_KindsAndNoOverlap(t *testing.T) {
	content := "单体架构年均故障 4 次、累计中断 150 分钟；同业成本下降 18%；E001 显示 12 家样本。"
	claims := ExtractFactualClaims(content)

	byText := map[string]string{}
	for _, c := range claims {
		byText[c.Text] = c.Kind
	}
	require.Equal(t, ClaimKindEvidence, byText["E001"])
	require.Equal(t, ClaimKindNumber, byText["150"])
	require.Equal(t, ClaimKindNumber, byText["18%"])

	// "18%" 不得再被拆成 "18" 重复计数（重叠去重）。
	for _, c := range claims {
		require.NotEqual(t, "18", c.Text, "重叠匹配必须去重：18%% 不应再产出 18")
	}
}

func TestCheckGrounding_SupportedVsUnsupported(t *testing.T) {
	materials := "证据 E001：单体年均故障 4 次、累计中断 150 分钟；同业成本下降 18%。"
	// 发言复述了 150 与 18%，但编造了 99%。
	content := "本院认为累计中断 150 分钟，成本下降 18%，可用性达 99%。"

	unsupported, total := CheckGrounding(content, materials)
	require.Equal(t, 3, total, "应抽出 150 / 18% / 99% 三条数字主张")
	require.Len(t, unsupported, 1)
	require.Equal(t, "99%", unsupported[0].Text)
	require.Equal(t, ClaimKindNumber, unsupported[0].Kind)
}

func TestCheckGrounding_NoFalsePositiveFromDigitRunConcat(t *testing.T) {
	// 素材里只有 "4150"，不应把发言里的 "150" 判为有据（跨边界数字拼接不得算命中）。
	materials := "总里程 4150 公里"
	unsupported, total := CheckGrounding("中断 150 分钟", materials)
	require.Equal(t, 1, total)
	require.Len(t, unsupported, 1, "150 不得被 4150 的子串误判为有据")
}

func TestCheckGrounding_EvidenceIDLiteral(t *testing.T) {
	materials := "证据 E001：用户陈述。"
	unsupported, _ := CheckGrounding("依据 E001 与 E009 判断。", materials)
	require.Len(t, unsupported, 1)
	require.Equal(t, "E009", unsupported[0].Text)
	require.Equal(t, ClaimKindEvidence, unsupported[0].Kind)
}

func TestCheckGrounding_NoClaims_ReturnsEmpty(t *testing.T) {
	unsupported, total := CheckGrounding("本院认为应当谨慎权衡双方论点。", "任意素材")
	require.Equal(t, 0, total)
	require.Empty(t, unsupported)
}

func TestBuildGroundingMaterials_IncludesEvidenceFindingsTranscript(t *testing.T) {
	session := model.CourtSession{Title: "拆不拆", OptionA: "拆", OptionB: "不拆", Context: "背景"}
	evidences := []model.Evidence{{EvidenceID: "E001", Source: "user", Type: "document", Content: "EVIDENCE-NUM-82万"}}
	findings := []investigation.Finding{{Investigator: "inv", Query: "q", Summary: "FINDING-NUM-41%", ResultCount: 1}}
	messages := []model.Message{{ActionType: "speak", Content: "TRANSCRIPT-NUM-450"}}

	got := BuildGroundingMaterials(session, evidences, findings, messages)
	require.Contains(t, got, "EVIDENCE-NUM-82万")
	require.Contains(t, got, "FINDING-NUM-41%")
	require.Contains(t, got, "TRANSCRIPT-NUM-450")
}

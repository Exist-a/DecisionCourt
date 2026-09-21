package courtroom

import (
	"testing"

	"github.com/decisioncourt/backend/internal/model"
)

// v2.10 ADR 0044 #7: 从判决书文本提取证据 display id。
func TestExtractEvidenceDisplayIDs(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "canonical form",
			content: "根据 E001 与 E002 的记载，本院认为……",
			want:    []string{"E001", "E002"},
		},
		{
			name:    "unpadded variants",
			content: "证据 E1 与 E-002 相互矛盾",
			want:    []string{"E001", "E002"},
		},
		{
			name:    "dedup preserves first-seen order",
			content: "E002 支持原告；E001 支持被告；E002 再次出现",
			want:    []string{"E002", "E001"},
		},
		{
			name:    "no evidence refs",
			content: "本院基于双方陈述的自由心证作出判断。",
			want:    nil,
		},
		{
			name:    "empty",
			content: "",
			want:    nil,
		},
		{
			name:    "lowercase accepted",
			content: "参见 e003 的结论",
			want:    []string{"E003"},
		},
		{
			name:    "four-digit not treated as display id",
			content: "编号 E1234 不是证据",
			want:    nil,
		},
		{
			name:    "scientific notation not matched",
			content: "误差为 3e5 量级",
			want:    nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractEvidenceDisplayIDs(tc.content)
			if len(got) != len(tc.want) {
				t.Fatalf("want %v got %v", tc.want, got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("idx %d: want %s got %s (full: %v)", i, tc.want[i], got[i], got)
				}
			}
		})
	}
}

// v2.10 ADR 0044 #7: 准确率计算 —— 全部命中 / 部分命中 / 全不命中。
func TestComputeVerdictEvidenceAccuracy(t *testing.T) {
	evidences := []model.Evidence{
		{EvidenceID: "E001"},
		{EvidenceID: "E002"},
	}

	cases := []struct {
		name    string
		content string
		want    float64
		wantOK  bool
	}{
		{
			name:    "all referenced ids exist",
			content: "依据 E001 与 E002，本院认定……",
			want:    1.0,
			wantOK:  true,
		},
		{
			name:    "half referenced ids exist",
			content: "依据 E001 与 E009，本院认定……",
			want:    0.5,
			wantOK:  true,
		},
		{
			name:    "no referenced id exists (hallucination)",
			content: "依据 E101 与 E202，本院认定……",
			want:    0.0,
			wantOK:  true,
		},
		{
			name:    "no refs at all — not applicable",
			content: "本院基于庭审整体印象作出判断。",
			want:    0,
			wantOK:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ComputeVerdictEvidenceAccuracy(tc.content, evidences)
			if ok != tc.wantOK {
				t.Fatalf("ok: want %v got %v", tc.wantOK, ok)
			}
			if !tc.wantOK {
				return
			}
			if diff := got - tc.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("ratio: want %.4f got %.4f", tc.want, got)
			}
		})
	}
}

// v2.10 ADR 0044 #7: 判决书引用了证据但庭审没有证据 → 全部凭空引用，ratio=0 且 ok=true。
func TestComputeVerdictEvidenceAccuracy_NoEvidencesInTrial(t *testing.T) {
	got, ok := ComputeVerdictEvidenceAccuracy("依据 E001 认定", nil)
	if !ok {
		t.Fatal("should be applicable when verdict references evidence")
	}
	if got != 0 {
		t.Errorf("want 0 (all references are fabricated) got %f", got)
	}
}

// v2.10 ADR 0044 #7: 库里的 evidence_id 零填充差异不应导致误判。
func TestComputeVerdictEvidenceAccuracy_ZeroPaddingInsensitive(t *testing.T) {
	// 库里存 "E1"（未填充），判决书引用 "E001"
	evidences := []model.Evidence{{EvidenceID: "E1"}}
	got, ok := ComputeVerdictEvidenceAccuracy("依据 E001 认定", evidences)
	if !ok {
		t.Fatal("should be applicable")
	}
	if got != 1.0 {
		t.Errorf("zero-padding variants should match, want 1.0 got %f", got)
	}
}

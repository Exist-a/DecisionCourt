package agent

import (
	"reflect"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// fixedUUIDs 让测试用例用可读的常量代替裸 uuid 字面量；视觉上更接近
// 设计文档示例，也方便 diff 检视。
var (
	uuidE001 = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	uuidE002 = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	uuidE003 = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	uuidGhost = uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
)

func sampleEvidences() []model.Evidence {
	return []model.Evidence{
		{ID: uuidE001, EvidenceID: "E001"},
		{ID: uuidE002, EvidenceID: "E002"},
		{ID: uuidE003, EvidenceID: "E003"},
	}
}

func TestNormalizeEvidenceRefs_AllDisplayIDs_Passthrough(t *testing.T) {
	refs := []string{"E001", "E002"}
	got := NormalizeEvidenceRefs(refs, sampleEvidences())
	want := []string{"E001", "E002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("display_id inputs should pass through; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_AllUUIDs_MappedToDisplayIDs(t *testing.T) {
	// 模拟 LLM 错误地把 evidence 行的 DB UUID 当 evidence_refs 返回。
	refs := []string{uuidE001.String(), uuidE002.String()}
	got := NormalizeEvidenceRefs(refs, sampleEvidences())
	want := []string{"E001", "E002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UUID inputs should be mapped to display_id; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_MixedDisplayIDAndUUID(t *testing.T) {
	// 实际场景中 LLM 可能已经学会了 display_id，但偶尔混入一行 UUID。
	refs := []string{"E001", uuidE002.String(), "E003"}
	got := NormalizeEvidenceRefs(refs, sampleEvidences())
	want := []string{"E001", "E002", "E003"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mixed inputs should normalize element-wise; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_UnknownUUID_FallsBackToOriginal(t *testing.T) {
	// UUID 但不在当前 session 证据列表里 —— 保留原值，方便审计看到
	// 真实流出数据，而不是默默丢。
	refs := []string{uuidGhost.String(), "E002"}
	got := NormalizeEvidenceRefs(refs, sampleEvidences())
	want := []string{uuidGhost.String(), "E002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown UUID should pass through verbatim; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_DropsEmptyAndWhitespace(t *testing.T) {
	refs := []string{"", "  ", "E001", "\t\n"}
	got := NormalizeEvidenceRefs(refs, sampleEvidences())
	want := []string{"E001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blank entries should be dropped; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_EmptyInputs(t *testing.T) {
	if got := NormalizeEvidenceRefs(nil, sampleEvidences()); got != nil {
		t.Fatalf("nil refs should return nil; got=%v", got)
	}
	if got := NormalizeEvidenceRefs([]string{}, sampleEvidences()); got != nil {
		t.Fatalf("empty refs should return nil; got=%v", got)
	}
	// 全空串最终也是 nil
	if got := NormalizeEvidenceRefs([]string{"", "  "}, sampleEvidences()); got != nil {
		t.Fatalf("all-blank refs should return nil; got=%v", got)
	}
}

func TestNormalizeEvidenceRefs_NilEvidences_PassesThrough(t *testing.T) {
	// evidences 为 nil 时退化为"原 refs 过滤空串"。这是 legacy 调用方
	// 的兼容行为 —— 不传 evidences 时不能把已有 display_id 误转成 nil。
	refs := []string{"E001", "E002"}
	got := NormalizeEvidenceRefs(refs, nil)
	want := []string{"E001", "E002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("nil evidences should still pass display_id through; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_StableOrdering(t *testing.T) {
	// 顺序敏感：策略笔记按 LLM 返回顺序展示，乱序会让前端 chip 闪。
	refs := []string{uuidE003.String(), "E001", uuidE002.String()}
	got := NormalizeEvidenceRefs(refs, sampleEvidences())
	want := []string{"E003", "E001", "E002"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output order should match input order; got=%v want=%v", got, want)
	}
}

func TestNormalizeEvidenceRefs_SkipsZeroUUIDOrEmptyDisplayIDRows(t *testing.T) {
	// 防御性：evidences 里可能混入 ID=uuid.Nil 或 EvidenceID="" 的脏行，
	// 不应污染索引。
	refs := []string{uuidE001.String()}
	evs := []model.Evidence{
		{ID: uuid.Nil, EvidenceID: "EGHOST"},
		{ID: uuidE001, EvidenceID: "E001"},
		{ID: uuidE002, EvidenceID: ""}, // 脏行
	}
	got := NormalizeEvidenceRefs(refs, evs)
	want := []string{"E001"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dirty rows should be ignored; got=%v want=%v", got, want)
	}
}
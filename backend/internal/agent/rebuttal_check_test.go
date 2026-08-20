package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// fakeRebuttalRepo implements RebuttalRepository for unit tests.
// standingSet 是人为配置的 standing 状态 evidence display_ids。
type fakeRebuttalRepo struct {
	standingSet []string
	listErr     error
	insertErr   error
	insertCalls []insertCall
}

type insertCall struct {
	sessionID, aggressor, rebuttedID string
	strength                          float64
	rationale                         string
}

func (f *fakeRebuttalRepo) ListStandingRebuttedIDs(_ context.Context, _ string, _ []string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.standingSet, nil
}

func (f *fakeRebuttalRepo) Insert(_ context.Context, sessionID, aggressor, rebuttedID string, strength float64, rationale string) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.insertCalls = append(f.insertCalls, insertCall{sessionID, aggressor, rebuttedID, strength, rationale})
	return nil
}

// TestApplySpeakerRebuttalCheck_EmptyEvidenceRefs 验证空 EvidenceRefs 直接 pass
// (避免每次发言都查 DB)。
func TestApplySpeakerRebuttalCheck_EmptyEvidenceRefs(t *testing.T) {
	repo := &fakeRebuttalRepo{standingSet: []string{"E001"}}
	speaker := Speaker{Content: "我的发言", EvidenceRefs: nil}
	got, err := applySpeakerRebuttalCheck(context.Background(), speaker, "session-1", repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("空 EvidenceRefs 应返回 nil, got %v", got)
	}
}

// TestApplySpeakerRebuttalCheck_NilRepo 验证 nil repo 直接 pass (向后兼容)。
func TestApplySpeakerRebuttalCheck_NilRepo(t *testing.T) {
	speaker := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001"}}
	got, err := applySpeakerRebuttalCheck(context.Background(), speaker, "session-1", nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("nil repo 应返回 nil, got %v", got)
	}
}

// TestApplySpeakerRebuttalCheck_NoStandingRebutted 验证 repo 返回空集合时直接 pass。
func TestApplySpeakerRebuttalCheck_NoStandingRebutted(t *testing.T) {
	repo := &fakeRebuttalRepo{standingSet: nil}
	speaker := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001", "E002"}}
	got, err := applySpeakerRebuttalCheck(context.Background(), speaker, "session-1", repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != nil {
		t.Errorf("无 standing rebuttal 应返回 nil, got %v", got)
	}
}

// TestApplySpeakerRebuttalCheck_Intersect 验证 Speaker.EvidenceRefs 与 standing
// 集合求交集,返回违规 evidence IDs。
func TestApplySpeakerRebuttalCheck_Intersect(t *testing.T) {
	repo := &fakeRebuttalRepo{standingSet: []string{"E001", "E003", "E005"}}
	speaker := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001", "E002", "E003", "E004"}}
	got, err := applySpeakerRebuttalCheck(context.Background(), speaker, "session-1", repo)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// 期望违规 = {E001, E003} (∩ standing ∩ EvidenceRefs)
	want := map[string]bool{"E001": true, "E003": true}
	if len(got) != len(want) {
		t.Errorf("violated len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("unexpected violation: %q", v)
		}
	}
}

// TestApplySpeakerRebuttalCheck_RepoError 验证 repo 错误时透传。
func TestApplySpeakerRebuttalCheck_RepoError(t *testing.T) {
	repo := &fakeRebuttalRepo{listErr: errors.New("db down")}
	speaker := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001"}}
	_, err := applySpeakerRebuttalCheck(context.Background(), speaker, "session-1", repo)
	if err == nil {
		t.Error("expected error from repo, got nil")
	}
}

// TestApplySpeakerRebuttalRetryLoop_NilRepo_NoOp 验证 nil repo 跳过 check (向后兼容)。
func TestApplySpeakerRebuttalRetryLoop_NilRepo_NoOp(t *testing.T) {
	out := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001"}}
	got, _ := applySpeakerRebuttalRetryLoop(out, &ReActRunner{}, context.Background(), nil, "sess-1", nil)
	if got.RebuttalRejected {
		t.Error("nil repo 不应触发 RebuttalRejected")
	}
	if len(got.RebuttalViolations) > 0 {
		t.Errorf("nil repo 不应有 violations, got %v", got.RebuttalViolations)
	}
}

// TestApplySpeakerRebuttalRetryLoop_PassOnFirstTry 验证无 standing rebuttal 时
// 直接返回 (不 retry, 不标 rejected)。
func TestApplySpeakerRebuttalRetryLoop_PassOnFirstTry(t *testing.T) {
	repo := &fakeRebuttalRepo{standingSet: nil}
	r := &ReActRunner{
		llm: &countingLLM{responses: []string{}},
		cfg: RunnerConfig{},
	}
	out := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001"}}
	got, _ := applySpeakerRebuttalRetryLoop(out, r, context.Background(), nil, "sess-1", repo)
	if got.RebuttalRejected {
		t.Error("无 standing 不应 rejected")
	}
}

// TestApplySpeakerRebuttalRetryLoop_FailThenFallback 验证 2 次 retry 后仍违规
// 时 fallback 标记 RebuttalRejected=true + RebuttalViolations=违规 IDs。
func TestApplySpeakerRebuttalRetryLoop_FailThenFallback(t *testing.T) {
	repo := &fakeRebuttalRepo{standingSet: []string{"E001"}}
	// 给 LLM 3 个无效 JSON 触发 fallback (与 novelty 测试对齐模式)
	r := &ReActRunner{
		llm: &countingLLM{responses: []string{
			"not valid json",
			"not valid json",
			"not valid json",
		}},
		cfg: RunnerConfig{},
	}
	out := Speaker{Content: "我的发言", EvidenceRefs: []string{"E001"}}
	got, _ := applySpeakerRebuttalRetryLoop(out, r, context.Background(), nil, "sess-1", repo)
	if !got.RebuttalRejected {
		t.Error("2 次 retry 后仍违规, 应标记 RebuttalRejected=true")
	}
	if len(got.RebuttalViolations) == 0 || got.RebuttalViolations[0] != "E001" {
		t.Errorf("RebuttalViolations 应含 E001, got %v", got.RebuttalViolations)
	}
}

// countingLLM 返回预设的 response (用于 retry 路径触发),与 novelty 测试共用模式。
type countingLLM struct {
	responses []string
	calls     int
}

func (c *countingLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	if c.calls >= len(c.responses) {
		return "", llm.Usage{}, errors.New("no more responses")
	}
	resp := c.responses[c.calls]
	c.calls++
	return resp, llm.Usage{}, nil
}

func (c *countingLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	close(out)
	return out
}

// Compile-time 接口断言
var _ llm.Client = (*countingLLM)(nil)
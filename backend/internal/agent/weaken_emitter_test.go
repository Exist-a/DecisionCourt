package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// fakeWeakenSink captures every Insert call for assertion in tests. It
// implements agent.WeakenSink (which already matches belief.WeakenRepository
// structurally) so we can drive EmitWeakenFromOutput without a real DB.
type fakeWeakenSink struct {
	rows       []model.EvidenceWeakenLink
	insertErr  error
}

func (f *fakeWeakenSink) Insert(_ context.Context, link model.EvidenceWeakenLink) (model.EvidenceWeakenLink, error) {
	if f.insertErr != nil {
		return model.EvidenceWeakenLink{}, f.insertErr
	}
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	f.rows = append(f.rows, link)
	return link, nil
}

// fakeResolver maps "E001" → knownEvidenceID and returns false otherwise.
// Mirrors the production EvidenceIDByDisplayID contract.
type fakeResolver struct {
	known map[string]uuid.UUID
	err   error
}

func (f fakeResolver) EvidenceIDByDisplayID(_ context.Context, _ uuid.UUID, displayID string) (uuid.UUID, bool) {
	if f.err != nil {
		return uuid.Nil, false
	}
	id, ok := f.known[displayID]
	return id, ok
}

// TestValidWeakenDeclarations_DropsInvalid exercises the validator's three
// rejection branches: empty id, unknown target, out-of-range strength.
func TestValidWeakenDeclarations_DropsInvalid(t *testing.T) {
	out := AgentOutput{
		Weaken: []WeakenDeclaration{
			{EvidenceID: "", Target: "prosecutor", Strength: 0.4},               // empty id
			{EvidenceID: "E001", Target: "investigator", Strength: 0.4},        // bad target
			{EvidenceID: "E001", Target: "prosecutor", Strength: 0},            // zero strength
			{EvidenceID: "E001", Target: "prosecutor", Strength: 1.5},          // too strong
			{EvidenceID: "E001", Target: "defender", Strength: 0.5},            // ok
			{EvidenceID: "E002", Target: "prosecutor", Strength: 0.3, Rationale: "ok"}, // ok
		},
	}
	got := out.ValidWeakenDeclarations()
	if len(got) != 2 {
		t.Fatalf("expected 2 valid declarations got %d", len(got))
	}
	if got[0].EvidenceID != "E001" || got[0].Target != "defender" {
		t.Fatalf("first valid entry wrong: %+v", got[0])
	}
}

// TestEmitWeakenFromOutput_WritesRows covers the happy-path: a single valid
// declaration triggers one Insert, fields carried through verbatim.
func TestEmitWeakenFromOutput_WritesRows(t *testing.T) {
	sink := &fakeWeakenSink{}
	resolver := fakeResolver{known: map[string]uuid.UUID{
		"E001": uuid.MustParse("11111111-1111-1111-1111-111111111111"),
	}}
	out := AgentOutput{
		Weaken: []WeakenDeclaration{
			{EvidenceID: "E001", Target: "prosecutor", Strength: 0.5, Rationale: "data source"},
		},
	}
	meta := MemoryMeta{SessionID: uuid.New(), AgentType: "defender", Round: 1}
	err := EmitWeakenFromOutput(context.Background(), sink, resolver, meta, out)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(sink.rows) != 1 {
		t.Fatalf("expected 1 row got %d", len(sink.rows))
	}
	row := sink.rows[0]
	if row.EvidenceID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("evidence_id not resolved: %s", row.EvidenceID)
	}
	if row.TargetAgent != model.AgentProsecutor {
		t.Fatalf("target not passed through: %s", row.TargetAgent)
	}
	if row.AggressorAgent != "defender" {
		t.Fatalf("aggressor_agent not passed through: %s", row.AggressorAgent)
	}
	if row.WeakenStrength != 0.5 {
		t.Fatalf("expected strength carried through; got %.3f", row.WeakenStrength)
	}
	if row.Rationale != "data source" {
		t.Fatalf("rationale not carried: %q", row.Rationale)
	}
	if row.ID == uuid.Nil {
		t.Fatalf("expected ID to be auto-assigned by sink")
	}
}

// TestEmitWeakenFromOutput_StrengthOutOfRangeRejected is paired with the
// strength-clamp defensive path: invalid strengths are filtered out by
// isValidWeaken BEFORE we ever reach clampStrength, so 0 / negative values
// must NOT produce rows.
func TestEmitWeakenFromOutput_StrengthOutOfRangeRejected(t *testing.T) {
	sink := &fakeWeakenSink{}
	resolver := fakeResolver{known: map[string]uuid.UUID{"E001": uuid.New()}}
	out := AgentOutput{
		Weaken: []WeakenDeclaration{
			{EvidenceID: "E001", Target: "prosecutor", Strength: -0.2}, // invalid
			{EvidenceID: "E001", Target: "prosecutor", Strength: 1.5},  // invalid
		},
	}
	if err := EmitWeakenFromOutput(context.Background(), sink, resolver, MemoryMeta{}, out); err != nil {
		t.Fatal(err)
	}
	if len(sink.rows) != 0 {
		t.Fatalf("expected 0 rows after validation rejected both; got %d", len(sink.rows))
	}
}

// TestEmitWeakenFromOutput_SkipsUnknownEvidence ensures unresolved
// display_ids are silently dropped (NOT persisted as zero-UUID rows).
func TestEmitWeakenFromOutput_SkipsUnknownEvidence(t *testing.T) {
	sink := &fakeWeakenSink{}
	resolver := fakeResolver{known: map[string]uuid.UUID{
		"E001": uuid.MustParse("11111111-1111-1111-1111-111111111111"),
	}}
	out := AgentOutput{
		Weaken: []WeakenDeclaration{
			{EvidenceID: "E001", Target: "prosecutor", Strength: 0.4}, // known
			{EvidenceID: "E999", Target: "prosecutor", Strength: 0.4}, // unknown
		},
	}
	meta := MemoryMeta{AgentType: "defender"}
	if err := EmitWeakenFromOutput(context.Background(), sink, resolver, meta, out); err != nil {
		t.Fatal(err)
	}
	if len(sink.rows) != 1 {
		t.Fatalf("expected exactly 1 row (the known evidence) got %d", len(sink.rows))
	}
}

// TestEmitWeakenFromOutput_NilSinkAndResolver verifies the no-op shortcut:
// when either dependency is nil the call is a clean pass through (no
// panic, no error). Mirrors the pre-v0.6 safe default.
func TestEmitWeakenFromOutput_NilSinkAndResolver(t *testing.T) {
	out := AgentOutput{
		Weaken: []WeakenDeclaration{{EvidenceID: "E001", Target: "prosecutor", Strength: 0.4}},
	}
	if err := EmitWeakenFromOutput(context.Background(), nil, nil, MemoryMeta{}, out); err != nil {
		t.Fatalf("nil sink+resolver should be no-op, got: %v", err)
	}
}

// TestEmitWeakenFromOutput_NilRepo confirms a nil repo on its own (resolver
// present) is also a no-op so callers wiring pieces incrementally work.
func TestEmitWeakenFromOutput_NilRepo(t *testing.T) {
	resolver := fakeResolver{known: map[string]uuid.UUID{"E001": uuid.New()}}
	out := AgentOutput{
		Weaken: []WeakenDeclaration{{EvidenceID: "E001", Target: "prosecutor", Strength: 0.4}},
	}
	if err := EmitWeakenFromOutput(context.Background(), nil, resolver, MemoryMeta{}, out); err != nil {
		t.Fatalf("nil repo should be no-op, got: %v", err)
	}
}

// TestEmitWeakenFromOutput_PropagatesRepoError confirms the call surfaces a
// sink failure so the runner can log it; we don't currently fail the trial
// but the error must not be swallowed silently.
func TestEmitWeakenFromOutput_PropagatesRepoError(t *testing.T) {
	sink := &fakeWeakenSink{insertErr: errInsertFailed}
	resolver := fakeResolver{known: map[string]uuid.UUID{"E001": uuid.New()}}
	out := AgentOutput{
		Weaken: []WeakenDeclaration{{EvidenceID: "E001", Target: "prosecutor", Strength: 0.4}},
	}
	err := EmitWeakenFromOutput(context.Background(), sink, resolver, MemoryMeta{}, out)
	if err == nil {
		t.Fatal("expected Insert error to be propagated")
	}
	if !strings.Contains(err.Error(), "insert failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestEmitWeakenFromOutput_AcceptsBeliefWeakenRepository confirms the
// adapter (MapToWeakenRepository) wires up cleanly so production wiring
// can pass a *belief.GormWeakenRepository straight through. Uses the in-
// memory repo so we don't need a real DB.
func TestEmitWeakenFromOutput_AcceptsBeliefWeakenRepository(t *testing.T) {
	beliefRepo := belief.NewInMemoryWeakenRepository(nil)
	resolver := fakeResolver{known: map[string]uuid.UUID{"E001": uuid.New()}}
	out := AgentOutput{
		Weaken: []WeakenDeclaration{{EvidenceID: "E001", Target: "prosecutor", Strength: 0.3, Rationale: "audit"}},
	}
	sink := MapToWeakenRepository(beliefRepo)
	if sink == nil {
		t.Fatal("adapter returned nil for non-nil repo")
	}
	err := EmitWeakenFromOutput(context.Background(), sink, resolver, MemoryMeta{}, out)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := beliefRepo.ListBySession(context.Background(), uuid.Nil)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row written to belief repo, got %d", len(rows))
	}
}

// errInsertFailed is a sentinel error used to verify the emit path
// propagates repo errors unchanged.
var errInsertFailed = testErr("insert failed")

type testErr string

func (e testErr) Error() string { return string(e) }

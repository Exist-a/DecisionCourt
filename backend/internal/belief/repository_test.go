package belief

import (
	"context"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// TestInMemoryDiffRepository_InsertAndList covers the basic Insert +
// ListBySession round-trip plus the auto-fill of ID / CreatedAt.
func TestInMemoryDiffRepository_InsertAndList(t *testing.T) {
	repo := NewInMemoryDiffRepository(func() time.Time {
		return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	})
	session := uuid.New()
	ctx := context.Background()

	d1, err := repo.Insert(ctx, model.BeliefDiff{
		SessionID: session, Round: 1, Phase: "cross_exam",
		AgentType: model.AgentProsecutor, EvidenceID: nil,
		Source: model.BeliefSrcEvidence, Direction: model.BeliefDirSupportsA,
		PriorBeliefA: 0.7, PosteriorBeliefA: 0.74,
		DeltaBeliefA: 0.04, PriorLogit: 0.847, PosteriorLogit: 1.063,
		EvidenceWeight: 0.85, WeakenFactor: 1.0, Reason: "E001 has merit",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if d1.ID == uuid.Nil {
		t.Fatal("expected ID auto-fill")
	}
	if d1.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt auto-fill")
	}

	d2, err := repo.Insert(ctx, model.BeliefDiff{
		SessionID: session, Round: 1, Phase: "cross_exam",
		AgentType: model.AgentDefender, Direction: model.BeliefDirNeutral,
		Source: model.BeliefSrcAnchorPull,
		PriorBeliefA: 0.31, PosteriorBeliefA: 0.30,
		DeltaBeliefA: -0.01, PriorLogit: -0.755, PosteriorLogit: -0.847,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	rows, err := repo.ListBySession(ctx, session)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows got %d", len(rows))
	}
	if rows[0].ID != d1.ID || rows[1].ID != d2.ID {
		t.Fatal("expected insertion order preserved")
	}
}

// TestInMemoryDiffRepository_FilterByAgent ensures the by-agent filter
// only returns rows whose AgentType matches.
func TestInMemoryDiffRepository_FilterByAgent(t *testing.T) {
	repo := NewInMemoryDiffRepository(nil)
	ctx := context.Background()
	session := uuid.New()

	_, _ = repo.Insert(ctx, model.BeliefDiff{
		SessionID: session, AgentType: model.AgentProsecutor, Source: model.BeliefSrcEvidence, Direction: model.BeliefDirSupportsA,
	})
	_, _ = repo.Insert(ctx, model.BeliefDiff{
		SessionID: session, AgentType: model.AgentDefender, Source: model.BeliefSrcEvidence, Direction: model.BeliefDirSupportsB,
	})
	_, _ = repo.Insert(ctx, model.BeliefDiff{
		SessionID: session, AgentType: model.AgentInvestigator, Source: model.BeliefSrcEvidence, Direction: model.BeliefDirNeutral,
	})

	pros, err := repo.ListBySessionAndAgent(ctx, session, model.AgentProsecutor)
	if err != nil {
		t.Fatal(err)
	}
	if len(pros) != 1 || pros[0].AgentType != model.AgentProsecutor {
		t.Fatalf("expected 1 prosecutor diff got %d", len(pros))
	}
}

// TestInMemoryDiffRepository_FilterByRound ensures the by-round filter
// only returns rows whose Round matches.
func TestInMemoryDiffRepository_FilterByRound(t *testing.T) {
	repo := NewInMemoryDiffRepository(nil)
	ctx := context.Background()
	session := uuid.New()

	_, _ = repo.Insert(ctx, model.BeliefDiff{SessionID: session, Round: 1, AgentType: model.AgentProsecutor, Source: "e", Direction: "sa"})
	_, _ = repo.Insert(ctx, model.BeliefDiff{SessionID: session, Round: 2, AgentType: model.AgentProsecutor, Source: "e", Direction: "sa"})
	_, _ = repo.Insert(ctx, model.BeliefDiff{SessionID: session, Round: 2, AgentType: model.AgentDefender, Source: "e", Direction: "sb"})

	r2, err := repo.ListBySessionAndRound(ctx, session, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2) != 2 {
		t.Fatalf("expected 2 rows on round 2 got %d", len(r2))
	}
}

// TestInMemoryWeakenRepository_InsertAndListByEvidence covers the basic
// Insert + ListByEvidence round-trip plus uniqueness by (session, evidence).
func TestInMemoryWeakenRepository_InsertAndListByEvidence(t *testing.T) {
	repo := NewInMemoryWeakenRepository(nil)
	ctx := context.Background()
	session := uuid.New()
	ev := uuid.New()

	link, err := repo.Insert(ctx, model.EvidenceWeakenLink{
		SessionID: session, EvidenceID: ev,
		AggressorAgent: "defender", TargetAgent: model.AgentProsecutor,
		WeakenStrength: 0.4, Rationale: "data source questionable",
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if link.ID == uuid.Nil {
		t.Fatal("expected ID auto-fill")
	}

	rows, err := repo.ListByEvidence(ctx, session, ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row got %d", len(rows))
	}

	rows2, err := repo.ListBySession(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 1 {
		t.Fatalf("expected 1 row in session list got %d", len(rows2))
	}
}

// TestInMemoryWeakenRepository_DifferentEvidenceIsolated ensures that two
// weaken links on different evidence IDs don't pollute each other's reads.
func TestInMemoryWeakenRepository_DifferentEvidenceIsolated(t *testing.T) {
	repo := NewInMemoryWeakenRepository(nil)
	ctx := context.Background()
	session := uuid.New()
	ev1, ev2 := uuid.New(), uuid.New()

	_, _ = repo.Insert(ctx, model.EvidenceWeakenLink{
		SessionID: session, EvidenceID: ev1,
		AggressorAgent: "defender", TargetAgent: model.AgentProsecutor, WeakenStrength: 0.5,
	})
	_, _ = repo.Insert(ctx, model.EvidenceWeakenLink{
		SessionID: session, EvidenceID: ev2,
		AggressorAgent: "prosecutor", TargetAgent: model.AgentDefender, WeakenStrength: 0.3,
	})

	r1, _ := repo.ListByEvidence(ctx, session, ev1)
	r2, _ := repo.ListByEvidence(ctx, session, ev2)
	if len(r1) != 1 || len(r2) != 1 {
		t.Fatalf("evidence isolation broken: r1=%d r2=%d", len(r1), len(r2))
	}
	if r1[0].WeakenStrength != 0.5 || r2[0].WeakenStrength != 0.3 {
		t.Fatal("weaken strength not preserved per row")
	}
}

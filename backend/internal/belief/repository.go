// Package belief implements the v0.6 Bayesian-log-odds belief engine plus
// the BeliefDiff audit trail and weakening-edge store.
//
// Public surface area:
//   - Engine: pure-function belief update + convergence check
//   - DiffRepository: persists BeliefDiff rows for audit / replay / UI
//   - WeakenRepository: persists EvidenceWeakenLink rows for query access
//
// Both repositories have two implementations (GORM production, in-memory
// test) following the project's existing separation convention.
package belief

import (
	"context"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// DiffRepository persists the per-event belief diffs so the frontend can
// stream a BeliefDiff timeline and an offline replayer can reconstruct any
// session's trajectory logit-by-logit.
//
// Methods are additive and idempotent at the application layer (Insert is
// only called from the engine's Update method, which produces one diff per
// trigger). All read methods return rows ordered by CreatedAt ASC.
type DiffRepository interface {
	// Insert stores a new diff row. Caller may leave ID / CreatedAt unset.
	Insert(ctx context.Context, diff model.BeliefDiff) (model.BeliefDiff, error)

	// ListBySession returns every diff for a session, ordered by CreatedAt asc.
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]model.BeliefDiff, error)

	// ListBySessionAndAgent returns diffs filtered to a single agent_type,
	// still ordered by CreatedAt asc.
	ListBySessionAndAgent(ctx context.Context, sessionID uuid.UUID, agentType model.AgentType) ([]model.BeliefDiff, error)

	// ListBySessionAndRound returns diffs limited to one round (most recent
	// round-driven UI views use this to avoid loading the full history).
	ListBySessionAndRound(ctx context.Context, sessionID uuid.UUID, round int) ([]model.BeliefDiff, error)
}

// WeakenRepository persists weaken-edge declarations. The belief engine
// reads all rows for a (session, evidence) pair when computing the per-agent
// effective weight.
type WeakenRepository interface {
	// Insert stores one weakening declaration. The application layer is
	// responsible for clamping WeakenStrength to [0,1] before Insert.
	Insert(ctx context.Context, link model.EvidenceWeakenLink) (model.EvidenceWeakenLink, error)

	// ListByEvidence returns every weaken declaration targeting one piece of
	// evidence. Used by the belief engine to compute the effective
	// multiplier before applying impact.
	ListByEvidence(ctx context.Context, sessionID, evidenceID uuid.UUID) ([]model.EvidenceWeakenLink, error)

	// ListBySession returns every weaken declaration ever made in a session,
	// for the audit-trail UI on the verdict page.
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]model.EvidenceWeakenLink, error)
}

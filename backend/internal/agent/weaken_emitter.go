package agent

import (
	"context"
	"strings"

	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// WeakenHook is the seam between the ReActRunner (LLM-driven loop) and the
// belief package's WeakenRepository. Whenever a reflect (or speak) step
// emits one or more valid WeakenDeclarations the runner invokes the hook
// with the AgentOutput so the orchestrator can persist them as rows in
// evidence_weaken_links (which UpdateWithDiff reads to attenuate impact).
//
// The hook runs synchronously inside the ReAct loop. Implementations MUST
// be fast (< 10ms) and MUST NOT mutate the AgentOutput. Errors are
// returned but the runner does NOT fail the trial — it logs and continues.
//
// May be nil; a nil hook simply means "don't persist weaken declarations",
// which is the safe default for callers that don't yet wire belief v0.6.
type WeakenHook func(ctx context.Context, out AgentOutput, meta MemoryMeta) error

// WeakenSink is the contract the hook implementation must satisfy.
// *belief.GormWeakenRepository already implements it via its Insert signature.
type WeakenSink interface {
	Insert(ctx context.Context, link model.EvidenceWeakenLink) (model.EvidenceWeakenLink, error)
}

// EvidenceResolver maps an LLM-supplied *display_id* (e.g. "E001") to the
// underlying evidence row. The hook needs this so the runner can keep
// speaking in human-friendly identifiers and we still write foreign keys to
// the canonical UUIDs.
//
// Implementations are expected to be O(n) over the session's evidence list
// (≤ 50 rows per session in practice); no indexing required.
type EvidenceResolver interface {
	EvidenceIDByDisplayID(ctx context.Context, sessionID uuid.UUID, displayID string) (uuid.UUID, bool)
}

// EmitWeakenFromOutput persists every valid WeakenDeclaration emitted by
// an Agent. Returns nil if out has no valid declarations. Returns an error
// only if a write fails; the runner logs + continues so a transient DB
// hiccup never aborts a trial.
//
// The function is exported so the orchestrator can call it directly when it
// wants to drive weaken persistence outside the runner (e.g. after a
// tool_call step that emits a weaken declaration as a side-effect).
func EmitWeakenFromOutput(
	ctx context.Context,
	repo WeakenSink,
	resolver EvidenceResolver,
	meta MemoryMeta,
	out AgentOutput,
) error {
	if repo == nil || resolver == nil {
		return nil
	}
	if !out.HasWeaken() {
		return nil
	}

	for _, decl := range out.ValidWeakenDeclarations() {
		evidenceID, ok := resolver.EvidenceIDByDisplayID(ctx, meta.SessionID, decl.EvidenceID)
		if !ok {
			// EvidenceDisplayID doesn't resolve — silently skip rather than
			// fail the trial. The runner will log the missing-reference so
			// the operator can audit.
			continue
		}

		link := model.EvidenceWeakenLink{
			SessionID:      meta.SessionID,
			EvidenceID:     evidenceID,
			AggressorAgent: strings.TrimSpace(meta.AgentType),
			TargetAgent:    model.AgentType(strings.TrimSpace(decl.Target)),
			WeakenStrength: clampStrength(decl.Strength),
			Rationale:      strings.TrimSpace(decl.Rationale),
		}
		// AggressorMsg stays nil for now (v0.6 doesn't thread the message
		// UUID through MemoryMeta; that lands in BE-4 when we add the WS
		// events).
		if _, err := repo.Insert(ctx, link); err != nil {
			return err
		}
	}
	return nil
}

// clampStrength normalizes a Weaken strength into the [0.01, 1.0] open
// interval. 0 becomes 0.01 (a hairline weaken still useful for the audit
// trail); > 1 is capped at 1.0. Negative input is treated as 0.
func clampStrength(s float64) float64 {
	if s <= 0 {
		return 0.01
	}
	if s > 1 {
		return 1
	}
	return s
}

// MapToWeakenRepository adapts a belief.WeakenRepository so it satisfies the
// WeakenSink interface declared above. Kept separate from the persist path
// so we don't import belief from agent at construction sites.
func MapToWeakenRepository(repo belief.WeakenRepository) WeakenSink {
	if repo == nil {
		return nil
	}
	return wekSinkAdapter{repo: repo}
}

type wekSinkAdapter struct {
	repo belief.WeakenRepository
}

func (a wekSinkAdapter) Insert(ctx context.Context, link model.EvidenceWeakenLink) (model.EvidenceWeakenLink, error) {
	return a.repo.Insert(ctx, link)
}

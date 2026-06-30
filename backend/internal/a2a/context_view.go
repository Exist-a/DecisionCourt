// Package a2a context_view implements the LLM context projection layer.
//
// As described in PRD §4.5 and the v0.5 memory-a2a-redesign document, every
// Agent's LLM prompt must be assembled through BuildContextView so that:
//
//  1. Each Agent only sees its own private memory (no cross-agent leakage).
//  2. Each Agent never sees the opposing side's `reasoning` field, even on
//     public messages — the SanitizedPayload() projection strips it.
//  3. The Clerk explicitly opts OUT of any private memory read.
//
// The view is consumed by Orchestrator right before each LLM call; see
// internal/agent/orchestrator.go (v0.5 PR 3 will wire it in).
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// LLMContext is the projected, per-Agent context that the Orchestrator hands
// to the LLM at the start of each speak. It is intentionally narrow: anything
// not in this struct is not in the prompt.
type LLMContext struct {
	// WorkingMemory holds the public messages as seen by selfAgent. Messages
	// originating from a *different* agent (i.e. the opposing side) have had
	// their `reasoning` field stripped via SanitizedPayload. Messages authored
	// by selfAgent itself keep their full payload so the agent can reflect on
	// its own prior reasoning.
	WorkingMemory []model.A2AMessage

	// PrivateMemory holds the private (visibility=private) messages authored by
	// selfAgent for selfAgent. This is the Agent's episodic memory: strategy
	// notes, opponent weaknesses, self-corrections, and evidence evaluations.
	PrivateMemory []model.A2AMessage

	// Beliefs is a placeholder for the belief engine snapshot; v0.5 PR 3 will
	// populate it from internal/belief/engine.go. We expose it here so the
	// caller does not need a second function signature.
	Beliefs map[string]float64
}

// HasContent reports whether the context has any meaningful payload to feed
// into the LLM prompt. An empty context (no public messages, no private
// memory) still returns a valid struct, so callers should branch on this flag
// before doing string formatting.
func (c *LLMContext) HasContent() bool {
	return len(c.WorkingMemory) > 0 || len(c.PrivateMemory) > 0
}

// PrivateMemoryTypeStrings returns the four MessageType values that the
// BuildContextView pipeline recognises as private episodic memory. Tests use
// this to assert that all four private types are correctly routed.
func PrivateMemoryTypeStrings() []string {
	return []string{
		string(MessageTypeStrategyNote),
		string(MessageTypeOpponentWeakness),
		string(MessageTypeSelfCorrection),
		string(MessageTypeEvidenceEval),
	}
}

// IsPrivateMemoryMessageType reports whether t is one of the four v0.5 private
// episodic-memory message types. Callers (audit dashboard, MemoryAuditPanel)
// use this to decide whether to render a row under the "策略笔记" tab vs the
// "公共庭审记录" feed.
func IsPrivateMemoryMessageType(t MessageType) bool {
	switch t {
	case MessageTypeStrategyNote,
		MessageTypeOpponentWeakness,
		MessageTypeSelfCorrection,
		MessageTypeEvidenceEval:
		return true
	}
	return false
}

// BuildContextView assembles an LLMContext for `selfAgent` in `sessionID`.
//
// Rules (matching PRD §4.5.3 + §7.4):
//
//   - Private messages authored BY selfAgent go to PrivateMemory (full payload).
//   - Private messages addressed TO selfAgent are also visible (full payload).
//   - Public messages authored BY a *different* agent have their `reasoning`
//     field stripped via SanitizedPayload.
//   - Public messages authored BY selfAgent keep their full payload.
//   - The orchestrator (AddressOrchestrator) sees everything; callers that
//     pass AddressOrchestrator as selfAgent get the union of public + every
//     agent's private stream.
//
// Errors are non-fatal in production: the caller can fall back to an empty
// LLMContext and continue the trial. We still return errors so tests can
// assert on corrupt payloads.
func (b *Bus) BuildContextView(
	ctx context.Context,
	sessionID uuid.UUID,
	selfAgent string,
) (*LLMContext, error) {
	if selfAgent == "" {
		return nil, fmt.Errorf("a2a: BuildContextView requires selfAgent")
	}

	rows, err := b.ListVisibleTo(ctx, sessionID, selfAgent)
	if err != nil {
		return nil, fmt.Errorf("a2a: list visible: %w", err)
	}

	out := &LLMContext{
		WorkingMemory: make([]model.A2AMessage, 0, len(rows)),
		PrivateMemory: make([]model.A2AMessage, 0, 4),
		Beliefs:       map[string]float64{},
	}

	for _, row := range rows {
		if row.Visibility == string(VisibilityPrivate) {
			out.PrivateMemory = append(out.PrivateMemory, row)
			continue
		}

		// public row — apply reasoning stripping if it came from a different agent
		if row.FromAgent == selfAgent || row.FromAgent == AddressOrchestrator || selfAgent == AddressOrchestrator {
			out.WorkingMemory = append(out.WorkingMemory, row)
			continue
		}
		sanitized, err := sanitizeMessageRow(row)
		if err != nil {
			// Sanitization is best-effort: skip the row rather than fail the trial.
			// We still keep the envelope so the agent knows the opposing side said
			// *something* at this round.
			stripped := model.A2AMessage{
				ID:          row.ID,
				SessionID:   row.SessionID,
				MessageUUID: row.MessageUUID,
				Round:       row.Round,
				Phase:       row.Phase,
				FromAgent:   row.FromAgent,
				ToAgent:     row.ToAgent,
				MessageType: row.MessageType,
				Visibility:  row.Visibility,
				Payload:     "{}",
				MemoryRefs:  row.MemoryRefs,
				CreatedAt:   row.CreatedAt,
			}
			out.WorkingMemory = append(out.WorkingMemory, stripped)
			continue
		}
		out.WorkingMemory = append(out.WorkingMemory, sanitized)
	}

	// Stable ordering: round ascending, then created_at ascending. This keeps
	// the rendered narrative deterministic across runs (helps tests).
	sort.SliceStable(out.WorkingMemory, func(i, j int) bool {
		if out.WorkingMemory[i].Round != out.WorkingMemory[j].Round {
			return out.WorkingMemory[i].Round < out.WorkingMemory[j].Round
		}
		return out.WorkingMemory[i].CreatedAt.Before(out.WorkingMemory[j].CreatedAt)
	})
	sort.SliceStable(out.PrivateMemory, func(i, j int) bool {
		if out.PrivateMemory[i].Round != out.PrivateMemory[j].Round {
			return out.PrivateMemory[i].Round < out.PrivateMemory[j].Round
		}
		return out.PrivateMemory[i].CreatedAt.Before(out.PrivateMemory[j].CreatedAt)
	})

	return out, nil
}

// SanitizeForViewer is the single-row counterpart to BuildContextView. It is
// exposed so the Orchestrator can sanitize on-demand (e.g. when constructing
// a custom ad-hoc prompt that does not need the full view).
//
// Rules:
//   - viewer == AddressOrchestrator: returns the row unchanged.
//   - viewer == FromAgent: returns the row unchanged (self sees own reasoning).
//   - viewer is any other agent:
//   - If visibility == private: returns ErrNotVisible.
//   - If visibility == public: strips `reasoning` from Payload.
//   - If the row's payload JSON is malformed: returns ErrMalformedPayload.
func (b *Bus) SanitizeForViewer(
	row model.A2AMessage,
	viewerAgent string,
) (model.A2AMessage, error) {
	if viewerAgent == "" {
		return model.A2AMessage{}, fmt.Errorf("a2a: SanitizeForViewer requires viewerAgent")
	}
	if viewerAgent == AddressOrchestrator || viewerAgent == row.FromAgent {
		return row, nil
	}
	if row.Visibility == string(VisibilityPrivate) {
		return model.A2AMessage{}, ErrNotVisible
	}
	return sanitizeMessageRow(row)
}

// ErrNotVisible is returned by SanitizeForViewer when viewer is not entitled
// to read a private message. We export this so callers (e.g. the orchestrator
// hot path) can branch on it without string matching.
var ErrNotVisible = fmt.Errorf("a2a: message not visible to viewer")

// ErrMalformedPayload is returned when a stored Payload column cannot be
// decoded as JSON. v0.5 does not auto-repair; the row is skipped.
var ErrMalformedPayload = fmt.Errorf("a2a: malformed payload")

// sanitizeMessageRow decodes row.Payload, strips the `reasoning` key, and
// re-encodes. If decoding fails we return ErrMalformedPayload so the caller
// can decide whether to drop or surface the error.
func sanitizeMessageRow(row model.A2AMessage) (model.A2AMessage, error) {
	if row.Payload == "" {
		return row, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return model.A2AMessage{}, fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	if _, hasReasoning := payload["reasoning"]; !hasReasoning {
		// nothing to strip — keep the original JSON to avoid touching unrelated data
		return row, nil
	}
	delete(payload, "reasoning")
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return model.A2AMessage{}, fmt.Errorf("a2a: re-marshal payload: %w", err)
	}
	out := row
	out.Payload = string(rewritten)
	return out, nil
}

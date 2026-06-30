package agent

import (
	"context"
	"strings"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/google/uuid"
)

// MemoryHook is the seam between the ReActRunner (LLM-driven loop) and the
// A2A bus (private episodic memory channel). Whenever a reflect (or speak)
// step carries a complete memory entry (HasMemory() == true), the runner
// invokes the hook with the AgentOutput so the orchestrator can persist it.
//
// The hook runs synchronously inside the ReAct loop. Implementations MUST
// be fast (< 10ms) and MUST NOT mutate the AgentOutput (it is shared with
// the rest of the iteration). Errors are returned but the runner does NOT
// fail the trial — it logs and continues.
//
// May be nil; a nil hook simply means "don't persist memory", which is the
// safe default for callers that don't yet wire A2A.
type MemoryHook func(ctx context.Context, out AgentOutput, meta MemoryMeta) error

// MemoryMeta is the routing context the runner cannot know on its own
// (session / agent identity / round). The orchestrator injects these when
// constructing the runner via RunnerConfig.MemoryHook closure.
type MemoryMeta struct {
	SessionID uuid.UUID
	AgentType string // e.g. "prosecutor"
	Round     int
	Phase     string // e.g. "cross_exam"
}

// ToA2AMemoryMessageType maps a reflect-emitted MemoryKind to its
// corresponding A2A MessageType. Unknown kinds fall back to
// MessageTypeStrategyNote so the audit log is never empty; callers should
// pre-filter via IsKnownMemoryKind before invoking EmitMemoryFromHook.
func ToA2AMemoryMessageType(k MemoryKind) a2a.MessageType {
	switch k {
	case MemoryKindStrategyNote:
		return a2a.MessageTypeStrategyNote
	case MemoryKindOpponentWeakness:
		return a2a.MessageTypeOpponentWeakness
	case MemoryKindSelfCorrection:
		return a2a.MessageTypeSelfCorrection
	case MemoryKindEvidenceEval:
		return a2a.MessageTypeEvidenceEval
	default:
		return a2a.MessageTypeStrategyNote
	}
}

// A2AMemoryEmitter is the contract the hook implementation must satisfy.
// *a2a.Bus already implements it via its Send(ctx, Message) signature.
type A2AMemoryEmitter interface {
	Send(ctx context.Context, msg a2a.Message) (a2a.Message, error)
}

// EmitMemoryFromOutput builds and sends a private A2A message from a memory
// entry. It is exported so the orchestrator can call it directly when it
// wants to drive memory persistence outside the runner (e.g. to record an
// evidence_eval right after a tool_call step).
//
// Returns nil if out does not carry a complete memory entry. Returns an
// error only if emitter.Send fails; callers should log + continue (the
// trial should not abort due to a memory persistence failure).
func EmitMemoryFromOutput(
	ctx context.Context,
	emitter A2AMemoryEmitter,
	meta MemoryMeta,
	out AgentOutput,
) error {
	if emitter == nil {
		return nil
	}
	if !out.HasMemory() {
		return nil
	}

	msg := buildPrivateMemoryMessage(meta, out)
	if _, err := emitter.Send(ctx, msg); err != nil {
		return err
	}
	return nil
}

// buildPrivateMemoryMessage is the pure-data conversion from
// (meta + AgentOutput) -> a2a.Message. Split out from
// EmitMemoryFromOutput so unit tests can assert the envelope shape without
// spinning up an A2A bus.
func buildPrivateMemoryMessage(meta MemoryMeta, out AgentOutput) a2a.Message {
	payload := map[string]interface{}{
		"memory_type": string(out.MemoryType),
		"content":     strings.TrimSpace(out.MemoryNote),
		"round":       meta.Round,
	}
	if len(out.EvidenceRefs) > 0 {
		payload["linked_evidence_ids"] = out.EvidenceRefs
	}
	if out.Reasoning != "" {
		// We deliberately do NOT include `reasoning` in the public side of
		// the payload — the message is private and reasoning is already
		// embedded in the memory note content. This keeps memory payloads
		// easy to render in the frontend MemoryAuditPanel.
	}

	return a2a.Message{
		SessionID:   meta.SessionID,
		Round:       meta.Round,
		Phase:       meta.Phase,
		From:        meta.AgentType,
		To:          meta.AgentType, // self-to-self = private
		MessageType: ToA2AMemoryMessageType(out.MemoryType),
		Visibility:  a2a.VisibilityPrivate,
		Payload:     payload,
	}
}

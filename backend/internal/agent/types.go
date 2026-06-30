package agent

import (
	"strings"

	"github.com/decisioncourt/backend/internal/model"
)

// ActionKind distinguishes whether the LLM wants to speak or call a tool.
// The ReAct runner switches on Action before validation so the same struct
// can carry both speech and tool-call payloads.
type ActionKind string

const (
	// ActionSpeak terminates the ReAct loop and produces the final Speaker.
	ActionSpeak ActionKind = "speak"
	// ActionToolCall invokes a registered tool and feeds the observation back
	// into the next LLM iteration.
	ActionToolCall ActionKind = "tool_call"
	// ActionReflect lets the LLM iterate its Thought without producing
	// speech or invoking any tool. The runner records the new Thought and
	// continues the loop so the LLM can reason multiple steps before
	// committing to a final action. NormalizeAction does NOT default to
	// ActionReflect; legacy callers stay ActionSpeak.
	ActionReflect ActionKind = "reflect"
)

// MemoryKind is the category of private episodic memory an LLM can attach to
// a reflect (or speak) step. See internal/a2a/types.go for the four private
// MessageType constants; the strings here must stay in sync.
type MemoryKind string

const (
	// MemoryKindStrategyNote is a generic strategy note (next-round plan).
	MemoryKindStrategyNote MemoryKind = "strategy_note"
	// MemoryKindOpponentWeakness records a gap in the opposing side's case.
	MemoryKindOpponentWeakness MemoryKind = "opponent_weakness"
	// MemoryKindSelfCorrection flags that the agent wants to revise an
	// earlier argument in a future round.
	MemoryKindSelfCorrection MemoryKind = "self_correction"
	// MemoryKindEvidenceEval records an internal evaluation of evidence
	// strength (emitted automatically after investigator_search tool calls).
	MemoryKindEvidenceEval MemoryKind = "evidence_eval"
)

// IsKnownMemoryKind reports whether k is one of the four valid v0.5 episodic
// memory kinds. Tests use this to assert the classifier rejects garbage
// values without crashing.
func IsKnownMemoryKind(k MemoryKind) bool {
	switch k {
	case MemoryKindStrategyNote,
		MemoryKindOpponentWeakness,
		MemoryKindSelfCorrection,
		MemoryKindEvidenceEval:
		return true
	}
	return false
}

// AgentOutput is the expected JSON output from every Agent. The Action / Tool /
// ToolInput fields are optional; when omitted (legacy callers) Action defaults
// to ActionSpeak so existing parse paths continue to work.
//
// v0.5 addendum: MemoryType and MemoryNote are *optional* v0.5 fields. When
// the LLM emits action="reflect" with both populated, the runner fires the
// MemoryHook so the orchestrator can persist it as a private A2A message.
// The fields default to "" so callers and tests that pre-date v0.5 keep
// working unchanged.
type AgentOutput struct {
	Reasoning    string                 `json:"reasoning"`
	Content      string                 `json:"content"`
	EvidenceRefs []string               `json:"evidence_refs"`
	Confidence   float64                `json:"confidence"`
	Stance       string                 `json:"stance"`
	Action       ActionKind             `json:"action,omitempty"`
	Tool         string                 `json:"tool,omitempty"`
	ToolInput    map[string]interface{} `json:"tool_input,omitempty"`
	// MemoryType (v0.5) — when non-empty AND MemoryNote is also non-empty,
	// the runner emits a private episodic-memory message via the MemoryHook.
	MemoryType MemoryKind `json:"memory_type,omitempty"`
	// MemoryNote (v0.5) — the content of the memory entry. Free-form text;
	// the runner passes it through verbatim into the A2A payload.
	MemoryNote string `json:"memory_note,omitempty"`
}

// NormalizeAction fills in the default Action so legacy callers (and tests
// that don't set Action explicitly) keep working as ActionSpeak.
func (o *AgentOutput) NormalizeAction() {
	if o.Action == "" {
		o.Action = ActionSpeak
	}
}

// HasMemory reports whether the output carries a complete memory entry
// (both MemoryType and MemoryNote populated with a known kind). The note is
// trimmed so whitespace-only entries do not count. Callers use this before
// firing the MemoryHook to avoid no-op invocations.
func (o *AgentOutput) HasMemory() bool {
	return IsKnownMemoryKind(o.MemoryType) && strings.TrimSpace(o.MemoryNote) != ""
}

// Speaker represents an Agent ready to speak in the courtroom.
type Speaker struct {
	Agent        model.Agent
	Content      string
	Reasoning    string
	EvidenceRefs []string
	Confidence   float64
	Stance       string
}

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
//
// v0.6 addendum: Weaken is an OPTIONAL v0.6 field. Each entry declares that
// the agent wants to attack an existing evidence piece's transmission
// rather than its truth (per the 异构论辩图谱 patent). The runner fires
// WeakenHook so the orchestrator can persist it to evidence_weaken_links.
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
	// Weaken (v0.6) — list of evidence pieces the agent wants to neutralise.
	// See WeakenDeclaration below.
	Weaken []WeakenDeclaration `json:"weaken,omitempty"`
	// Rebut (v1.0.2 候选 4) — list of evidence pieces the agent wants to
	// rebut. See RebuttalDeclaration below. Symmetric to Weaken but targets
	// CONTENT (not transmission). Persisted by RebuttalHook into
	// evidence_rebuttal_links table with default status='standing'.
	Rebut []RebuttalDeclaration `json:"rebut,omitempty"`
}

// WeakenDeclaration is a single "weakening edge" declared by an LLM agent.
// The ReAct runner forwards every non-empty declaration to the WeakenHook so
// the orchestrator can persist it as a row in evidence_weaken_links; the
// belief engine reads those rows when applying future evidence impact.
//
// All fields are required; the runner's ApplyOutputToMemoryHook path
// performs validation/clamping before persisting.
type WeakenDeclaration struct {
	// EvidenceID is the *display_id* (e.g. "E001") the user sees in the
	// EvidenceBoard, NOT the DB UUID. The hook resolves it to the actual
	// UUID via the session's evidence list before Inserting.
	EvidenceID string `json:"evidence_id"`
	// Target is the agent whose belief the weaken should attenuate. Use
	// "prosecutor" or "defender"; investigator / clerk weaken declarations
	// are silently dropped (those agents don't anchor strongly enough for
	// weakening to matter).
	Target string `json:"target"`
	// Strength is a 0..1 attenuation amount. 0.4 = "this evidence should
	// carry 60% of its normal weight when affecting Target".
	Strength float64 `json:"strength"`
	// Rationale is a short free-text reason shown on the verdict page's
	// audit card. Optional.
	Rationale string `json:"rationale,omitempty"`
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

// HasWeaken reports whether the output carries at least one valid
// WeakenDeclaration. We treat a declaration as valid when EvidenceID is
// non-empty, Target is prosecutor or defender, and Strength is in (0, 1).
// Hooks use this before persisting to avoid no-op database writes.
func (o *AgentOutput) HasWeaken() bool {
	for _, w := range o.Weaken {
		if isValidWeaken(w) {
			return true
		}
	}
	return false
}

// isValidWeaken is a small predicate used by HasWeaken and the WeakenHook
// to drop obviously-broken entries.
func isValidWeaken(w WeakenDeclaration) bool {
	if strings.TrimSpace(w.EvidenceID) == "" {
		return false
	}
	if w.Target != "prosecutor" && w.Target != "defender" {
		return false
	}
	if w.Strength <= 0 || w.Strength > 1 {
		return false
	}
	return true
}

// ValidWeakenDeclarations returns a slice containing only the entries that
// pass isValidWeaken. Both hooks and tests call this so the loud-failure
// path ("this entry would have crashed") is centralised.
func (o *AgentOutput) ValidWeakenDeclarations() []WeakenDeclaration {
	out := make([]WeakenDeclaration, 0, len(o.Weaken))
	for _, w := range o.Weaken {
		if isValidWeaken(w) {
			out = append(out, w)
		}
	}
	return out
}

// RebuttalDeclaration (v1.0.2 候选 4) is a single "rebuttal edge" declared by
// an LLM agent. Symmetric to WeakenDeclaration but targets evidence CONTENT
// (not transmission): "E001 不成立因为 X" 而非 "E001 传播削弱 0.5"。
//
// ReAct runner 在 speak 之前把所有 valid rebuttal declarations 转给
// RebuttalHook,Service 层把每条写入 evidence_rebuttal_links 表
// (default status='standing')。后续 applySpeakerRebuttalCheck (PR-3)
// 在 validateSpeak 阶段硬拒引用 standing 状态的 evidence。
//
// 字段对齐 WeakenDeclaration: 复用 "声明 schema" 不引入新 ActionKind,
// 让 LLM 在同一次 speak 中可以既 rebut 又 speak (区别于 Weaken 仅在
// reflect 阶段声明, 详见 react_runner.go)。
type RebuttalDeclaration struct {
	// RebuttedEvidenceID is the *display_id* (e.g. "E001") the user sees in
	// the EvidenceBoard. RebuttalHook resolves it to actual UUID via
	// session's evidence list before Inserting.
	RebuttedEvidenceID string `json:"rebutted_evidence_id"`
	// Strength is 0..1 rebuttal strength. 后端不直接衰减 weight (与 Weaken
	// 不同), 但记录到 evidence_rebuttal_links 供前端 audit / chip 显示。
	Strength float64 `json:"strength"`
	// Rationale is a short free-text reason (≤50 字, prompt 约束).
	Rationale string `json:"rationale,omitempty"`
}

// HasRebuttal reports whether the output has at least one valid
// RebuttalDeclaration. Hooks use this before persisting to avoid no-op DB writes.
func (o *AgentOutput) HasRebuttal() bool {
	for _, r := range o.Rebut {
		if isValidRebuttal(r) {
			return true
		}
	}
	return false
}

// isValidRebuttal drops obviously-broken rebuttal entries.
// 同 Weaken 策略: RebuttedEvidenceID 非空 + Strength ∈ (0, 1].
func isValidRebuttal(r RebuttalDeclaration) bool {
	if strings.TrimSpace(r.RebuttedEvidenceID) == "" {
		return false
	}
	if r.Strength <= 0 || r.Strength > 1 {
		return false
	}
	return true
}

// ValidRebuttalDeclarations returns the slice containing only entries that
// pass isValidRebuttal. Symmetric to ValidWeakenDeclarations.
func (o *AgentOutput) ValidRebuttalDeclarations() []RebuttalDeclaration {
	out := make([]RebuttalDeclaration, 0, len(o.Rebut))
	for _, r := range o.Rebut {
		if isValidRebuttal(r) {
			out = append(out, r)
		}
	}
	return out
}

// Speaker represents an Agent ready to speak in the courtroom.
type Speaker struct {
	Agent        model.Agent
	Content      string
	Reasoning    string
	EvidenceRefs []string
	Confidence   float64
	Stance       string
	// v0.10.21 PR-B: 发言被硬截断标记 + 原始字符数 (中文友好, 按 rune 计)
	// ContentTruncated=true 表示 Content 已被 truncateRunes 截到 300 字以内。
	// OriginalRunes 记录截断前的字符数, 用于审计 / 前端 chip 显示。
	ContentTruncated bool `json:"content_truncated,omitempty"`
	OriginalRunes    int  `json:"original_runes,omitempty"`
	// v0.10.23 候选 2: 新意度 Jaccard 检查 reject 标记 + 实际 jaccard 值
	// NoveltyRejected=true 表示本发言触发了"换角度 retry" (2 次 retry 后仍 > 0.6),
	// 即最终返回的 Speaker 仍然与自己历史发言重复。
	// NoveltyJaccard 记录触发时的实际 jaccard 值 (0-1), 用于前端 chip + 审计。
	// JSON omitempty 向后兼容: 旧后端 / 旧前端不会显示这两个字段。
	NoveltyRejected bool    `json:"novelty_rejected,omitempty"`
	NoveltyJaccard   float64 `json:"novelty_jaccard,omitempty"`
	// v0.10.24 候选 1: LLM-as-judge stance 一致性 reject 标记 + 实际理由
	// StanceRejected=true 表示本发言触发了"stance judge 打回" (2 次 retry 后仍 false),
	// 即最终返回的 Speaker 仍然与其当前 belief 方向不一致 (LLM judge 主观判定)。
	// StanceJudgeReason 记录 judge LLM 最后输出的 reason (≤50 字), 用于审计 + 前端 chip tooltip。
	// JSON omitempty 向后兼容: 旧后端 / 旧前端不会显示这两个字段。
	StanceRejected   bool   `json:"stance_rejected,omitempty"`
	StanceJudgeReason string `json:"stance_judge_reason,omitempty"`
	// v1.0.2 候选 4: 已反驳证据 hard reject 标记 + 违规 evidence IDs
	// RebuttalRejected=true 表示本发言触发了"已反驳证据 hard reject" (2 次 retry 后仍含
	// standing rebuttal), 即最终返回的 Speaker 仍然引用了已反驳未翻盘的证据 (PRD §4.3.3)。
	// RebuttalViolations 列出被拒绝引用的 evidence IDs (E00X 格式), 用于前端 chip
	// + 审计 trail (类似 StanceJudgeReason)。与 stance/novelty 同等级 guard。
	// JSON omitempty 向后兼容: 旧后端 / 旧前端不会显示这两个字段。
	RebuttalRejected   bool     `json:"rebuttal_rejected,omitempty"`
	RebuttalViolations []string `json:"rebuttal_violations,omitempty"`
}

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

// publicPayloadWhitelist 定义"允许投影给对方 Agent"的 payload 键。
//
// v2.11 (deferred D13-B)：这是**正向白名单**，不是黑名单。之前的实现只 delete
// 一个 `reasoning` 键 —— 新增任何敏感字段（planned_next_move、内部置信度、
// 工具参数……）都必须**人工记得**加进剥离逻辑，漏了就静默泄漏（ADR 0003 已记录
// 此弱点）。改成白名单后，未登记的键**默认不进入**对方视图：
// 安全性从"记得删"变成"记得加"，漏登记只会让对方少看到合法信息（**可见**的降级），
// 而不是泄漏（**不可见**的风险）。
//
// 新增公共事件类型、或给某个 payload 加字段时，必须在这里登记 ——
// 有测试枚举 MessageType 常量做护栏（TestPublicPayloadWhitelist_CoversAllTypes）。
var publicPayloadWhitelist = map[MessageType][]string{
	// orchestrator.go 的公开发言
	MessageTypeSpeech: {"content", "stance", "confidence", "evidence_refs"},
	// investigation/service.go 的公开派单
	MessageTypeDispatch: {"query", "dispatched_by"},
	// investigation/service.go 的公开回报
	MessageTypeReport: {"query", "dispatched_by", "finding_id", "result_count", "summary", "source"},
}

// unknownTypeFallbackKeys 是未登记的 MessageType 的兜底放行键。
//
// 只放行 `content`：保留"对方说了什么"的信封语义（与 sanitizeMessageRow 在
// payload 解不开时的行为一致），同时不泄漏任何结构化字段。
var unknownTypeFallbackKeys = []string{"content"}

// AllMessageTypes 列出本包已声明的全部 MessageType 常量。
//
// 新增 MessageType 时**必须**加进来：TestPublicPayloadWhitelist_CoversAllTypes
// 用它断言"每个类型都被有意识地决定过该怎么投影"（进白名单 / 走 unknown 兜底 /
// 属于私有记忆永不被投影）。漏了会测试失败，而不是静默让新类型只放行 content。
func AllMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeSpeech,
		MessageTypeEvidence,
		MessageTypeChallenge,
		MessageTypeInquiry,
		MessageTypeVerdictTask,
		MessageTypeDispatch,
		MessageTypeReport,
		MessageTypeStrategyNote,
		MessageTypeOpponentWeakness,
		MessageTypeSelfCorrection,
		MessageTypeEvidenceEval,
	}
}

// PrivateMemoryMessageTypes 返回 4 个"私有 episodic memory"类型。
// 它们永远不该出现在对方视图里（可见性由 ListVisibleTo 的 SQL 保证），
// 因此**不得**出现在 publicPayloadWhitelist 中 —— 有测试钉住这一点。
func PrivateMemoryMessageTypes() []MessageType {
	return []MessageType{
		MessageTypeStrategyNote,
		MessageTypeOpponentWeakness,
		MessageTypeSelfCorrection,
		MessageTypeEvidenceEval,
	}
}

// ProjectPayloadForOpponent 把公共消息的 payload 正向投影成"对方可见"的副本。
//
// 只保留白名单内的键；未登记的 MessageType 只保留 unknownTypeFallbackKeys。
// 返回新 map（不修改入参）。
func ProjectPayloadForOpponent(msgType MessageType, payload map[string]interface{}) map[string]interface{} {
	keys, ok := publicPayloadWhitelist[msgType]
	if !ok {
		keys = unknownTypeFallbackKeys
	}
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, exists := payload[k]; exists {
			out[k] = v
		}
	}
	return out
}

// sanitizeMessageRow 把一行公共消息投影成"对方可见"的版本：解 JSON → 白名单投影
// → 重新编码。解不开时返回 ErrMalformedPayload，让调用方决定丢弃还是暴露错误。
//
// v2.11 (deferred D13-B)：从"删 reasoning 一个键"改为"正向白名单投影"。
// 注意：即使 payload 里没有任何敏感键，现在也会重新编码（因为未登记的键会被丢掉）
// —— 这是有意的：新字段默认不进入对方视图。
func sanitizeMessageRow(row model.A2AMessage) (model.A2AMessage, error) {
	if row.Payload == "" {
		return row, nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return model.A2AMessage{}, fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	projected := ProjectPayloadForOpponent(MessageType(row.MessageType), payload)
	rewritten, err := json.Marshal(projected)
	if err != nil {
		return model.A2AMessage{}, fmt.Errorf("a2a: re-marshal payload: %w", err)
	}
	out := row
	out.Payload = string(rewritten)
	return out, nil
}

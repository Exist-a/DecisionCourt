// Package a2a implements the Agent-to-Agent message bus described in PRD §4.4.
// The Bus routes messages between agents, enforces visibility rules, persists
// every message for audit, and exposes context-view helpers that strip private
// payloads from the perspective of each recipient.
package a2a

import (
	"time"

	"github.com/google/uuid"
)

// MessageType enumerates the kinds of A2A messages that can flow on the bus.
type MessageType string

const (
	// MessageTypeSpeech is a public speaking turn (e.g. opening / cross-exam).
	MessageTypeSpeech MessageType = "speech"
	// MessageTypeEvidence is a new piece of evidence added to the public board.
	MessageTypeEvidence MessageType = "evidence"
	// MessageTypeChallenge is one agent challenging another's argument or evidence.
	MessageTypeChallenge MessageType = "challenge"
	// MessageTypeInquiry is a question raised to another agent or the user.
	MessageTypeInquiry MessageType = "inquiry"
	// MessageTypeVerdictTask is a request to the Clerk to assemble a verdict.
	MessageTypeVerdictTask MessageType = "verdict_task"
	// MessageTypeDispatch is a request to the Investigator to run a search.
	MessageTypeDispatch MessageType = "dispatch"
	// MessageTypeReport is the Investigator's private response to a dispatch.
	MessageTypeReport MessageType = "report"

	// === v0.5 Episodic Memory via A2A private channel (PR 1) ===
	// 下列 4 个 MessageType 均为 visibility=private，由 self_agent 写入 + 仅 self_agent + orchestrator 可读。

	// MessageTypeStrategyNote is an Agent's private strategic note for its own
	// future reference. Written automatically after every speak, and may also be
	// emitted explicitly from the ReAct reflect step. Visibility: private.
	MessageTypeStrategyNote MessageType = "strategy_note"
	// MessageTypeOpponentWeakness is an Agent's private observation of a gap
	// or vulnerability in the opposing side's argument. Visibility: private.
	MessageTypeOpponentWeakness MessageType = "opponent_weakness"
	// MessageTypeSelfCorrection is an Agent's private reminder to revise its
	// own earlier argument in a subsequent round. Visibility: private.
	MessageTypeSelfCorrection MessageType = "self_correction"
	// MessageTypeEvidenceEval is an Agent's private internal evaluation of a
	// piece of evidence (e.g. "E001 strongly supports option_a"). Visibility:
	// private.
	MessageTypeEvidenceEval MessageType = "evidence_eval"
)

// Visibility controls who is allowed to read a message payload.
type Visibility string

const (
	// VisibilityPublic is readable by all agents in the session.
	VisibilityPublic Visibility = "public"
	// VisibilityPrivate is readable only by ToAgent (e.g. dispatched reports).
	VisibilityPrivate Visibility = "private"
)

// Address identifies an agent endpoint on the bus. We use AgentType strings
// (e.g. "prosecutor", "defender") plus the special address "orchestrator"
// which is the only address permitted to see every private message.
const (
	AddressOrchestrator = "orchestrator"
)

// Message is the canonical A2A envelope. Payload is stored as raw JSON to
// preserve the original structure regardless of the message_type.
//
// IMPORTANT (v0.5 修复)：SessionID 是 court_sessions.id 主键（uuid.UUID），
// 用于 a2a_messages 表的 FK。SessionUUID 是 court_sessions.session_uuid
// 字符串列，用于 WebSocket hub.Broadcast 的 room 寻址。两把钥匙不同！
// 之前 Bus.Send 用 SessionID.String() 当作 broadcast key，导致
// a2a.message 广播进了一个没有任何客户端的房间，被 hub 静默丢弃。
// 现在 Bus.Send 优先使用 SessionUUID 字段（如果调用方传入）；如果为空
// 则 fallback 到 SessionID.String() 并打 WARN 日志，提醒调用方修复。
type Message struct {
	ID          uuid.UUID              `json:"id"`
	MessageUUID string                 `json:"message_uuid"`
	SessionID   uuid.UUID              `json:"session_id"`
	// SessionUUID 是 court_sessions.session_uuid 字符串列（与 SessionID
	// 主键不同），用于 WebSocket hub.Broadcast 的房间寻址。Bus.Send 在
	// 广播时优先使用本字段；如果为空 fallback 到 SessionID.String()。
	SessionUUID string                 `json:"session_uuid,omitempty"`
	Round       int                    `json:"round"`
	Phase       string                 `json:"phase"`
	From        string                 `json:"from"`
	To          string                 `json:"to"`
	MessageType MessageType            `json:"message_type"`
	Visibility  Visibility             `json:"visibility"`
	Payload     map[string]interface{} `json:"payload"`
	MemoryRefs  []string               `json:"memory_references"`
	CreatedAt   time.Time              `json:"created_at"`
}

// SanitizedPayload returns a copy of the payload with the reasoning field
// removed. Public messages use this when projected to the opposing side so
// private chains-of-thought never leak across the bench.
func (m Message) SanitizedPayload() map[string]interface{} {
	out := make(map[string]interface{}, len(m.Payload))
	for k, v := range m.Payload {
		if k == "reasoning" {
			continue
		}
		out[k] = v
	}
	return out
}

package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Broadcaster is the minimal interface Bus needs to push audit events to the
// websocket hub. We keep it as a function shape so this package does not
// depend on the courtroom/api packages.
type Broadcaster func(sessionUUID string, eventType string, payload map[string]interface{})

// Repository is the persistence interface Bus needs. The default production
// implementation uses GORM over PostgreSQL; tests can swap in InMemoryRepository.
type Repository interface {
	Insert(ctx context.Context, row model.A2AMessage) (model.A2AMessage, error)
	ListVisibleTo(ctx context.Context, sessionID uuid.UUID, viewer string) ([]model.A2AMessage, error)
	// ListPrivateMemory returns every A2A row in `sessionID` whose
	// message_type is one of the four v0.5 episodic-memory kinds
	// (strategy_note / opponent_weakness / self_correction / evidence_eval).
	//
	// Unlike ListVisibleTo this is NOT scoped by a viewer — by product
	// design the user-facing MemoryAuditPanel renders BOTH sides' private
	// strategy notes (the "差异化卖点" — users see lawyers' "内心戏").
	// The Real-Courthouse toggle is a UI-only redacted view; the backend
	// always returns the full set so toggling mid-trial cannot desync the
	// LLM or backend state.
	ListPrivateMemory(ctx context.Context, sessionID uuid.UUID) ([]model.A2AMessage, error)
}

// Clock returns the current time. The Bus uses this to stamp CreatedAt on
// outgoing messages so tests can run with a deterministic clock.
type Clock func() time.Time

// Bus is the central A2A router. Every Agent-to-Agent interaction in the
// courtroom goes through Send so that visibility, persistence, and audit are
// guaranteed in one place.
type Bus struct {
	repo      Repository
	broadcast Broadcaster
	clock     Clock
}

// NewBus constructs a Bus. broadcaster may be nil in tests.
func NewBus(repo Repository, broadcaster Broadcaster) *Bus {
	if broadcaster == nil {
		broadcaster = func(string, string, map[string]interface{}) {}
	}
	return &Bus{
		repo:      repo,
		broadcast: broadcaster,
		clock:     func() time.Time { return time.Now().UTC() },
	}
}

// NewBusWithClock is a variant for tests that need deterministic timestamps.
func NewBusWithClock(repo Repository, broadcaster Broadcaster, clock Clock) *Bus {
	if broadcaster == nil {
		broadcaster = func(string, string, map[string]interface{}) {}
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Bus{repo: repo, broadcast: broadcaster, clock: clock}
}

// NewGormRepository returns a Repository backed by a GORM/Postgres database.
func NewGormRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

type gormRepository struct {
	db *gorm.DB
}

func (r *gormRepository) Insert(ctx context.Context, row model.A2AMessage) (model.A2AMessage, error) {
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return model.A2AMessage{}, err
	}
	return row, nil
}

func (r *gormRepository) ListVisibleTo(ctx context.Context, sessionID uuid.UUID, viewer string) ([]model.A2AMessage, error) {
	var rows []model.A2AMessage
	q := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at asc")
	if viewer != AddressOrchestrator {
		q = q.Where(
			"visibility = ? OR to_agent = ? OR from_agent = ?",
			string(VisibilityPublic), viewer, viewer,
		)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListPrivateMemory returns all rows whose message_type is one of the four
// v0.5 episodic-memory kinds. See Repository.ListPrivateMemory for the
// rationale (MemoryAuditPanel renders both sides' notes by design).
//
// SQL: WHERE session_id = ? AND message_type IN (?,?,?,?) ORDER BY created_at
func (r *gormRepository) ListPrivateMemory(ctx context.Context, sessionID uuid.UUID) ([]model.A2AMessage, error) {
	var rows []model.A2AMessage
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Where("message_type IN ?", []string{
			string(MessageTypeStrategyNote),
			string(MessageTypeOpponentWeakness),
			string(MessageTypeSelfCorrection),
			string(MessageTypeEvidenceEval),
		}).
		Order("created_at asc").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Send validates msg, fills in defaults, persists it via the Repository,
// and notifies the websocket hub. visibility defaults to public if unset.
func (b *Bus) Send(ctx context.Context, msg Message) (Message, error) {
	if msg.SessionID == uuid.Nil {
		return Message{}, fmt.Errorf("a2a: SessionID required")
	}
	if msg.From == "" {
		return Message{}, fmt.Errorf("a2a: From required")
	}
	if msg.To == "" {
		return Message{}, fmt.Errorf("a2a: To required")
	}
	if msg.MessageType == "" {
		return Message{}, fmt.Errorf("a2a: MessageType required")
	}
	if msg.Visibility == "" {
		msg.Visibility = VisibilityPublic
	}
	if msg.MessageUUID == "" {
		msg.MessageUUID = uuid.New().String()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = b.clock()
	}

	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		return Message{}, fmt.Errorf("a2a: marshal payload: %w", err)
	}

	row := model.A2AMessage{
		SessionID:   msg.SessionID,
		MessageUUID: msg.MessageUUID,
		Round:       msg.Round,
		Phase:       msg.Phase,
		FromAgent:   msg.From,
		ToAgent:     msg.To,
		MessageType: string(msg.MessageType),
		Payload:     string(payloadBytes),
		Visibility:  string(msg.Visibility),
		MemoryRefs:  msg.MemoryRefs,
	}
	persisted, err := b.repo.Insert(ctx, row)
	if err != nil {
		return Message{}, fmt.Errorf("a2a: persist message: %w", err)
	}
	msg.ID = persisted.ID

	// Audit broadcast: surface the envelope but strip private payloads so
	// the websocket event is safe for any subscriber.
	auditPayload := map[string]interface{}{
		"message_uuid": msg.MessageUUID,
		"from":         msg.From,
		"to":           msg.To,
		"message_type": string(msg.MessageType),
		"round":        msg.Round,
		"phase":        msg.Phase,
		"visibility":   string(msg.Visibility),
		"id":           persisted.ID,
		"created_at":   persisted.CreatedAt,
	}
	// Public messages always include payload (the whole point of public).
	// v0.5 private episodic-memory messages (strategy_note / opponent_weakness
	// / self_correction / evidence_eval) also include payload — they are
	// addressed self→self and the user-facing MemoryAuditPanel relies on
	// payload.content to render the timeline. Other private messages
	// (dispatch/report) keep their payload server-side only so the
	// opposing side never sees an agent's hidden reasoning chain.
	if msg.Visibility == VisibilityPublic || IsPrivateMemoryMessageType(msg.MessageType) {
		auditPayload["payload"] = msg.Payload
	}
	// v0.5 修复：优先用 Message.SessionUUID（court_sessions.session_uuid 字
	// 符串列，与 WebSocket hub.Join 的 key 一致）。Fallback 到 SessionID
	// 时打 WARN 日志 —— 调用方忘了填 SessionUUID 时能在日志里立刻看到。
	sessionUUID := msg.SessionUUID
	if sessionUUID == "" {
		sessionUUID = uuid.UUID(msg.SessionID).String()
		log.Printf("[a2a] WARN: a2a.message broadcast using SessionID.String() fallback — caller should set Message.SessionUUID to match hub room key (got sessionID=%s)", sessionUUID)
	}
	b.broadcast(sessionUUID, "a2a.message", auditPayload)
	log.Printf("[a2a] %s → %s (%s, visibility=%s, round=%d)",
		msg.From, msg.To, msg.MessageType, msg.Visibility, msg.Round)

	return msg, nil
}

// ListVisibleTo returns A2A messages that `viewer` is allowed to see.
// Public messages are visible to everyone; private messages are visible only
// to the named To recipient (and to the sender of the message).
func (b *Bus) ListVisibleTo(ctx context.Context, sessionID uuid.UUID, viewer string) ([]model.A2AMessage, error) {
	return b.repo.ListVisibleTo(ctx, sessionID, viewer)
}

// ListPrivateMemory returns every v0.5 episodic-memory row in `sessionID`,
// regardless of sender. See Repository.ListPrivateMemory for why this is
// the correct primitive for user-facing MemoryAuditPanel hydration on
// session restore (verdict page refresh, browser back from verdict, etc.).
func (b *Bus) ListPrivateMemory(ctx context.Context, sessionID uuid.UUID) ([]model.A2AMessage, error) {
	return b.repo.ListPrivateMemory(ctx, sessionID)
}

// DecodePayload unmarshals a persisted row's payload into a typed map.
func DecodePayload(row model.A2AMessage) (map[string]interface{}, error) {
	if row.Payload == "" {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(row.Payload), &out); err != nil {
		return nil, fmt.Errorf("a2a: decode payload: %w", err)
	}
	return out, nil
}
// Package private_memory implements per-Agent private notes described in
// PRD §4.5. Each owning Agent can read its own memories; no other Agent
// (including the opposing side) is allowed to read them. The Orchestrator
// may read every pool for audit purposes.
package private_memory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Type enumerates the categories of private memory entries defined in
// PRD §4.5.2.
type Type string

const (
	TypeStrategyNote    Type = "strategy_note"
	TypeEvidenceEval    Type = "evidence_eval"
	TypeOpponentWeakness Type = "opponent_weakness"
	TypeSelfCorrection  Type = "self_correction"
)

// ErrNotOwned is returned by List when the caller is not the owning Agent
// and is not the orchestrator.
var ErrNotOwned = errors.New("private_memory: caller not allowed to read this pool")

// Entry is the canonical record that flows through Append.
type Entry struct {
	ID                uuid.UUID
	MemoryUUID        string
	SessionID         uuid.UUID
	AgentID           uuid.UUID
	Round             int
	Type              Type
	Content           string
	LinkedEvidenceIDs  []string
	LinkedMessageUUIDs []string
	CreatedAt         time.Time
}

// Repository abstracts persistence. The default GORM implementation lives in
// gorm_repository.go; tests use InMemoryRepository.
type Repository interface {
	Append(ctx context.Context, e Entry) (Entry, error)
	List(ctx context.Context, sessionID, agentID uuid.UUID, viewerRole string) ([]model.PrivateMemory, error)
}

// NewEntry is a convenience constructor that fills in MemoryUUID and
// CreatedAt with zero values so the repository can stamp them.
func NewEntry(sessionID, agentID uuid.UUID, round int, t Type, content string) Entry {
	return Entry{
		SessionID: sessionID,
		AgentID:   agentID,
		Round:     round,
		Type:      t,
		Content:   content,
	}
}

// Validate ensures an Entry has the minimum required fields before persisting.
func (e Entry) Validate() error {
	if e.SessionID == uuid.Nil {
		return fmt.Errorf("private_memory: SessionID required")
	}
	if e.AgentID == uuid.Nil {
		return fmt.Errorf("private_memory: AgentID required")
	}
	if e.Content == "" {
		return fmt.Errorf("private_memory: Content required")
	}
	switch e.Type {
	case TypeStrategyNote, TypeEvidenceEval, TypeOpponentWeakness, TypeSelfCorrection:
	case "":
		return fmt.Errorf("private_memory: Type required")
	default:
		return fmt.Errorf("private_memory: invalid Type %q", e.Type)
	}
	return nil
}

// NewGormRepository returns the production GORM-backed Repository.
func NewGormRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

type gormRepository struct {
	db *gorm.DB
}

func (r *gormRepository) Append(ctx context.Context, e Entry) (Entry, error) {
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	if e.MemoryUUID == "" {
		e.MemoryUUID = uuid.New().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	row := model.PrivateMemory{
		SessionID:          e.SessionID,
		AgentID:            e.AgentID,
		MemoryUUID:         e.MemoryUUID,
		Round:              e.Round,
		Type:               string(e.Type),
		Content:            e.Content,
		LinkedEvidenceIDs:  e.LinkedEvidenceIDs,
		LinkedMessageUUIDs: e.LinkedMessageUUIDs,
		CreatedAt:          e.CreatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return Entry{}, err
	}
	e.ID = row.ID
	return e, nil
}

func (r *gormRepository) List(ctx context.Context, sessionID, agentID uuid.UUID, viewerRole string) ([]model.PrivateMemory, error) {
	// viewerRole semantics:
	//   "orchestrator" → can read any pool
	//   "agent:<agent_uuid>" → only matches its own AgentID
	//   anything else → forbidden
	switch {
	case viewerRole == "orchestrator":
		// pass
	case len(viewerRole) > len(agentRolePrefix) && viewerRole[:len(agentRolePrefix)] == agentRolePrefix:
		requesterID, err := uuid.Parse(viewerRole[len(agentRolePrefix):])
		if err != nil {
			return nil, ErrNotOwned
		}
		if requesterID != agentID {
			return nil, ErrNotOwned
		}
	default:
		return nil, ErrNotOwned
	}

	var rows []model.PrivateMemory
	if err := r.db.WithContext(ctx).
		Where("session_id = ? AND agent_id = ?", sessionID, agentID).
		Order("created_at asc").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

const agentRolePrefix = "agent:"
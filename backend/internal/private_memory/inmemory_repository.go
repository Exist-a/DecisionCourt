package private_memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// InMemoryRepository is an in-process Repository for tests. Same caveats as
// a2a.InMemoryRepository: data is lost on restart, not safe for production.
type InMemoryRepository struct {
	mu    sync.Mutex
	rows  []model.PrivateMemory
	clock func() time.Time
}

// NewInMemoryRepository returns a fresh repository. Pass nil for clock to use
// time.Now; pass a fixed clock for deterministic tests.
func NewInMemoryRepository(clock func() time.Time) *InMemoryRepository {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryRepository{clock: clock}
}

func (r *InMemoryRepository) Append(_ context.Context, e Entry) (Entry, error) {
	if err := e.Validate(); err != nil {
		return Entry{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if e.MemoryUUID == "" {
		e.MemoryUUID = uuid.New().String()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = r.clock()
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
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	row.ID = e.ID
	r.rows = append(r.rows, row)
	return e, nil
}

func (r *InMemoryRepository) List(_ context.Context, sessionID, agentID uuid.UUID, viewerRole string) ([]model.PrivateMemory, error) {
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

	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]model.PrivateMemory, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID != sessionID || row.AgentID != agentID {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
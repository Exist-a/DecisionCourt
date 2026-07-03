package a2a

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// InMemoryRepository is an in-process Repository for tests and ephemeral
// development runs. NOT safe for production use: data is lost on restart.
type InMemoryRepository struct {
	mu    sync.Mutex
	rows  []model.A2AMessage
	clock func() time.Time
}

// NewInMemoryRepository returns a fresh repository. Pass a custom clock for
// deterministic tests; nil defaults to time.Now.
func NewInMemoryRepository(clock func() time.Time) *InMemoryRepository {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryRepository{clock: clock}
}

func (r *InMemoryRepository) Insert(_ context.Context, row model.A2AMessage) (model.A2AMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = r.clock()
	}
	r.rows = append(r.rows, row)
	return row, nil
}

func (r *InMemoryRepository) ListVisibleTo(_ context.Context, sessionID uuid.UUID, viewer string) ([]model.A2AMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]model.A2AMessage, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID != sessionID {
			continue
		}
		if viewer == AddressOrchestrator {
			out = append(out, row)
			continue
		}
		if row.Visibility == string(VisibilityPublic) ||
			row.ToAgent == viewer ||
			row.FromAgent == viewer {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// ListPrivateMemory returns every v0.5 episodic-memory row in `sessionID`.
// See a2a.Repository.ListPrivateMemory for rationale.
func (r *InMemoryRepository) ListPrivateMemory(_ context.Context, sessionID uuid.UUID) ([]model.A2AMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]model.A2AMessage, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID != sessionID {
			continue
		}
		if !IsPrivateMemoryMessageType(MessageType(row.MessageType)) {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
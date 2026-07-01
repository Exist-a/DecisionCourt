package belief

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// InMemoryDiffRepository is the test/dev implementation of DiffRepository.
// NOT safe for production: data is lost on restart and there is no
// concurrency model across processes. Mirrors the convention used by
// a2a.InMemoryRepository and investigation.InMemoryRepository.
type InMemoryDiffRepository struct {
	mu    sync.Mutex
	rows  []model.BeliefDiff
	clock func() time.Time
}

// NewInMemoryDiffRepository builds a fresh repo. Passing nil for clock makes
// it use time.Now().UTC(); passing a deterministic clock is the canonical
// way to make tests reproducible.
func NewInMemoryDiffRepository(clock func() time.Time) *InMemoryDiffRepository {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryDiffRepository{clock: clock}
}

func (r *InMemoryDiffRepository) Insert(_ context.Context, row model.BeliefDiff) (model.BeliefDiff, error) {
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

func (r *InMemoryDiffRepository) ListBySession(_ context.Context, sessionID uuid.UUID) ([]model.BeliefDiff, error) {
	return r.listFiltered(sessionID, "", -1), nil
}

func (r *InMemoryDiffRepository) ListBySessionAndAgent(_ context.Context, sessionID uuid.UUID, agentType model.AgentType) ([]model.BeliefDiff, error) {
	return r.listFiltered(sessionID, agentType, -1), nil
}

func (r *InMemoryDiffRepository) ListBySessionAndRound(_ context.Context, sessionID uuid.UUID, round int) ([]model.BeliefDiff, error) {
	return r.listFiltered(sessionID, "", round), nil
}

func (r *InMemoryDiffRepository) listFiltered(sessionID uuid.UUID, agentType model.AgentType, round int) []model.BeliefDiff {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.BeliefDiff, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID != sessionID {
			continue
		}
		if agentType != "" && row.AgentType != agentType {
			continue
		}
		if round >= 0 && row.Round != round {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// InMemoryWeakenRepository is the test/dev implementation of WeakenRepository.
type InMemoryWeakenRepository struct {
	mu    sync.Mutex
	rows  []model.EvidenceWeakenLink
	clock func() time.Time
}

// NewInMemoryWeakenRepository builds a fresh repo. See InMemoryDiffRepository
// for the clock convention.
func NewInMemoryWeakenRepository(clock func() time.Time) *InMemoryWeakenRepository {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryWeakenRepository{clock: clock}
}

func (r *InMemoryWeakenRepository) Insert(_ context.Context, row model.EvidenceWeakenLink) (model.EvidenceWeakenLink, error) {
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

func (r *InMemoryWeakenRepository) ListByEvidence(_ context.Context, sessionID, evidenceID uuid.UUID) ([]model.EvidenceWeakenLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.EvidenceWeakenLink, 0)
	for _, row := range r.rows {
		if row.SessionID != sessionID || row.EvidenceID != evidenceID {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *InMemoryWeakenRepository) ListBySession(_ context.Context, sessionID uuid.UUID) ([]model.EvidenceWeakenLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.EvidenceWeakenLink, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID != sessionID {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

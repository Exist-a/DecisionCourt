package investigation

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// InMemoryRepository is an in-process Repository for tests. Same caveats
// as a2a.InMemoryRepository: data is lost on restart, not safe for prod.
type InMemoryRepository struct {
	mu    sync.Mutex
	rows  []Finding
	clock func() time.Time
}

// NewInMemoryRepository returns a fresh repository. Pass a custom clock
// for deterministic time-ordering tests; nil defaults to time.Now.
func NewInMemoryRepository(clock func() time.Time) *InMemoryRepository {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &InMemoryRepository{clock: clock}
}

// Create stamps a fresh Finding with UUID + CreatedAt and stores it.
func (r *InMemoryRepository) Create(_ context.Context, f *Finding) error {
	if f == nil {
		return ErrNilFinding
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.FindingUUID == "" {
		f.FindingUUID = uuid.New().String()
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = r.clock()
	}
	r.rows = append(r.rows, *f)
	return nil
}

// GetByID fetches a Finding by its primary key.
func (r *InMemoryRepository) GetByID(_ context.Context, id uuid.UUID) (*Finding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].ID == id {
			f := r.rows[i]
			return &f, nil
		}
	}
	return nil, ErrNotFound
}

// GetByUUID fetches a Finding by its audit-friendly UUID.
func (r *InMemoryRepository) GetByUUID(_ context.Context, findingUUID string) (*Finding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].FindingUUID == findingUUID {
			f := r.rows[i]
			return &f, nil
		}
	}
	return nil, ErrNotFound
}

// ListBySession returns all findings for one session, ordered by CreatedAt
// ascending (oldest first). Matches the contract the frontend timeline
// relies on.
func (r *InMemoryRepository) ListBySession(_ context.Context, sessionID uuid.UUID) ([]Finding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Finding, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID == sessionID {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// ErrNotFound is returned by Get* when the requested Finding doesn't exist.
var ErrNotFound = &findingError{"investigation: finding not found"}

// ErrNilFinding is returned by Create when the caller passes nil.
var ErrNilFinding = &findingError{"investigation: nil finding"}

type findingError struct{ msg string }

func (e *findingError) Error() string { return e.msg }
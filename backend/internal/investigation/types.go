// Package investigation implements the Investigator service defined in
// PRD §4.6 and refined in docs/decisioncourt-ux-refinement.md.
//
// Each Finding is the durable record of one Investigator search dispatched
// by a side (prosecutor / defender). Findings are intentionally kept
// separate from Evidence because they:
//
//   - are generated automatically by the Investigator, not user-submitted
//   - do not affect belief engines
//   - are visible to BOTH sides (a real courtroom transcript would also
//     record each lawyer's investigation requests)
//
// The Service is the only sanctioned entry point: callers must go through
// RecordFinding, which (1) runs the search, (2) persists the finding, and
// (3) emits public A2A dispatch + report messages so the bus, the audit
// log, and the frontend all observe the same event.
package investigation

import (
	"context"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// Repository persists InvestigationFinding rows. The default GORM impl
// lives in gorm_repository.go; tests use InMemoryRepository.
type Repository interface {
	Create(ctx context.Context, f *Finding) error
	GetByID(ctx context.Context, id uuid.UUID) (*Finding, error)
	GetByUUID(ctx context.Context, findingUUID string) (*Finding, error)
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]Finding, error)
}

// Finding is the in-memory representation that flows through Service.
// Note: Dispatcher and Investigator are AgentType strings (not uuid) —
// they identify the role, not a specific Agent row, because the
// Investigator itself is a singleton per session.
type Finding struct {
	ID             uuid.UUID
	FindingUUID    string
	SessionID      uuid.UUID
	Dispatcher     string
	Investigator   string
	Query          string
	Summary        string
	ResultCount    int
	SourceProvider string
	A2AMessageID   *uuid.UUID
	CreatedAt      time.Time
	RawResult      []string
}

// ToModel converts the in-memory Finding to its GORM row form.
func (f Finding) ToModel() model.InvestigationFinding {
	return model.InvestigationFinding{
		ID:             f.ID,
		FindingUUID:    f.FindingUUID,
		SessionID:      f.SessionID,
		Dispatcher:     f.Dispatcher,
		Investigator:   f.Investigator,
		Query:          f.Query,
		Summary:        f.Summary,
		ResultCount:    f.ResultCount,
		SourceProvider: f.SourceProvider,
		A2AMessageID:   f.A2AMessageID,
		CreatedAt:      f.CreatedAt,
		RawResult:      model.StringSlice(f.RawResult),
	}
}

// FromModel converts a GORM row back to the in-memory form.
func FromModel(m model.InvestigationFinding) Finding {
	return Finding{
		ID:             m.ID,
		FindingUUID:    m.FindingUUID,
		SessionID:      m.SessionID,
		Dispatcher:     m.Dispatcher,
		Investigator:   m.Investigator,
		Query:          m.Query,
		Summary:        m.Summary,
		ResultCount:    m.ResultCount,
		SourceProvider: m.SourceProvider,
		A2AMessageID:   m.A2AMessageID,
		CreatedAt:      m.CreatedAt,
		RawResult:      []string(m.RawResult),
	}
}
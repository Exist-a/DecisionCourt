package investigation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/google/uuid"
)

// Service is the entry point for recording Investigator findings. It owns
// the (searcher, repository, a2a bus) triple and serializes them so a
// single RecordFinding call runs the search, persists the finding, and
// publishes the public A2A dispatch + report events.
type Service struct {
	repo     Repository
	a2aBus   *a2a.Bus
	searcher search.Provider
}

// NewService wires the dependencies. a2aBus and searcher are required so
// callers can't accidentally bypass public visibility.
func NewService(repo Repository, a2aBus *a2a.Bus, searcher search.Provider) *Service {
	if a2aBus == nil {
		panic("investigation: a2a.Bus is required")
	}
	if searcher == nil {
		panic("investigation: search.Provider is required")
	}
	return &Service{repo: repo, a2aBus: a2aBus, searcher: searcher}
}

// SummaryMaxLen is the upper bound on Finding.Summary. Long search-result
// lists get truncated so the persistence layer doesn't blow up on verbose
// providers (bocha can return > 100KB per query).
const SummaryMaxLen = 1024

// SummaryTopN is how many search results get serialized into Summary before
// truncation kicks in.
const SummaryTopN = 5

// RecordFinding runs the search via the configured searcher, persists a
// Finding row, and emits PUBLIC A2A dispatch + report messages.
//
// Visibility is public because the courtroom rule (per UX refinement §1)
// is that any lawyer's investigator requests become part of the trial
// transcript — both sides can see what was looked up. This is symmetric
// with normal-courtroom practice.
//
// Returns the persisted Finding. The Finding.ID can be used to fetch the
// full row later; the Finding.FindingUUID is the audit-friendly identifier
// that flows through the A2A report payload.
func (s *Service) RecordFinding(
	ctx context.Context,
	session model.CourtSession,
	dispatcher string,
	query string,
) (*Finding, error) {
	if dispatcher != string(model.AgentProsecutor) && dispatcher != string(model.AgentDefender) {
		return nil, fmt.Errorf("investigation: dispatcher must be %q or %q, got %q",
			model.AgentProsecutor, model.AgentDefender, dispatcher)
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("investigation: query is required")
	}

	// 1) Run the actual search. Errors here are fatal — we can't manufacture
	//    findings from thin air.
	results, err := s.searcher.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("investigation: search: %w", err)
	}

	// 2) Build the in-memory Finding (no ID yet; repo stamps it).
	summary := buildSummary(results)
	f := &Finding{
		SessionID:      session.ID,
		Dispatcher:     dispatcher,
		Investigator:   string(model.AgentInvestigator),
		Query:          query,
		Summary:        summary,
		ResultCount:    len(results),
		SourceProvider: s.searcher.Name(),
		RawResult:      resultsToStrings(results),
	}

	// 3) Public A2A dispatch (dispatcher → investigator) BEFORE we persist,
	//    so any listener that watches the bus sees the request first.
	if _, err := s.a2aBus.Send(ctx, a2a.Message{
		SessionID:   session.ID,
		Round:       session.CurrentRound,
		Phase:       string(session.CurrentPhase),
		From:        dispatcher,
		To:          string(model.AgentInvestigator),
		MessageType: a2a.MessageTypeDispatch,
		Visibility:  a2a.VisibilityPublic,
		Payload: map[string]interface{}{
			"query":         query,
			"dispatched_by": dispatcher,
		},
	}); err != nil {
		return nil, fmt.Errorf("investigation: send dispatch: %w", err)
	}

	// 4) Persist.
	if err := s.repo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("investigation: persist: %w", err)
	}

	// 5) Public A2A report (investigator → dispatcher). The report payload
	//    carries the Finding's UUID + a small summary so a UI rendering the
	//    bus alone (without DB access) can still show what came back.
	if _, err := s.a2aBus.Send(ctx, a2a.Message{
		SessionID:   session.ID,
		Round:       session.CurrentRound,
		Phase:       string(session.CurrentPhase),
		From:        string(model.AgentInvestigator),
		To:          dispatcher,
		MessageType: a2a.MessageTypeReport,
		Visibility:  a2a.VisibilityPublic,
		Payload: map[string]interface{}{
			"query":         query,
			"dispatched_by": dispatcher,
			"finding_id":    f.FindingUUID,
			"result_count":  f.ResultCount,
			"summary":       summary,
			"source":        s.searcher.Name(),
		},
	}); err != nil {
		// Finding is already persisted; we log but don't fail the call.
		// Returning the Finding keeps the ReAct loop moving forward.
		return f, fmt.Errorf("investigation: send report: %w", err)
	}

	return f, nil
}

// ListBySession returns all findings for a session in chronological order
// (oldest first). Used by the dedicated InvestigatorPanel on the frontend.
func (s *Service) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]Finding, error) {
	return s.repo.ListBySession(ctx, sessionID)
}

// buildSummary joins the first SummaryTopN result titles/contents into a
// human-readable paragraph. Truncates to SummaryMaxLen runes.
func buildSummary(results []search.Result) string {
	if len(results) == 0 {
		return "无搜索结果"
	}
	n := len(results)
	if n > SummaryTopN {
		n = SummaryTopN
	}
	parts := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		r := results[i]
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = strings.TrimSpace(r.URL)
		}
		if title == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("[%d] %s", i+1, title))
	}
	if len(results) > SummaryTopN {
		parts = append(parts, fmt.Sprintf("… 其余 %d 条已省略", len(results)-SummaryTopN))
	}
	out := strings.Join(parts, "; ")
	runeOut := []rune(out)
	if len(runeOut) > SummaryMaxLen {
		return string(runeOut[:SummaryMaxLen]) + "…"
	}
	return out
}

// resultsToStrings flattens the search results into a JSON-friendly string
// slice for the optional RawResult column.
func resultsToStrings(results []search.Result) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, fmt.Sprintf("%s | %s | %s",
			strings.TrimSpace(r.Title), strings.TrimSpace(r.URL), strings.TrimSpace(r.Content)))
	}
	return out
}
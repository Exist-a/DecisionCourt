package api

// REST tests for the v0.6 belief-diff audit trail endpoint.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestGetBeliefDiffs_NoRepoReturnsEmpty verifies the legacy deployment
// fallback: when no DiffRepository is wired, the endpoint returns 200
// with an empty list instead of 500.
func TestGetBeliefDiffs_NoRepoReturnsEmpty(t *testing.T) {
	h := &Handler{
		service:  nil,
		sessionLookup: func(_ string) (model.CourtSession, bool) {
			return model.CourtSession{ID: uuid.New(), SessionUUID: "sess-no-repo"}, true
		},
	}
	r := ginEngine(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/sess-no-repo/belief-diffs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "no-repo path must return 200")
	var body struct {
		Code int         `json:"code"`
		Data interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
}

// TestGetBeliefDiffs_SessionNotFound verifies the 404 path.
func TestGetBeliefDiffs_SessionNotFound(t *testing.T) {
	h := &Handler{
		service:  nil,
		sessionLookup: func(_ string) (model.CourtSession, bool) { return model.CourtSession{}, false },
	}
	r := ginEngine(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/missing/belief-diffs", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestBeliefDiffResponse_ShapesAllFields verifies the response transformer
// emits every documented field so the frontend BeliefDiffCard can render
// without a follow-up fetch.
func TestBeliefDiffResponse_ShapesAllFields(t *testing.T) {
	evidenceID := uuid.New()
	d := model.BeliefDiff{
		ID:                uuid.New(),
		SessionID:         uuid.New(),
		Round:             2,
		Phase:             "cross_exam",
		AgentType:         model.AgentProsecutor,
		EvidenceID:        &evidenceID,
		Source:            model.BeliefSrcEvidence,
		Direction:         model.BeliefDirSupportsA,
		PriorBeliefA:      0.75,
		PosteriorBeliefA:  0.78,
		DeltaBeliefA:      0.03,
		PriorLogit:        1.0986,
		PosteriorLogit:    1.2736,
		EvidenceWeight:    0.504,
		WeakenFactor:      1.0,
		Reason:            "新证据：选项 A 的关键优势",
	}
	out := beliefDiffResponse(d)

	// All 14 documented fields must be present.
	for _, key := range []string{
		"id", "round", "phase", "agent_type", "evidence_id",
		"source", "direction", "prior_belief_a", "posterior_belief_a",
		"delta_belief_a", "prior_logit", "posterior_logit",
		"evidence_weight", "weaken_factor", "reason", "created_at",
	} {
		_, ok := out[key]
		require.True(t, ok, "field %q must be present in response", key)
	}
	require.Equal(t, evidenceID.String(), out["evidence_id"])
	require.Equal(t, "prosecutor", out["agent_type"])
}

// TestBeliefDiffResponse_NilEvidenceID handles the case where the diff
// has no associated evidence row (e.g. anchor-pull or weaken-only diffs).
func TestBeliefDiffResponse_NilEvidenceID(t *testing.T) {
	d := model.BeliefDiff{
		AgentType: model.AgentDefender,
		Source:    model.BeliefSrcAnchorPull,
	}
	out := beliefDiffResponse(d)
	require.Equal(t, "", out["evidence_id"], "nil evidence_id should serialize as empty string")
}

// verify we can call ListBySession through the repo (smoke test for the
// in-memory implementation that the handler test would have hit).
func TestBeliefInMemoryRepo_ListBySession_Smoke(t *testing.T) {
	repo := belief.NewInMemoryDiffRepository(nil)
	sessionID := uuid.New()
	evidenceID := uuid.New()
	_, err := repo.Insert(context.Background(), model.BeliefDiff{
		SessionID:    sessionID,
		AgentType:    model.AgentProsecutor,
		EvidenceID:   &evidenceID,
		Source:       model.BeliefSrcEvidence,
		Direction:    model.BeliefDirSupportsA,
		PriorBeliefA: 0.7, PosteriorBeliefA: 0.75, DeltaBeliefA: 0.05,
		PriorLogit: 0.8473, PosteriorLogit: 1.0986,
		EvidenceWeight: 0.5, WeakenFactor: 1.0, Reason: "test",
	})
	require.NoError(t, err)
	rows, err := repo.ListBySession(context.Background(), sessionID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

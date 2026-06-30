package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/investigation"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// newInvestigationOnlyHandler spins up a Handler with only the
// investigation service wired. courtroom.Service is nil because these
// tests don't exercise courtroom routes.
func newInvestigationOnlyHandler(inv *investigation.Service) *Handler {
	return &Handler{investigationService: inv}
}

// ginEngine wraps the Handler.RegisterRoutes inside a gin.Engine. We
// avoid the package-level Default() to keep test output quiet.
func ginEngine(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h.RegisterRoutes(r)
	return r
}

// stubSearcherForList is a minimal search.Provider used to record a
// handful of findings via the real investigation.Service. Mirrors the
// pattern in investigation/service_test.go.
type stubSearcherForList struct {
	results []search.Result
}

func (s *stubSearcherForList) Name() string { return "stub-list" }
func (s *stubSearcherForList) Search(_ context.Context, _ string) ([]search.Result, error) {
	return s.results, nil
}

// makeSession returns an in-memory CourtSession with a stable
// SessionUUID. The session isn't persisted to a DB; handler tests inject
// the lookup directly.
func makeSession() model.CourtSession {
	return model.CourtSession{
		ID:             uuid.New(),
		SessionUUID:    "sess-list-" + uuid.New().String()[:8],
		Title:          "列表测试庭审",
		OptionA:        "A",
		OptionB:        "B",
		CurrentPhase:   model.PhaseCrossExam,
		CurrentRound:   1,
		Status:         model.StatusActive,
	}
}

// TestGetInvestigations_ReturnsFindingsInOrder verifies the new
// /investigations endpoint returns a chronological list with the right
// fields so the InvestigatorPanel can hydrate on first load.
func TestGetInvestigations_ReturnsFindingsInOrder(t *testing.T) {
	session := makeSession()

	repo := investigation.NewInMemoryRepository(nil)
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	searcher := &stubSearcherForList{
		results: []search.Result{
			{Title: "报告 1", URL: "u1", Content: "c1"},
			{Title: "报告 2", URL: "u2", Content: "c2"},
		},
	}
	invSvc := investigation.NewService(repo, bus, searcher)

	// Record 3 findings in sequence so CreatedAt differs.
	ctx := context.Background()
	for _, q := range []string{"query-A", "query-B", "query-C"} {
		f, err := invSvc.RecordFinding(ctx, session, string(model.AgentProsecutor), q)
		require.NoError(t, err)
		require.NotEmpty(t, f.FindingUUID)
	}

	h := newInvestigationOnlyHandler(invSvc)
	h.sessionLookup = func(sessionUUID string) (model.CourtSession, bool) {
		if sessionUUID == session.SessionUUID {
			return session, true
		}
		return model.CourtSession{}, false
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/investigations", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var body struct {
		Code int                      `json:"code"`
		Data struct {
			Findings []map[string]interface{} `json:"findings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 0, body.Code)
	require.Len(t, body.Data.Findings, 3)

	// Ordered by CreatedAt ASC.
	for i, want := range []string{"query-A", "query-B", "query-C"} {
		require.Equal(t, want, body.Data.Findings[i]["query"])
		require.Equal(t, string(model.AgentProsecutor), body.Data.Findings[i]["dispatcher"])
		require.Equal(t, "stub-list", body.Data.Findings[i]["source"])
		require.Equal(t, float64(2), body.Data.Findings[i]["result_count"])
		require.NotEmpty(t, body.Data.Findings[i]["finding_uuid"])
		require.NotEmpty(t, body.Data.Findings[i]["summary"])
		require.NotEmpty(t, body.Data.Findings[i]["created_at"])
	}
}

// TestGetInvestigations_EmptyArrayWhenNoFindings ensures we return a 200
// with findings: [] (not 404, not null) when the session exists but the
// Investigator never ran.
func TestGetInvestigations_EmptyArrayWhenNoFindings(t *testing.T) {
	session := makeSession()

	repo := investigation.NewInMemoryRepository(nil)
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	invSvc := investigation.NewService(repo, bus, &stubSearcherForList{})

	h := newInvestigationOnlyHandler(invSvc)
	h.sessionLookup = func(sessionUUID string) (model.CourtSession, bool) {
		if sessionUUID == session.SessionUUID {
			return session, true
		}
		return model.CourtSession{}, false
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+session.SessionUUID+"/investigations", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	body := w.Body.String()
	require.Contains(t, body, `"findings":[]`, "应该返回空数组而非 null")
	require.NotContains(t, body, `"findings":null`)
}

// TestGetInvestigations_SessionNotFound covers the negative path so the
// frontend can detect a 404 and skip hydration gracefully.
func TestGetInvestigations_SessionNotFound(t *testing.T) {
	repo := investigation.NewInMemoryRepository(nil)
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	invSvc := investigation.NewService(repo, bus, &stubSearcherForList{})

	h := newInvestigationOnlyHandler(invSvc)
	h.sessionLookup = func(_ string) (model.CourtSession, bool) {
		return model.CourtSession{}, false
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/does-not-exist/investigations", nil)
	ginEngine(h).ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
	require.Contains(t, w.Body.String(), "庭审不存在")
}

// TestGetInvestigations_FiltersBySession makes sure findings for one
// session never leak into another's listing — a guard against
// cross-session privacy regressions.
func TestGetInvestigations_FiltersBySession(t *testing.T) {
	s1 := makeSession()
	s2 := makeSession()

	repo := investigation.NewInMemoryRepository(nil)
	a2aRepo := a2a.NewInMemoryRepository(nil)
	bus := a2a.NewBus(a2aRepo, nil)
	invSvc := investigation.NewService(repo, bus, &stubSearcherForList{results: []search.Result{
		{Title: "X", URL: "u", Content: "x"},
	}})

	ctx := context.Background()
	_, err := invSvc.RecordFinding(ctx, s1, string(model.AgentProsecutor), "s1-query")
	require.NoError(t, err)
	_, err = invSvc.RecordFinding(ctx, s1, string(model.AgentProsecutor), "s1-query-2")
	require.NoError(t, err)
	_, err = invSvc.RecordFinding(ctx, s2, string(model.AgentDefender), "s2-query")
	require.NoError(t, err)

	h := newInvestigationOnlyHandler(invSvc)
	h.sessionLookup = func(s string) (model.CourtSession, bool) {
		if s == s1.SessionUUID {
			return s1, true
		}
		if s == s2.SessionUUID {
			return s2, true
		}
		return model.CourtSession{}, false
	}

	// s1 应该看到 2 条
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+s1.SessionUUID+"/investigations", nil)
	ginEngine(h).ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code)
	var b1 struct {
		Data struct {
			Findings []map[string]interface{} `json:"findings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &b1))
	require.Len(t, b1.Data.Findings, 2)

	// s2 应该看到 1 条
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms/"+s2.SessionUUID+"/investigations", nil)
	ginEngine(h).ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var b2 struct {
		Data struct {
			Findings []map[string]interface{} `json:"findings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &b2))
	require.Len(t, b2.Data.Findings, 1)
	require.Equal(t, string(model.AgentDefender), b2.Data.Findings[0]["dispatcher"])
}
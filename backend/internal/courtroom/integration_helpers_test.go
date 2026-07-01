//go:build integration

package courtroom

// 本文件抽取自原 service_integration_test.go 里的类型、fixture、断言 helper。
// 集成测试用例拆成 *_integration_test.go 后缀的几个场景文件
// （integration_full_flow_test.go / integration_evidence_test.go /
//  integration_round_test.go），这些场景文件都依赖本文件的公共逻辑。
//
// 运行方式（需要真实 PG + LLM API key，本地 docker + .env）：
//   go test -tags integration -run TestStandard ./internal/courtroom/...
//   go test -tags integration -short ./internal/courtroom/...    # 仅单元子集
//
// fixtures 写入 backend/test-output/<scenario>-<sessionUUID>.json 以便复盘；
// 该目录已加入 .gitignore，UUID 命名保证幂等。

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/config"
	"github.com/decisioncourt/backend/internal/evidence"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/decisioncourt/backend/internal/search"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// ---------- 状态快照类型 ----------

// testState captures intermediate and final state for debugging.
type testState struct {
	SessionUUID   string                 `json:"session_uuid"`
	Title         string                 `json:"title"`
	Mode          string                 `json:"mode"`
	MaxRounds     int                    `json:"max_rounds"`
	FinalPhase    string                 `json:"final_phase"`
	FinalRound    int                    `json:"final_round"`
	Converged     bool                   `json:"converged"`
	Agents        []model.Agent          `json:"agents"`
	Messages      []messageSnapshot      `json:"messages"`
	Evidences     []model.Evidence       `json:"evidences"`
	Verdict       *model.Verdict         `json:"verdict,omitempty"`
	Events        []eventSnapshot        `json:"events"`
	AssertsPassed []string               `json:"asserts_passed"`
	AssertsFailed []string               `json:"asserts_failed"`
	StartedAt     time.Time              `json:"started_at"`
	FinishedAt    time.Time              `json:"finished_at"`
	DurationSec   float64                `json:"duration_sec"`
	Extra         map[string]interface{} `json:"extra,omitempty"`
}

type messageSnapshot struct {
	Phase        string   `json:"phase"`
	Round        int      `json:"round"`
	AgentType    string   `json:"agent_type"`
	Name         string   `json:"name"`
	Content      string   `json:"content"`
	EvidenceRefs []string `json:"evidence_refs"`
	Metadata     string   `json:"metadata"`
}

type eventSnapshot struct {
	Timestamp string                 `json:"timestamp"`
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
}

// ---------- Fixture ----------

type testFixture struct {
	t        *testing.T
	svc      *Service
	db       *gorm.DB
	events   []eventSnapshot
	eventsMu sync.Mutex
}

// loadEnv 从仓库根的 .env 读 key=value 并塞进当前 process 的环境变量。
// 这样 test 不需要把数据库/llm 配置硬编码，又能跟 docker-compose 集成。
func loadEnv(t *testing.T) {
	envPath := filepath.Join("..", "..", "..", ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if err := os.Setenv(key, val); err != nil {
			t.Fatalf("failed to set env %s: %v", key, err)
		}
	}
}

// setupFixture 用 .env + 真实 PG + 真实 LLM 构建端到端 fixture。
// 集成测试场景的入口函数。被 integration_*_test.go 复用。
func setupFixture(t *testing.T) *testFixture {
	loadEnv(t)

	config.Load()

	require.NoError(t, model.Connect(), "database connection failed")

	llmClient, err := llm.NewClient()
	require.NoError(t, err, "LLM client must initialize with real API key")

	db := model.DB
	bus := a2a.NewBus(a2a.NewGormRepository(db), nil)
	memRepo := private_memory.NewGormRepository(db)
	orchestrator := agent.NewOrchestratorLegacy(llmClient, bus, memRepo)
	evidenceSvc := evidence.NewService(db, llmClient)
	searcher, err := search.NewProvider(config.AppConfig.SearchProvider, config.AppConfig.BochaAPIKey)
	require.NoError(t, err)

	f := &testFixture{
		t:      t,
		db:     db,
		events: make([]eventSnapshot, 0),
	}

	svc := NewService(db, orchestrator, evidenceSvc, searcher, bus, func(sessionUUID string, event Event) {
		f.eventsMu.Lock()
		defer f.eventsMu.Unlock()
		f.events = append(f.events, eventSnapshot{
			Timestamp: event.Timestamp,
			Type:      event.Type,
			Payload:   event.Payload,
		})
	})
	f.svc = svc
	return f
}

func (f *testFixture) createSession(title, optionA, optionB, ctx, mode string) model.CourtSession {
	session, err := f.svc.CreateSession(title, optionA, optionB, ctx, mode)
	require.NoError(f.t, err)
	return session
}

func (f *testFixture) loadMessages(sessionID interface{}) []model.Message {
	var msgs []model.Message
	require.NoError(f.t, f.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&msgs).Error)
	return msgs
}

func (f *testFixture) loadEvidences(sessionID interface{}) []model.Evidence {
	var evs []model.Evidence
	require.NoError(f.t, f.db.Where("session_id = ?", sessionID).Order("created_at asc").Find(&evs).Error)
	return evs
}

func (f *testFixture) loadAgents(sessionID interface{}) []model.Agent {
	var agents []model.Agent
	require.NoError(f.t, f.db.Where("session_id = ?", sessionID).Find(&agents).Error)
	return agents
}

func (f *testFixture) loadVerdict(sessionID interface{}) *model.Verdict {
	var v model.Verdict
	err := f.db.Where("session_id = ?", sessionID).First(&v).Error
	if err != nil {
		return nil
	}
	return &v
}

func (f *testFixture) dumpState(session model.CourtSession, extra map[string]interface{}) testState {
	msgs := f.loadMessages(session.ID)
	evs := f.loadEvidences(session.ID)
	agents := f.loadAgents(session.ID)
	verdict := f.loadVerdict(session.ID)

	var msgSnaps []messageSnapshot
	for _, m := range msgs {
		agentType := ""
		name := ""
		if m.AgentID != nil {
			for _, a := range agents {
				if a.ID == *m.AgentID {
					agentType = string(a.AgentType)
					name = a.Name
					break
				}
			}
		}
		msgSnaps = append(msgSnaps, messageSnapshot{
			Phase:        m.Phase,
			Round:        m.Round,
			AgentType:    agentType,
			Name:         name,
			Content:      m.Content,
			EvidenceRefs: []string(m.EvidenceRefs),
			Metadata:     m.Metadata,
		})
	}

	var fresh model.CourtSession
	require.NoError(f.t, f.db.Where("id = ?", session.ID).First(&fresh).Error)

	return testState{
		SessionUUID: session.SessionUUID,
		Title:       session.Title,
		Mode:        session.Mode,
		MaxRounds:   session.MaxRounds,
		FinalPhase:  string(fresh.CurrentPhase),
		FinalRound:  fresh.CurrentRound,
		Converged:   fresh.Converged,
		Agents:      agents,
		Messages:    msgSnaps,
		Evidences:   evs,
		Verdict:     verdict,
		Events:      f.events,
		Extra:       extra,
	}
}

// saveJSON 把状态写入 backend/test-output/，目录已 .gitignore。
// UUID 文件名保证幂等：同一会话多次跑只会覆盖同一个文件。
func (f *testFixture) saveJSON(t *testing.T, state testState, filename string) {
	dir := filepath.Join("..", "..", "test-output")
	require.NoError(t, os.MkdirAll(dir, 0755))
	path := filepath.Join(dir, filename)
	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))
	t.Logf("state dumped to %s", path)
}

// ---------- 公共断言 helpers ----------

func assertMessageCounts(msgs []messageSnapshot, finalRound int, converged bool) []string {
	counts := make(map[string]int)
	for _, m := range msgs {
		key := fmt.Sprintf("%s_r%d_%s", m.Phase, m.Round, m.AgentType)
		counts[key]++
	}

	var failures []string

	// opening must always be present.
	for _, key := range []string{"opening_r0_prosecutor", "opening_r0_defender"} {
		if counts[key] != 1 {
			failures = append(failures, fmt.Sprintf("expected 1 message for %s, got %d", key, counts[key]))
		}
	}

	// cross_exam rounds: from 1 to finalRound (or maxRounds if not converged).
	maxRound := finalRound
	if converged {
		maxRound = finalRound
	}
	for round := 1; round <= maxRound; round++ {
		for _, agentType := range []string{"prosecutor", "defender"} {
			key := fmt.Sprintf("cross_exam_r%d_%s", round, agentType)
			if counts[key] != 1 {
				failures = append(failures, fmt.Sprintf("expected 1 message for %s, got %d", key, counts[key]))
			}
		}
	}

	// closing must be present at finalRound.
	for _, agentType := range []string{"prosecutor", "defender"} {
		key := fmt.Sprintf("closing_r%d_%s", finalRound, agentType)
		if counts[key] != 1 {
			failures = append(failures, fmt.Sprintf("expected 1 message for %s, got %d", key, counts[key]))
		}
	}

	return failures
}

// assertNoDuplicateAdjacent 检查两条相邻不同 agent 的发言内容是否完全一致。
func assertNoDuplicateAdjacent(msgs []messageSnapshot) []string {
	var failures []string
	for i := 1; i < len(msgs); i++ {
		prev := msgs[i-1]
		curr := msgs[i]
		if prev.AgentType != curr.AgentType && prev.Content == curr.Content {
			failures = append(failures, fmt.Sprintf("adjacent messages %d (%s) and %d (%s) have identical content", i-1, prev.AgentType, i, curr.AgentType))
		}
	}
	return failures
}

// assertNoSelfRepetition 检查同一 agent 在不同轮次的内容是否高度雷同（80% 以上词重合）。
func assertNoSelfRepetition(msgs []messageSnapshot) []string {
	var failures []string
	byAgent := make(map[string][]messageSnapshot)
	for _, m := range msgs {
		byAgent[m.AgentType] = append(byAgent[m.AgentType], m)
	}
	for agentType, agentMsgs := range byAgent {
		for i := 1; i < len(agentMsgs); i++ {
			prev := agentMsgs[i-1]
			curr := agentMsgs[i]
			if prev.Content == curr.Content {
				failures = append(failures, fmt.Sprintf("agent %s repeated identical content between %s/r%d and %s/r%d", agentType, prev.Phase, prev.Round, curr.Phase, curr.Round))
			} else {
				// Flag high similarity (>80% of words identical and order preserved) as repetition.
				prevWords := strings.Fields(prev.Content)
				currWords := strings.Fields(curr.Content)
				if len(prevWords) > 0 && len(currWords) > 0 {
					matches := 0
					pi := 0
					for _, cw := range currWords {
						found := false
						for j := pi; j < len(prevWords); j++ {
							if cw == prevWords[j] {
								matches++
								pi = j + 1
								found = true
								break
							}
						}
						if !found {
							pi = 0
						}
					}
					longer := len(prevWords)
					if len(currWords) > longer {
						longer = len(currWords)
					}
					if float64(matches)/float64(longer) > 0.8 {
						failures = append(failures, fmt.Sprintf("agent %s has highly repetitive content (%.0f%% similar) between %s/r%d and %s/r%d", agentType, float64(matches)/float64(longer)*100, prev.Phase, prev.Round, curr.Phase, curr.Round))
					}
				}
			}
		}
	}
	return failures
}

func assertNoFakeEvidenceRefs(msgs []messageSnapshot, evidenceIDs map[string]bool) []string {
	var failures []string
	for i, m := range msgs {
		for _, ref := range m.EvidenceRefs {
			if !evidenceIDs[ref] {
				failures = append(failures, fmt.Sprintf("message %d (%s) references unknown evidence %s", i, m.AgentType, ref))
			}
		}
	}
	return failures
}

func assertBeliefsUpdated(agents []model.Agent, evidenceCount int) []string {
	var failures []string
	for _, a := range agents {
		if a.AgentType == model.AgentProsecutor {
			if evidenceCount == 0 {
				continue
			}
			if a.BeliefA == 0.75 {
				failures = append(failures, fmt.Sprintf("prosecutor belief_a did not move from initial 0.75 despite %d evidences", evidenceCount))
			}
		}
		if a.AgentType == model.AgentDefender {
			if evidenceCount == 0 {
				continue
			}
			if a.BeliefA == 0.25 {
				failures = append(failures, fmt.Sprintf("defender belief_a did not move from initial 0.25 despite %d evidences", evidenceCount))
			}
		}
	}
	return failures
}

func assertJudgeBiasCompliance(agents []model.Agent, messages []messageSnapshot, events []eventSnapshot) []string {
	var failures []string
	var judge *model.Agent
	for _, a := range agents {
		if a.AgentType == model.AgentJudge {
			judge = &a
			break
		}
	}

	if judge == nil {
		failures = append(failures, "judge agent not found")
		return failures
	}

	if judge.BeliefA < 0 || judge.BeliefA > 1 {
		failures = append(failures, fmt.Sprintf("judge belief_a out of range [0,1]: %.4f", judge.BeliefA))
	}
	if judge.BeliefB < 0 || judge.BeliefB > 1 {
		failures = append(failures, fmt.Sprintf("judge belief_b out of range [0,1]: %.4f", judge.BeliefB))
	}

	sum := judge.BeliefA + judge.BeliefB
	if math.Abs(sum-1.0) > 0.01 {
		failures = append(failures, fmt.Sprintf("judge belief_a + belief_b != 1: %.4f + %.4f = %.4f", judge.BeliefA, judge.BeliefB, sum))
	}

	judgeBeliefUpdates := 0
	for _, e := range events {
		if e.Type == "judge.belief_update" {
			judgeBeliefUpdates++
		}
	}
	if judgeBeliefUpdates == 0 {
		failures = append(failures, "judge did not broadcast any belief update events")
	}

	return failures
}

// runStandardAssertions 是所有 standard mode 集成测试共用的标准断言束。
func runStandardAssertions(t *testing.T, state testState, evidenceCount int) (passed, failed []string) {
	if state.FinalPhase == "deliberation" {
		passed = append(passed, "final_phase_is_deliberation")
	} else {
		failed = append(failed, fmt.Sprintf("final_phase expected deliberation, got %s", state.FinalPhase))
	}

	countFailures := assertMessageCounts(state.Messages, state.FinalRound, state.Converged)
	if len(countFailures) == 0 {
		passed = append(passed, "message_counts_correct")
	} else {
		failed = append(failed, countFailures...)
	}

	adjFailures := assertNoDuplicateAdjacent(state.Messages)
	if len(adjFailures) == 0 {
		passed = append(passed, "no_duplicate_adjacent_content")
	} else {
		failed = append(failed, adjFailures...)
	}

	repFailures := assertNoSelfRepetition(state.Messages)
	if len(repFailures) == 0 {
		passed = append(passed, "no_self_repetition")
	} else {
		failed = append(failed, repFailures...)
	}

	evidenceIDs := make(map[string]bool)
	for _, e := range state.Evidences {
		evidenceIDs[e.EvidenceID] = true
	}
	fakeFailures := assertNoFakeEvidenceRefs(state.Messages, evidenceIDs)
	if len(fakeFailures) == 0 {
		passed = append(passed, "no_fake_evidence_refs")
	} else {
		failed = append(failed, fakeFailures...)
	}

	beliefFailures := assertBeliefsUpdated(state.Agents, evidenceCount)
	if len(beliefFailures) == 0 {
		passed = append(passed, "beliefs_updated_when_evidence_present")
	} else {
		failed = append(failed, beliefFailures...)
	}

	judgeBiasFailures := assertJudgeBiasCompliance(state.Agents, state.Messages, state.Events)
	if len(judgeBiasFailures) == 0 {
		passed = append(passed, "judge_bias_compliant")
	} else {
		failed = append(failed, judgeBiasFailures...)
	}

	if state.Verdict != nil {
		assert.NotEmpty(t, state.Verdict.Summary, "verdict summary should not be empty")
		assert.NotEmpty(t, state.Verdict.Content, "verdict content should not be empty")
		assert.GreaterOrEqual(t, state.Verdict.OptionAScore, 0.0)
		assert.LessOrEqual(t, state.Verdict.OptionAScore, 1.0)
		assert.GreaterOrEqual(t, state.Verdict.OptionBScore, 0.0)
		assert.LessOrEqual(t, state.Verdict.OptionBScore, 1.0)
		passed = append(passed, "verdict_generated_with_valid_scores")
	} else {
		failed = append(failed, "verdict_not_generated")
	}

	return passed, failed
}

// containsJSONLeak 当 failed 列表里出现 raw JSON 内容相关词条时返回 true。
func containsJSONLeak(failures []string) bool {
	for _, f := range failures {
		if strings.Contains(f, "raw JSON") {
			return true
		}
	}
	return false
}

// 防止 imported and not used：context / sync 是被 fixture 间接使用。
var _ = context.Background
var _ sync.Mutex

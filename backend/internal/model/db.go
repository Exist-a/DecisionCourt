package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decisioncourt/backend/internal/config"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// StringSlice is a custom GORM type for JSONB arrays that can scan from
// PostgreSQL jsonb values whether they were stored as []byte or string.
type StringSlice []string

func (s *StringSlice) Scan(value interface{}) error {
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, s)
	case string:
		return json.Unmarshal([]byte(v), s)
	case nil:
		*s = nil
		return nil
	default:
		return fmt.Errorf("unsupported Scan value type %T for StringSlice", value)
	}
}

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(s)
}

var DB *gorm.DB

// CourtPhase represents the current phase of a courtroom session.
type CourtPhase string

const (
	PhaseIdle             CourtPhase = "idle"
	PhaseClarification    CourtPhase = "clarification"
	PhaseOptionGeneration CourtPhase = "option_generation"
	PhaseOpening          CourtPhase = "opening"
	PhaseEvidence         CourtPhase = "evidence"
	PhaseCrossExam        CourtPhase = "cross_exam"
	PhaseClosing          CourtPhase = "closing"
	PhaseDeliberation     CourtPhase = "deliberation"
	PhaseVerdict          CourtPhase = "verdict"
	PhaseAppeal           CourtPhase = "appeal"
)

// CourtStatus represents the overall status of a session.
type CourtStatus string

const (
	StatusActive   CourtStatus = "active"
	StatusPaused   CourtStatus = "paused"
	StatusCompleted CourtStatus = "completed"
	StatusAborted  CourtStatus = "aborted"
)

// AgentType defines the role of an agent.
type AgentType string

const (
	AgentProsecutor  AgentType = "prosecutor"
	AgentDefender    AgentType = "defender"
	AgentInvestigator AgentType = "investigator"
	AgentClerk       AgentType = "clerk"
	AgentJudge       AgentType = "judge"
)

// CourtSession stores the basic information and current state of a trial.
type CourtSession struct {
	ID             uuid.UUID   `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionUUID    string      `gorm:"type:varchar(36);uniqueIndex;not null"`
	Title          string      `gorm:"type:varchar(255);not null"`
	OptionA        string      `gorm:"type:varchar(255)"`
	OptionB        string      `gorm:"type:varchar(255)"`
	Context        string      `gorm:"type:text"`
	Mode           string      `gorm:"type:varchar(20);default:'standard'"`
	MaxRounds      int         `gorm:"default:3"`
	CurrentPhase   CourtPhase  `gorm:"type:varchar(50);default:'idle'"`
	CurrentRound   int         `gorm:"default:0"`
	Status         CourtStatus `gorm:"type:varchar(20);default:'active'"`
	Converged      bool        `gorm:"default:false"`
	CreatedAt      time.Time
	UpdatedAt      time.Time

	Agents          []Agent          `gorm:"foreignKey:SessionID"`
	Evidences       []Evidence       `gorm:"foreignKey:SessionID"`
	Messages        []Message        `gorm:"foreignKey:SessionID"`
	BeliefSnapshots []BeliefSnapshot `gorm:"foreignKey:SessionID"`
	Verdict         *Verdict         `gorm:"foreignKey:SessionID"`
	LLMCalls        []LLMCall        `gorm:"foreignKey:SessionID"`
	SearchLogs      []SearchLog      `gorm:"foreignKey:SessionID"`
}

// Agent stores the role and current belief state of an agent in a session.
type Agent struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	SessionID     uuid.UUID `gorm:"type:uuid;index;not null" json:"session_id"`
	AgentUUID     string    `gorm:"type:varchar(36);uniqueIndex;not null" json:"agent_uuid"`
	AgentType     AgentType `gorm:"type:varchar(50);not null" json:"agent_type"`
	Name          string    `gorm:"type:varchar(100);not null" json:"name"`
	Role          string    `gorm:"type:text" json:"role"`
	BeliefA       float64   `gorm:"type:float;default:0.5" json:"belief_a"`
	BeliefB       float64   `gorm:"type:float;default:0.5" json:"belief_b"`
	Model         string    `gorm:"type:varchar(50)" json:"model"`
	Temperature   float64   `gorm:"type:float;default:0.7" json:"temperature"`
	SystemPrompt  string    `gorm:"type:text" json:"system_prompt"`
	Status        string    `gorm:"type:varchar(20);default:'active'" json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Evidence stores all evidence submitted during a trial.
type Evidence struct {
	ID                uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	SessionID         uuid.UUID `gorm:"type:uuid;index;not null" json:"session_id"`
	EvidenceID        string    `gorm:"type:varchar(50);not null" json:"evidence_id"`
	Type              string    `gorm:"type:varchar(30);not null" json:"type"`
	Source            string    `gorm:"type:varchar(30);not null" json:"source"`
	Content           string    `gorm:"type:text;not null" json:"content"`
	URL               string    `gorm:"type:varchar(500)" json:"url"`
	SubmittedBy       string    `gorm:"type:varchar(50);not null" json:"submitted_by"`
	CredibilityScore  float64   `gorm:"type:float;default:0.5" json:"credibility_score"`
	RelevanceScore    float64   `gorm:"type:float;default:0.5" json:"relevance_score"`
	ImpactOnOptionA    float64   `gorm:"type:float;default:0" json:"impact_on_option_a"`
	ImpactOnOptionB    float64   `gorm:"type:float;default:0" json:"impact_on_option_b"`
	ConstraintStrength float64   `gorm:"type:float;default:0" json:"constraint_strength"`
	Status             string    `gorm:"type:varchar(20);default:'admitted'" json:"status"`
	ChallengeReason    string    `gorm:"type:text" json:"challenge_reason"`
	CreatedAt          time.Time `json:"created_at"`
}

// Message stores agent speeches, user actions, and system events.
type Message struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	SessionID     uuid.UUID `gorm:"type:uuid;index;not null" json:"session_id"`
	AgentID       *uuid.UUID `gorm:"type:uuid;index" json:"agent_id"`
	Phase         string    `gorm:"type:varchar(50)" json:"phase"`
	Round         int       `gorm:"default:0" json:"round"`
	Content       string    `gorm:"type:text" json:"content"`
	EvidenceRefs  StringSlice `gorm:"type:jsonb;default:'[]'" json:"evidence_refs"`
	ActionType    string    `gorm:"type:varchar(50);not null" json:"action_type"`
	Metadata      string    `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt     time.Time `json:"created_at"`
}

// BeliefSnapshot records agent belief states after each round.
type BeliefSnapshot struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID     uuid.UUID `gorm:"type:uuid;index;not null"`
	AgentID       uuid.UUID `gorm:"type:uuid;index;not null"`
	Round         int       `gorm:"not null"`
	BeliefA       float64   `gorm:"type:float;not null"`
	BeliefB       float64   `gorm:"type:float;not null"`
	Delta         float64   `gorm:"type:float;default:0"`
	TriggerEvent  string    `gorm:"type:varchar(50)"`
	CreatedAt     time.Time
}

// Verdict stores the final decision report for a session.
type Verdict struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID        uuid.UUID `gorm:"type:uuid;uniqueIndex;not null"`
	Content          string    `gorm:"type:text"`
	Summary          string    `gorm:"type:text"`
	OptionAScore     float64   `gorm:"type:float;default:0"`
	OptionBScore     float64   `gorm:"type:float;default:0"`
	ConsensusPoints  string    `gorm:"type:jsonb;default:'[]'"`
	DivergencePoints string    `gorm:"type:jsonb;default:'[]'"`
	Recommendation   string    `gorm:"type:text"`
	UserFeedback     string    `gorm:"type:varchar(20);default:'none'"`
	CreatedAt        time.Time
}

// LLMCall logs every LLM invocation for cost and observability.
type LLMCall struct {
	ID                uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID         uuid.UUID `gorm:"type:uuid;index;not null"`
	AgentID           *uuid.UUID `gorm:"type:uuid;index"`
	TaskType          string    `gorm:"type:varchar(50)"`
	Model             string    `gorm:"type:varchar(50)"`
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	CostUSD           float64   `gorm:"type:decimal(10,6)"`
	CostCNY           float64   `gorm:"type:decimal(10,6)"`
	LatencyMs         int
	Status            string    `gorm:"type:varchar(20);default:'success'"`
	ErrorMsg          string    `gorm:"type:text"`
	CreatedAt         time.Time
}

// SearchLog records every search performed by the investigator agent.
type SearchLog struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID    uuid.UUID `gorm:"type:uuid;index;not null"`
	AgentID      *uuid.UUID `gorm:"type:uuid;index"`
	Provider     string    `gorm:"type:varchar(30)"`
	Query        string    `gorm:"type:text"`
	ResultCount  int
	LatencyMs    int
	CreatedAt    time.Time
}

// A2AMessage records every Agent-to-Agent message routed through the A2A bus.
// Visibility marks whether the payload is public (visible to all agents) or
// private to a single recipient (e.g. prosecutor-only dispatch report).
type A2AMessage struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID   uuid.UUID `gorm:"type:uuid;index;not null"`
	MessageUUID string    `gorm:"type:varchar(36);uniqueIndex;not null"`
	Round       int       `gorm:"default:0"`
	Phase       string    `gorm:"type:varchar(50)"`
	FromAgent   string    `gorm:"type:varchar(50);not null"`
	ToAgent     string    `gorm:"type:varchar(50);not null"`
	MessageType string    `gorm:"type:varchar(30);not null"`
	Payload     string    `gorm:"type:jsonb;not null"`
	Visibility  string    `gorm:"type:varchar(20);not null;default:'public'"`
	MemoryRefs  StringSlice `gorm:"type:jsonb;default:'[]'"`
	CreatedAt   time.Time
}

// PrivateMemory stores per-agent private notes that are never visible to
// other agents. Only the owning agent and the orchestrator can read these.
type PrivateMemory struct {
	ID                uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID         uuid.UUID `gorm:"type:uuid;index;not null"`
	AgentID           uuid.UUID `gorm:"type:uuid;index;not null"`
	MemoryUUID        string    `gorm:"type:varchar(36);uniqueIndex;not null"`
	Round             int       `gorm:"default:0"`
	Type              string    `gorm:"type:varchar(50);not null"`
	Content           string    `gorm:"type:text;not null"`
	LinkedEvidenceIDs  StringSlice `gorm:"type:jsonb;default:'[]'"`
	LinkedMessageUUIDs StringSlice `gorm:"type:jsonb;default:'[]'"`
	CreatedAt         time.Time
}

// Connect establishes the database connection and runs auto-migration.
func Connect() error {
	dsn := config.AppConfig.DatabaseURL
	if dsn == "" {
		dsn = "postgres://user:pass@localhost:5432/decisioncourt?sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	DB = db

	return db.AutoMigrate(
		&CourtSession{},
		&Agent{},
		&Evidence{},
		&Message{},
		&BeliefSnapshot{},
		&Verdict{},
		&LLMCall{},
		&SearchLog{},
		&A2AMessage{},
		&PrivateMemory{},
		&InvestigationFinding{},
	)
}

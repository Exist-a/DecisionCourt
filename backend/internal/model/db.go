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
	// v0.8.3 安全：OwnerID 关联到 User.UserID(匿名 user_id)。
	// 鉴权时若 viewer != session.OwnerID,GET 端点返回 403,write 端点 403。
	// 旧 session 在迁移时 OwnerID 为空,后端按"未鉴权"对待(只允许 owner 访问)。
	OwnerID        string      `gorm:"type:varchar(64);index;not null;default:''"`
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
	// v0.8.3 安全：SubmittedBy 现在是真实的 anonymous user_id（不再是写死的 "user"）。
	// 旧记录保持 "user" 字符串,但前端不再显示成"用户"——可选择性迁移为 "legacy_anon"。
	SubmittedBy       string    `gorm:"type:varchar(64);not null" json:"submitted_by"`
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
	// TrialSummary 是庭审过程纪要（1-2 句叙事：双方核心攻防 + 关键转折点）。
	// 与 Summary（采纳建议）不同，TrialSummary 让用户"读完之后能复述整场庭审发生了什么"。
	// v0.5+ UX 增量；由 ClerkAgent 在 verdict 生成时输出。
	TrialSummary     string    `gorm:"type:text"`
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

// User 记录匿名身份。user_id 由前端在 localStorage 生成(UUIDv4)首次访问
// 时通过 POST /api/v1/auth/anon 报到后端；后端用 user_id 签发 JWT,后续
// 请求带 cookie 即可。无密码 / 无邮箱 / 无 PII——纯匿名计数器。
//
// UserID 格式约定：`anon_<32-hex>`(前端用 `crypto.randomUUID().replace(/-/g,'')`)。
// 我们不在这里强制格式(允许自定义前缀方便测试),但要 index 以便 JOIN 提速。
type User struct {
	UserID     string    `gorm:"type:varchar(64);primary_key" json:"user_id"`
	FirstSeen  time.Time `gorm:"not null" json:"first_seen"`
	LastSeen   time.Time `gorm:"not null" json:"last_seen"`
	// 简单的 IP+UA 指纹,只用于风控(异常登录告警),不做任何反向追踪。
	// 留空表示未记录(GDPR 友好)。
	// LastUA 受 PG varchar(200) 限制,写入前在 util.TruncateUA 截断到 ≤ 200 rune;
	// 移动浏览器/微信内置浏览器 UA 常达 450+ 字符,直接 INSERT 会触发 SQLSTATE 22001。
	LastIP     string    `gorm:"type:varchar(45)" json:"last_ip,omitempty"`
	LastUA     string    `gorm:"type:varchar(200)" json:"last_ua,omitempty"`
}

// AuditLog 记录所有"重要操作"用于安全审计。
// 不记录常规 GET,只记录:登录/登出/Export/SubmitEvidence/ProcessUserAction/PhaseTransition。
// 未来 CI 跑 `grep suspicious AuditLog` 自动告警。
type AuditLog struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	UserID      string    `gorm:"type:varchar(64);index;not null"`
	Action      string    `gorm:"type:varchar(50);not null;index"`  // e.g. "auth.anon" / "session.export"
	Target      string    `gorm:"type:varchar(100);index"`          // e.g. session_uuid
	IP          string    `gorm:"type:varchar(45)"`
	// UA 受 PG varchar(200) 限制,写入前在 util.TruncateUA 截断到 ≤ 200 rune。
	UA          string    `gorm:"type:varchar(200)"`
	Result      string    `gorm:"type:varchar(20);default:'ok'"`    // "ok" / "denied" / "error"
	Reason      string    `gorm:"type:varchar(200)"`                // 拒绝原因 / 错误摘要
	CreatedAt   time.Time `gorm:"index"`
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
		&User{}, // P0-1 鉴权:anon_auth upsert 必需,前几版遗漏导致首次启动后 /auth/anon 一调就 SQLSTATE 42P01
		&AuditLog{}, // P0-1 鉴权:writeAudit 必需,否则 audit_log INSERT 也炸
		&CourtSession{},
		&Agent{},
		&Evidence{},
		&Message{},
		&BeliefSnapshot{},
		&BeliefDiff{},
		&EvidenceWeakenLink{},
		&Verdict{},
		&LLMCall{},
		&SearchLog{},
		&A2AMessage{},
		&PrivateMemory{},
		&InvestigationFinding{},
		// v0.8 白盒化：业务级 span / 状态机迁移审计
		&DecisionEvent{},
	)
}

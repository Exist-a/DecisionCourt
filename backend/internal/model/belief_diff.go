package model

import (
	"time"

	"github.com/google/uuid"
)

// BeliefDiff records one observed belief-state transition for a single agent
// in a single round. Combined with the underlying BeliefSnapshot table this
// gives us an event-sourced audit trail: snapshots are aggregated views, diffs
// are the events.
//
// One BeliefDiff row maps to ONE trigger (an evidence impact, an anchor pull
// after enough rounds, or a weaken-edge application). If multiple triggers
// fire in the same round, multiple rows are written in the order they fired.
//
// The Posterior/BeliefA fields are clamped to [0.05, 0.95] and rounded to 4
// decimal places; the logit twins are kept verbatim so an offline re-plot can
// recover the original numbers.
type BeliefDiff struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID        uuid.UUID `gorm:"type:uuid;index;not null"`
	Round            int       `gorm:"not null"`
	Phase            string    `gorm:"type:varchar(32);not null"`
	AgentType        AgentType `gorm:"type:varchar(32);not null"`
	EvidenceID       *uuid.UUID `gorm:"type:uuid;index"`
	Source           string    `gorm:"type:varchar(32);not null"`     // "evidence" | "weaken" | "anchor_pull" | "stance"
	Direction        string    `gorm:"type:varchar(16);not null"`     // "supports_a" | "supports_b" | "neutral"
	PriorBeliefA     float64   `gorm:"type:decimal(5,4);not null"`
	PosteriorBeliefA float64   `gorm:"type:decimal(5,4);not null"`
	DeltaBeliefA     float64   `gorm:"type:decimal(6,4);not null"`
	PriorLogit       float64   `gorm:"type:decimal(8,4);not null"`
	PosteriorLogit   float64   `gorm:"type:decimal(8,4);not null"`
	EvidenceWeight   float64   `gorm:"type:decimal(5,3)"`
	WeakenFactor     float64   `gorm:"type:decimal(4,2);default:1.0"` // multiplier on the *next* evidence application
	Reason           string    `gorm:"type:text"`
	CreatedAt        time.Time
}

// TableName explicitly pins the SQL table name; GORM defaults to pluralising
// the struct name (belief_diffs) which already matches, but we spell it
// anyway to avoid GORM auto-pluralisation surprises on future renames.
func (BeliefDiff) TableName() string { return "belief_diffs" }

// BelieSource enumerates the possible Source field values. Kept as typed
// constants so callers can use the type system instead of magic strings.
const (
	BeliefSrcEvidence    = "evidence"
	BeliefSrcWeaken      = "weaken"
	BeliefSrcAnchorPull  = "anchor_pull"
	// BeliefSrcStance records the per-speak belief micro-adjustment applied
	// by Service.applySpeakToBelief (v0.5+ design, see service.go). Without
	// this row the audit trail has a gap between consecutive evidence diffs,
	// making it look like belief_a "jumped" with no recorded cause.
	BeliefSrcStance = "stance"

	// BeliefSrcRebuttal (v2.9 PR-4 / ADR 0043): 法官 verdict 时, 每个 standing
	// 状态的证据被"显式忽略"写入一条 belief_diffs row (weight=0.0, direction=neutral)。
	// 让 audit trail 在 verdict 阶段也能溯源"这条 evidence 之前有 standing 反
	// 驳,法官判决时为何采用/未采用"。
	// 注意: 这只是 trail, 不影响 BelieA/B 数值 — verdict 阶段的 BeliefA/B
	// 是 judge LLM 直接输出 (Service.go L1613-1617 写), 不重算。
	BeliefSrcRebuttal = "rebuttal"
)

// BeliefDirection enumerates the possible Direction field values.
const (
	BeliefDirSupportsA = "supports_a"
	BeliefDirSupportsB = "supports_b"
	BeliefDirNeutral   = "neutral"
)

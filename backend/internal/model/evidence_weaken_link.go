package model

import (
	"time"

	"github.com/google/uuid"
)

// EvidenceWeakenLink records a "weakening edge" — an explicit attack by one
// agent against the *transmission* of an evidence piece, rather than its
// underlying truth. Inspired by 异构论辩图谱 (China patent CN202610034750):
// weakening edges point at edges instead of nodes, so the next time the
// pointed-to evidence tries to move any agent's belief, the multiplier is
// reduced by max(weaken_strength) targeting that agent.
//
// Each row is one weakening declaration. Rows are append-only; we keep all
// history so a later round can re-trace "who first questioned E001?".
type EvidenceWeakenLink struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID      uuid.UUID `gorm:"type:uuid;index;not null"`
	EvidenceID     uuid.UUID `gorm:"type:uuid;index;not null"`
	AggressorMsg   *uuid.UUID `gorm:"type:uuid"`                    // optional: the message that declared the weaken
	AggressorAgent string    `gorm:"type:varchar(32);not null"`     // who declared it (defender / prosecutor)
	TargetAgent    AgentType `gorm:"type:varchar(32);not null"`     // whose belief is attenuated
	WeakenStrength float64   `gorm:"type:decimal(4,2);not null"`    // 0..1, attenuation amount
	Rationale      string    `gorm:"type:text"`
	CreatedAt      time.Time
}

// TableName explicit (mirrors convention of other model files).
func (EvidenceWeakenLink) TableName() string { return "evidence_weaken_links" }

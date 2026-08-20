package model

import (
	"time"

	"github.com/google/uuid"
)

// EvidenceRebuttalLink records a "rebuttal edge" — an explicit rebuttal
// declaration by one agent against another piece of evidence.
//
// v1.0.2 (候选 4): 实现 PRD §4.3.3 提出的 "已反驳证据集合跟踪" 状态机。
//
// 语义区别于 EvidenceWeakenLink (v0.6):
//   - WeakenLink: "质疑某条 evidence 的传播" (attacker → target_agent, weight ×衰减)
//   - RebuttalLink: "反驳某条 evidence 的内容" (aggressor → rebutted_evidence,
//                   内容层否定,后端硬拒引用)
//
// 设计决策 (ADR 0030):
//   - 独立表 (而非 Evidence 表加字段), 关系型设计支持多轮链 (A→B→C)
//   - 状态机: standing (未翻盘) / overturned (被翻盘) / withdrawn (撤回)
//   - Strength 0..1 标识反驳强度 (供前端 audit trail 显示, 后端不直接衰减 weight)
//
// 每个 row 是单次 rebuttal 声明;rows 是 append-only, 保留历史以供
// "谁先质疑 E001 / 谁翻盘了" 回放。
type EvidenceRebuttalLink struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primary_key;default:gen_random_uuid()"`
	SessionID           uuid.UUID  `gorm:"type:uuid;index;not null"`
	RebuttedEvidenceID  uuid.UUID  `gorm:"type:uuid;index;not null"` // 被反驳的 evidence (E001 等)
	RebuttingEvidenceID *uuid.UUID `gorm:"type:uuid;index"`          // 反驳用的 evidence (可能为 nil: 纯立场反驳)
	AggressorMsgID      *uuid.UUID `gorm:"type:uuid"`               // 反驳方发言的 Message.ID (可能 nil: 系统层注入)
	AggressorAgent      string     `gorm:"type:varchar(32);not null"`
	// Status: standing (未翻盘, 后端硬拒引用) / overturned (被翻盘, 恢复正常引用) / withdrawn (撤回)
	Status    string  `gorm:"type:varchar(20);not null;default:'standing'"`
	Strength  float64 `gorm:"type:decimal(4,2);not null;default:0.5"` // 0..1, 反驳强度 (frontend display / audit)
	Rationale string  `gorm:"type:text"`                               // 简短理由 (≤50 字, prompt 约束)
	CreatedAt time.Time
}

// TableName explicit (mirrors convention of other model files).
func (EvidenceRebuttalLink) TableName() string { return "evidence_rebuttal_links" }

// Status 常量 — 后端硬拒只针对 "standing" 状态 (ADR 0030 §决策)。
const (
	RebuttalStatusStanding   = "standing"
	RebuttalStatusOverturned = "overturned"
	RebuttalStatusWithdrawn  = "withdrawn"
)
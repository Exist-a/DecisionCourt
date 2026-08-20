// Package courtroom - rebuttal_repository.go
//
// v1.0.2 候选 4: GORM RebuttalRepository 实现 (PR-4 落地点)。
// 对称 v0.6 belief.GormWeakenRepository, 但语义不同:
//   - WeakenRepository: 衰减 evidence 传播对某 agent 的影响 (advisory)
//   - RebuttalRepository: standing 状态 evidence 被引用时 hard reject (强制 guard)
//
// 与 agent.RebuttalRepository 接口 (PR-3) 的契约差异:
//   - agent.RebuttalRepository: 窄接口 (ListStandingRebuttedIDs + Insert)
//   - 本包 RebuttalRepository: 完整 CRUD (ListBySession / ListByEvidence / Insert /
//     UpdateStatus / ListStandingByEvidence),供 REST API + Service 用
//
// 接口位于本包 (而不是 agent 包),因为本包 owner courtroom.Service 调用。
package courtroom

import (
	"context"
	"strings"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RebuttalRepository (v1.0.2 候选 4) 完整接口,供 courtroom.Service 调用。
//
// 注意: 本接口与 agent.RebuttalRepository (PR-3 hard reject 用) 是不同视角的
// 抽象: agent 接口是 narrow (够用即可), 本接口是 broad (Service + REST 用)。
// 双向适配由 GormRebuttalRepository 同时实现两个接口完成。
type RebuttalRepository interface {
	// Insert 持久化一条 rebuttal link (default status='standing').
	Insert(ctx context.Context, link model.EvidenceRebuttalLink) (model.EvidenceRebuttalLink, error)

	// ListBySession 返回 session 所有 rebuttal links (按 created_at ASC).
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]model.EvidenceRebuttalLink, error)

	// ListByEvidence 返回针对某条 evidence 的所有 rebuttal links.
	ListByEvidence(ctx context.Context, sessionID, evidenceID uuid.UUID) ([]model.EvidenceRebuttalLink, error)

	// ListStandingByDisplayIDs 是 PR-3 agent 接口的实现,接受 display_ids
	// (e.g. ["E001"]) 转换为 UUID 后批量查 standing 状态.
	// 返回 display_ids 列表 (顺序与 standing 集合一致).
	ListStandingByDisplayIDs(ctx context.Context, sessionID uuid.UUID, displayIDs []string) ([]string, error)

	// UpdateStatus 翻盘/撤回 link (standing → overturned/withdrawn).
	// 返回更新后的 row.
	UpdateStatus(ctx context.Context, linkID uuid.UUID, newStatus string) (model.EvidenceRebuttalLink, error)
}

// GormRebuttalRepository 是 RebuttalRepository 的 GORM 实现.
type GormRebuttalRepository struct {
	db *gorm.DB
}

// NewGormRebuttalRepository 构造 GORM 实现. 调方须保证 schema 已迁移 (model.Connect).
func NewGormRebuttalRepository(db *gorm.DB) *GormRebuttalRepository {
	return &GormRebuttalRepository{db: db}
}

// Insert 实现 RebuttalRepository.Insert.
func (r *GormRebuttalRepository) Insert(ctx context.Context, link model.EvidenceRebuttalLink) (model.EvidenceRebuttalLink, error) {
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	if link.Status == "" {
		link.Status = model.RebuttalStatusStanding
	}
	if err := r.db.WithContext(ctx).Create(&link).Error; err != nil {
		return model.EvidenceRebuttalLink{}, err
	}
	return link, nil
}

// ListBySession 实现 RebuttalRepository.ListBySession.
func (r *GormRebuttalRepository) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]model.EvidenceRebuttalLink, error) {
	var rows []model.EvidenceRebuttalLink
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// ListByEvidence 实现 RebuttalRepository.ListByEvidence.
func (r *GormRebuttalRepository) ListByEvidence(ctx context.Context, sessionID, evidenceID uuid.UUID) ([]model.EvidenceRebuttalLink, error) {
	var rows []model.EvidenceRebuttalLink
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND rebutted_evidence_id = ?", sessionID, evidenceID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// ListStandingByDisplayIDs 是 PR-3 agent 接口的实现.
//
// 算法:
//  1. 把 display_ids 转为 UUIDs (查 evidence 表 session_id + display_id)
//  2. 用 UUIDs 批量查 rebuttal_links 表 where status='standing'
//  3. 把命中的 UUIDs 映射回 display_ids 返回
//
// 为简化 (n ≤ 50 per session, 单次查), 用 sessionID + display_id IN 一次查。
func (r *GormRebuttalRepository) ListStandingByDisplayIDs(ctx context.Context, sessionID uuid.UUID, displayIDs []string) ([]string, error) {
	if len(displayIDs) == 0 {
		return nil, nil
	}
	// Step 1: 把 display_ids 转为 UUIDs
	var evidences []model.Evidence
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND evidence_id IN ?", sessionID, displayIDs).
		Find(&evidences).Error
	if err != nil {
		return nil, err
	}
	if len(evidences) == 0 {
		return nil, nil
	}
	uuidByDisplay := make(map[string]uuid.UUID, len(evidences))
	uuidList := make([]uuid.UUID, 0, len(evidences))
	for _, e := range evidences {
		uuidByDisplay[e.EvidenceID] = e.ID
		uuidList = append(uuidList, e.ID)
	}

	// Step 2: 批量查 rebuttal_links standing
	var rows []model.EvidenceRebuttalLink
	err = r.db.WithContext(ctx).
		Where("session_id = ? AND rebutted_evidence_id IN ? AND status = ?",
			sessionID, uuidList, model.RebuttalStatusStanding).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	// Step 3: UUID → display_id
	displayByUUID := make(map[uuid.UUID]string, len(evidences))
	for d, u := range uuidByDisplay {
		displayByUUID[u] = d
	}
	out := make([]string, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if d, ok := displayByUUID[r.RebuttedEvidenceID]; ok {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	return out, nil
}

// UpdateStatus 实现 RebuttalRepository.UpdateStatus.
func (r *GormRebuttalRepository) UpdateStatus(ctx context.Context, linkID uuid.UUID, newStatus string) (model.EvidenceRebuttalLink, error) {
	// 校验 newStatus 是合法值 (避免任意字符串写入)
	switch newStatus {
	case model.RebuttalStatusStanding, model.RebuttalStatusOverturned, model.RebuttalStatusWithdrawn:
		// 合法
	default:
		return model.EvidenceRebuttalLink{}, gorm.ErrInvalidData
	}
	var row model.EvidenceRebuttalLink
	err := r.db.WithContext(ctx).
		Model(&row).
		Where("id = ?", linkID).
		Update("status", newStatus).Error
	if err != nil {
		return model.EvidenceRebuttalLink{}, err
	}
	if err := r.db.WithContext(ctx).
		Where("id = ?", linkID).
		First(&row).Error; err != nil {
		return model.EvidenceRebuttalLink{}, err
	}
	return row, nil
}

// asAgentRebuttalRepository 把本包 RebuttalRepository 适配成 agent.RebuttalRepository
// (PR-3 窄接口). Service 在 SetRebuttalRepository(o.rebutRead) 时调此适配.
func asAgentRebuttalRepository(r RebuttalRepository) agent.RebuttalRepository {
	if r == nil {
		return nil
	}
	return agentRebuttalAdapter{repo: r}
}

type agentRebuttalAdapter struct {
	repo RebuttalRepository
}

func (a agentRebuttalAdapter) ListStandingRebuttedIDs(ctx context.Context, sessionIDStr string, evidenceDisplayIDs []string) ([]string, error) {
	if sessionIDStr == "" {
		return nil, nil
	}
	sid, err := uuid.Parse(strings.TrimSpace(sessionIDStr))
	if err != nil {
		return nil, err
	}
	return a.repo.ListStandingByDisplayIDs(ctx, sid, evidenceDisplayIDs)
}

func (a agentRebuttalAdapter) Insert(ctx context.Context, sessionID, aggressor, rebuttedDisplayID string, strength float64, rationale string) error {
	// 本适配仅用于 ListStandingRebuttedIDs;Insert 路径走 RebuttalSink (rebuttal_emitter.go)。
	// 此方法不应被调用 (agent.RebuttalRepository 接口契约包含但本适配不直接持久化)。
	return nil
}

// AsAgentRebuttalRepository 暴露适配函数 (大写),供 Service.SetRebuttalRepository 调用.
func AsAgentRebuttalRepository(r RebuttalRepository) agent.RebuttalRepository {
	return asAgentRebuttalRepository(r)
}
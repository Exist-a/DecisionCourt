package belief

import (
	"context"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormDiffRepository is the GORM-backed production implementation of
// DiffRepository. It owns no state beyond the *gorm.DB handle.
type GormDiffRepository struct {
	db *gorm.DB
}

// NewGormDiffRepository wires the repo to the existing GORM handle. Caller
// is responsible for ensuring the schema has been migrated (see model.Connect).
func NewGormDiffRepository(db *gorm.DB) *GormDiffRepository {
	return &GormDiffRepository{db: db}
}

func (r *GormDiffRepository) Insert(ctx context.Context, diff model.BeliefDiff) (model.BeliefDiff, error) {
	if diff.ID == uuid.Nil {
		diff.ID = uuid.New()
	}
	// CreatedAt is filled by GORM with default:now() at insert time, so we
	// leave it zero — avoids clock-skew between app and DB.
	if err := r.db.WithContext(ctx).Create(&diff).Error; err != nil {
		return model.BeliefDiff{}, err
	}
	return diff, nil
}

func (r *GormDiffRepository) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]model.BeliefDiff, error) {
	var rows []model.BeliefDiff
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *GormDiffRepository) ListBySessionAndAgent(ctx context.Context, sessionID uuid.UUID, agentType model.AgentType) ([]model.BeliefDiff, error) {
	var rows []model.BeliefDiff
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND agent_type = ?", sessionID, agentType).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *GormDiffRepository) ListBySessionAndRound(ctx context.Context, sessionID uuid.UUID, round int) ([]model.BeliefDiff, error) {
	var rows []model.BeliefDiff
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND round = ?", sessionID, round).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

// GormWeakenRepository is the GORM-backed production implementation of
// WeakenRepository.
type GormWeakenRepository struct {
	db *gorm.DB
}

// NewGormWeakenRepository wires the repo to the existing GORM handle. Caller
// is responsible for ensuring the schema has been migrated (see model.Connect).
func NewGormWeakenRepository(db *gorm.DB) *GormWeakenRepository {
	return &GormWeakenRepository{db: db}
}

func (r *GormWeakenRepository) Insert(ctx context.Context, link model.EvidenceWeakenLink) (model.EvidenceWeakenLink, error) {
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	if err := r.db.WithContext(ctx).Create(&link).Error; err != nil {
		return model.EvidenceWeakenLink{}, err
	}
	return link, nil
}

func (r *GormWeakenRepository) ListByEvidence(ctx context.Context, sessionID, evidenceID uuid.UUID) ([]model.EvidenceWeakenLink, error) {
	var rows []model.EvidenceWeakenLink
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND evidence_id = ?", sessionID, evidenceID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

func (r *GormWeakenRepository) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]model.EvidenceWeakenLink, error) {
	var rows []model.EvidenceWeakenLink
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at ASC").
		Find(&rows).Error
	return rows, err
}

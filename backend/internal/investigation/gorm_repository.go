package investigation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NewGormRepository returns the production GORM-backed Repository.
func NewGormRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

type gormRepository struct {
	db *gorm.DB
}

func (r *gormRepository) Create(ctx context.Context, f *Finding) error {
	if f == nil {
		return ErrNilFinding
	}
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.FindingUUID == "" {
		f.FindingUUID = uuid.New().String()
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	row := f.ToModel()
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("investigation: gorm create: %w", err)
	}
	return nil
}

func (r *gormRepository) GetByID(ctx context.Context, id uuid.UUID) (*Finding, error) {
	var row model.InvestigationFinding
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("investigation: gorm get: %w", err)
	}
	f := FromModel(row)
	return &f, nil
}

func (r *gormRepository) GetByUUID(ctx context.Context, findingUUID string) (*Finding, error) {
	var row model.InvestigationFinding
	if err := r.db.WithContext(ctx).Where("finding_uuid = ?", findingUUID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("investigation: gorm get: %w", err)
	}
	f := FromModel(row)
	return &f, nil
}

func (r *gormRepository) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]Finding, error) {
	var rows []model.InvestigationFinding
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_at asc").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("investigation: gorm list: %w", err)
	}
	out := make([]Finding, 0, len(rows))
	for _, row := range rows {
		out = append(out, FromModel(row))
	}
	return out, nil
}
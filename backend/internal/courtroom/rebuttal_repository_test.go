package courtroom

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
)

// InMemoryRebuttalRepository 是 RebuttalRepository 的内存实现 (测试用)。
// 用 sync.Map 模拟 DB,够 PR-4 单元测试 + REST 测试用。
// Production 路径用 GormRebuttalRepository (单例 wire 一次)。
type InMemoryRebuttalRepository struct {
	mu   sync.Mutex
	rows map[uuid.UUID]model.EvidenceRebuttalLink
}

func NewInMemoryRebuttalRepository() *InMemoryRebuttalRepository {
	return &InMemoryRebuttalRepository{rows: make(map[uuid.UUID]model.EvidenceRebuttalLink)}
}

func (r *InMemoryRebuttalRepository) Insert(_ context.Context, link model.EvidenceRebuttalLink) (model.EvidenceRebuttalLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if link.ID == uuid.Nil {
		link.ID = uuid.New()
	}
	if link.Status == "" {
		link.Status = model.RebuttalStatusStanding
	}
	if link.CreatedAt.IsZero() {
		link.CreatedAt = time.Now()
	}
	r.rows[link.ID] = link
	return link, nil
}

func (r *InMemoryRebuttalRepository) ListBySession(_ context.Context, sessionID uuid.UUID) ([]model.EvidenceRebuttalLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.EvidenceRebuttalLink, 0)
	for _, row := range r.rows {
		if row.SessionID == sessionID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *InMemoryRebuttalRepository) ListByEvidence(_ context.Context, sessionID, evidenceID uuid.UUID) ([]model.EvidenceRebuttalLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.EvidenceRebuttalLink, 0)
	for _, row := range r.rows {
		if row.SessionID == sessionID && row.RebuttedEvidenceID == evidenceID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (r *InMemoryRebuttalRepository) ListStandingByDisplayIDs(_ context.Context, _ uuid.UUID, displayIDs []string) ([]string, error) {
	// in-memory 实现只返回非空输入的前缀 (测试用, 简化 RebuttedEvidenceID <-> display_id 映射)
	// Production GORM 实现做真正的 join. 这里只验接口契约。
	return displayIDs, nil
}

func (r *InMemoryRebuttalRepository) UpdateStatus(_ context.Context, linkID uuid.UUID, newStatus string) (model.EvidenceRebuttalLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[linkID]
	if !ok {
		return model.EvidenceRebuttalLink{}, errors.New("link not found")
	}
	switch newStatus {
	case model.RebuttalStatusStanding, model.RebuttalStatusOverturned, model.RebuttalStatusWithdrawn:
	default:
		return model.EvidenceRebuttalLink{}, errors.New("invalid status")
	}
	row.Status = newStatus
	r.rows[linkID] = row
	return row, nil
}

// TestInMemoryRebuttalRepository_Insert_DefaultStatus 验证 Insert 设默认 status='standing'.
func TestInMemoryRebuttalRepository_Insert_DefaultStatus(t *testing.T) {
	repo := NewInMemoryRebuttalRepository()
	link, err := repo.Insert(context.Background(), model.EvidenceRebuttalLink{
		SessionID:          uuid.New(),
		RebuttedEvidenceID: uuid.New(),
		AggressorAgent:     "defender",
	})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if link.Status != model.RebuttalStatusStanding {
		t.Errorf("default Status = %q, want %q", link.Status, model.RebuttalStatusStanding)
	}
	if link.ID == uuid.Nil {
		t.Errorf("Insert should assign UUID, got Nil")
	}
}

// TestInMemoryRebuttalRepository_UpdateStatus_Validation 验证 UpdateStatus 拒绝非法 status.
func TestInMemoryRebuttalRepository_UpdateStatus_Validation(t *testing.T) {
	repo := NewInMemoryRebuttalRepository()
	link, _ := repo.Insert(context.Background(), model.EvidenceRebuttalLink{
		SessionID:          uuid.New(),
		RebuttedEvidenceID: uuid.New(),
		AggressorAgent:     "prosecutor",
	})
	// 非法 status
	_, err := repo.UpdateStatus(context.Background(), link.ID, "bogus_status")
	if err == nil {
		t.Error("非法 status 应该被拒绝")
	}
	// 合法 status
	updated, err := repo.UpdateStatus(context.Background(), link.ID, model.RebuttalStatusOverturned)
	if err != nil {
		t.Fatalf("UpdateStatus overturned failed: %v", err)
	}
	if updated.Status != model.RebuttalStatusOverturned {
		t.Errorf("Status = %q, want %q", updated.Status, model.RebuttalStatusOverturned)
	}
}

// TestInMemoryRebuttalRepository_ListBySession 验证 session 过滤.
func TestInMemoryRebuttalRepository_ListBySession(t *testing.T) {
	repo := NewInMemoryRebuttalRepository()
	s1 := uuid.New()
	s2 := uuid.New()
	repo.Insert(context.Background(), model.EvidenceRebuttalLink{
		SessionID: s1, RebuttedEvidenceID: uuid.New(), AggressorAgent: "prosecutor",
	})
	repo.Insert(context.Background(), model.EvidenceRebuttalLink{
		SessionID: s1, RebuttedEvidenceID: uuid.New(), AggressorAgent: "defender",
	})
	repo.Insert(context.Background(), model.EvidenceRebuttalLink{
		SessionID: s2, RebuttedEvidenceID: uuid.New(), AggressorAgent: "prosecutor",
	})

	s1Links, _ := repo.ListBySession(context.Background(), s1)
	if len(s1Links) != 2 {
		t.Errorf("session 1 应有 2 links, got %d", len(s1Links))
	}
	s2Links, _ := repo.ListBySession(context.Background(), s2)
	if len(s2Links) != 1 {
		t.Errorf("session 2 应有 1 link, got %d", len(s2Links))
	}
}

// TestAsAgentRebuttalRepository_Adapter 验证 narrow adapter 正常转换.
func TestAsAgentRebuttalRepository_Adapter(t *testing.T) {
	repo := NewInMemoryRebuttalRepository()
	adapted := AsAgentRebuttalRepository(repo)
	if adapted == nil {
		t.Fatal("adapter 不应为 nil")
	}
	// 空 sessionID 应返回 nil (适配层 short-circuit)
	got, err := adapted.ListStandingRebuttedIDs(context.Background(), "", []string{"E001"})
	if err != nil {
		t.Errorf("空 sessionID 不应返回 error: %v", err)
	}
	if got != nil {
		t.Errorf("空 sessionID 应返回 nil, got %v", got)
	}
}
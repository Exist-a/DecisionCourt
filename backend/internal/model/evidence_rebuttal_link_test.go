package model

import (
	"testing"

	"github.com/google/uuid"
)

// TestEvidenceRebuttalLink_TableName 验证表名固定为 evidence_rebuttal_links
// (GORM auto-migration + 反向引用需要稳定表名)。
func TestEvidenceRebuttalLink_TableName(t *testing.T) {
	link := EvidenceRebuttalLink{}
	if got := link.TableName(); got != "evidence_rebuttal_links" {
		t.Errorf("TableName = %q, want %q", got, "evidence_rebuttal_links")
	}
}

// TestEvidenceRebuttalLink_StatusConstants 验证 status 字符串常量。
// 后端硬拒逻辑只对 'standing' 状态生效 (PRD §4.3.3 "未翻盘")。
func TestEvidenceRebuttalLink_StatusConstants(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{RebuttalStatusStanding, "standing"},
		{RebuttalStatusOverturned, "overturned"},
		{RebuttalStatusWithdrawn, "withdrawn"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("status constant = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestEvidenceRebuttalLink_DefaultStatus 验证 default tag。
// 新建的 rebuttal link 默认 status='standing' (后端硬拒立即生效)。
func TestEvidenceRebuttalLink_DefaultStatus(t *testing.T) {
	link := EvidenceRebuttalLink{
		SessionID:          uuid.New(),
		RebuttedEvidenceID: uuid.New(),
		AggressorAgent:     "defender",
	}
	if link.Status != "" {
		t.Errorf("未显式设 Status 应为空 (依赖 GORM default), got = %q", link.Status)
	}
	// 后端填默认值 (Service 层 / GORM hook):
	link.Status = RebuttalStatusStanding
	if link.Status != "standing" {
		t.Errorf("explicit set Status='standing' 后应为 standing, got = %q", link.Status)
	}
}

// TestEvidenceRebuttalLink_FieldsRequired 验证必填字段约束 (GORM tag)。
// 后端 INSERT 时如果缺这些字段会失败 (DB 约束)。
func TestEvidenceRebuttalLink_FieldsRequired(t *testing.T) {
	link := EvidenceRebuttalLink{}
	if link.SessionID != uuid.Nil {
		t.Errorf("zero-value SessionID 应为 uuid.Nil")
	}
	if link.RebuttedEvidenceID != uuid.Nil {
		t.Errorf("zero-value RebuttedEvidenceID 应为 uuid.Nil")
	}
	if link.AggressorAgent != "" {
		t.Errorf("zero-value AggressorAgent 应为空字符串")
	}
	if link.Strength != 0 {
		t.Errorf("zero-value Strength 应为 0")
	}
}
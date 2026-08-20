package agent

import (
	"context"
	"strings"

	"github.com/decisioncourt/backend/internal/model"
)

// RebuttalHook (v1.0.2 候选 4) is the seam between the ReActRunner and the
// courtroom RebuttalRepository. Whenever a speak step emits one or more valid
// RebuttalDeclarations the runner invokes the hook with the AgentOutput so
// the orchestrator can persist them as rows in evidence_rebuttal_links
// (default status='standing',后端硬拒引用)。
//
// Symmetric to WeakenHook (v0.6) but targets CONTENT (not transmission):
// WeakenHook → weaken_links (削弱 evidence 传播对某 agent 的影响)
// RebuttalHook → rebuttal_links (标记 evidence 内容被反驳,后端硬拒引用)
//
// May be nil; a nil hook simply means "don't persist rebuttal declarations",
// which is the safe default for callers that don't yet wire v1.0.2.
//
// Failure isolation mirrors WeakenHook: hook error is logged but NEVER aborts
// the trial. (Trial 优先完成, rebuttal 持久化失败可后续 retry。)
type RebuttalHook func(ctx context.Context, out AgentOutput, meta MemoryMeta) error

// RebuttalSink is the contract the hook implementation must satisfy.
// Implementation lives in courtroom.RebuttalRepository (PR-4) wrapping
// GORM model.EvidenceRebuttalLink.
type RebuttalSink interface {
	Insert(ctx context.Context, link model.EvidenceRebuttalLink) (model.EvidenceRebuttalLink, error)
}

// EmitRebuttalFromOutput persists every valid RebuttalDeclaration emitted by
// an Agent. Returns nil if out has no valid declarations. Returns an error
// only if a write fails; the runner logs + continues so a transient DB
// hiccup never aborts a trial.
//
// Symmetric to EmitWeakenFromOutput. Key differences:
//   - Weaken 写 evidence_weaken_links,需要 TargetAgent;Rebuttal 不需要
//     (rebuttal 是公开内容反驳,不区分目标 agent)
//   - Rebuttal 默认 status='standing' (GORM default);后续 UpdateStatus
//     接口可把 standing → overturned (翻盘)
func EmitRebuttalFromOutput(
	ctx context.Context,
	repo RebuttalSink,
	resolver EvidenceResolver,
	meta MemoryMeta,
	out AgentOutput,
) error {
	if repo == nil || resolver == nil {
		return nil
	}
	if !out.HasRebuttal() {
		return nil
	}

	for _, decl := range out.ValidRebuttalDeclarations() {
		evidenceID, ok := resolver.EvidenceIDByDisplayID(ctx, meta.SessionID, decl.RebuttedEvidenceID)
		if !ok {
			// RebuttedEvidenceID doesn't resolve (e.g. LLM 引用了一个不存在的 E00X)。
			// 静默跳过而不是 fail the trial — runner 会打 log 让 operator 审计。
			continue
		}

		link := model.EvidenceRebuttalLink{
			SessionID:          meta.SessionID,
			RebuttedEvidenceID: evidenceID,
			AggressorAgent:     strings.TrimSpace(meta.AgentType),
			Status:             model.RebuttalStatusStanding,
			Strength:           clampStrength(decl.Strength),
			Rationale:          strings.TrimSpace(decl.Rationale),
		}
		// RebuttingEvidenceID + AggressorMsgID 留 nil (本 PR 不强制要求 LLM 引用
		// 反驳方 evidence; 后续 v1.0.x 可加 RebuttingEvidenceID 字段让 LLM
		// 引用自己的反驳 evidence, 当前 schema 已支持)。
		if _, err := repo.Insert(ctx, link); err != nil {
			return err
		}
	}
	return nil
}
package courtroom

// v0.9 (ADR 0012 §决策 5) 启动扫描恢复。
//
// 解决:阿里云 ECS → OOM / 运维重启 / 镜像升级 → 进程挂掉 → 当前 active
// 的 trial 工作流中断 → 用户刷新页面看到"trial 卡在 opening 阶段,所有
// agent 都不说话"。凌晨发生没运维在场 = P1 事故。
//
// 设计:
//   - 启动时扫 status=active 且 phase ∈ {opening, evidence, cross_exam,
//     closing, deliberation} 的 session
//   - 对每个 session,根据 phase 调对应 resume 函数(已存在,本 PR 复用)
//   - 限并发 ≤maxConcurrent(默认 5)—— 防止 startup hang
//   - 写 metric + audit log:recovery_attempted_total / succeeded / failed
//   - **不重跑 investigation / appeal**(异步任务,挂掉无所谓)
//
// 简历叙述:
//   "设计启动恢复扫描:进程重启后自动恢复 active trial 的工作流(限并发
//    ≤5),实战场景阿里云 OOM 后用户无感知继续推进。"

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/model"
)

// RecoverActiveSessions 启动时调用,扫描 active session 并恢复工作流。
// maxConcurrent ≤ 0 → 默认 5。
//
// 调用方:cmd/server/main.go 在 HTTP server 启动前调用(异步,不等完成)。
func (s *Service) RecoverActiveSessions(ctx context.Context, maxConcurrent int) error {
	if maxConcurrent <= 0 {
		maxConcurrent = 5
	}

	// 防御性检查:测试或初始化不完整时不要 panic,直接 log 跳过。
	if s.db == nil {
		slog.Warn("recovery: db is nil, skipping (test or partial init?)")
		return nil
	}

	start := time.Now()

	// 1. 扫描 status=active 且 phase ∈ {可恢复 phase} 的 session
	var sessions []model.CourtSession
	if err := s.db.WithContext(ctx).
		Where("status = ?", model.StatusActive).
		Where("current_phase IN ?", []model.CourtPhase{
			model.PhaseOpening,
			model.PhaseEvidence,
			model.PhaseCrossExam,
			model.PhaseClosing,
			model.PhaseDeliberation,
		}).
		Find(&sessions).Error; err != nil {
		slog.Error("recovery: scan failed", "error", err)
		return err
	}

	slog.Info("recovery: scan complete",
		"active_sessions", len(sessions),
		"max_concurrent", maxConcurrent)

	if len(sessions) == 0 {
		s.recoveryAttemptedTotal.Add(1)
		s.recoverySucceededTotal.Add(1)
		return nil
	}

	// 2. 限并发恢复
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	var succeeded, failed int64
	var mu sync.Mutex

	for _, session := range sessions {
		wg.Add(1)
		go func(sess model.CourtSession) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			s.recoveryAttemptedTotal.Add(1)

			if err := s.recoverOneSession(ctx, sess); err != nil {
				slog.Warn("recovery: session failed",
					"session_uuid", sess.SessionUUID,
					"phase", sess.CurrentPhase,
					"round", sess.CurrentRound,
					"error", err)
				s.recoveryFailedTotal.Add(1)
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				slog.Info("recovery: session succeeded",
					"session_uuid", sess.SessionUUID,
					"phase", sess.CurrentPhase)
				s.recoverySucceededTotal.Add(1)
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}(session)
	}
	wg.Wait()

	elapsed := time.Since(start)
	slog.Info("recovery: all done",
		"attempted", len(sessions),
		"succeeded", succeeded,
		"failed", failed,
		"elapsed_ms", elapsed.Milliseconds())

	return nil
}

// recoverOneSession 根据 phase 调对应 resume 函数。
// 复用 service.go 已有的 resumeOpening/resumeCrossExam/resumeClosing
// 逻辑,不重复实现。
func (s *Service) recoverOneSession(ctx context.Context, session model.CourtSession) error {
	switch session.CurrentPhase {
	case model.PhaseOpening:
		return s.resumeOpening(session)
	case model.PhaseEvidence, model.PhaseCrossExam:
		return s.resumeCrossExam(session)
	case model.PhaseClosing:
		return s.resumeClosing(session)
	case model.PhaseDeliberation:
		// 评议阶段通常是 judge 单方 LLM,可在下次 action 时自然驱动。
		// 这里不强恢复(避免 judge 重复发言)。
		slog.Info("recovery: skip deliberation (will resume on next action)",
			"session_uuid", session.SessionUUID)
		return nil
	}
	return nil
}

// RecoveryStats 返回恢复统计(给 observability 用)。
func (s *Service) RecoveryStats() (attempted, succeeded, failed uint64) {
	return s.recoveryAttemptedTotal.Load(),
		s.recoverySucceededTotal.Load(),
		s.recoveryFailedTotal.Load()
}
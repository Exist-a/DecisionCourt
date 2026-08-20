package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/decisioncourt/backend/internal/llm"
)

// RebuttalRepository (v1.0.2 候选 4) 是抽象接口,让 applySpeakerRebuttalCheck
// 不直接依赖 DB。Service 层在 PR-4 注入 GORM 实现 (courtroom.RebuttalRepository)。
//
// 接口只暴露"查 standing 状态的被反驳 evidence IDs"和"插入 rebuttal link"两
// 个最小操作,刻意保持窄契约,便于测试用 fake 实现。
//
// 设计取舍:
//   - ListStandingRebuttedIDs 按 session + evidence display_ids 批量查,O(n) 内存
//   - Insert 用于 RebuttalHook 持久化 (PR-4 接入, 本 PR 提供 fake)
//   - 不暴露 UpdateStatus (overturn) — 留给后续 v1.0.x 单独 PR (涉及 LLM-as-judge
//     "翻盘"判定,超出本 ADR 范围)
type RebuttalRepository interface {
	// ListStandingRebuttedIDs returns display_ids (e.g. ["E001", "E003"]) of evidence
	// pieces in the given session that are CURRENTLY in 'standing' status
	// (rebutted but not yet overturned). nil-safe: returns ([], nil) when no links.
	ListStandingRebuttedIDs(ctx context.Context, sessionID string, evidenceDisplayIDs []string) ([]string, error)

	// Insert persists a single rebuttal link. Implementation should set
	// Status='standing' default if caller doesn't specify.
	Insert(ctx context.Context, sessionID, aggressorAgent, rebuttedDisplayID string, strength float64, rationale string) error
}

// applySpeakerRebuttalCheck v1.0.2 候选 4: 已反驳证据集合检查 (纯算法, 不 reject)
//
// 输入:
//   - s: 当前 Speaker (含 EvidenceRefs)
//   - repo: RebuttalRepository (PR-4 注入 GORM;test 用 fake)
//
// 输出:
//   - violatedDisplayIDs ([]string): standing 状态且 s.EvidenceRefs 含的 evidence IDs
//
// 算法:
//   - Speaker.EvidenceRefs 为空 → 返回 (nil, nil) 不触发
//   - 调 repo.ListStandingRebuttedIDs 拿 standing 集合的 display_ids
//   - 求交集 → 返回 violatedDisplayIDs (供 retry hint + fallback RebuttalViolations 用)
//
// 不触及 §2.1 裁决 (中度): "被反驳且未翻盘"是 PRD §4.3.3 明确语义,后续
// applySpeakerRebuttalRetryLoop 仍按用户授权的 2 次 retry + fallback 设计实装。
//
// 调用点: applySpeakerRebuttalRetryLoop (Run() 内 ActionSpeak 分支,与 novelty 同等级 guard)
func applySpeakerRebuttalCheck(ctx context.Context, s Speaker, sessionID string, repo RebuttalRepository) ([]string, error) {
	if len(s.EvidenceRefs) == 0 || repo == nil {
		return nil, nil
	}
	standing, err := repo.ListStandingRebuttedIDs(ctx, sessionID, s.EvidenceRefs)
	if err != nil {
		return nil, fmt.Errorf("rebuttal check: list standing: %w", err)
	}
	if len(standing) == 0 {
		return nil, nil
	}

	// 求交集: standing 集合 ∩ EvidenceRefs
	violated := make([]string, 0, len(standing))
	for _, eid := range s.EvidenceRefs {
		for _, sid := range standing {
			if eid == sid {
				violated = append(violated, eid)
				break
			}
		}
	}
	return violated, nil
}

// rebuttalMaxRetries v1.0.2 候选 4: 后端硬拒 retry 上限 (与 noveltyMaxRetries=2 一致)
const rebuttalMaxRetries = 2

// applySpeakerRebuttalRetryLoop v1.0.2 候选 4: 已反驳证据 hard reject 主循环
//
// 复用 validateSpeak retry 模式 (L407-436): 用 system hint 注入 LLM,
// 强制 LLM 换 evidence 引用或声明 rebut。最多重试 rebuttalMaxRetries (2) 次,
// 失败 fallback 返回最终 Speaker (带 RebuttalRejected=true + RebuttalViolations=违规 IDs)。
//
// 与 applySpeakerNoveltyRetryLoop 关键区别:
//   - novelty: prompt hint 让 LLM "换角度" (任意换)
//   - rebuttal: prompt hint 让 LLM "换 evidence / 声明 rebut" (具体引导)
//
// 输入:
//   - out: 第一次 LLM 生成的 Speaker (含 EvidenceRefs)
//   - r: ReActRunner (拿 llm client + systemBase + SpeakerAgent for meta)
//   - ctx / messages: LLM 调用上下文
//   - sessionID: 当前 session UUID (string format 透传到 repo)
//   - repo: RebuttalRepository (PR-4 注入)
//
// 输出: 调整后的 Speaker + 调整后的 messages (含 retry hints)
//
// 调用点: Run() 内 ActionSpeak 分支的 streaming success path + validateSpeak retry path
func applySpeakerRebuttalRetryLoop(
	out Speaker,
	r *ReActRunner,
	ctx context.Context,
	messages []llm.Message,
	sessionID string,
	repo RebuttalRepository,
) (Speaker, []llm.Message) {
	// repo 为 nil → 跳过检查 (向后兼容 + pre-v1.0.2 调用方)
	if repo == nil {
		return out, messages
	}

	for retryIdx := 0; retryIdx < rebuttalMaxRetries; retryIdx++ {
		violated, err := applySpeakerRebuttalCheck(ctx, out, sessionID, repo)
		if err != nil {
			// repo 错误 (DB hiccup): log 不 fail,fallback 标记 rejected 让用户知道
			out.RebuttalRejected = true
			out.RebuttalViolations = []string{"<repo_error>"}
			return out, messages
		}
		if len(violated) == 0 {
			return out, messages // 通过,返回最终 out
		}

		// 失败: 注入 hint 让 LLM 换 evidence 或声明 rebut
		hint := llm.Message{
			Role: "system",
			Content: fmt.Sprintf(
				"你刚才引用了已被反驳的证据: %v (状态: standing, 对方反驳未翻盘)。\n"+
					"PRD §4.3.3 规则: 禁止引用被反驳且未翻盘的证据。\n"+
					"请选择以下方式之一重新输出 action=\"speak\":\n"+
					"  - 换一条未被反驳的证据 (evidence_refs 中移除 %v)\n"+
					"  - 在 rebut 字段声明反驳某条 evidence ID 翻盘 (RebuttalDeclaration.rebutted_evidence_id=%q)\n"+
					"  - 论证不依赖这条证据 (改用纯逻辑推理)\n"+
					"(本次是第 %d/%d 次重试)",
				violated, violated, violated[0], retryIdx+1, rebuttalMaxRetries,
			),
		}
		retryMsgs := append(append([]llm.Message{}, messages...), hint)
		retryContent, _, retryErr := r.llm.Complete(
			r.injectGatewayTrace(ctx, "react_rebuttal_retry"),
			r.systemBase,
			retryMsgs,
			llm.CompletionOptions{
				Model:       "",
				Temperature: 0.5, // default, 不需要升高(与 novelty 不同)
				MaxTokens:   500,
				JSONMode:    true,
			},
		)
		if retryErr != nil {
			out.RebuttalRejected = true
			out.RebuttalViolations = violated
			return out, retryMsgs
		}
		var retryOut AgentOutput
		if err := json.Unmarshal([]byte(retryContent), &retryOut); err != nil {
			out.RebuttalRejected = true
			out.RebuttalViolations = violated
			return out, retryMsgs
		}
		retryOut.NormalizeAction()
		if retryOut.Action != ActionSpeak || retryOut.Content == "" {
			out.RebuttalRejected = true
			out.RebuttalViolations = violated
			return out, retryMsgs
		}
		// 更新 out + messages,下一轮循环再检查
		out = Speaker{
			Content:      retryOut.Content,
			Reasoning:    retryOut.Reasoning,
			EvidenceRefs: retryOut.EvidenceRefs,
			Confidence:   retryOut.Confidence,
			Stance:       retryOut.Stance,
		}
		messages = retryMsgs
	}

	// 2 次 retry 后仍违规,标记 RebuttalRejected fallback 返回
	finalViolated, _ := applySpeakerRebuttalCheck(ctx, out, sessionID, repo)
	if len(finalViolated) > 0 {
		out.RebuttalRejected = true
		out.RebuttalViolations = finalViolated
	}
	return out, messages
}
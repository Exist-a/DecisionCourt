package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/google/uuid"
)

// buildA2AContextBlock renders the A2A projection-layer prompt sections for one
// speaking turn. It calls BuildContextView **once** and consumes all three parts
// of the projection:
//
//  1. "## 对方的公开发言（投影层产出）" —— 来自 LLMContext.WorkingMemory，即已经
//     为当前 viewer 投影过的行（对手行已被正向白名单剥离 reasoning）。这是 M1
//     接线：投影产物现在真的被提示词消费（此前零消费者，见
//     docs/todo/deferred-items-2026-10-09.md §M1）。
//  2. "## 你之前的策略笔记（v0.5 私有记忆，仅你可见）" —— 来自 PrivateMemory。
//  3. "## 当前信念状态（来自投影层）" —— 来自 LLMContext.Beliefs（M3：该字段现在
//     真的被填充 —— 见下方 selfBeliefA 的写入）。
//
// 隔离不变量（M5 联动）：本消费者**只读投影后的 WorkingMemory 行**，绝不重新读取
// 对手消息的原始 payload。
//
// Failure isolation：任何 BuildContextView 失败都只记日志并返回 ""，不让庭审因
// 投影抖动而中断。
func (o *Orchestrator) buildA2AContextBlock(
	ctx context.Context,
	sessionID uuid.UUID,
	selfType string,
	selfBeliefA float64,
) string {
	if o.a2aBus == nil || sessionID == uuid.Nil || selfType == "" {
		return ""
	}

	view, err := o.a2aBus.BuildContextView(ctx, sessionID, selfType)
	if err != nil {
		log.Printf("[orchestrator] BuildContextView failed for %s in session %s: %v",
			selfType, sessionID, err)
		return ""
	}

	// M3：把信念引擎的产物（agent.BeliefA）写进投影层暴露的 Beliefs，使它不再是
	// 永远空的占位符；下面的信念区段直接从 view.Beliefs 渲染，证明提示词消费的是
	// 投影层的值（而非直接读 model.Agent）。
	if view.Beliefs == nil {
		view.Beliefs = map[string]float64{}
	}
	view.Beliefs[selfType] = selfBeliefA

	var sb strings.Builder

	// 1. 对方的公开发言（投影后；对手行已剥离 reasoning）
	if len(view.WorkingMemory) > 0 {
		var opp strings.Builder
		for _, m := range view.WorkingMemory {
			if m.MessageType != string(a2a.MessageTypeSpeech) {
				continue // 只看公开发言；派单/回报另有专门区段
			}
			if m.FromAgent == selfType || m.FromAgent == a2a.AddressOrchestrator {
				continue // 本方 / orchestration 行不算"对方"
			}
			content, stance, confidence, refs := extractSpeechPayload(m.Payload)
			if content == "" {
				continue
			}
			fmt.Fprintf(&opp, "- [round=%d %s 置信度=%.2f", m.Round, stance, confidence)
			if len(refs) > 0 {
				fmt.Fprintf(&opp, " 引用=%s", strings.Join(refs, ","))
			}
			fmt.Fprintf(&opp, "] %s\n", truncateForMemory(content, memoryNoteMaxRunes))
		}
		if opp.Len() > 0 {
			sb.WriteString("\n## 对方的公开发言（已按投影层剥离其推理链，仅可见结论与引用）\n")
			sb.WriteString("这是对方（及其代表）本轮之前公开发表过的结论。你只能看到结论与证据引用，看不到对方的推理过程。\n")
			sb.WriteString(opp.String())
		}
	}

	// 2. 本方私有策略笔记
	if len(view.PrivateMemory) > 0 {
		var priv strings.Builder
		for _, m := range view.PrivateMemory {
			content, memType := extractMemoryPayload(m.Payload)
			if content == "" {
				continue
			}
			if memType == "" {
				memType = "memory"
			}
			fmt.Fprintf(&priv, "- [%s round=%d] %s\n", memType, m.Round, truncateForMemory(content, memoryNoteMaxRunes))
		}
		if priv.Len() > 0 {
			sb.WriteString("\n## 你之前的策略笔记（v0.5 私有记忆，仅你可见）\n")
			sb.WriteString("按时间顺序回顾你自己的反思；下一轮发言时避免重复论述，或基于这些笔记深挖。\n")
			sb.WriteString(priv.String())
		}
	}

	// 3. 当前信念状态（来自投影层 LLMContext.Beliefs）
	if v, ok := view.Beliefs[selfType]; ok {
		fmt.Fprintf(&sb, "\n## 当前信念状态\n你（%s）对选项 A 的信念度：%.2f\n", selfType, v)
	}

	return sb.String()
}

// extractSpeechPayload 从（可能已被投影的）公开发言 payload 中取出渲染需要的字段。
// 任何解析失败都返回零值，让调用方安全跳过该行。
func extractSpeechPayload(payloadJSON string) (content, stance string, confidence float64, refs []string) {
	if payloadJSON == "" {
		return "", "", 0, nil
	}
	var p map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return "", "", 0, nil
	}
	if c, ok := p["content"].(string); ok {
		content = c
	}
	if s, ok := p["stance"].(string); ok {
		stance = s
	}
	if f, ok := p["confidence"].(float64); ok {
		confidence = f
	}
	if raw, ok := p["evidence_refs"].([]interface{}); ok {
		for _, r := range raw {
			if s, ok := r.(string); ok {
				refs = append(refs, s)
			}
		}
	}
	return content, stance, confidence, refs
}

// extractMemoryPayload decodes an A2A private-message payload JSON and
// returns the (content, memory_type) tuple the runner prompt cares about.
// Returns empty strings on any decode failure so the caller can skip the
// row safely.
func extractMemoryPayload(payloadJSON string) (content, memoryType string) {
	if payloadJSON == "" {
		return "", ""
	}
	var p map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return "", ""
	}
	if c, ok := p["content"].(string); ok {
		content = c
	}
	if mt, ok := p["memory_type"].(string); ok {
		memoryType = mt
	}
	return content, memoryType
}

// truncateForMemory is a defensive truncator that respects UTF-8 boundaries.
// We keep it tiny — the runtime hot path only needs to bound the per-row size
// so the section total stays under the token budget.
//
// 2026-10-10 Round 2：改为 rune 语义（原实现裸 s[:max] 按字节切，中文会切出
// 半个汉字，产出非法 UTF-8 拼进 prompt）。
func truncateForMemory(s string, maxRunes int) string {
	if maxRunes < 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}

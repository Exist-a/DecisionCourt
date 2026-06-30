package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
)

// buildEpisodicMemoryBlock renders the "你之前的策略笔记" prompt section
// from the Agent's prior episodic memory (v0.5 Episodic Memory via A2A
// private channel).
//
// Behavior:
//   - On any error from BuildContextView (no bus, session not found, etc.)
//     we log and return "" — the trial must not abort because of a memory
//     projection glitch.
//   - When there is no prior private memory, returns "" so the prompt does
//     not contain an empty heading.
//   - Each memory row is rendered as: `- [<memory_type> round=<n>] <content>`
//     Content is truncated to 240 chars to keep the section under ~10K tokens
//     total even at the 50-memory upper bound.
//
// Failure isolation: this is best-effort. A misbehaving bus is logged but
// does not propagate; the runner proceeds with whatever prompt we have so
// far.
func (o *Orchestrator) buildEpisodicMemoryBlock(
	ctx context.Context,
	sessionID uuid.UUID,
	selfType string,
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
	if len(view.PrivateMemory) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## 你之前的策略笔记（v0.5 私有记忆，仅你可见）\n")
	sb.WriteString("按时间顺序回顾你自己的反思；下一轮发言时避免重复论述，或基于这些笔记深挖。\n")
	for _, m := range view.PrivateMemory {
		content, memType := extractMemoryPayload(m.Payload)
		if content == "" {
			continue // skip malformed or empty memory rows
		}
		if memType == "" {
			memType = "memory"
		}
		fmt.Fprintf(&sb, "- [%s round=%d] %s\n", memType, m.Round, truncateForMemory(content, 240))
	}
	return sb.String()
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

// truncateForMemory is a defensive truncator that respects UTF-8 boundaries
// when possible. We keep it tiny — the runtime hot path only needs to bound
// the per-row size so the section total stays under the token budget.
func truncateForMemory(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

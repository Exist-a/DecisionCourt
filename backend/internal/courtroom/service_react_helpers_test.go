package courtroom

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/decisioncourt/backend/internal/agent"
	"github.com/decisioncourt/backend/internal/llm"
)

// reactScriptedLLM is a deterministic LLM that walks through a configured
// script of ReAct steps. Used by both unit and integration tests so the
// Service-level speak path can be asserted without hitting the real API.
type reactScriptedLLM struct {
	mu      sync.Mutex
	scripts []string
	calls   int
}

func (r *reactScriptedLLM) Complete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) (string, llm.Usage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := r.calls
	r.calls++
	if idx >= len(r.scripts) {
		idx = len(r.scripts) - 1
	}
	return r.scripts[idx], llm.Usage{}, nil
}

// StreamComplete：测试默认假 LLM 不实现流式 —— 它立刻关闭 channel。
// 真正的 streaming 测试用 streamingScriptedLLM（见 react_runner_streaming_test.go）。
func (r *reactScriptedLLM) StreamComplete(_ context.Context, _ string, _ []llm.Message, _ llm.CompletionOptions) <-chan llm.StreamChunk {
	out := make(chan llm.StreamChunk, 1)
	out <- llm.StreamChunk{Done: true, Err: fmt.Errorf("reactScriptedLLM: streaming not supported in this fixture")}
	close(out)
	return out
}

func speakJSON(content, stance string, confidence float64) string {
	b, _ := json.Marshal(agent.AgentOutput{
		Action:       agent.ActionSpeak,
		Reasoning:    "final thought",
		Content:      content,
		EvidenceRefs: []string{},
		Confidence:   confidence,
		Stance:       stance,
	})
	return string(b)
}

func toolJSON(tool string, input map[string]interface{}) string {
	b, _ := json.Marshal(agent.AgentOutput{
		Action:    agent.ActionToolCall,
		Reasoning: "thinking",
		Tool:      tool,
		ToolInput: input,
	})
	return string(b)
}
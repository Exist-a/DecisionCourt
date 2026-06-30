package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
)

// Tool is the contract every ReAct-callable tool must satisfy. Tools run
// inside the agent's process and must be safe to invoke from concurrent
// courtroom sessions (use per-session locking internally if stateful).
type Tool interface {
	// Name returns the tool identifier that the LLM emits in AgentOutput.Tool.
	// Names must be unique within a Runner.
	Name() string
	// Description is rendered into the system prompt so the LLM knows when
	// to invoke the tool. Keep it short and concrete.
	Description() string
	// Execute runs the tool. It receives the raw ToolInput map the LLM
	// produced and must return either a string observation (which becomes
	// the next user message in the ReAct loop) or an error (surfaced as
	// `[tool_error] <msg>` in the observation).
	Execute(ctx context.Context, input map[string]interface{}) (string, error)
}

// Step is a snapshot of one ReAct iteration, surfaced via StepHook so the
// courtroom service can stream progress events to the websocket before the
// final Speaker is produced.
type Step struct {
	Index       int                    `json:"index"`
	Thought     string                 `json:"thought"`
	Action      string                 `json:"action"`
	ToolName    string                 `json:"tool_name,omitempty"`
	ToolInput   map[string]interface{} `json:"tool_input,omitempty"`
	Observation string                 `json:"observation,omitempty"`
	Error       string                 `json:"error,omitempty"`
	ElapsedMs   int64                  `json:"elapsed_ms"`
}

// StepHook receives every Step the runner produces. The courtroom service
// uses it to broadcast agent.cot_step events; tests use it to assert the
// loop walked the expected path. May be nil.
type StepHook func(step Step)

// SpeakChunkCallback receives incremental content chunks when the runner
// streams the final speech content via LLM.StreamComplete. chunk is the
// new fragment from this tick; accumulated is the full content seen so
// far (already includes chunk). May be nil — in which case streaming is
// skipped and the JSON-mode decision's content field is used as-is.
type SpeakChunkCallback func(chunk, accumulated string)

// AgentGatewayTrace carries session / agent / task metadata for the
// Agent Gateway recorder (v0.5+). Zero value is treated as disabled and
// the runner falls back to inheriting trace from ctx.
type AgentGatewayTrace struct {
	SessionUUID string
	AgentType   string
	TaskType    string
}

// injectGatewayTrace 在每次 LLM 调用前对 ctx 注入 trace。taskType 由
// 调用方按当前 ReAct 阶段传入（think / reflect / speak / speak_stream）。
func (r *ReActRunner) injectGatewayTrace(ctx context.Context, taskType string) context.Context {
	if r.cfg.AgentGatewayTrace.SessionUUID == "" &&
		r.cfg.AgentGatewayTrace.AgentType == "" &&
		r.cfg.AgentGatewayTrace.TaskType == "" {
		return ctx
	}
	existing := agent_gateway.FromContext(ctx)
	sid := r.cfg.AgentGatewayTrace.SessionUUID
	if sid == "" {
		sid = existing.SessionUUID
	}
	return agent_gateway.WithTrace(ctx, agent_gateway.Trace{
		SessionUUID: sid,
		AgentType:   r.cfg.AgentGatewayTrace.AgentType,
		TaskType:    taskType,
	})
}

// RunnerConfig tunes the ReAct loop. Zero values fall back to the
// recommended defaults (max 4 iterations, max 3 reflects, 30s timeout).
type RunnerConfig struct {
	MaxIterations int
	MaxReflects   int
	Timeout       time.Duration
	// AgentGatewayTrace (v0.5+), if non-zero, makes the runner inject
	// session / agent / task into ctx before every LLM call. This wires
	// ReActRunner to the Agent Gateway recorder so its 20+ steps per
	// speaker show up in llm_calls with correct trace fields.
	AgentGatewayTrace AgentGatewayTrace
	// AllowedTools, if non-empty, restricts the runner to invoking only
	// tools whose Name() appears in the list. This is defense-in-depth: the
	// runner itself rejects unknown tools anyway.
	AllowedTools []string
	// OnIterStart, if non-nil, fires once per iteration BEFORE the LLM is
	// called. The courtroom service uses this to broadcast
	// agent.thinking_started so the frontend can render a thinking bubble
	// immediately instead of waiting for the first cot_step.
	OnIterStart func(iter int)
	// OnSpeakChunk, if non-nil, fires once per content chunk when the
	// runner streams the final speech content. Only fires for the speak
	// action — tool_call / reflect are JSON-decision steps that don't
	// produce user-facing content.
	OnSpeakChunk SpeakChunkCallback
	// MemoryHook (v0.5), if non-nil, fires whenever a reflect (or speak)
	// step's AgentOutput carries a complete memory entry (HasMemory()).
	// The orchestrator wires this to EmitMemoryFromOutput to persist the
	// entry as a private A2A message. Nil is safe and disables memory
	// persistence — useful for tests and for callers that don't yet
	// integrate with A2A.
	MemoryHook MemoryHook
	// MemoryMeta (v0.5) supplies the session/agent identity that the
	// runner cannot know on its own. Required when MemoryHook is set;
	// otherwise it is ignored.
	MemoryMeta MemoryMeta
}

// ReActRunner runs a Thought→Action→Observation loop on top of an LLM
// client. The caller supplies the system prompt and a registry of Tools;
// the runner handles JSON parsing, retry-on-parse-failure, tool dispatch,
// observation feedback, and per-step event emission.
type ReActRunner struct {
	llm        llm.Client
	systemBase string
	tools      map[string]Tool
	toolOrder  []string // stable ordering for prompt description
	cfg        RunnerConfig
	stepHook   StepHook
}

// NewReActRunner builds a runner. systemBase is the role-specific prompt
// (e.g. ProsecutorPrompt); tools is the registry. cfg.MaxIterations and
// cfg.Timeout fall back to 4 and 30s respectively when zero.
func NewReActRunner(client llm.Client, systemBase string, tools map[string]Tool, cfg RunnerConfig) *ReActRunner {
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 4
	}
	if cfg.MaxReflects <= 0 {
		cfg.MaxReflects = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	toolOrder := make([]string, 0, len(tools))
	for name := range tools {
		toolOrder = append(toolOrder, name)
	}
	return &ReActRunner{
		llm:        client,
		systemBase: systemBase,
		tools:      tools,
		toolOrder:  toolOrder,
		cfg:        cfg,
	}
}

// SetStepHook registers a callback invoked once per ReAct iteration.
// Pass nil to clear. Safe to call before Run only.
func (r *ReActRunner) SetStepHook(hook StepHook) {
	r.stepHook = hook
}

// ToolsDescription returns a stable, LLM-friendly listing of registered
// tools, suitable for inclusion in the system prompt.
func (r *ReActRunner) ToolsDescription() string {
	if len(r.toolOrder) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n## 可用工具\n")
	sb.WriteString("当且仅当你需要更多客观信息来支撑论点时，输出 action=tool_call 并填好 tool / tool_input。\n")
	for _, name := range r.toolOrder {
		tool := r.tools[name]
		fmt.Fprintf(&sb, "- %s: %s\n", tool.Name(), tool.Description())
	}
	sb.WriteString("\n## 输出 action 说明\n")
	sb.WriteString("- action=\"speak\": 你已经准备好发言，按原有 JSON 格式输出 content / evidence_refs / confidence / stance。\n")
	sb.WriteString("- action=\"tool_call\": 你需要先调用工具，输出 tool / tool_input 并保持 content 为空。\n")
	return sb.String()
}

// Run executes the ReAct loop until the LLM emits a speak action, the loop
// hits MaxIterations, or ctx is cancelled. transcript is the courtroom
// history injected as part of the system context so the LLM can reason
// about what was already said.
func (r *ReActRunner) Run(ctx context.Context, transcript []model.Message) (Speaker, []Step, error) {
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	messages := r.buildInitialMessages(transcript)
	steps := make([]Step, 0, r.cfg.MaxIterations)
	reflectCount := 0

	for iter := 0; iter < r.cfg.MaxIterations; iter++ {
		if err := ctx.Err(); err != nil {
			return Speaker{}, steps, err
		}

		// OnIterStart fires BEFORE the LLM call so callers can broadcast
		// "thinking started" the moment the lawyer begins reasoning. Safe
		// to leave nil — Run does the existence check itself.
		if r.cfg.OnIterStart != nil {
			r.cfg.OnIterStart(iter)
		}

		stepStart := time.Now()
		content, _, err := r.llm.Complete(r.injectGatewayTrace(ctx, "react_think"), r.systemBase, messages, llm.CompletionOptions{
			Model:       "",
			Temperature: 0.7,
			MaxTokens:   500,
			JSONMode:    true,
		})
		if err != nil {
			return Speaker{}, steps, fmt.Errorf("react iter %d: llm call: %w", iter, err)
		}

		var out AgentOutput
		if err := json.Unmarshal([]byte(content), &out); err != nil {
			// Retry once with a system-level correction hint injected into
			// the message stream.
			hint := llm.Message{
				Role: "system",
				Content: fmt.Sprintf(
					"你上一轮输出不是合法 JSON：%s。请严格按 JSON 格式重新输出。",
					err.Error(),
				),
			}
			retryMsgs := append(append([]llm.Message{}, messages...), hint)
			retryContent, _, retryErr := r.llm.Complete(r.injectGatewayTrace(ctx, "react_think_retry"), r.systemBase, retryMsgs, llm.CompletionOptions{
				Model:       "",
				Temperature: 0.5,
				MaxTokens:   500,
				JSONMode:    true,
			})
			if retryErr != nil {
				return Speaker{}, steps, fmt.Errorf("react iter %d: retry llm: %w", iter, retryErr)
			}
			if err := json.Unmarshal([]byte(retryContent), &out); err != nil {
				return Speaker{}, steps, fmt.Errorf("react iter %d: parse output: %w (raw: %s)", iter, err, retryContent)
			}
			// Surface the recovery via the message stream so the model can
			// see what it produced in the retry attempt.
			messages = append(messages, hint, llm.Message{Role: "assistant", Content: retryContent})
		}
		out.NormalizeAction()

		step := Step{
			Index:     iter,
			Thought:   out.Reasoning,
			Action:    string(out.Action),
			ElapsedMs: time.Since(stepStart).Milliseconds(),
		}

		switch out.Action {
		case ActionToolCall:
			step.ToolName = out.Tool
			step.ToolInput = out.ToolInput

			if r.cfg.AllowedTools != nil {
				allowed := false
				for _, name := range r.cfg.AllowedTools {
					if name == out.Tool {
						allowed = true
						break
					}
				}
				if !allowed {
					return Speaker{}, steps, fmt.Errorf("react iter %d: tool %q not in allowed list", iter, out.Tool)
				}
			}

			tool, ok := r.tools[out.Tool]
			if !ok {
				return Speaker{}, steps, fmt.Errorf("react iter %d: tool %q not registered", iter, out.Tool)
			}

			obs, toolErr := tool.Execute(ctx, out.ToolInput)
			if toolErr != nil {
				step.Observation = fmt.Sprintf("[tool_error] %s", toolErr.Error())
				step.Error = toolErr.Error()
			} else {
				step.Observation = obs
			}
			step.ElapsedMs = time.Since(stepStart).Milliseconds()
			steps = append(steps, step)
			r.emitStep(step)

			// Push the assistant turn + observation back into the message
			// stream so the next iteration sees them.
			messages = append(messages,
				llm.Message{Role: "assistant", Content: content},
				llm.Message{Role: "user", Content: "Observation: " + step.Observation},
			)
			continue

		case ActionReflect:
			if reflectCount >= r.cfg.MaxReflects {
				step.Observation = fmt.Sprintf("[reflect_cap_reached] 已达反思上限 (%d)，下一轮必须 action=\"speak\" 或 action=\"tool_call\"。", r.cfg.MaxReflects)
				step.Error = "reflect_cap_reached"
			} else {
				reflectCount++
				step.Observation = fmt.Sprintf("[reflect] %d/%d", reflectCount, r.cfg.MaxReflects)
			}
			step.ElapsedMs = time.Since(stepStart).Milliseconds()
			steps = append(steps, step)
			r.emitStep(step)

			// v0.5: if the LLM attached a memory entry to this reflect
			// step, fire the MemoryHook so the orchestrator can persist
			// it as a private A2A message. Failures are logged but do
			// not abort the trial — the user's speech must still be
			// produced.
			if r.cfg.MemoryHook != nil && out.HasMemory() {
				if err := r.cfg.MemoryHook(ctx, out, r.cfg.MemoryMeta); err != nil {
					// memory persistence failure must not break the loop
					_ = err // intentionally swallowed; orchestrator logs
				}
			}

			// Push the assistant turn + reflection prompt back into the
			// message stream so the next iteration continues reasoning.
			messages = append(messages,
				llm.Message{Role: "assistant", Content: content},
				llm.Message{Role: "user", Content: "Reflect: " + step.Observation + " —— 请基于上述思考继续推演；当论点成熟时输出 action=\"speak\"，需要更多证据时输出 action=\"tool_call\"。"},
			)
			continue

		case ActionSpeak:
			// 流式生成 content —— 在 validateSpeak 之前填充，避免空 content
			// 触发额外的 Complete retry（流式已经接管 content 责任）。
			//
			// 设计要点：
			//   - 流式成功 → 用 streamed content；validateSpeak 必过；不 retry
			//   - 流式失败 → 保留 out.Content（来自决策 JSON content 字段，
			//     通常为空）；validateSpeak 可能失败 → 走 retry 路径
			//     （用 Complete 重新生成完整 JSON）—— 这是兜底，确保 speak
			//     永远能给用户一份发言
			streamSucceeded := false
			if r.cfg.OnSpeakChunk != nil {
				if streamed, ok := r.streamSpeakContent(ctx, out, messages); ok {
					out.Content = streamed
					streamSucceeded = true
				}
			}
			// 仅在流式失败时才允许 retry 路径（流式成功时 content 必非空，
			// validateSpeak 必通过，没必要 retry 浪费时间）。
			if streamSucceeded {
				step.ElapsedMs = time.Since(stepStart).Milliseconds()
				steps = append(steps, step)
				r.emitStep(step)

				return Speaker{
					Content:      out.Content,
					Reasoning:    out.Reasoning,
					EvidenceRefs: out.EvidenceRefs,
					Confidence:   out.Confidence,
					Stance:       out.Stance,
				}, steps, nil
			}
			if err := validateSpeak(&out); err != nil {
				// One retry with a correction hint, same pattern as parse
				// failures, to keep the loop deterministic.
				hint := llm.Message{
					Role: "system",
					Content: fmt.Sprintf(
						"你上一轮输出不合法：%s。请修正后重新输出 action=\"speak\"。",
						err.Error(),
					),
				}
				retryMsgs := append(append([]llm.Message{}, messages...), hint)
				retryContent, _, retryErr := r.llm.Complete(r.injectGatewayTrace(ctx, "react_reflect_retry"), r.systemBase, retryMsgs, llm.CompletionOptions{
					Model:       "",
					Temperature: 0.5,
					MaxTokens:   500,
					JSONMode:    true,
				})
				if retryErr == nil {
					var retryOut AgentOutput
					if json.Unmarshal([]byte(retryContent), &retryOut) == nil {
						retryOut.NormalizeAction()
						if retryOut.Action == ActionSpeak && validateSpeak(&retryOut) == nil {
							out = retryOut
						}
					}
				}
				// If still invalid after retry, fall through with the
				// partial output so we still produce a Speaker rather than
				// aborting the user's turn.
			}
			step.ElapsedMs = time.Since(stepStart).Milliseconds()
			steps = append(steps, step)
			r.emitStep(step)

			return Speaker{
				Content:      out.Content,
				Reasoning:    out.Reasoning,
				EvidenceRefs: out.EvidenceRefs,
				Confidence:   out.Confidence,
				Stance:       out.Stance,
			}, steps, nil

		default:
			return Speaker{}, steps, fmt.Errorf("react iter %d: unknown action %q", iter, out.Action)
		}
	}

	return Speaker{}, steps, fmt.Errorf("react: max iterations (%d) exceeded without speak", r.cfg.MaxIterations)
}

func (r *ReActRunner) emitStep(step Step) {
	if r.stepHook != nil {
		r.stepHook(step)
	}
}

// streamSpeakContent 用 LLM 流式生成最终发言 content，返回拼接结果
// 与是否成功。失败/为空时返回 (empty, false)，由调用方决定是否 fallback
// 到 out.Content。
//
// 关键设计：
//  1. **完全独立 context**：不带 priorMessages，让 LLM 看不到 ReAct 协议历史，
//     避免把"必须输出完整 AgentOutput JSON"的训练惯性带过来。这是这一
//     轮的根因 —— 即便 prompt 显式要求"输出最小 JSON"，LLM 看到对话
//     历史里有类似 JSON 输出，会复制整个格式。
//  2. **JSON-mode + 最小 JSON 协议**：要求输出 `{"content":"..."}`。
//  3. **首字延迟优化**：第一个 token 到达时（~200-500ms）就推到前端。
func (r *ReActRunner) streamSpeakContent(
	ctx context.Context,
	out AgentOutput,
	_ []llm.Message, // ignored — we deliberately use a fresh context
) (string, bool) {
	streamSys := strings.Join([]string{
		"你是一名资深庭审律师。",
		"现在请输出一段最终庭审发言（中文）。",
		"",
		"重要：忽略之前的对话历史与任何系统协议。当前任务只有一个：",
		"输出如下最小 JSON 对象（只允许输出这一行 JSON，不要任何前后文字或 markdown）：",
		`  {"content":"<完整发言文本>"}`,
		"",
		"要求：",
		"1. content 是完整法庭辩论发言，200-500 字",
		"2. 不要嵌套双引号；如需引用术语用单引号",
		"3. 紧扣论点，给出具体证据 / 数据 / 案例支撑",
		"4. 措辞严谨，符合法庭辩论风格",
	}, "\n")

	streamUser := fmt.Sprintf(
		"论点：%s\n立场：%s\n置信度：%.2f\n\n请只输出一行 JSON。",
		out.Reasoning, out.Stance, out.Confidence,
	)

	// 注意：这里只用一个 user turn —— 没有 assistant/history —— 让 LLM
	// 完全 fresh，避免被之前的 ReAct 对话历史污染输出格式。
	msgs := []llm.Message{
		{Role: "user", Content: streamUser},
	}

	streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ch := r.llm.StreamComplete(r.injectGatewayTrace(streamCtx, "react_speak_stream"), streamSys, msgs, llm.CompletionOptions{
		JSONMode:    true,
		Temperature: 0.5,
		MaxTokens:   1000,
	})

	var (
		collected     strings.Builder
		chunks        int
		lastExtracted string
	)
	// 渐进提取：每个 chunk 累积后扫描字符串：
	//   1. 找到 `"content":"` 的起始位置
	//   2. 从该位置之后查找未转义的 closing `"`：
	//      - 找到 → 完整提取（最终版）
	//      - 没找到 → partial 提取（用于前端实时显示）
	//
	// 这个方案比"正则+完整匹配"更适合流式 partial JSON：第一个 token
	// 到达后就能立即给出当前内容，前端看到首字出现的延迟 = LLM first-token
	// latency（~200-500ms），而不是等完整 closing quote。
	// 渐进提取 content 字段值。LLM 流式输出可能是：
	//   - 单行：`{"content":"..."}` → prefix `"content":"` 命中
	//   - 多行：`{\n  "content": "..."\n}` → 需要容忍 `: ` 之间的空格/换行
	//
	// 我们用更宽松的扫描：先找到 `"content"` 关键词，再向后跳过任意
	// whitespace + 一个冒号 + 任意 whitespace，然后期待 `"` 起始。
	streamDone := false
	for !streamDone {
		select {
		case <-streamCtx.Done():
			// ctx 取消（外部 cancel / 30s 超时）—— 立刻返回 false
			// 让调用方走 retry 兜底，不能让 for-range 卡住整个 trial。
			return "", false
		case c, ok := <-ch:
			if !ok {
				// channel 正常关闭 → 跳出 select
				streamDone = true
				break
			}
			if c.Err != nil {
				return "", false
			}
			if c.Done {
				streamDone = true
				break
			}
			collected.WriteString(c.Content)
			chunks++
			_ = chunks

			// 渐进提取 content 字段值（每个 chunk 后扫一次）
			raw := collected.String()
			fieldIdx := indexOfJSONField(raw, "content")
			if fieldIdx < 0 {
				continue // content 字段还没出现
			}
			// 跳过字段名后到 ":" 之间的任意 whitespace
			i := fieldIdx + len(`"content"`)
			for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
				i++
			}
			if i >= len(raw) || raw[i] != ':' {
				continue
			}
			i++
			for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
				i++
			}
			if i >= len(raw) || raw[i] != '"' {
				continue
			}
			i++ // skip opening "
			// 扫描未转义的 closing "
			end := -1
			for j := i; j < len(raw); j++ {
				if raw[j] == '\\' && j+1 < len(raw) {
					j++ // 跳过下一个字符（转义）
					continue
				}
				if raw[j] == '"' {
					end = j
					break
				}
			}
			var rawValue string
			if end < 0 {
				rawValue = raw[i:] // partial
			} else {
				rawValue = raw[i:end]
			}
			extracted := unquoteJSONString(rawValue)
			if extracted != lastExtracted {
				lastExtracted = extracted
				if r.cfg.OnSpeakChunk != nil {
					r.cfg.OnSpeakChunk(c.Content, extracted)
				}
			}
		}
	}

	if lastExtracted == "" {
		return "", false
	}
	return lastExtracted, true
}

// indexOfJSONField 在 raw 字符串中找到 JSON 字段名的位置，例如
// `"content"`。它容忍字段名前后的任意字符（包括空白 / 其他字段 / 数组
// 元素），但要求该字段名是完整的 `"<field>"` 形式。返回 field 起始处
// 的 index，未找到返回 -1。
func indexOfJSONField(raw, field string) int {
	target := `"` + field + `"`
	idx := strings.Index(raw, target)
	if idx < 0 {
		return -1
	}
	return idx
}

// unquoteJSONString 解码 JSON 转义（\"、\\、\n、\t 等），让前端拿到
// 真正的中文字符串而不是带反斜杠的转义形式。
func unquoteJSONString(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '"':
				sb.WriteByte('"')
				i++
			case '\\':
				sb.WriteByte('\\')
				i++
			case 'n':
				sb.WriteByte('\n')
				i++
			case 't':
				sb.WriteByte('\t')
				i++
			case 'r':
				sb.WriteByte('\r')
				i++
			default:
				sb.WriteByte(s[i])
			}
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func (r *ReActRunner) buildInitialMessages(transcript []model.Message) []llm.Message {
	system := r.systemBase + r.ToolsDescription()
	if len(transcript) > 0 {
		var sb strings.Builder
		sb.WriteString(system)
		sb.WriteString("\n\n## 庭审历史（按时间顺序）\n")
		for _, m := range transcript {
			role := m.ActionType
			if role == "" {
				role = "message"
			}
			fmt.Fprintf(&sb, "- [%s] %s\n", role, truncateForPrompt(m.Content, 240))
		}
		system = sb.String()
	}
	return []llm.Message{
		{Role: "system", Content: system},
	}
}

func validateSpeak(o *AgentOutput) error {
	if strings.TrimSpace(o.Reasoning) == "" {
		return fmt.Errorf("empty reasoning")
	}
	if strings.TrimSpace(o.Content) == "" {
		return fmt.Errorf("empty content")
	}
	if o.Confidence < 0 || o.Confidence > 1 {
		return fmt.Errorf("confidence out of range")
	}
	switch o.Stance {
	case "pro_a", "pro_b", "challenge", "neutral":
		// ok
	default:
		return fmt.Errorf("invalid stance: %q", o.Stance)
	}
	return nil
}

func truncateForPrompt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

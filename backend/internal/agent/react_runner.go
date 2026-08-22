package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/util"
)

// ErrReactMaxIterations is returned by ReActRunner.Run when the loop
// reaches cfg.MaxIterations without ever producing a speak action.
//
// v0.10.17 (silent-error-fix): 让上层 (courtroom/service.go) 用
// errors.Is 分类用户可见错误,而不是匹配 err.Error() 字符串。
// 历史原因:之前用 fmt.Errorf("react: max iterations ...") 返回,
// 前端看到的是裸字符串,无法按"可恢复 vs 不可恢复"分类。
var ErrReactMaxIterations = errors.New("react: max iterations exceeded without speak")

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
	// WeakenHook (v0.6), if non-nil, fires whenever a reflect (or speak)
	// step's AgentOutput carries ≥1 valid WeakenDeclaration (HasWeaken()).
	// The orchestrator wires this to EmitWeakenFromOutput to persist the
	// declaration as a row in evidence_weaken_links; subsequent belief
	// updates then attenuate that evidence's impact. Nil is safe and
	// disables weaken persistence — useful for callers that don't yet
	// integrate with belief v0.6.
	WeakenHook WeakenHook
	// RebuttalHook (v1.0.2 候选 4), if non-nil, fires whenever a speak step's
	// AgentOutput carries ≥1 valid RebuttalDeclaration (HasRebuttal()).
	// The orchestrator wires this to EmitRebuttalFromOutput to persist the
	// declaration as a row in evidence_rebuttal_links (default status='standing').
	// Subsequent applySpeakerRebuttalCheck (PR-3) hard-rejects speakers that
	// reference 'standing' rebuttal evidence. Nil is safe and disables
	// rebuttal persistence — useful for pre-v1.0.2 callers.
	RebuttalHook RebuttalHook
	// RebuttalRepository (v1.0.2 候选 4) 是抽象接口,让 applySpeakerRebuttalCheck
	// 拉 standing 状态 rebuttal evidence。Nil = 跳过 rebut hard-reject 检查
	// (向后兼容 + pre-v1.0.2 调用方)。
	RebuttalRepository RebuttalRepository
	// SpeakerHistory (v0.10.23 候选 2), if non-nil, 是当前 speaker 同 agent
	// 的历史 speak messages (跨 phase 跨 round, 但只看自己)。runner 用它做
	// 新意度 Jaccard 检查: 本次新发言 vs 自己历史任一条 jaccard > 0.6 → reject +
	// retry hint 强制换角度。Nil = 跳过检查 (向后兼容 + 测试用)。
	SpeakerHistory []model.Message
	// SpeakerBeliefA (v0.10.24 候选 1), 是当前 speaker 的 belief_A 数值。
	// runner 用它做 stance judge 触发条件 (老 isStanceConsistent 阈值 0.45/0.55)。
	// 0 是合法 belief 值, runner 用 SpeakerAgent 是否为零值判断是否启用。
	SpeakerBeliefA float64
	// SpeakerAgent (v0.10.24 候选 1), 是当前 speaker 的完整 agent 结构。
	// judge prompt 需要 AgentType (model.AgentProsecutor / AgentDefender)；
	// 老 isStanceConsistent 也需 AgentType 区分控辩。
	// RunnerConfig 按值复制 model.Agent (无指针, 安全)。
	SpeakerAgent model.Agent
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

			// v0.6: symmetric fire for weaken declarations. We do NOT short-
			// circuit on MemoryHook failure — weaken persistence is fully
			// independent of memory persistence so a flaky A2A bus should
			// never freeze the belief layer.
			if r.cfg.WeakenHook != nil && out.HasWeaken() {
				if err := r.cfg.WeakenHook(ctx, out, r.cfg.MemoryMeta); err != nil {
					_ = err // weaken persistence failure is best-effort
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
			//
			// v0.10.1 (ADR 0021):流式成功也要跑 hallucination validation。
			// 之前逻辑跳过 validateSpeak 假设"结构校验必过",但 hallucination
			// check(evidence_refs 空但内容含证据/案号/百分比)是新加的,
			// LLM 在 stress 下违反频率 60%。失败时强制走非流式 retry,
			// 让 LLM 看到错误信息重新生成。
			streamSucceeded := false
			// v1.0-patch (2026-08-22): hallucination 硬拒时保留 stream 内容作 fallback,
			// retry 后仍空时恢复, 避免整轮 cross_exam 因空 content 中断。
			var streamedFallback string
			if r.cfg.OnSpeakChunk != nil {
				if streamed, ok := r.streamSpeakContent(ctx, out, messages); ok {
					out.Content = streamed
					streamSucceeded = true
				}
			}
			if streamSucceeded {
				// 流式成功也跑 hallucination check,失败时回退到 retry 路径
				// v1.0-patch (2026-08-22): 修复第一次质证 empty content 中断整轮 bug。
				// 之前: validation 失败时 out.Content = "" 清空, retry 后仍失败 →
				//   fall-through 返回空 content → saveAgentMessage guard 返 error →
				//   整轮 cross_exam 中断 (用户反馈 "操作未能完成")。
				// 现在: 保留 streamed content 在局部变量, retry 路径跑完后如果
				//   out.Content 仍为空, 用 streamed content 恢复 (软降级, 不硬拒)。
				//   反幻觉初衷保留: retry 有机会重新生成干净内容; retry 失败时
				//   保留可能含具体数字的 stream 内容 (好过整轮中断)。
				if valResult := ValidateAgainstHallucination(out.Content, out.EvidenceRefs, nil); !valResult.OK {
					// 把 streamed content 存为 fallback (不清空 out.Content 之外的引用),
					// 走 retry 路径让 LLM 重新生成
					streamedFallback = out.Content
					slog.Warn("streamSpeakContent hallucination validation failed, falling back to retry",
						"mode", valResult.Issues[0].Mode,
						"pattern", valResult.Issues[0].Pattern,
						"content_len", len(streamedFallback),
					)
					streamSucceeded = false
					out.Content = "" // 清空,触发 retry 路径
				} else {
				step.ElapsedMs = time.Since(stepStart).Milliseconds()
				steps = append(steps, step)
				r.emitStep(step)

				// v1.0.2 候选 4: 持久化 rebuttal 声明 (在 novelty/stance judge 之后)
				// streamSucceeded 路径只触发 fire;后续 applySpeakerRebuttalCheck
				// (PR-3) 在 validateSpeak 阶段硬拒引用 standing rebuttal evidence。
				if r.cfg.RebuttalHook != nil && out.HasRebuttal() {
					if err := r.cfg.RebuttalHook(ctx, out, r.cfg.MemoryMeta); err != nil {
						// 同 MemoryHook/WeakenHook 失败隔离: log 不 fail
						_ = err
					}
				}

				// v0.10.23 候选 2: 新意度 retry 通路 (先 novelty 后 length limit)
				speaker := Speaker{
					Content:      out.Content,
					Reasoning:    out.Reasoning,
					EvidenceRefs: out.EvidenceRefs,
					Confidence:   out.Confidence,
					Stance:       out.Stance,
				}
				speaker, _ = applySpeakerStanceJudge(speaker, r, ctx, messages)
				speaker, _ = applySpeakerNoveltyRetryLoop(speaker, r, ctx, messages)
				// v1.0.2 候选 4: 已反驳证据 hard reject (streamSucceeded 路径)
				sessionIDStr := r.cfg.MemoryMeta.SessionUUID
				if sessionIDStr == "" {
					sessionIDStr = r.cfg.MemoryMeta.SessionID.String()
				}
				speaker, _ = applySpeakerRebuttalRetryLoop(speaker, r, ctx, messages, sessionIDStr, r.cfg.RebuttalRepository)
				return applySpeakerLengthLimit(speaker), steps, nil
			}
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
				// v1.0-patch (2026-08-22): retry 后 content 仍空时恢复
				// streamedFallback (软降级), 避免整轮 cross_exam 中断。
				if strings.TrimSpace(out.Content) == "" && streamedFallback != "" {
					slog.Warn("retry failed, restoring streamed fallback content",
						"fallback_len", len(streamedFallback),
					)
					out.Content = streamedFallback
				}
			}
			step.ElapsedMs = time.Since(stepStart).Milliseconds()
			steps = append(steps, step)
			r.emitStep(step)

			// v1.0.2 候选 4: 持久化 rebuttal 声明 (validateSpeak retry 路径)
			if r.cfg.RebuttalHook != nil && out.HasRebuttal() {
				if err := r.cfg.RebuttalHook(ctx, out, r.cfg.MemoryMeta); err != nil {
					_ = err // 失败隔离: log 不 fail
				}
			}

			// v0.10.23 候选 2: 新意度 retry 通路 (先 novelty 后 length limit)
			speaker := Speaker{
				Content:      out.Content,
				Reasoning:    out.Reasoning,
				EvidenceRefs: out.EvidenceRefs,
				Confidence:   out.Confidence,
				Stance:       out.Stance,
			}
			speaker, _ = applySpeakerStanceJudge(speaker, r, ctx, messages)
			speaker, _ = applySpeakerNoveltyRetryLoop(speaker, r, ctx, messages)
			// v1.0.2 候选 4: 已反驳证据 hard reject (与 stance/novelty 同级 guard,
			// 在 length limit 之前, 让最终 Speaker 不会引用 standing rebuttal)
			sessionIDStr := r.cfg.MemoryMeta.SessionUUID
			if sessionIDStr == "" {
				sessionIDStr = r.cfg.MemoryMeta.SessionID.String()
			}
			speaker, _ = applySpeakerRebuttalRetryLoop(speaker, r, ctx, messages, sessionIDStr, r.cfg.RebuttalRepository)
			return applySpeakerLengthLimit(speaker), steps, nil

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

// speakerMaxRunes v0.10.21 PR-B: 发言长度硬截断上限 (PRD §5.3 / README §5.3 一致口径)
// 按 rune (Unicode 字符) 计, 中文友好。300 字 ≈ 10-15 句中等长度发言。
const speakerMaxRunes = 300

// v0.10.23 候选 2: 新意度 Jaccard 检查常量
//   - noveltyThreshold: Jaccard > 该阈值触发 reject (PRD §4.3.3 规定 60% = 0.6)
//   - noveltyMaxRetries: reject 后 retry LLM 强制换角度的最大次数 (用户拍板 2 次)
const (
	noveltyThreshold  = 0.6
	noveltyMaxRetries = 2
)

// v0.10.24 候选 1: LLM-as-judge stance 一致性常量
//   - stanceJudgeMaxRetries: judge false 后 retry LLM 的最大次数 (用户拍板 2 次, 与 novelty 同构)
//   - stanceJudgeTemperature: 0.2 低温让 judge 稳定 (与 JudgeAssess L613 同温度)
const (
	stanceJudgeMaxRetries    = 2
	stanceJudgeTemperature   = 0.2
	stanceJudgeMaxTokens     = 200
)

// applySpeakerLengthLimit 对 Speaker.Content 强制应用 300 字硬截断。
// 若原始字符数 > 300:
//   - Content 改为 truncateRunes 截断后的字符串 (末尾追加 "...")
//   - ContentTruncated = true
//   - OriginalRunes = 原始字符数
//
// 调用点: Run() 全部正常 Speaker return 之前 (L397 / L440)。
// 错误路径 (返回 Speaker{}) 不需要截断, 因为 caller 拿到的是 error 不会渲染。
//
// 不触及 §2.1 裁决: 纯字符数限制, 客观规则, 不涉及任何判断。
func applySpeakerLengthLimit(s Speaker) Speaker {
	if utf8.RuneCountInString(s.Content) > speakerMaxRunes {
		s.OriginalRunes = utf8.RuneCountInString(s.Content)
		s.Content = truncateRunes(s.Content, speakerMaxRunes)
		s.ContentTruncated = true
	}
	return s
}

// applySpeakerNoveltyCheck v0.10.23 候选 2: 新意度 Jaccard 检查 (纯算法, 不 reject)
//
// 输入: 当前 Speaker + 同 agent 历史发言 messages
// 输出: rejected (bool), maxJaccard (float64)
//
// 算法: 与同 agent 历史每条 speak 算 Jaccard, 取最大值。
//   - 复用 util.BagOfWords + util.JaccardSimilarity (与 belief.convergence.go 同源)
//   - history 为空 / Speaker.Content 为空 → 返回 (false, 0) 不触发
//   - 任一历史 jaccard > noveltyThreshold (0.6) → rejected=true, maxJaccard=最大值
//
// 不触及 §2.1 裁决 (轻度): Jaccard 阈值 0.6 是客观数学, retry 行为是用户拍板的
// "2 次 retry, 失败 fallback" 设计。本次按用户授权实装, 不重新讨论。
//
// 调用点: applySpeakerNoveltyRetryLoop (Run() 内 ActionSpeak 分支, 仿 validateSpeak retry 模式)
func applySpeakerNoveltyCheck(s Speaker, history []model.Message) (rejected bool, maxJaccard float64) {
	if len(history) == 0 || s.Content == "" {
		return false, 0
	}
	currentTokens := util.BagOfWords(s.Content)
	if len(currentTokens) == 0 {
		return false, 0
	}
	for i := range history {
		h := &history[i]
		if h.ActionType != "speak" {
			continue
		}
		if h.Content == "" {
			continue
		}
		historicalTokens := util.BagOfWords(h.Content)
		j := util.JaccardSimilarity(currentTokens, historicalTokens)
		if j > maxJaccard {
			maxJaccard = j
		}
		if maxJaccard > noveltyThreshold {
			return true, maxJaccard
		}
	}
	return maxJaccard > noveltyThreshold, maxJaccard
}

// applySpeakerNoveltyRetryLoop v0.10.23 候选 2: 新意度 retry 主循环
//
// 复用 validateSpeak retry 模式 (L407-436): 用 system hint 注入 LLM,
// 强制 LLM 换角度生成新发言。最多重试 noveltyMaxRetries (2) 次, 失败 fallback
// 返回最终 Speaker (带 NoveltyRejected=true + NoveltyJaccard=实际值)。
//
// 输入:
//   - out: 第一次 LLM 生成的 Speaker (含 Content)
//   - r: ReActRunner (拿 llm client + systemBase)
//   - ctx / messages: LLM 调用上下文
//
// 输出: 调整后的 Speaker + 调整后的 messages (含 retry hints), updated flag
//
// 调用点: Run() 内 ActionSpeak 分支的 streaming success path + validateSpeak retry path
// (validateSpeak 失败后也会再次跑新意度检查, 因为 retry 后的输出可能仍重复)
func applySpeakerNoveltyRetryLoop(
	out Speaker,
	r *ReActRunner,
	ctx context.Context,
	messages []llm.Message,
) (Speaker, []llm.Message) {
	// history 为 nil → 跳过检查 (向后兼容 + 测试用)
	if r.cfg.SpeakerHistory == nil {
		return out, messages
	}

	for retryIdx := 0; retryIdx < noveltyMaxRetries; retryIdx++ {
		rejected, jaccard := applySpeakerNoveltyCheck(out, r.cfg.SpeakerHistory)
		if !rejected {
			return out, messages // 通过, 返回最终 out
		}

		// 失败: 注入 hint 让 LLM 换角度重生成
		hint := llm.Message{
			Role: "system",
			Content: fmt.Sprintf(
				"你刚才的发言与历史发言 Jaccard=%.2f (>0.6)，重复度过高。\n"+
					"请换角度重新输出 action=\"speak\"：\n"+
					"  - 引入新的证据 / 数字 / 反例\n"+
					"  - 反驳对方具体论点（而不是重复自己的）\n"+
					"  - 承认对方部分观点并重新立论\n"+
					"(本次是第 %d/%d 次换角度重试)",
				jaccard, retryIdx+1, noveltyMaxRetries,
			),
		}
		retryMsgs := append(append([]llm.Message{}, messages...), hint)
		retryContent, _, retryErr := r.llm.Complete(
			r.injectGatewayTrace(ctx, "react_novelty_retry"),
			r.systemBase,
			retryMsgs,
			llm.CompletionOptions{
				Model:       "",
				Temperature: 0.6, // 略高于 default 0.5, 鼓励换角度
				MaxTokens:   500,
				JSONMode:    true,
			},
		)
		if retryErr != nil {
			// LLM 调用失败, fallback 标记 rejected 返回
			out.NoveltyRejected = true
			out.NoveltyJaccard = jaccard
			return out, retryMsgs
		}
		var retryOut AgentOutput
		if err := json.Unmarshal([]byte(retryContent), &retryOut); err != nil {
			out.NoveltyRejected = true
			out.NoveltyJaccard = jaccard
			return out, retryMsgs
		}
		retryOut.NormalizeAction()
		if retryOut.Action != ActionSpeak || retryOut.Content == "" {
			out.NoveltyRejected = true
			out.NoveltyJaccard = jaccard
			return out, retryMsgs
		}
		// 更新 out + messages, 下一轮循环再检查
		out = Speaker{
			Content:      retryOut.Content,
			Reasoning:    retryOut.Reasoning,
			EvidenceRefs: retryOut.EvidenceRefs,
			Confidence:   retryOut.Confidence,
			Stance:       retryOut.Stance,
		}
		messages = retryMsgs
	}

	// 2 次 retry 后仍重复, 标记 NoveltyRejected fallback 返回
	finalRejected, finalJaccard := applySpeakerNoveltyCheck(out, r.cfg.SpeakerHistory)
	out.NoveltyRejected = finalRejected
	out.NoveltyJaccard = finalJaccard
	return out, messages
}

// applySpeakerStanceJudge v0.10.24 候选 1: LLM-as-judge stance 一致性 retry 主循环
//
// 触发条件: 老 isStanceConsistent(agent, out.Stance) == false 才调 judge
// (省 90% token — 一致时直接 pass)。
//
// 复用 validateSpeak retry 模式 (仿 applySpeakerNoveltyRetryLoop):
//   - judge 提示词: StanceJudgePrompt(agentType, beliefA, content)
//   - judge 输出 JSON: {is_consistent: bool, reason: string}
//   - false → 注入 hint 强制 LLM 换内容重生成 (不限制 stance 字段, 让 LLM 自然修正)
//   - 最多 stanceJudgeMaxRetries (2) 次 retry, 失败 fallback Speaker.StanceRejected=true
//     + StanceJudgeReason=最后一次 judge reason
//
// 注意: SpeakerAgent 字段类型为 model.Agent (按值复制), 不修改外部。
// 与 novelty 顺序: stance judge → novelty → length limit (stance 打回重生成 content,
// 必须在 novelty 之前; length limit 永远最后)。
//
// 触及 §2.1 (中度): LLM 主观判定。但用户已拍板 4 个标准 (judge prompt / 触发时机 / 失败动作 / token 成本),
// 本次按拍板实装, 不重新讨论。
func applySpeakerStanceJudge(
	out Speaker,
	r *ReActRunner,
	ctx context.Context,
	messages []llm.Message,
) (Speaker, []llm.Message) {
	// 1. fast filter: 老 isStanceConsistent 一致时跳过 judge (省 token)
	if isStanceConsistent(r.cfg.SpeakerAgent, out.Stance) {
		return out, messages
	}

	// 2. judge LLM 调用 + 2 次 retry
	for retryIdx := 0; retryIdx < stanceJudgeMaxRetries; retryIdx++ {
		// 调 judge LLM
		judgePrompt := StanceJudgePrompt(r.cfg.SpeakerAgent.AgentType, r.cfg.SpeakerBeliefA, out.Content)
		judgeMessages := []llm.Message{
			{Role: "system", Content: judgePrompt},
		}
		judgeContent, _, judgeErr := r.llm.Complete(
			r.injectGatewayTrace(ctx, "react_stance_judge"),
			r.systemBase,
			judgeMessages,
			llm.CompletionOptions{
				Model:       "",
				Temperature: stanceJudgeTemperature,
				MaxTokens:   stanceJudgeMaxTokens,
				JSONMode:    true,
			},
		)
		if judgeErr != nil {
			// judge LLM 失败 → 标记 fallback
			out.StanceRejected = true
			out.StanceJudgeReason = "judge LLM 调用失败: " + judgeErr.Error()
			return out, messages
		}

		// 3. 解析 judge 输出
		var judgeResult struct {
			IsConsistent bool   `json:"is_consistent"`
			Reason       string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(judgeContent), &judgeResult); err != nil {
			// 解析失败 → 标记 fallback (保守放行 Speaker)
			out.StanceRejected = true
			out.StanceJudgeReason = "judge 输出非 JSON: " + truncate(judgeContent, 50)
			return out, messages
		}

		if judgeResult.IsConsistent {
			// judge 判定一致 → pass
			return out, messages
		}

		// 4. judge 判定不一致 → 注入 hint 让 LLM 换内容重生成
		hint := llm.Message{
			Role: "system",
			Content: fmt.Sprintf(
				"你刚才的发言被 LLM 裁判判定为与当前信念度不一致 (原因: %s)。\n"+
					"请重新生成 action=\"speak\"，确保发言方向与你的信念度 %s 一致。\n"+
					"(本次是第 %d/%d 次 stance 重试)",
				judgeResult.Reason,
				beliefDirectionStr(r.cfg.SpeakerBeliefA),
				retryIdx+1, stanceJudgeMaxRetries,
			),
		}
		retryMsgs := append(append([]llm.Message{}, messages...), hint)
		retryContent, _, retryErr := r.llm.Complete(
			r.injectGatewayTrace(ctx, "react_stance_retry"),
			r.systemBase,
			retryMsgs,
			llm.CompletionOptions{
				Model:       "",
				Temperature: 0.5,
				MaxTokens:   500,
				JSONMode:    true,
			},
		)
		if retryErr != nil {
			out.StanceRejected = true
			out.StanceJudgeReason = judgeResult.Reason
			return out, retryMsgs
		}
		var retryOut AgentOutput
		if err := json.Unmarshal([]byte(retryContent), &retryOut); err != nil {
			out.StanceRejected = true
			out.StanceJudgeReason = judgeResult.Reason
			return out, retryMsgs
		}
		retryOut.NormalizeAction()
		if retryOut.Action != ActionSpeak || retryOut.Content == "" {
			out.StanceRejected = true
			out.StanceJudgeReason = judgeResult.Reason
			return out, retryMsgs
		}
		// 5. 更新 out + messages, 下一轮循环再 judge
		out = Speaker{
			Content:      retryOut.Content,
			Reasoning:    retryOut.Reasoning,
			EvidenceRefs: retryOut.EvidenceRefs,
			Confidence:   retryOut.Confidence,
			Stance:       retryOut.Stance,
		}
		messages = retryMsgs
	}

	// 6. 2 次 retry 后仍 judge false → 标记 StanceRejected fallback
	out.StanceRejected = true
	// 重做一次 judge 拿最新 reason (上面循环最后一次 judge 已保存, 重新调 1 次拿 reason)
	finalReason := judgeStanceOnce(r, ctx, r.cfg.SpeakerAgent.AgentType, r.cfg.SpeakerBeliefA, out.Content)
	out.StanceJudgeReason = finalReason
	return out, messages
}

// judgeStanceOnce 单次调 judge LLM, 用于 applySpeakerStanceJudge 最后 fallback 拿 reason
func judgeStanceOnce(r *ReActRunner, ctx context.Context, agentType model.AgentType, beliefA float64, content string) string {
	judgePrompt := StanceJudgePrompt(agentType, beliefA, content)
	judgeMessages := []llm.Message{{Role: "system", Content: judgePrompt}}
	judgeContent, _, err := r.llm.Complete(
		r.injectGatewayTrace(ctx, "react_stance_judge_final"),
		r.systemBase,
		judgeMessages,
		llm.CompletionOptions{
			Model:       "",
			Temperature: stanceJudgeTemperature,
			MaxTokens:   stanceJudgeMaxTokens,
			JSONMode:    true,
		},
	)
	if err != nil {
		return "judge LLM 失败: " + err.Error()
	}
	var result struct {
		IsConsistent bool   `json:"is_consistent"`
		Reason       string `json:"reason"`
	}
	if jerr := json.Unmarshal([]byte(judgeContent), &result); jerr != nil {
		return "judge 输出非 JSON"
	}
	return result.Reason
}

// beliefDirectionStr 把 belief_A 数值翻译成中文方向字符串 (用于 hint 文案)
func beliefDirectionStr(beliefA float64) string {
	switch {
	case beliefA > 0.55:
		return "支持选项 A (>0.55)"
	case beliefA < 0.45:
		return "支持选项 B (<0.45)"
	default:
		return "中性 (0.45-0.55, challenge 或 neutral 都允许)"
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
		// 2026-08-22 用户反馈 bug 修复: silent error 黑洞 — 流式解析可能收集了
		// raw 但 lastExtracted 仍空(LLM 输出了畸形 JSON: `{"content":""}` 闭合 quote 后
		// partial 提取到了空字符串;或 LLM 输出非 JSON markdown 代码块)。
		// 打印 raw 帮助诊断 (上限 500 字符避免日志爆)。
		rawDump := collected.String()
		if len(rawDump) > 500 {
			rawDump = rawDump[:500] + "...(truncated)"
		}
		slog.Warn("streamSpeakContent: empty lastExtracted",
			"chunks", chunks,
			"raw_len", collected.Len(),
			"raw_preview", rawDump,
		)
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

	// v0.10.1 (ADR 0021):硬编码验证 LLM 输出是否有 hallucination 模式。
	// baseRules 第 4/5/13/14 条虽然禁止,但 LLM 在 stress 下会违反。
	// 这里调用 output_validator.go 扫 content + evidence_refs。
	// Reject 会触发现有 retry 机制(react_runner.go:385),LLM 看到
	// FormatValidationIssuesForRetry() 解释后重新生成。
	//
	// 当前实现只验证 evidence_refs 空时的硬约束(用户最痛的 bug),
	// evidence_refs 非空时的 ID 校验(需 session 上下文)留 v0.11。
	valResult := ValidateAgainstHallucination(o.Content, o.EvidenceRefs, nil)
	if !valResult.OK {
		return fmt.Errorf("hallucination detected: %s", FormatValidationIssuesForRetry(valResult.Issues))
	}

	return nil
}

func truncateForPrompt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

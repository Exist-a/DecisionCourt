package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/decisioncourt/backend/internal/a2a"
	"github.com/decisioncourt/backend/internal/agent/tools"
	"github.com/decisioncourt/backend/internal/agent_gateway"
	"github.com/decisioncourt/backend/internal/belief"
	"github.com/decisioncourt/backend/internal/llm"
	"github.com/decisioncourt/backend/internal/model"
	"github.com/decisioncourt/backend/internal/private_memory"
	"github.com/google/uuid"
)

type Orchestrator struct {
	llmClient   llm.Client
	a2aBus      *a2a.Bus
	memoryRepo  private_memory.Repository
	weakenRepo  belief.WeakenRepository
	evidenceLkp EvidenceResolver
}

// NewOrchestrator wires the orchestrator with an A2A bus (for message
// routing / isolation / audit) and a private-memory repository. The Bus and
// Repository are required: this enforces that every speaking turn goes
// through the bus and every strategy note lands in private memory.
//
// v0.6: weakenRepo and evidenceLkp are optional. When both are wired the
// orchestrator activates the WeakenHook so ReAct reflect steps can declare
// weakening edges that the belief engine reads when applying future impact.
func NewOrchestrator(client llm.Client, bus *a2a.Bus, memRepo private_memory.Repository, weakenRepo belief.WeakenRepository, evidenceLkp EvidenceResolver) *Orchestrator {
	if bus == nil {
		panic("agent: a2a.Bus is required")
	}
	if memRepo == nil {
		panic("agent: private_memory.Repository is required")
	}
	return &Orchestrator{
		llmClient:   client,
		a2aBus:      bus,
		memoryRepo:  memRepo,
		weakenRepo:  weakenRepo,
		evidenceLkp: evidenceLkp,
	}
}

// NewOrchestratorLegacy preserves the v0.5.x NewOrchestrator signature for
// the test suite (and any out-of-tree callers) that haven't migrated yet.
// It simply defers to NewOrchestrator with nil weakenRepo / nil resolver,
// which disables WeakenHook emission — equivalent to pre-v0.6 behaviour.
//
// Marked Deprecated in the godoc so new wiring uses the 5-arg form.
func NewOrchestratorLegacy(client llm.Client, bus *a2a.Bus, memRepo private_memory.Repository) *Orchestrator {
	return NewOrchestrator(client, bus, memRepo, nil, nil)
}

func (o *Orchestrator) ProsecutorSpeak(
	ctx context.Context,
	agent model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
) (Speaker, error) {
	prompt := ProsecutorPrompt(agent, session, evidences, "")
	enhanced := withArgumentSummary(messages, model.AgentProsecutor, model.AgentDefender)
	speaker, err := o.speak(ctx, agent, prompt, enhanced, len(evidences) > 0)
	if err != nil {
		return speaker, err
	}
	o.recordSideEffects(ctx, session, agent, model.AgentDefender, speaker, evidences)
	return speaker, nil
}

func (o *Orchestrator) DefenderSpeak(
	ctx context.Context,
	agent model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
) (Speaker, error) {
	prompt := DefenderPrompt(agent, session, evidences, "")
	enhanced := withArgumentSummary(messages, model.AgentDefender, model.AgentProsecutor)
	speaker, err := o.speak(ctx, agent, prompt, enhanced, len(evidences) > 0)
	if err != nil {
		return speaker, err
	}
	o.recordSideEffects(ctx, session, agent, model.AgentProsecutor, speaker, evidences)
	return speaker, nil
}

// ProsecutorSpeakWithReAct is the ReAct-enabled variant of ProsecutorSpeak.
// When dispatchFn is non-nil the lawyer is given the investigator_search
// tool and may call it zero or more times before producing its final
// speech. stepHook (optional) receives every Step so the courtroom can
// stream `agent.cot_step` events to the websocket as the loop runs.
// chunkCb (optional) receives incremental content chunks when the runner
// streams the final speech — pass nil to skip streaming.
func (o *Orchestrator) ProsecutorSpeakWithReAct(
	ctx context.Context,
	agent model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
	dispatchFn tools.DispatchFn,
	stepHook StepHook,
	chunkCb SpeakChunkCallback,
) (Speaker, []Step, error) {
	return o.lawyerSpeakReAct(
		ctx, agent, session, evidences, messages,
		model.AgentProsecutor, model.AgentDefender,
		dispatchFn, stepHook, chunkCb,
	)
}

// DefenderSpeakWithReAct is the symmetric ReAct variant for the Defender.
func (o *Orchestrator) DefenderSpeakWithReAct(
	ctx context.Context,
	agent model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
	dispatchFn tools.DispatchFn,
	stepHook StepHook,
	chunkCb SpeakChunkCallback,
) (Speaker, []Step, error) {
	return o.lawyerSpeakReAct(
		ctx, agent, session, evidences, messages,
		model.AgentDefender, model.AgentProsecutor,
		dispatchFn, stepHook, chunkCb,
	)
}

// lawyerSpeakReAct is the shared implementation behind both lawyer ReAct
// entry points. It builds a runner with the investigator_search tool bound
// to (session, self), executes the Thought→Action→Observation loop, and
// on success applies the same A2A publish + private-memory write side
// effects as the non-ReAct path so downstream consumers see no difference.
func (o *Orchestrator) lawyerSpeakReAct(
	ctx context.Context,
	agent model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
	self model.AgentType,
	opponent model.AgentType,
	dispatchFn tools.DispatchFn,
	stepHook StepHook,
	chunkCb SpeakChunkCallback,
) (Speaker, []Step, error) {
	var promptBuilder func(model.Agent, model.CourtSession, []model.Evidence, string) string
	switch self {
	case model.AgentProsecutor:
		promptBuilder = ProsecutorPrompt
	case model.AgentDefender:
		promptBuilder = DefenderPrompt
	default:
		return Speaker{}, nil, fmt.Errorf("lawyerSpeakReAct: unsupported self=%s", self)
	}

	toolMap := map[string]Tool{}
	if dispatchFn != nil {
		toolMap[tools.InvestigatorSearchToolName] = tools.NewInvestigatorSearchTool(
			session.SessionUUID, string(self), dispatchFn,
		)
	}

	systemPrompt := promptBuilder(agent, session, evidences, toolBlockForPrompt(toolMap))
	systemPrompt = systemPrompt + withArgumentSummaryText(messages, self, opponent)
	// v0.5: inject the agent's episodic memory (private strategy notes from
	// prior rounds). Order matters — argument summary first (immediate
	// context), then memory (deeper history). Failure is best-effort: an
	// empty string here simply means "no prior memory yet".
	systemPrompt = systemPrompt + o.buildEpisodicMemoryBlock(ctx, session.ID, string(self))

	runner := NewReActRunner(o.llmClient, systemPrompt, toolMap, RunnerConfig{
		MaxIterations: 4,
		Timeout:       30 * 1_000_000_000, // 30s; using ns to avoid time import here
		OnSpeakChunk:  chunkCb,
		AllowedTools:  nil,
		// v0.5+: Agent Gateway 白盒子集 — 把 session / agent 注入到
		// ReActRunner 的 ctx，让每次 think/reflect/speak 调用都能进
		// llm_calls 表。
		AgentGatewayTrace: AgentGatewayTrace{
			SessionUUID: session.SessionUUID,
			AgentType:   string(self),
		},
		// v0.5: wire the private-memory hook so reflect steps carrying a
		// memory_type + memory_note get persisted as A2A private messages.
		MemoryHook: o.makeMemoryHook(),
		MemoryMeta: MemoryMeta{
			SessionID: session.ID,
			AgentType: string(self),
			Round:     session.CurrentRound,
			Phase:     string(session.CurrentPhase),
			// v0.6: 把当前 session 的证据列表注入 hook，reflect
			// 步骤触发 EmitMemoryFromOutput 时用 NormalizeEvidenceRefs
			// 把 LLM 返回的 UUID 映射回 display_id。
			Evidences: evidences,
		},
		// v0.6: weaken hook. We only activate it if both the repository
		// and the evidence resolver are wired — otherwise we silently
		// drop weaken declarations instead of crashing. (Most pre-v0.6
		// callers don't supply either, so this stays a no-op.)
		WeakenHook: o.makeWeakenHook(),
	})
	runner.SetStepHook(stepHook)

	speaker, steps, err := runner.Run(ctx, messages)
	if err != nil {
		return speaker, steps, err
	}
	speaker.Agent = agent
	o.recordSideEffects(ctx, session, agent, opponent, speaker, evidences)
	return speaker, steps, nil
}

// makeMemoryHook returns a MemoryHook closure that persists AgentOutput
// memory entries as private A2A messages. The closure captures o.a2aBus so
// each Runner instance routes to the same bus, and the runner can call it
// without knowing about A2A types.
//
// v0.5 design choice: we deliberately write ONLY to the A2A bus here, NOT
// to the legacy private_memory.Repository. PR 4 will introduce a parallel
// "dual-write" period where this hook also touches the legacy repo to
// validate parity; PR 4.5 (deferred) will drop the legacy write.
//
// Failure isolation: a Send error is logged but never returned. The runner
// relies on the hook being best-effort — aborting the trial because of a
// memory persistence glitch would be far worse UX than silently skipping
// the note.
func (o *Orchestrator) makeMemoryHook() MemoryHook {
	return func(ctx context.Context, out AgentOutput, meta MemoryMeta) error {
		_ = ctx
		// v0.5 修复：runner 内部 ctx 是 30s 超时 + courtroom service 用
		// withCancel 包裹。reflect 步骤触发 hook 时 ctx 可能已被外层
		// cancel，导致 a2aBus.Send 立刻报 "context canceled"。派生独立
		// 短超时 background ctx，确保 emit 永远能在隔离环境下完成。
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer writeCancel()
		err := EmitMemoryFromOutput(writeCtx, o.a2aBus, meta, out)
		if err != nil {
			log.Printf("[orchestrator] private memory emit failed for %s: %v", meta.AgentType, err)
		}
		return err
	}
}

// makeWeakenHook returns a WeakenHook closure that persists AgentOutput
// weaken declarations to the belief.WeakenRepository. Returns a no-op when
// either the repo or the evidence resolver is nil so pre-v0.6 callers keep
// functioning unchanged.
//
// Failure isolation mirrors makeMemoryHook: a Send error is logged but
// never returned — persist failures never abort the trial.
func (o *Orchestrator) makeWeakenHook() WeakenHook {
	return func(ctx context.Context, out AgentOutput, meta MemoryMeta) error {
		if o.weakenRepo == nil || o.evidenceLkp == nil {
			return nil
		}
		writeCtx, writeCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer writeCancel()
		if err := EmitWeakenFromOutput(writeCtx, MapToWeakenRepository(o.weakenRepo), o.evidenceLkp, meta, out); err != nil {
			log.Printf("[orchestrator] weaken emit failed for %s: %v", meta.AgentType, err)
			return err
		}
		return nil
	}
}

// withArgumentSummaryText is the system-prompt-only projection of
// withArgumentSummary: it returns the summary string instead of injecting
// it as a message, because the ReAct runner owns its message stream.
func withArgumentSummaryText(messages []model.Message, selfType, opponentType model.AgentType) string {
	if len(messages) == 0 {
		return ""
	}
	var selfLatest, opponentLatest *model.Message
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.ActionType != "speak" {
			continue
		}
		if opponentLatest == nil {
			opponentLatest = &messages[i]
			continue
		}
		if selfLatest == nil {
			selfLatest = &messages[i]
			break
		}
	}
	var sb strings.Builder
	sb.WriteString("\n## 本轮对话摘要（你必须基于这些信息回应，禁止重复）\n")
	if opponentLatest != nil {
		sb.WriteString(fmt.Sprintf("对方刚刚说：%s\n", truncate(opponentLatest.Content, 150)))
	}
	if selfLatest != nil {
		sb.WriteString(fmt.Sprintf("你之前说：%s\n", truncate(selfLatest.Content, 100)))
		sb.WriteString("注意：不要重复你之前的话，必须提出新论点或针对对方最新发言进行反驳。\n")
	} else {
		sb.WriteString("这是你的首次发言，请清晰亮明立场。\n")
	}
	return sb.String()
}

// recordSideEffects runs after every speaking turn. It (1) publishes a
// public A2A speech message that contains both the public content AND the
// private reasoning (so the bus has a faithful audit record), and (2)
// persists a private strategy_note to BOTH the new A2A private channel
// (MemoryAuditPanel reads from this) AND the legacy private_memory repo
// (kept during the dual-write transition window; PR 4.5 will drop the
// legacy write). Failures are logged but never propagated: the
// user-visible turn already succeeded and must not be rolled back.
//
// v0.5 修复：之前只写老表，前端 MemoryAuditPanel 永远看不到 —— 现在
// 额外写一条 visibility=private / message_type=strategy_note 的 A2A
// 消息，前端 hydrate 成 MemoryEntry。Memory Hook 路径（reflect 步骤
// 自动分析）保持不变。
//
// ctx 处理：recordSideEffects 在 courtroom service 的 withCancel 包裹
// 下被调用，speak 一返回外层就可能 cancel()。这里派生一个 3s 的独立
// background context，避免"ctx canceled"导致 a2aBus.Send /
// memoryRepo.Append 失败（之前日志里出现过多次）。
func (o *Orchestrator) recordSideEffects(
	_ context.Context,
	session model.CourtSession,
	agent model.Agent,
	opponent model.AgentType,
	speaker Speaker,
	evidences []model.Evidence,
) {
	// 派生独立短超时 ctx：speak 完成后 caller 的 ctx 可能已 cancel。
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer writeCancel()

	// v0.6 修复：把 speaker.EvidenceRefs 里混入的 DB UUID 归一化回
	// display_id，确保下游 A2A 消息和 private_memory 表的
	// linked_evidence_ids 字段对前端一致。LLM 偶尔会瞥见模型输入里的
	// UUID 并把它们当作 evidence_refs 返回（详见
	// .trae/documents/memory-a2a-redesign.md §"已发现但未做"）。
	normalizedRefs := NormalizeEvidenceRefs(speaker.EvidenceRefs, evidences)

	a2aMsg := a2a.Message{
		SessionID:   session.ID,
		SessionUUID: session.SessionUUID, // v0.5 修复：必须填，否则 hub 找不到房间
		Round:       session.CurrentRound,
		Phase:       string(session.CurrentPhase),
		From:        string(agent.AgentType),
		To:          string(opponent),
		MessageType: a2a.MessageTypeSpeech,
		Visibility:  a2a.VisibilityPublic,
		Payload: map[string]interface{}{
			"content":       speaker.Content,
			"reasoning":     speaker.Reasoning,
			"stance":        speaker.Stance,
			"confidence":    speaker.Confidence,
			"evidence_refs": normalizedRefs,
		},
	}
	if _, err := o.a2aBus.Send(writeCtx, a2aMsg); err != nil {
		log.Printf("[orchestrator] a2a bus send failed for %s: %v", agent.AgentType, err)
	}

	// v0.5 Episodic Memory via A2A private channel —— 写一条
	// strategy_note（自动）到 A2A Bus，hydrate 给前端 MemoryAuditPanel。
	// to=自己、visibility=private、payload 结构化字段（stance / confidence /
	// reasoning / linked_evidence_ids）让前端能渲染成结构化卡片，而不是
	// raw dump "立场=pro_a 置信度=0.80 reasoning=..." 这种串。
	if speaker.Content != "" || speaker.Reasoning != "" {
		privateMsg := a2a.Message{
			SessionID:   session.ID,
			SessionUUID: session.SessionUUID, // v0.5 修复：必须填，否则 hub 找不到房间
			Round:       session.CurrentRound,
			Phase:       string(session.CurrentPhase),
			From:        string(agent.AgentType),
			To:          string(agent.AgentType),
			MessageType: a2a.MessageTypeStrategyNote,
			Visibility:  a2a.VisibilityPrivate,
			Payload: map[string]interface{}{
				"memory_type":         string(private_memory.TypeStrategyNote),
				"stance":              speaker.Stance,
				"confidence":          speaker.Confidence,
				"reasoning":           speaker.Reasoning,
				"linked_evidence_ids": normalizedRefs,
				// 兼容老调用方：保留 content 字段（结构化字段读不到时 fallback 用）。
				"content": fmt.Sprintf("立场 %s · 置信度 %.0f%% · %s",
					stanceLabel(speaker.Stance), speaker.Confidence*100, speaker.Reasoning),
			},
		}
		if _, err := o.a2aBus.Send(writeCtx, privateMsg); err != nil {
			log.Printf("[orchestrator] a2a private strategy_note write failed for %s: %v", agent.AgentType, err)
		}
	}

	// 双写过渡期：保留老 private_memory.Repository 写入，PR 4.5 drop。
	if agent.ID == uuid.Nil {
		log.Printf("[orchestrator] skip private memory write: agent.ID is zero for %s", agent.AgentType)
		return
	}
	memEntry := private_memory.NewEntry(
		session.ID,
		agent.ID,
		session.CurrentRound,
		private_memory.TypeStrategyNote,
		fmt.Sprintf(
			"立场=%s 置信度=%.2f reasoning=%s",
			speaker.Stance, speaker.Confidence, speaker.Reasoning,
		),
	)
	memEntry.LinkedEvidenceIDs = normalizedRefs
	if _, err := o.memoryRepo.Append(writeCtx, memEntry); err != nil {
		log.Printf("[orchestrator] private memory write failed for %s: %v", agent.AgentType, err)
	}
}

// stanceLabel 把 Speaker.Stance 这种内部代号（"pro_a" / "pro_b" /
// "challenge"）转成中文短标签，给 v0.5 策略笔记结构化卡片用作 fallback
// content 字段，避免直接暴露 raw 字符串。
func stanceLabel(stance string) string {
	switch stance {
	case "pro_a":
		return "支持选项A"
	case "pro_b":
		return "支持选项B"
	case "challenge":
		return "质疑证据"
	default:
		return stance
	}
}

func (o *Orchestrator) InvestigatorSpeak(
	ctx context.Context,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
) (Speaker, error) {
	prompt := InvestigatorPrompt(session, evidences)
	agent := model.Agent{
		AgentUUID: "agent_inv_001",
		AgentType: model.AgentInvestigator,
		Name:      "调查员",
		BeliefA:   0.5,
		BeliefB:   0.5,
	}
	return o.speak(ctx, agent, prompt, messages, len(evidences) > 0)
}

// traceFor 是 v0.5+ 的 Agent Gateway trace 注入点。orchestrator 的每次
// llmClient.Complete 调用前都用它把 session / agent / task 写到 ctx，
// 装饰器从 ctx 读出来写 llm_calls 表。如果 ctx 里已经有 trace（外层
// 比如 ReActRunner 注入过），SessionUUID 继承下来、AgentType/TaskType
// 以本次调用为准（更内层的调用方权威）。
func traceFor(ctx context.Context, session model.CourtSession, agentType model.AgentType, taskType string) context.Context {
	existing := agent_gateway.FromContext(ctx)
	sid := session.SessionUUID
	if sid == "" {
		sid = existing.SessionUUID
	}
	return agent_gateway.WithTrace(ctx, agent_gateway.Trace{
		SessionUUID: sid,
		AgentType:   string(agentType),
		TaskType:    taskType,
	})
}

func (o *Orchestrator) GenerateVerdict(
	ctx context.Context,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
	judgeDecision JudgeDecision,
) (map[string]interface{}, error) {
	log.Printf("[GenerateVerdict] start session=%s preferred=%s beliefA=%.2f beliefB=%.2f",
		session.SessionUUID, judgeDecision.Preferred, judgeDecision.BeliefA, judgeDecision.BeliefB)

	prompt := ClerkPromptWithJudgeDecision(session, evidences, messages, judgeDecision)
	log.Printf("[GenerateVerdict] prompt length=%d", len(prompt))

	ctx = traceFor(ctx, session, model.AgentClerk, "verdict")
	content, _, err := o.llmClient.Complete(ctx, prompt, []llm.Message{}, llm.CompletionOptions{
		Model:       "",
		Temperature: 0.3,
		MaxTokens:   2000,
		JSONMode:    true,
	})
	if err != nil {
		log.Printf("[GenerateVerdict] LLM error: %v", err)
		return nil, err
	}
	log.Printf("[GenerateVerdict] LLM response length=%d, first 200 chars: %s",
		len(content), truncate(content, 200))

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		log.Printf("[GenerateVerdict] JSON parse error: %v, raw content (first 500 chars): %s",
			err, truncate(content, 500))
		return nil, fmt.Errorf("failed to parse verdict JSON: %w", err)
	}

	log.Printf("[GenerateVerdict] success, keys=%v", resultKeys(result))
	return result, nil
}

func resultKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ClerkSummary generates a brief summary of the current round.
func (o *Orchestrator) ClerkSummary(
	ctx context.Context,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
	round int,
) (string, error) {
	prompt := ClerkSummaryPrompt(session, evidences, messages, round)

	ctx = traceFor(ctx, session, model.AgentClerk, "summary")
	content, _, err := o.llmClient.Complete(ctx, prompt, []llm.Message{}, llm.CompletionOptions{
		Model:       "",
		Temperature: 0.3,
		MaxTokens:   500,
		JSONMode:    true,
	})
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// If JSON parsing fails, return the raw content as summary
		return content, nil
	}

	if summary, ok := result["summary"].(string); ok {
		return summary, nil
	}
	return content, nil
}

// JudgeAssess allows the judge to assess the current debate and update beliefs.
func (o *Orchestrator) JudgeAssess(
	ctx context.Context,
	judge model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
) (beliefA, beliefB float64, reasoning string, err error) {
	prompt := JudgePrompt(session, evidences, messages, judge.BeliefA, judge.BeliefB)

	ctx = traceFor(ctx, session, model.AgentJudge, "assess")
	content, _, err := o.llmClient.Complete(ctx, prompt, []llm.Message{}, llm.CompletionOptions{
		Model:       "",
		Temperature: 0.3,
		MaxTokens:   500,
		JSONMode:    true,
	})
	if err != nil {
		return judge.BeliefA, judge.BeliefB, "", err
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return judge.BeliefA, judge.BeliefB, content, nil
	}

	if ba, ok := result["belief_a"].(float64); ok {
		beliefA = ba
	} else {
		beliefA = judge.BeliefA
	}
	if bb, ok := result["belief_b"].(float64); ok {
		beliefB = bb
	} else {
		beliefB = judge.BeliefB
	}
	if r, ok := result["reasoning"].(string); ok {
		reasoning = r
	}

	return beliefA, beliefB, reasoning, nil
}

// JudgeDecision represents the judge's final ruling decision.
type JudgeDecision struct {
	BeliefA        float64 `json:"belief_a"`
	BeliefB        float64 `json:"belief_b"`
	Preferred      string  `json:"preferred"` // "option_a" or "option_b" or "neutral"
	Reasoning      string  `json:"reasoning"`
	Recommendation string  `json:"recommendation"`
}

// JudgeFinalDecision allows the judge to make a final ruling based on beliefs.
func (o *Orchestrator) JudgeFinalDecision(
	ctx context.Context,
	judge model.Agent,
	session model.CourtSession,
	evidences []model.Evidence,
	messages []model.Message,
) (JudgeDecision, error) {
	prompt := JudgeFinalPrompt(session, evidences, messages, judge.BeliefA, judge.BeliefB)

	ctx = traceFor(ctx, session, model.AgentJudge, "final")
	content, _, err := o.llmClient.Complete(ctx, prompt, []llm.Message{}, llm.CompletionOptions{
		Model:       "",
		Temperature: 0.2,
		MaxTokens:   800,
		JSONMode:    true,
	})
	if err != nil {
		return JudgeDecision{}, err
	}

	var result JudgeDecision
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return JudgeDecision{}, fmt.Errorf("failed to parse judge decision JSON: %w", err)
	}

	// Ensure belief values are valid
	if result.BeliefA < 0 || result.BeliefA > 1 {
		result.BeliefA = judge.BeliefA
	}
	if result.BeliefB < 0 || result.BeliefB > 1 {
		result.BeliefB = judge.BeliefB
	}

	return result, nil
}

func (o *Orchestrator) speak(
	ctx context.Context,
	agent model.Agent,
	systemPrompt string,
	messages []model.Message,
	hasEvidence bool,
) (Speaker, error) {
	var llmMessages []llm.Message
	for _, m := range messages {
		role := "assistant"
		if m.ActionType == "system" {
			role = "system"
		}
		llmMessages = append(llmMessages, llm.Message{
			Role:    role,
			Content: m.Content,
		})
	}

	content, _, err := o.llmClient.Complete(traceFor(ctx, model.CourtSession{}, agent.AgentType, "speak"), systemPrompt, llmMessages, llm.CompletionOptions{
		Model:       "",
		Temperature: 0.8,
		MaxTokens:   500,
		JSONMode:    true,
	})
	if err != nil {
		return Speaker{Agent: agent}, err
	}

	output, err := parseOutput(content, hasEvidence)
	if err != nil {
		// Retry once if the output violates evidence rules.
		retryPrompt := systemPrompt + "\n\n## 重要提醒\n你上一轮输出不合法：" + err.Error() + "。请严格按 JSON 格式重新生成。"
		retryContent, _, retryErr := o.llmClient.Complete(traceFor(ctx, model.CourtSession{}, agent.AgentType, "speak_retry"), retryPrompt, llmMessages, llm.CompletionOptions{
			Model:       "",
			Temperature: 0.7,
			MaxTokens:   500,
			JSONMode:    true,
		})
		if retryErr == nil {
			if retryOutput, retryParseErr := parseOutput(retryContent, hasEvidence); retryParseErr == nil {
				output = retryOutput
			} else {
				// Fallback: try to extract JSON from markdown or raw content
				extracted, ok := extractJSONFromContent(retryContent)
				if ok {
					return Speaker{
						Agent:        agent,
						Content:      extracted,
						Reasoning:    "",
						EvidenceRefs: []string{},
						Confidence:   0.5,
						Stance:       inferStance(agent),
					}, nil
				}
				return Speaker{
					Agent:        agent,
					Content:      "[系统错误：AI回复格式异常，已记录]",
					Reasoning:    "",
					EvidenceRefs: []string{},
					Confidence:   0.5,
					Stance:       inferStance(agent),
				}, nil
			}
		} else {
			// Fallback: try to extract JSON from markdown or raw content
			extracted, ok := extractJSONFromContent(content)
			if ok {
				return Speaker{
					Agent:        agent,
					Content:      extracted,
					Reasoning:    "",
					EvidenceRefs: []string{},
					Confidence:   0.5,
					Stance:       inferStance(agent),
				}, nil
			}
			return Speaker{
				Agent:        agent,
				Content:      "[系统错误：AI回复格式异常，已记录]",
				Reasoning:    "",
				EvidenceRefs: []string{},
				Confidence:   0.5,
				Stance:       inferStance(agent),
			}, nil
		}
	}

	if !isStanceConsistent(agent, output.Stance) {
		// Retry once with explicit correction hint.
		retryPrompt := systemPrompt + "\n\n## 重要提醒\n你上一轮输出的 stance 与你当前的信念度不一致。请重新生成，确保 stance 与信念度方向一致。"
		retryContent, _, retryErr := o.llmClient.Complete(traceFor(ctx, model.CourtSession{}, agent.AgentType, "stance_retry"), retryPrompt, llmMessages, llm.CompletionOptions{
			Model:       "",
			Temperature: 0.7,
			MaxTokens:   500,
			JSONMode:    true,
		})
		if retryErr == nil {
			if retryOutput, retryParseErr := parseOutput(retryContent, hasEvidence); retryParseErr == nil && isStanceConsistent(agent, retryOutput.Stance) {
				output = retryOutput
			}
		}
	}

	return Speaker{
		Agent:        agent,
		Content:      output.Content,
		Reasoning:    output.Reasoning,
		EvidenceRefs: output.EvidenceRefs,
		Confidence:   output.Confidence,
		Stance:       output.Stance,
	}, nil
}

func parseOutput(content string, hasEvidence bool) (AgentOutput, error) {
	var output AgentOutput
	if err := json.Unmarshal([]byte(content), &output); err != nil {
		return AgentOutput{}, err
	}
	if strings.TrimSpace(output.Reasoning) == "" {
		return AgentOutput{}, fmt.Errorf("empty reasoning")
	}
	if strings.TrimSpace(output.Content) == "" {
		return AgentOutput{}, fmt.Errorf("empty content")
	}
	// When evidence exists, require at least one reference to prevent hallucination.
	// When no evidence exists, allow empty evidence_refs.
	if hasEvidence && len(output.EvidenceRefs) == 0 {
		return AgentOutput{}, fmt.Errorf("empty evidence_refs")
	}
	if output.Confidence < 0 || output.Confidence > 1 {
		return AgentOutput{}, fmt.Errorf("confidence out of range")
	}
	if !isValidStance(output.Stance) {
		return AgentOutput{}, fmt.Errorf("invalid stance: %s", output.Stance)
	}
	return output, nil
}

func isValidStance(stance string) bool {
	switch stance {
	case "pro_a", "pro_b", "challenge", "neutral":
		return true
	}
	return false
}

func withArgumentSummary(messages []model.Message, selfType model.AgentType, opponentType model.AgentType) []model.Message {
	if len(messages) == 0 {
		return messages
	}

	// Heuristic: in the debate the most recent speak is from the opponent, the one before that is from self.
	var selfLatest, opponentLatest *model.Message
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.ActionType != "speak" {
			continue
		}
		if opponentLatest == nil {
			opponentLatest = &messages[i]
			continue
		}
		if selfLatest == nil {
			selfLatest = &messages[i]
			break
		}
	}

	var summary strings.Builder
	summary.WriteString("## 本轮对话摘要（你必须基于这些信息回应，禁止重复）\n")
	if opponentLatest != nil {
		summary.WriteString(fmt.Sprintf("对方刚刚说：%s\n", truncate(opponentLatest.Content, 150)))
	}
	if selfLatest != nil {
		summary.WriteString(fmt.Sprintf("你之前说：%s\n", truncate(selfLatest.Content, 100)))
		summary.WriteString("注意：不要重复你之前的话，必须提出新论点或针对对方最新发言进行反驳。\n")
	} else {
		summary.WriteString("这是你的首次发言，请清晰亮明立场。\n")
	}

	result := []model.Message{{
		SessionID:  messages[0].SessionID,
		ActionType: "system",
		Content:    summary.String(),
	}}
	result = append(result, messages...)
	return result
}

func inferStance(agent model.Agent) string {
	if agent.BeliefA > 0.55 {
		return "pro_a"
	}
	if agent.BeliefA < 0.45 {
		return "pro_b"
	}
	return "neutral"
}

func isStanceConsistent(agent model.Agent, stance string) bool {
	switch agent.AgentType {
	case model.AgentProsecutor:
		// Prosecutor should not explicitly support B.
		if stance == "pro_b" && agent.BeliefA > 0.45 {
			return false
		}
	case model.AgentDefender:
		// Defender should not explicitly support A.
		if stance == "pro_a" && agent.BeliefA < 0.55 {
			return false
		}
	}

	// General belief-direction check.
	if stance == "pro_a" && agent.BeliefA < 0.45 {
		return false
	}
	if stance == "pro_b" && agent.BeliefA > 0.55 {
		return false
	}
	return true
}

// extractJSONFromContent tries to find and parse a JSON object from LLM output.
// Handles common cases like markdown code blocks (```json ... ```).
func extractJSONFromContent(content string) (string, bool) {
	// Remove markdown code block markers
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	var raw struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(trimmed), &raw); err == nil && raw.Content != "" {
		return raw.Content, true
	}

	// Fallback: try to trim everything before first { and after last }
	first := strings.Index(trimmed, "{")
	last := strings.LastIndex(trimmed, "}")
	if first >= 0 && last > first {
		trimmed = trimmed[first : last+1]
		var raw2 struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(trimmed), &raw2); err == nil && raw2.Content != "" {
			return raw2.Content, true
		}
	}

	return "", false
}

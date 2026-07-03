export type CourtPhase =
  | "idle"
  | "clarification"
  | "option_generation"
  | "opening"
  | "evidence"
  | "cross_exam"
  | "closing"
  | "deliberation"
  | "verdict"
  | "appeal";

export type CourtStatus = "active" | "paused" | "completed" | "aborted";

export type AgentType = "prosecutor" | "defender" | "investigator" | "clerk" | "judge";

export type EvidenceType =
  | "fact"
  | "data"
  | "expert_opinion"
  | "preference"
  | "constraint";

export type EvidenceSource =
  | "user"
  | "web_search"
  | "agent_question"
  | "clarification_answer";

export type EvidenceStatus = "admitted" | "challenged" | "rejected";

export interface CourtSession {
  session_uuid: string;
  title: string;
  option_a: string;
  option_b: string;
  context: string;
  mode: "quick" | "standard" | "deep";
  max_rounds: number;
  current_phase: CourtPhase;
  current_round: number;
  status: CourtStatus;
  converged: boolean;
  created_at: string;
  updated_at: string;
}

export interface Agent {
  agent_uuid: string;
  agent_type: AgentType;
  name: string;
  role: string;
  belief_a: number;
  belief_b: number;
  model?: string;
  temperature?: number;
  system_prompt?: string;
  status: string;
}

export interface Evidence {
  evidence_id: string;
  type: EvidenceType;
  source: EvidenceSource;
  content: string;
  url?: string | null;
  submitted_by: string;
  credibility_score: number;
  relevance_score: number;
  impact_on_option_a: number;
  impact_on_option_b: number;
  status: EvidenceStatus;
  challenge_reason?: string;
  created_at: string;
}

export interface CotStep {
  index: number;
  thought: string;
  action: string;
  tool_name?: string;
  tool_input?: Record<string, unknown>;
  observation?: string;
  error?: string;
  elapsed_ms: number;
}

export interface Message {
  id: string;
  agent_id?: string;
  agent_type?: AgentType;
  name?: string;
  phase: CourtPhase;
  round: number;
  action_type:
    | "speak"
    | "submit_evidence"
    | "ask_question"
    | "search"
    | "phase_change"
    | "clarification_question"
    | "clarification_answer"
    | "option_generated"
    | "system";
  content: string;
  evidence_refs?: string[];
  cot_steps?: CotStep[];
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface BeliefSnapshot {
  agent_id: string;
  agent_type: AgentType;
  round: number;
  belief_a: number;
  belief_b: number;
  delta: number;
  trigger_event?: string;
}

export interface Verdict {
  verdict_id: string;
  session_uuid: string;
  content: string;
  summary: string;
  /**
   * 庭审过程纪要（v0.5+ UX 增量）。
   * 与 summary（采纳建议）不同：trial_summary 是 1-2 句叙事，
   * 告诉用户"庭审中双方怎么攻防、关键转折点在哪"。
   * 老 verdict（v0.5 之前生成）没有此字段，可能为空字符串。
   */
  trial_summary?: string;
  option_a_score: number;
  option_b_score: number;
  consensus_points: string[] | string;
  divergence_points: string[] | string;
  recommendation: string;
  user_feedback?: "helpful" | "not_helpful" | "none";
  created_at: string;
}

export interface CreateSessionRequest {
  title: string;
  option_a?: string;
  option_b?: string;
  context?: string;
  mode?: "quick" | "standard" | "deep";
}

export interface CreateSessionResponse {
  code: number;
  data: CourtSession;
}

export interface SubmitEvidenceRequest {
  content: string;
  type: EvidenceType;
  source?: EvidenceSource;
}

export interface UserActionRequest {
  action:
    | "direct_verdict"
    | "skip_agent"
    | "answer_question"
    | "pause"
    | "resume"
    | "submit_evidence"
    | "interrupt"
    | "start_cross_exam"
    | "continue_cross_exam"
    // v0.8.3 新增：法官在 verdict 阶段点"补充证据重新开庭"，回到
    // evidence 阶段（保持原 round）。参见 decisioncourt-prd §7.2 + 后端
    // internal/courtroom/service.go::reopenTrial。
    | "reopen_trial";
  question_id?: string;
  answer?: string;
  query?: string;
  content?: string;
  type?: EvidenceType;
}

export interface UserInterruptEvent extends CourtEvent {
  type: "user.interrupt";
  payload: {
    content: string;
    phase: CourtPhase;
    round: number;
  };
}

export interface CourtEvent {
  type: string;
  payload: unknown;
  timestamp: string;
}

export interface AgentSpeakEvent extends CourtEvent {
  type: "agent.speak";
  payload: {
    agent_id: string;
    agent_type: AgentType;
    name: string;
    phase: CourtPhase;
    round: number;
    content: string;
    evidence_refs?: string[];
    belief_a?: number;
    belief_b?: number;
  };
}

export interface AgentCotStepEvent extends CourtEvent {
  type: "agent.cot_step";
  payload: {
    agent_id: string;
    agent_type: AgentType;
    step: CotStep;
  };
}

export interface EvidenceAddedEvent extends CourtEvent {
  type: "evidence.added";
  payload: Evidence;
}

export interface EvidenceChallengedEvent extends CourtEvent {
  type: "evidence.challenged";
  payload: {
    evidence_id: string;
    agent_id: string;
    agent_type: AgentType;
    reason: string;
  };
}

export interface BeliefUpdatedEvent extends CourtEvent {
  type: "belief.updated";
  payload: {
    round: number;
    agent_id: string;
    agent_type: AgentType;
    belief_a: number;
    belief_b: number;
    delta: number;
  };
}

export interface PhaseChangedEvent extends CourtEvent {
  type: "phase.changed";
  payload: {
    previous_phase: CourtPhase;
    current_phase: CourtPhase;
    current_round?: number;
    message?: string;
  };
}

export interface UserActionRequiredEvent extends CourtEvent {
  type: "user.action.required";
  payload: {
    action: string;
    question_id?: string;
    question?: string;
    purpose?: string;
    skip_allowed?: boolean;
  };
}

export interface SearchStartedEvent extends CourtEvent {
  type: "search.started";
  payload: {
    agent_id: string;
    dispatcher?: string;
    query: string;
  };
}

export interface SearchCompletedEvent extends CourtEvent {
  type: "search.completed";
  payload: {
    agent_id: string;
    dispatcher?: string;
    query: string;
    result_count: number;
    finding_id?: string;
  };
}

// agent.thinking_started / agent.thinking_finished 是 ReAct 起步/结束事件，
// 前端 ThinkingBubble 组件订阅这两个事件。
export interface AgentThinkingStartedEvent extends CourtEvent {
  type: "agent.thinking_started";
  payload: {
    agent_id: string;
    agent_type: AgentType;
  };
}

export interface AgentThinkingFinishedEvent extends CourtEvent {
  type: "agent.thinking_finished";
  payload: {
    agent_id: string;
    agent_type: AgentType;
  };
}

/**
 * 流式发言增量事件：每收到一个 chunk，前端 AgentAvatar bubble 的
 * 内容就实时增长 —— 这就是「打字机」效果。accumulated 是到当前为止的
 * 完整 content，前端通常直接显示 accumulated 即可。
 */
export interface AgentSpeakChunkEvent extends CourtEvent {
  type: "agent.speak_chunk";
  payload: {
    agent_id: string;
    agent_type: AgentType;
    chunk: string;
    accumulated: string;
  };
}

// a2a.message 事件承载调查员派遣/回报（公开可见性），前端 InvestigatorPanel
// 组件订阅这两个 payload。
export type A2AVisibility = "public" | "private";

export interface A2AMessageEvent extends CourtEvent {
  type: "a2a.message";
  payload: {
    id: string;
    message_uuid: string;
    session_id: string;
    round: number;
    phase: string;
    from_agent: string;
    to_agent: string;
    message_type:
      | "speech"
      | "evidence"
      | "challenge"
      | "inquiry"
      | "verdict_task"
      | "dispatch"
      | "report"
      | "strategy_note"
      | "opponent_weakness"
      | "self_correction"
      | "evidence_eval"
      | string;
    visibility: A2AVisibility;
    payload: Record<string, unknown>;
    created_at: string;
  };
}

// ============================================================
// v0.6 Belief Engine — 信念引擎结构化审计
// ============================================================
//
// 后端在 v0.6 把「证据 → 信念变化」的每一步都写入 belief_diffs 表，
// 同时通过 belief.diff 事件推给前端。前端 BeliefDiffCard 渲染单条
// diff 卡片；StanceChart 增加收敛判断（基于 belief.convergence）。
//
// 详见 .trae/documents/belief-engine-v06.md（待写）。
export type BeliefDiffSource = "evidence" | "weaken" | "anchor_pull";
export type BeliefDirection = "supports_a" | "supports_b" | "neutral";

/**
 * BeliefDiff is the frontend-friendly projection of one row in
 * belief_diffs. Each row = one (evidence piece, agent) pair from the
 * Bayesian-log-odds engine. Frontend BeliefDiffCard renders it as a
 * one-line timeline entry: "0.75 → 0.78 (Δ+0.03, weight=0.50)".
 */
export interface BeliefDiff {
  id: string;
  round: number;
  phase: string;
  agent_type: AgentType;
  evidence_id?: string;
  source: BeliefDiffSource;
  direction: BeliefDirection;
  prior_belief_a: number;
  posterior_belief_a: number;
  delta_belief_a: number;
  prior_logit: number;
  posterior_logit: number;
  evidence_weight: number;
  /** 1 - max(weaken strength) targeting this agent for this evidence.
   *  1.0 = no weakening, 0.0 = fully blocked. */
  weaken_factor: number;
  reason: string;
  created_at: string;
}

/**
 * ConvergenceInfo is the v0.6 multi-signal convergence reason emitted
 * by belief.convergence. UI shows it as a ConvergenceBadge next to the
 * trial summary.
 */
export interface ConvergenceInfo {
  reason:
    | "reasoning_oscillation"
    | "consensus"
    | "belief_stable"
    | "max_rounds";
  round: number;
  /** Human-readable Chinese caption for tooltips / a11y. */
  reason_message: string;
  /** Wall-clock time when the convergence was detected. */
  detectedAt: string;
}

export interface BeliefDiffEvent extends CourtEvent {
  type: "belief.diff";
  payload: BeliefDiff;
}

export interface BeliefConvergenceEvent extends CourtEvent {
  type: "belief.convergence";
  payload: {
    reason: ConvergenceInfo["reason"];
    round: number;
    converged: boolean;
    reason_message: string;
  };
}

// ============================================================
// v0.5 Episodic Memory — 前端私有策略笔记类型
// ============================================================
//
// 后端 a2a_messages 表中 visibility=private 的 4 种 MessageType
// （strategy_note / opponent_weakness / self_correction / evidence_eval）
// 在前端统一抽象为 MemoryEntry。详见 .trae/documents/memory-a2a-redesign.md
// §PR 4。

/** v0.5 私有情节记忆的 4 种 kind。 */
export type MemoryKind =
  | "strategy_note"
  | "opponent_weakness"
  | "self_correction"
  | "evidence_eval";

/**
 * MemoryEntry is the frontend-friendly projection of an A2A private
 * message. The frontend store keeps these in chronological order so the
 * MemoryAuditPanel can render a stable timeline.
 *
 * Note: `content` is omitted when the user enables "真实法庭模式" — the
 * panel renders only kind + count in that mode. Storing the full entry
 * regardless keeps toggle UX instant (no re-fetch required).
 */
export interface MemoryEntry {
  /** Backend a2a_messages.id (uuid string). Stable across renders. */
  id: string;
  kind: MemoryKind;
  /** Owning agent type ("prosecutor" / "defender" / "investigator"). */
  agentType: AgentType;
  round: number;
  phase: string;
  /** Free-form note text from the LLM. May be empty when toggled off. */
  content: string;
  /** Linked evidence IDs (parsed from payload.linked_evidence_ids). */
  linkedEvidenceIds: string[];
  /** ISO-8601 timestamp from a2a_messages.created_at. */
  createdAt: string;
  // === v0.5 结构化字段（后端 recordSideEffects 在 strategy_note 写入） ===
  /** Agent 本轮立场（"pro_a" / "pro_b" / "challenge"）。可选 —— 老调用方可能没填。 */
  stance?: string;
  /** 置信度 0..1。 */
  confidence?: number;
  /** LLM 推理链 —— 为什么持这个立场。 */
  reasoning?: string;
}

// InvestigationFinding 是调查发现（区别于用户证据）的展示模型，由后端
// GET /api/v1/courtrooms/:uuid/investigations 返回。
export interface InvestigationFinding {
  finding_uuid: string;
  session_uuid: string;
  dispatcher: string;
  investigator: string;
  query: string;
  summary: string;
  result_count: number;
  source_provider: string;
  /**
   * 每条搜索结果以 "title | url | content" 形式串联成一行。点击
   * InvestigatorPanel 里的某条调查发现会展开显示完整内容。
   */
  raw_results?: string[];
  created_at: string;
}

export interface VerdictReadyEvent extends CourtEvent {
  type: "verdict.ready";
  payload: {
    verdict_id: string;
    summary: string;
    /** v0.5+ 新增：庭审过程纪要（老 payload 可能没有这个字段） */
    trial_summary?: string;
    option_a_score: number;
    option_b_score: number;
  };
}

export interface ErrorEvent extends CourtEvent {
  type: "error";
  payload: {
    code: string;
    message: string;
  };
}

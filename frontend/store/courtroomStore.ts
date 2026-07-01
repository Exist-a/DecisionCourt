import { create } from "zustand";
import { flushSync } from "react-dom";
import type {
  Agent,
  AgentType,
  BeliefSnapshot,
  CotStep,
  CourtPhase,
  CourtSession,
  Evidence,
  InvestigationFinding,
  Message,
  Verdict,
  CourtEvent,
  MemoryEntry,
  MemoryKind,
  BeliefDiff,
  ConvergenceInfo,
} from "@/types";

const phaseLabels: Record<CourtPhase, string> = {
  idle: "立案",
  clarification: "问题澄清",
  option_generation: "选项生成",
  opening: "开庭陈述",
  evidence: "举证阶段",
  cross_exam: "质证阶段",
  closing: "结案陈词",
  deliberation: "判决生成中",
  verdict: "判决已生成",
  appeal: "上诉/再审",
};

function formatPhaseMessage(phase: CourtPhase, round?: number) {
  const label = phaseLabels[phase] ?? phase;
  return round ? `进入阶段：${label}（第 ${round} 轮）` : `进入阶段：${label}`;
}

interface CourtroomState {
  session: CourtSession | null;
  agents: Agent[];
  evidences: Evidence[];
  messages: Message[];
  beliefSnapshots: BeliefSnapshot[];
  verdict: Verdict | null;
  isLoading: boolean;
  pendingUserAction: {
    action: string;
    question_id?: string;
    question?: string;
    purpose?: string;
    skip_allowed?: boolean;
  } | null;
  /**
   * cotTrail buffers `agent.cot_step` events that arrive during a single
   * lawyer speaking turn. When the corresponding `agent.speak` lands we
   * attach the buffered steps to the new message and clear the trail.
   * Keyed by agent_type so prosecutor and defender can have parallel trails
   * (they never overlap in practice but this is defensive).
   */
  cotTrail: Record<AgentType, CotStep[]>;
  /**
   * activeThinking tracks which agent is currently mid-ReAct reasoning
   * (i.e. agent.thinking_started has fired but thinking_finished has not).
   * Frontend ThinkingBubble component reads this to render the cloud.
   */
  activeThinking: Record<
    AgentType,
    { agentId: string; startedAt: string } | undefined
  >;
  /**
   * activeInvestigation describes the in-flight Investigator search.
   * Set by search.started, cleared by search.completed (success or
   * failure). The Investigator avatar reads this to render a spinner
   * and the "正在调查" bubble.
   *
   * Single-value (not a map) because in practice only one search runs
   * at a time within a session — concurrent dispatches are blocked by
   * the courtroom service's session lock.
   */
  activeInvestigation: {
    dispatcher: string;
    query: string;
    startedAt: string;
  } | null;
  /**
   * streamingContent 描述当前正在流式输出的律师发言内容。后端 speak
   * action 走 LLM.StreamComplete，每收到一个 chunk 就把 accumulated
   * 推送过来，前端 AgentAvatar bubble 直接显示 accumulated + 闪烁光标
   * 即可实现打字机效果。agent.speak 事件到达时被清空。
   */
  streamingContent: {
    agentId: string;
    agentType: Agent["agent_type"];
    accumulated: string;
  } | null;
  /**
   * investigationEvents is the chronological feed for the InvestigatorPanel:
   * each entry represents one dispatch or one report. Stored separately
   * from messages because investigation activity is a continuous stream
   * that doesn't map 1:1 to courtroom messages.
   */
  investigationEvents: InvestigationEvent[];

  /**
   * memoryEntries is the v0.5 episodic-memory timeline. Each entry is one
   * A2A private message (visibility=private, message_type ∈ strategy_note
   * / opponent_weakness / self_correction / evidence_eval) hydrated from
   * the a2a.message WebSocket stream. Stored chronologically so the
   * MemoryAuditPanel can render a stable list without re-sorting per
   * render.
   */
  memoryEntries: MemoryEntry[];

  /**
   * realCourthouseMode is the v0.5 "真实法庭模式" toggle. When true,
   * MemoryAuditPanel hides individual memory.content and shows only
   * kind + count summary. UI-only filter — does NOT change what the
   * backend stores or what the LLM sees, so toggling mid-trial is safe.
   * Default false ("AI 可视化模式") per memory-a2a-redesign.md §1.5.
   */
  realCourthouseMode: boolean;

  /**
   * v0.6 belief engine: chronological list of all BeliefDiff rows
   * received via WebSocket. The frontend BeliefDiffCard renders the most
   * recent N entries. Persisted by the backend in belief_diffs table;
   * re-hydrated on session restore via GET /belief-diffs.
   */
  beliefDiffs: BeliefDiff[];

  /**
   * v0.6 convergence: the most recent ConvergenceInfo emitted by the
   * engine. null when the trial is still in progress. Once set, it
   * stays set (UI shows a ConvergenceBadge next to the verdict).
   */
  convergenceInfo: ConvergenceInfo | null;

  setSession: (session: CourtSession) => void;
  setAgents: (agents: Agent[]) => void;
  updateAgentBelief: (
    agentType: Agent["agent_type"],
    beliefA: number,
    beliefB: number,
  ) => void;
  addEvidence: (evidence: Evidence) => void;
  challengeEvidence: (evidenceId: string, reason: string) => void;
  addMessage: (message: Message) => void;
  addBeliefSnapshot: (snapshot: BeliefSnapshot) => void;
  setPhase: (phase: CourtPhase, round?: number) => void;
  setVerdict: (verdict: Verdict) => void;
  setLoading: (loading: boolean) => void;
  setPendingUserAction: (action: CourtroomState["pendingUserAction"]) => void;
  appendCotStep: (agentType: AgentType, step: CotStep) => void;
  consumeCotTrail: (agentType: AgentType) => CotStep[];
  setActiveThinking: (
    agentType: AgentType,
    meta: { agentId: string; startedAt: string } | undefined,
  ) => void;
  appendInvestigationEvent: (event: InvestigationEvent) => void;
  /**
   * completeInvestigationEvent patches an existing dispatch entry into a
   * report entry. The InvestigatorPanel uses this so the dispatch row's
   * spinner stops when the matching report comes back.
   *
   * Returns true when an entry was patched, false when no candidate was
   * found. Callers should fall back to appending a fresh report entry in
   * that case so the UI never shows orphan "搜索中" rows.
   *
   * Match priority (most → least specific):
   *   1) kind=dispatch AND status=searching AND dispatcher AND query all match
   *   2) kind=dispatch AND status=searching AND dispatcher matches (query
   *      tolerance — covers LLM rephrasing the query between tool_call
   *      and search.started)
   *   3) kind=dispatch AND dispatcher matches (legacy rows that pre-date
   *      the status field — these would otherwise spin forever)
   */
  completeInvestigationEvent: (
    match: {
      dispatcher: string;
      query: string;
    },
    patch: Partial<InvestigationEvent>,
  ) => boolean;
  setInvestigationFindings: (findings: InvestigationFinding[]) => void;
  setActiveInvestigation: (
    info: { dispatcher: string; query: string; startedAt: string } | null,
  ) => void;
  /**
   * applySpeakChunk 更新流式发言内容。每次 chunk 到达都会调用一次。
   * 通常 accumulated 字段值会越来越长，前端 AgentAvatar 直接显示即可。
   */
  applySpeakChunk: (chunk: {
    agentId: string;
    agentType: Agent["agent_type"];
    accumulated: string;
  }) => void;
  /**
   * clearStreamingContent 在发言结束（agent.speak 到达或思考结束）时
   * 被调用，把 streamingContent 重置为 null，让 bubble 切换到完整内容。
   */
  clearStreamingContent: () => void;
  /**
   * setMemoryEntries hydrates the v0.5 episodic-memory timeline from a
   * REST snapshot (e.g. on session restore). Pass entries already sorted
   * by round + createdAt asc; the store does NOT re-sort.
   */
  setMemoryEntries: (entries: MemoryEntry[]) => void;
  /**
   * appendMemoryEntry inserts a single v0.5 memory entry at the correct
   * chronological position. Idempotent on `id` — re-applying the same
   * event (e.g. on reconnect) is a no-op.
   */
  appendMemoryEntry: (entry: MemoryEntry) => void;
  /**
   * toggleRealCourthouseMode flips the v0.5 "真实法庭模式" UI flag.
   * Pure UI state — does NOT touch the backend or LLM context.
   */
  toggleRealCourthouseMode: () => void;
  /**
   * v0.6 belief engine setters.
   *
   * - appendBeliefDiff: insert a single diff at the right chronological
   *   position. Idempotent on id.
   * - setBeliefDiffs: bulk-load on session restore.
   * - setConvergenceInfo: replace the current convergence state.
   */
  appendBeliefDiff: (diff: BeliefDiff) => void;
  setBeliefDiffs: (diffs: BeliefDiff[]) => void;
  setConvergenceInfo: (info: ConvergenceInfo | null) => void;
  reset: () => void;
}

export interface InvestigationEvent {
  id: string;
  kind: "dispatch" | "report";
  /** 调查状态：dispatch 时为 "searching"，report 到达后变成 "completed"
   *  或 "failed"。InvestigatorPanel 据此决定显示 spinner 还是 ✓。 */
  status?: "searching" | "completed" | "failed";
  dispatcher: string;
  query: string;
  findingId?: string;
  resultCount?: number;
  summary?: string;
  /** 后端返回的原始搜索结果，每条 "title | url | content"。点击行展开后显示。 */
  rawResults?: string[];
  createdAt: string;
}

/**
 * findDispatchIndex locates the dispatch entry that a search.completed
 * event should "upgrade" into a report row. Walked back-to-front so the
 * most recent dispatch is preferred. Returns -1 when no candidate exists.
 */
function findDispatchIndex(
  events: InvestigationEvent[],
  match: { dispatcher: string; query: string },
): number {
  // 优先级 1：status='searching' AND dispatcher AND query 全部匹配
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i];
    if (
      ev.kind === "dispatch" &&
      ev.status === "searching" &&
      ev.dispatcher === match.dispatcher &&
      ev.query === match.query
    ) {
      return i;
    }
  }
  // 优先级 2：status='searching' AND dispatcher（query 宽容，LLM 可能
  // 在 tool_call 和 search.started 之间改写 query）
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i];
    if (
      ev.kind === "dispatch" &&
      ev.status === "searching" &&
      ev.dispatcher === match.dispatcher
    ) {
      return i;
    }
  }
  // 优先级 3：任何 dispatch + 同 dispatcher（兜底覆盖旧数据缺 status 字段）
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i];
    if (ev.kind === "dispatch" && ev.dispatcher === match.dispatcher) {
      return i;
    }
  }
  return -1;
}

const initialState = {
  session: null,
  agents: [],
  evidences: [],
  messages: [],
  beliefSnapshots: [],
  verdict: null,
  isLoading: false,
  pendingUserAction: null,
  cotTrail: {
    prosecutor: [],
    defender: [],
    investigator: [],
    clerk: [],
    judge: [],
  } as Record<AgentType, CotStep[]>,
  activeThinking: {
    prosecutor: undefined,
    defender: undefined,
    investigator: undefined,
    clerk: undefined,
    judge: undefined,
  } as Record<AgentType, { agentId: string; startedAt: string } | undefined>,
  activeInvestigation: null as CourtroomState["activeInvestigation"],
  streamingContent: null as CourtroomState["streamingContent"],
  investigationEvents: [] as InvestigationEvent[],
  memoryEntries: [] as MemoryEntry[],
  realCourthouseMode: false,
  beliefDiffs: [] as BeliefDiff[],
  convergenceInfo: null as CourtroomState["convergenceInfo"],
};

export const useCourtroomStore = create<CourtroomState>((set, get) => ({
  ...initialState,

  setSession: (session) => set({ session }),

  setAgents: (agents) => set({ agents }),

  updateAgentBelief: (agentType, beliefA, beliefB) =>
    set((state) => ({
      agents: state.agents.map((agent) =>
        agent.agent_type === agentType
          ? { ...agent, belief_a: beliefA, belief_b: beliefB }
          : agent,
      ),
    })),

  addEvidence: (evidence) =>
    set((state) => ({
      evidences: [...state.evidences, evidence],
    })),

  challengeEvidence: (evidenceId, reason) =>
    set((state) => ({
      evidences: state.evidences.map((e) =>
        e.evidence_id === evidenceId
          ? { ...e, status: "challenged" as const, challenge_reason: reason }
          : e,
      ),
    })),

  addMessage: (message) =>
    set((state) => ({
      messages: [...state.messages, message],
    })),

  addBeliefSnapshot: (snapshot) =>
    set((state) => ({
      beliefSnapshots: [...state.beliefSnapshots, snapshot],
    })),

  setPhase: (phase, round) =>
    set((state) => ({
      session: state.session
        ? {
            ...state.session,
            current_phase: phase,
            current_round: round ?? state.session.current_round,
          }
        : null,
    })),

  setVerdict: (verdict) => set({ verdict }),

  setLoading: (loading) => set({ isLoading: loading }),

  setPendingUserAction: (action) => set({ pendingUserAction: action }),

  appendCotStep: (agentType, step) =>
    set((state) => ({
      cotTrail: {
        ...state.cotTrail,
        [agentType]: [...(state.cotTrail[agentType] ?? []), step],
      },
    })),

  consumeCotTrail: (agentType) => {
    const trail = get().cotTrail[agentType] ?? [];
    set((state) => ({
      cotTrail: { ...state.cotTrail, [agentType]: [] },
    }));
    return trail;
  },

  setActiveThinking: (agentType, meta) =>
    set((state) => ({
      activeThinking: { ...state.activeThinking, [agentType]: meta },
    })),

  appendInvestigationEvent: (event) =>
    set((state) => ({
      investigationEvents: [...state.investigationEvents, event],
    })),

  completeInvestigationEvent: (match, patch) => {
    let matched = false;
    set((state) => ({
      investigationEvents: (() => {
        // 找最后一条 dispatch 行（index 最大的）。优先 status='searching'，
        // 其次 status 缺省的旧数据。dispatcher 必须严格匹配。
        const idx = findDispatchIndex(state.investigationEvents, match);
        if (idx < 0) return state.investigationEvents;
        matched = true;
        const next = state.investigationEvents.slice();
        next[idx] = { ...next[idx], ...patch, kind: "report" };
        return next;
      })(),
    }));
    return matched;
  },

  setActiveInvestigation: (info) => set({ activeInvestigation: info }),

  applySpeakChunk: (chunk) => {
    // 关键：React 18 automatic batching 会在 onmessage 等 native event
    // handler 里把多次 setState 合并到 1 次 commit,导致 LLM 流式 chunks
    // 看起来是"一次渲染"。这里用 flushSync 强制每次 setState 立即同步
    // commit,确保 191 个 chunks 真的逐字渲染。
    flushSync(() => {
      set(() => ({
        streamingContent: {
          agentId: chunk.agentId,
          agentType: chunk.agentType,
          accumulated: chunk.accumulated,
        },
      }));
    });
  },

  clearStreamingContent: () => set({ streamingContent: null }),

  /**
   * setInvestigationFindings hydrates the investigation feed from the
   * /investigations REST endpoint on first page load. Each historical
   * finding becomes a single "report" event so the live InvestigatorPanel
   * sees a continuous timeline. Dispatch events (which only live in the
   * websocket stream) are intentionally skipped — there's no historical
   * dispatch event to replay.
   */
  setInvestigationFindings: (findings) =>
    set(() => ({
      investigationEvents: findings.map((f) => ({
        id: `hist_${f.finding_uuid}`,
        kind: "report" as const,
        status: "completed" as const,
        dispatcher: f.dispatcher,
        query: f.query,
        findingId: f.finding_uuid,
        resultCount: f.result_count,
        summary: f.summary,
        rawResults: f.raw_results,
        createdAt: f.created_at,
      })),
    })),

  setMemoryEntries: (entries) =>
    set(() => ({
      memoryEntries: [...entries].sort(
        (a, b) =>
          a.round - b.round ||
          new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime(),
      ),
    })),

  appendMemoryEntry: (entry) =>
    set((state) => {
      // Idempotency: skip if we've already inserted this id (reconnect / replay).
      if (state.memoryEntries.some((e) => e.id === entry.id)) {
        return state;
      }
      const next = [...state.memoryEntries, entry];
      next.sort(
        (a, b) =>
          a.round - b.round ||
          new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime(),
      );
      return { memoryEntries: next };
    }),

  toggleRealCourthouseMode: () =>
    set((state) => ({ realCourthouseMode: !state.realCourthouseMode })),

  appendBeliefDiff: (diff) =>
    set((state) => {
      // Idempotency on id (reconnect / replay safety).
      if (state.beliefDiffs.some((d) => d.id === diff.id)) {
        return state;
      }
      const next = [...state.beliefDiffs, diff];
      next.sort(
        (a, b) =>
          a.round - b.round ||
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
      );
      return { beliefDiffs: next };
    }),

  setBeliefDiffs: (diffs) =>
    set(() => ({
      beliefDiffs: [...diffs].sort(
        (a, b) =>
          a.round - b.round ||
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
      ),
    })),

  setConvergenceInfo: (info) => set({ convergenceInfo: info }),

  reset: () => set(initialState),
}));

export function applyCourtEvent(event: CourtEvent) {
  const store = useCourtroomStore.getState();

  switch (event.type) {
    case "agent.speak": {
      const p = event.payload as {
        agent_id: string;
        agent_type: Agent["agent_type"];
        name: string;
        phase: CourtPhase;
        round: number;
        content: string;
        evidence_refs?: string[];
      };
      // 流式到 speak 结束：清掉 streamingContent，让 Avatar bubble 切到
      // 完整内容（来自 addMessage）。如果流式失败，这里也起到 fallback
      // 作用 —— 此时 content 已经是完整字符串。
      store.clearStreamingContent();
      const cotSteps = store.consumeCotTrail(p.agent_type);
      store.addMessage({
        id: `${p.agent_id}_${Date.now()}`,
        agent_id: p.agent_id,
        agent_type: p.agent_type,
        name: p.name,
        phase: p.phase,
        round: p.round,
        action_type: "speak",
        content: p.content,
        evidence_refs: p.evidence_refs ?? [],
        cot_steps: cotSteps.length > 0 ? cotSteps : undefined,
        created_at: event.timestamp,
      });
      break;
    }

    case "agent.cot_step": {
      const p = event.payload as {
        agent_id: string;
        agent_type: Agent["agent_type"];
        step: CotStep;
      };
      if (p.agent_type) {
        store.appendCotStep(p.agent_type, p.step);
        // 注意：tool_call/investigator_search 不在这里创建 dispatch entry，
        // 而是在 search.started 那里创建 —— 单一来源避免孤儿 searching。
      }
      break;
    }

    case "search.started": {
      const p = event.payload as {
        agent_id: string;
        dispatcher?: string;
        query: string;
      };
      const dispatcher = p.dispatcher ?? "investigator";
      // 单一来源：dispatch 行只在 search.started 创建 —— search.completed
      // 会升级这一条。如果之前的 searching dispatch 没匹配上 completed
      // （孤儿），本次新 dispatch 会进入事件列表。orphan 会在
      // settleOrphanDispatches 中被处理。
      store.appendInvestigationEvent({
        id: `dispatch_${event.timestamp}_${dispatcher}`,
        kind: "dispatch",
        status: "searching",
        dispatcher,
        query: p.query,
        createdAt: event.timestamp,
      });
      store.setActiveInvestigation({
        dispatcher,
        query: p.query,
        startedAt: event.timestamp,
      });
      break;
    }

    case "search.completed": {
      const p = event.payload as {
        agent_id: string;
        dispatcher?: string;
        query: string;
        result_count?: number;
        finding_id?: string;
        success?: boolean;
        raw_results?: string[];
        summary?: string;
      };
      store.setActiveInvestigation(null);
      // 升级唯一对应的 dispatch 行；找不到则 append 新 report（兜底）。
      const dispatcher = p.dispatcher ?? "investigator";
      const status =
        p.success === false ? ("failed" as const) : ("completed" as const);
      const patch = {
        status,
        findingId: p.finding_id,
        resultCount: p.result_count,
        rawResults: p.raw_results,
        summary: p.summary,
        createdAt: event.timestamp,
        query: p.query,
      };
      const upgraded = store.completeInvestigationEvent(
        { dispatcher, query: p.query },
        patch,
      );
      if (!upgraded) {
        store.appendInvestigationEvent({
          id: `completed_${p.finding_id ?? Date.now()}`,
          kind: "report",
          status,
          dispatcher,
          query: p.query,
          findingId: p.finding_id,
          resultCount: p.result_count,
          rawResults: p.raw_results,
          summary: p.summary,
          createdAt: event.timestamp,
        });
      }
      break;
    }

    case "agent.thinking_started": {
      const p = event.payload as {
        agent_id: string;
        agent_type: Agent["agent_type"];
      };
      if (p.agent_type) {
        store.setActiveThinking(p.agent_type, {
          agentId: p.agent_id,
          startedAt: event.timestamp,
        });
      }
      break;
    }

    case "agent.thinking_finished": {
      const p = event.payload as {
        agent_id: string;
        agent_type: Agent["agent_type"];
      };
      if (p.agent_type) {
        store.setActiveThinking(p.agent_type, undefined);
      }
      break;
    }

    case "agent.speak_chunk": {
      const p = event.payload as {
        agent_id: string;
        agent_type: Agent["agent_type"];
        chunk: string;
        accumulated: string;
      };
      store.applySpeakChunk({
        agentId: p.agent_id,
        agentType: p.agent_type,
        accumulated: p.accumulated,
      });
      break;
    }

    case "a2a.message": {
      // 注意：a2a.message 不再向 investigationEvents 追加 entry —— 由
      // search.started / search.completed 统一管理 dispatch + report 状态，
      // 保证一次搜索只产生一条 entry。a2a.message 仍由其他 listener 订阅
      // 用于其他用途（如证据链追溯）。
      //
      // v0.5 Episodic Memory：visibility=private + message_type ∈
      // {strategy_note, opponent_weakness, self_correction, evidence_eval}
      // 的消息会被 hydrate 成 MemoryEntry 并追加到 memoryEntries 时间线。
      // 其他 a2a.message（public 类）继续被忽略。
      //
      // v0.5 修复：后端 a2aBus.Send 广播 envelope 字段是 "from" / "to"
      // （见 backend/internal/a2a/bus.go#L146-L156），之前前端用
      // `p.from_agent` 取值永远是 undefined → 全部 entry 错配成
      // "prosecutor" + 失去防御能力 + 辩方笔记看不见。
      const p = event.payload as {
        id?: string;
        message_uuid?: string;
        round?: number;
        phase?: string;
        from?: string;
        to?: string;
        message_type?: string;
        visibility?: string;
        payload?: Record<string, unknown>;
        created_at?: string;
      };
      if (
        p.visibility === "private" &&
        p.message_type &&
        isMemoryKind(p.message_type)
      ) {
        const payload = p.payload ?? {};
        // 后端 from 字段是 agent_type 字符串（"prosecutor" / "defender"
        // / "investigator"），直接作为 agentType 落库；若是 agent UUID
        // （早期版本残留），落到 investigator 兜底。
        const fromAgentType = mapFromToAgentType(p.from);
        const entry: MemoryEntry = {
          id: p.id ?? p.message_uuid ?? `mem_${Date.now()}_${Math.random()}`,
          kind: p.message_type as MemoryKind,
          agentType: fromAgentType,
          round: p.round ?? 0,
          phase: p.phase ?? "cross_exam",
          content: typeof payload.content === "string" ? payload.content : "",
          linkedEvidenceIds: Array.isArray(payload.linked_evidence_ids)
            ? (payload.linked_evidence_ids as string[])
            : [],
          createdAt: p.created_at ?? event.timestamp,
          // === v0.5 结构化字段（strategy_note 类型才有） ===
          // 后端在 recordSideEffects 把 speaker.Stance / Confidence /
          // Reasoning 拆成独立字段，前端 MemoryTimeline 渲染成结构化卡片
          // （立场 chip / 置信度条 / 推理段）。如果 payload 没这些字段
          // （老 a2a 消息），fallback 到 undefined —— UI 走"纯文本"路径。
          stance:
            typeof payload.stance === "string" ? payload.stance : undefined,
          confidence:
            typeof payload.confidence === "number"
              ? payload.confidence
              : undefined,
          reasoning:
            typeof payload.reasoning === "string"
              ? payload.reasoning
              : undefined,
        };
        store.appendMemoryEntry(entry);
      }
      break;
    }

    case "evidence.added": {
      store.addEvidence(event.payload as unknown as Evidence);
      break;
    }

    case "evidence.challenged": {
      const p = event.payload as {
        evidence_id: string;
        reason: string;
      };
      store.challengeEvidence(p.evidence_id, p.reason);
      break;
    }

    case "belief.updated": {
      const p = event.payload as {
        agent_type: Agent["agent_type"];
        belief_a: number;
        belief_b: number;
        round: number;
        delta: number;
      };
      store.updateAgentBelief(p.agent_type, p.belief_a, p.belief_b);
      store.addBeliefSnapshot({
        agent_id: p.agent_type,
        agent_type: p.agent_type,
        round: p.round,
        belief_a: p.belief_a,
        belief_b: p.belief_b,
        delta: p.delta,
        trigger_event: "cross_exam",
      });
      break;
    }

    case "phase.changed": {
      const p = event.payload as {
        current_phase: CourtPhase;
        current_round?: number;
        message?: string;
      };
      store.setPhase(p.current_phase, p.current_round);
      store.addMessage({
        id: `system_${Date.now()}`,
        phase: p.current_phase,
        round: p.current_round ?? 0,
        action_type: "system",
        content: formatPhaseMessage(p.current_phase, p.current_round),
        created_at: event.timestamp,
      });
      break;
    }

    case "user.action.required": {
      const p = event.payload as {
        action: string;
        question_id?: string;
        question?: string;
        purpose?: string;
        skip_allowed?: boolean;
      };
      store.setPendingUserAction(p);
      break;
    }

    case "verdict.ready": {
      // Verdict is fetched separately and applied via setVerdict.
      break;
    }

    case "belief.diff": {
      const p = event.payload as {
        id: string;
        round: number;
        phase: string;
        agent_type: Agent["agent_type"];
        evidence_id?: string;
        source: "evidence" | "weaken" | "anchor_pull";
        direction: "supports_a" | "supports_b" | "neutral";
        prior_belief_a: number;
        posterior_belief_a: number;
        delta_belief_a: number;
        prior_logit: number;
        posterior_logit: number;
        evidence_weight: number;
        weaken_factor: number;
        reason: string;
        created_at: string;
      };
      store.appendBeliefDiff({
        id: p.id,
        round: p.round,
        phase: p.phase,
        agent_type: p.agent_type,
        evidence_id: p.evidence_id || undefined,
        source: p.source,
        direction: p.direction,
        prior_belief_a: p.prior_belief_a,
        posterior_belief_a: p.posterior_belief_a,
        delta_belief_a: p.delta_belief_a,
        prior_logit: p.prior_logit,
        posterior_logit: p.posterior_logit,
        evidence_weight: p.evidence_weight,
        weaken_factor: p.weaken_factor,
        reason: p.reason,
        created_at: p.created_at,
      });
      break;
    }

    case "belief.convergence": {
      const p = event.payload as {
        reason:
          | "reasoning_oscillation"
          | "consensus"
          | "belief_stable"
          | "max_rounds";
        round: number;
        converged: boolean;
        reason_message: string;
      };
      if (p.converged) {
        store.setConvergenceInfo({
          reason: p.reason,
          round: p.round,
          reason_message: p.reason_message,
          detectedAt: event.timestamp,
        });
      }
      break;
    }

    default:
      break;
  }
}

/**
 * isMemoryKind narrows an arbitrary string to the v0.5 MemoryKind union.
 * Used by the a2a.message handler to decide whether to hydrate a
 * MemoryEntry. Must stay in sync with backend
 * internal/a2a.MessageTypeStrategyNote / OpponentWeakness / SelfCorrection
 * / EvidenceEval string constants.
 */
function isMemoryKind(s: string): s is MemoryKind {
  return (
    s === "strategy_note" ||
    s === "opponent_weakness" ||
    s === "self_correction" ||
    s === "evidence_eval"
  );
}

/**
 * mapFromToAgentType normalizes the backend's a2a envelope `from` field
 * (agent_type 字符串或 agent UUID) into the v0.5 MemoryEntry.agentType
 * domain. v0.5 起后端一律发 agent_type 字符串；保留 UUID 兜底以防
 * 历史数据残留或老客户端混入。
 */
function mapFromToAgentType(
  from: string | undefined,
): MemoryEntry["agentType"] {
  if (!from) return "prosecutor";
  const v = from.toLowerCase();
  if (v === "prosecutor") return "prosecutor";
  if (v === "defender") return "defender";
  if (v === "investigator") return "investigator";
  if (v === "clerk") return "clerk";
  if (v === "judge") return "judge";
  // agent UUID 残留（v0.4 之前）—— 落到 investigator 兜底，不丢数据。
  return "investigator";
}

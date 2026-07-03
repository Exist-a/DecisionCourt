"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useCourtroomStore, applyCourtEvent } from "@/store/courtroomStore";
import { api } from "@/lib/api";
import { createCourtWebSocket, type CourtEventHandler } from "@/lib/websocket";
import type { Agent, EvidenceType, UserActionRequest } from "@/types";
import { usePhaseUI } from "@/hooks/usePhaseUI";
import { AgentAvatar } from "./AgentAvatar";
import { ArgumentMap } from "./ArgumentMap";
import { EvidenceBoard } from "./EvidenceBoard";
import { MessageHistory } from "./MessageHistory";
import { InvestigatorPanel } from "./InvestigatorPanel";
import { MemoryAuditPanel } from "./MemoryAuditPanel";
import { ConvergenceBadge } from "./ConvergenceBadge";
import { BeliefTrajectoryTab } from "./BeliefTrajectoryTab";
import { PhaseGuide } from "./PhaseGuide";
import { HelpPopover } from "./HelpPopover";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  Gavel,
  Send,
  Plus,
  MessagesSquare,
  Search as SearchIcon,
  Brain,
  Activity,
} from "lucide-react";

interface CourtroomSceneProps {
  sessionId: string;
}

function getCurrentSpeakerName(
  agents: Agent[],
  bubble: { agentId: string; content: string } | null,
) {
  if (!bubble) return null;
  const agent = agents.find((a) => a.agent_uuid === bubble.agentId);
  return agent?.name ?? "Agent";
}

export function CourtroomScene({ sessionId }: CourtroomSceneProps) {
  const router = useRouter();
  const {
    session,
    agents,
    evidences,
    messages,
    pendingUserAction,
    activeInvestigation,
    memoryEntries,
    realCourthouseMode,
    beliefDiffs,
    convergenceInfo,
    setSession,
    setAgents,
    addEvidence,
    addMessage,
    setPendingUserAction,
    setVerdict,
    toggleRealCourthouseMode,
    setBeliefDiffs,
  } = useCourtroomStore();

  const [ws, setWs] = useState<ReturnType<typeof createCourtWebSocket> | null>(
    null,
  );
  const [lastSpeakerId, setLastSpeakerId] = useState<string | null>(null);
  const [currentBubble, setCurrentBubble] = useState<{
    agentId: string;
    content: string;
  } | null>(null);

  const [inputValue, setInputValue] = useState("");
  const [answer, setAnswer] = useState("");
  const [startingTrial, setStartingTrial] = useState(false);
  const [verdictReady, setVerdictReady] = useState(false);
  const [waitingForNextRound, setWaitingForNextRound] = useState(false);
  const [nextRound, setNextRound] = useState(2);
  // 右侧面板 Tab：庭审记录 vs 调查活动 vs 策略笔记 vs 信念轨迹。默认庭审记录。
  // v0.5 新增"策略笔记" tab，渲染 A2A 私有消息流（详见 memory-a2a-redesign.md §PR 4）。
  // v0.6 新增"信念轨迹" tab，渲染 belief_diffs 时间线 + 收敛徽章。
  const [sidebarTab, setSidebarTab] = useState<
    "messages" | "investigator" | "memory" | "belief"
  >("messages");

  // Load initial data and connect WebSocket
  useEffect(() => {
    let mounted = true;

    async function load() {
      try {
        const sessionRes = await api.getSession(sessionId);
        if (!mounted) return;
        if (sessionRes.code === 0) {
          setSession(sessionRes.data);
        }
      } catch (err) {
        console.error("[Courtroom] failed to load session:", err);
      }

      try {
        const agentsRes = await api.getAgents(sessionId);
        if (!mounted) return;
        if (agentsRes.code === 0) {
          const agentsData = agentsRes.data as { agents?: Agent[] } | Agent[];
          const agents = Array.isArray(agentsData)
            ? agentsData
            : (agentsData.agents ?? []);
          setAgents(agents);
        }
      } catch (err) {
        console.error("[Courtroom] failed to load agents:", err);
      }

      try {
        const evidencesRes = await api.getEvidences(sessionId);
        if (!mounted) return;
        if (evidencesRes.code === 0) {
          evidencesRes.data.evidences.forEach((e) => addEvidence(e));
        }
      } catch (err) {
        console.error("[Courtroom] failed to load evidences:", err);
      }

      try {
        const messagesRes = await api.getMessages(sessionId);
        if (!mounted) return;
        if (messagesRes.code === 0) {
          messagesRes.data.messages.forEach((m) => addMessage(m));
        }
      } catch (err) {
        console.error("[Courtroom] failed to load messages:", err);
      }

      // 历史调查发现：失败时不阻断 UI（老版本没有这个端点）。
      try {
        const invRes = await api.getInvestigations(sessionId);
        if (!mounted) return;
        if (invRes.code === 0 && Array.isArray(invRes.data.findings)) {
          useCourtroomStore.getState().setInvestigationFindings(invRes.data.findings);
        }
      } catch (err) {
        console.warn("[Courtroom] failed to load investigations (endpoint may be missing):", err);
      }

      // v0.6 信念轨迹历史：失败时不阻断 UI（老版本没这个端点）。
      // 成功时把整张 belief_diffs 表塞进 store，让 reconnection 场景
      // 也能恢复完整时间线，不需要等新一轮 belief.diff 事件。
      try {
        const diffRes = await api.getBeliefDiffs(sessionId);
        if (!mounted) return;
        if (diffRes.code === 0 && Array.isArray(diffRes.data.diffs)) {
          setBeliefDiffs(diffRes.data.diffs);
        }
      } catch (err) {
        console.warn("[Courtroom] failed to load belief diffs (endpoint may be missing):", err);
      }

      // v0.5 情节记忆时间线：失败时不阻断 UI（老版本没这个端点）。
      // 成功时把每条 private memory 作为 `a2a.message` 事件回灌 store，
      // 复用 applyCourtEvent 的同一条 parser 路径 —— 不再单独写一份
      // 还原逻辑。这是 v0.8.3 修复"刷新后策略笔记 Tab 全空"的关键。
      try {
        const memRes = await api.getVisibleMemory(sessionId);
        if (!mounted) return;
        if (memRes.code === 0 && Array.isArray(memRes.data.memory)) {
          for (const row of memRes.data.memory) {
            applyCourtEvent({
              type: "a2a.message",
              payload: row,
              timestamp: row.created_at || new Date().toISOString(),
            });
          }
        }
      } catch (err) {
        console.warn("[Courtroom] failed to load memory (endpoint may be missing):", err);
      }
    }

    load();

    const socket = createCourtWebSocket(sessionId);
    setWs(socket);

    const handler: CourtEventHandler = (event) => {
      applyCourtEvent(event);

      // Phase transition (e.g. continue_cross_exam → cross_exam round N+1)
      // implies a new round has begun — clear the "waiting" UI state.
      if (event.type === "phase.changed") {
        setWaitingForNextRound(false);
      }

      if (event.type === "agent.speak") {
        const payload = event.payload as {
          agent_id: string;
          content: string;
        };
        setLastSpeakerId(payload.agent_id);
        setCurrentBubble({
          agentId: payload.agent_id,
          content: payload.content,
        });
        setTimeout(() => {
          setLastSpeakerId(null);
          setCurrentBubble(null);
        }, 5000);
      }

      if (event.type === "judge.belief_update") {
        const payload = event.payload as {
          agent_uuid: string;
          belief_a: number;
          belief_b: number;
          reasoning: string;
        };
        // Update judge agent in store - use getState() to get current agents
        const currentAgents = useCourtroomStore.getState().agents;
        const updatedAgents = currentAgents.map((a) =>
          a.agent_uuid === payload.agent_uuid
            ? { ...a, belief_a: payload.belief_a, belief_b: payload.belief_b }
            : a,
        );
        useCourtroomStore.getState().setAgents(updatedAgents);
      }

      if (event.type === "clerk.round_summary") {
        const payload = event.payload as {
          round: number;
          summary: string;
          agent_uuid: string;
        };
        // Display clerk summary as a system message
        addMessage({
          id: `clerk-summary-${Date.now()}`,
          agent_id: payload.agent_uuid,
          phase: session?.current_phase ?? "cross_exam",
          round: payload.round,
          content: `【书记员总结】${payload.summary}`,
          evidence_refs: [],
          action_type: "system",
          created_at: new Date().toISOString(),
        });
      }

      if (event.type === "verdict.ready") {
        setVerdictReady(true);
      }

      if (event.type === "round.waiting_for_user") {
        const p = event.payload as {
          current_round: number;
          next_round: number;
          max_rounds: number;
        };
        setWaitingForNextRound(true);
        setNextRound(p.next_round);
      }

      if (event.type === "opening.finished") {
        // Opening finished, wait for user to start cross_exam
        setWaitingForNextRound(true);
        setNextRound(1);
      }
    };

    socket.on("*", handler);

    return () => {
      mounted = false;
      socket.off("*", handler);
      socket.disconnect();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId]);

  const sendAction = (action: UserActionRequest) => {
    ws?.send(action);
  };

  const handleSendInput = () => {
    if (!inputValue.trim()) return;
    sendAction({
      action: "submit_evidence",
      content: inputValue.trim(),
      type: "fact",
    });
    setInputValue("");
  };

  const handleSubmitEvidence = (content: string, type: EvidenceType) => {
    sendAction({ action: "submit_evidence", content, type });
  };

  const handleAnswerQuestion = () => {
    if (!pendingUserAction) return;
    sendAction({
      action: "answer_question",
      question_id: pendingUserAction.question_id,
      answer,
    });
    setAnswer("");
    setPendingUserAction(null);
  };

  const handleSkipQuestion = () => {
    sendAction({ action: "skip_agent" });
    setPendingUserAction(null);
  };

  const handleViewVerdict = async () => {
    if (!verdictReady) return;
    const res = await api.getVerdict(sessionId);
    if (res.code === 0) {
      setVerdict(res.data);
      router.push(`/verdict/${sessionId}`);
    }
  };

  const prosecutor = agents.find((a) => a.agent_type === "prosecutor");
  const defender = agents.find((a) => a.agent_type === "defender");
  const investigator = agents.find((a) => a.agent_type === "investigator");
  const clerk = agents.find((a) => a.agent_type === "clerk");
  const judge = agents.find((a) => a.agent_type === "judge");

  const currentSpeakerName = getCurrentSpeakerName(agents, currentBubble);

  // 单一数据源：所有阶段相关的 UI 文案
  const phaseUI = usePhaseUI({
    phase: session?.current_phase ?? "idle",
    round: session?.current_round ?? 0,
    verdictReady,
    waitingForNextRound,
    nextRound,
    isAnyAgentSpeaking: !!currentSpeakerName,
    currentSpeakerName,
  });

  if (!session) {
    return (
      <div className="min-h-screen bg-white text-slate-800 flex items-center justify-center">
        <p className="text-slate-400">正在加载庭审…</p>
      </div>
    );
  }

  return (
    <div className="h-screen bg-paper text-ink flex flex-col overflow-hidden">
      {/* Header — 案卷封面 */}
      <header className="border-b border-rule bg-paperDeep sticky top-0 z-10">
        <div className="container mx-auto max-w-6xl px-6 py-4 flex items-center justify-between">
          <div className="flex items-baseline gap-4">
            {/* 案卷章 */}
            <span className="seal-stamp w-10 h-10 text-base leading-none flex items-center justify-center">
              判
            </span>
            <div>
              <h1 className="text-display text-xl font-semibold text-ink leading-tight">
                {session.title}
              </h1>
              <div className="flex items-baseline gap-3 mt-0.5">
                <span className="text-display text-sm text-prosecution-ink font-medium">
                  {session.option_a}
                </span>
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                  vs
                </span>
                <span className="text-display text-sm text-defense-ink font-medium">
                  {session.option_b}
                </span>
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data ml-2">
                  · {phaseUI.phaseLabel}
                  {phaseUI.roundLabel && ` · ${phaseUI.roundLabel}`}
                </span>
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2">
            {convergenceInfo && <ConvergenceBadge info={convergenceInfo} />}
            <HelpPopover />
            {/*
              v0.8.3 按钮逻辑修正：派生判断改用 session.current_phase
              （DB 真值），而不是 verdictReady 这个只在 WS 实时推送时
              才会被设置的本地 boolean —— 后者在 verdict 阶段刷新后会
              一直是 false，导致用户看到错误的"直接判决"按钮。

              phase 派生：
                - "idle"                      → 开 庭
                - "verdict" / "appeal"        → 查看判决書
                - 其余（opening..deliberation）→ 直接判决

              verdictReady 这个本地 boolean 仍然保留，只用来驱动 amber
              banner 的入场动画和自动跳转行为（避免用户错过判决到达）。
            */}
            {session.current_phase === "idle" ? (
              <Button
                size="sm"
                disabled={startingTrial}
                onClick={async () => {
                  setStartingTrial(true);
                  try {
                    const res = await api.startTrial(sessionId);
                    if (res.code === 0) {
                      const updated = await api.getSession(sessionId);
                      if (updated.code === 0) setSession(updated.data);
                    }
                  } catch (err) {
                    console.error("[Courtroom] failed to start trial:", err);
                    alert("启动庭审失败，请检查网络或后端日志");
                  } finally {
                    setStartingTrial(false);
                  }
                }}
                className="bg-ink text-paper hover:bg-inkSoft rounded-sm px-4 h-9 font-data tracking-wider"
              >
                <Gavel className="w-3.5 h-3.5 mr-1.5" />
                {startingTrial ? "启动中…" : "开 庭"}
              </Button>
            ) : session.current_phase === "verdict" ||
              session.current_phase === "appeal" ? (
              <Button
                size="sm"
                onClick={handleViewVerdict}
                className="bg-seal text-paper hover:bg-seal-ink rounded-sm px-4 h-9 font-data tracking-wider"
              >
                <Gavel className="w-3.5 h-3.5 mr-1.5" />
                查 看 判 决 書
              </Button>
            ) : (
              <Button
                size="sm"
                onClick={() => sendAction({ action: "direct_verdict" })}
                className="bg-ink text-paper hover:bg-inkSoft rounded-sm px-4 h-9 font-data tracking-wider"
              >
                <Gavel className="w-3.5 h-3.5 mr-1.5" />
                直 接 判 决
              </Button>
            )}
          </div>
        </div>
      </header>

      {/* Phase guide */}
      {phaseUI.showGuide && (
        <PhaseGuide text={phaseUI.guideText} tone={phaseUI.guideTone} />
      )}

      {/* Verdict ready banner */}
      {verdictReady && (
        <div className="bg-amber-50 border-y border-rule px-6 py-3">
          <div className="container mx-auto max-w-6xl flex items-center justify-between">
            <p className="text-display text-sm text-judge-ink">
              <span className="seal-stamp w-5 h-5 text-[10px] mr-2 inline-flex items-center justify-center align-middle">判</span>
              判决书已落印归档 · 点击下方按钮查阅最终结论
            </p>
            <Button
              size="sm"
              onClick={handleViewVerdict}
              className="bg-seal text-paper hover:bg-seal-ink rounded-sm px-4 h-9 font-data tracking-wider"
            >
              <Gavel className="w-3.5 h-3.5 mr-1.5" />
              查 看 判 决 書
            </Button>
          </div>
        </div>
      )}

      {/* Main content */}
      <div className="flex-1 container mx-auto max-w-6xl px-6 py-5 flex gap-5 overflow-hidden">
        {/* Courtroom scene */}
        <div className="flex-1 flex flex-col gap-5 min-w-0 overflow-y-auto">
          {/* Agent arena — 庭审中央 */}
          <div className="relative flex flex-col items-center justify-start py-5 bg-white border border-rule rounded-sm shadow-paper">
            {/* 案卷标题 */}
            <div className="absolute top-3 left-4 phase-ribbon z-10">
              庭审现场
            </div>

            {/* Speaker hint */}
            <div className="absolute top-3 right-4 text-[11px] font-data tracking-wider text-inkSoft uppercase">
              {currentSpeakerName ? (
                <span className="flex items-center gap-1.5 text-prosecution-ink">
                  <span className="w-1.5 h-1.5 rounded-full bg-seal" />
                  {currentSpeakerName} 正在陈述
                </span>
              ) : (
                phaseUI.speakerHint
              )}
            </div>

            {/* 当事人对照行（最醒目） */}
            <div className="w-full max-w-3xl px-4 mt-9 mb-1">
              <div className="flex items-center justify-center gap-6">
                <div className="flex-1 text-right">
                  <div className="text-[10px] uppercase tracking-[0.25em] text-prosecution-ink font-data mb-0.5">
                    控方主张
                  </div>
                  <div className="text-display text-lg font-semibold text-ink leading-tight">
                    {session.option_a}
                  </div>
                </div>
                <div className="text-display text-inkFaint text-2xl font-light px-3">
                  ⚖
                </div>
                <div className="flex-1 text-left">
                  <div className="text-[10px] uppercase tracking-[0.25em] text-defense-ink font-data mb-0.5">
                    辩方主张
                  </div>
                  <div className="text-display text-lg font-semibold text-ink leading-tight">
                    {session.option_b}
                  </div>
                </div>
              </div>
              {/* 细线分隔 */}
              <div className="border-t border-rule mt-2" />
            </div>

            <div className="w-full max-w-3xl flex flex-col items-center gap-3 px-4 pb-1">
              {/* Top center: judge */}
              {judge && (
                <AgentAvatar
                  agent={judge}
                  isSpeaking={lastSpeakerId === judge.agent_uuid}
                  bubble={
                    currentBubble?.agentId === judge.agent_uuid
                      ? currentBubble.content
                      : null
                  }
                  showMeter={true}
                  optionA={session.option_a}
                  optionB={session.option_b}
                />
              )}

              {/* Middle row: prosecutor - investigator/clerk - defender */}
              <div className="w-full grid grid-cols-3 items-start gap-4">
                {/* Left: prosecutor */}
                <div className="flex justify-center">
                  {prosecutor ? (
                    <AgentAvatar
                      agent={prosecutor}
                      isSpeaking={lastSpeakerId === prosecutor.agent_uuid}
                      bubble={
                        currentBubble?.agentId === prosecutor.agent_uuid
                          ? currentBubble.content
                          : null
                      }
                      optionA={session.option_a}
                    />
                  ) : null}
                </div>

                {/* Center: investigator + clerk (stacked) */}
                <div className="flex flex-col items-center justify-center gap-6">
                  {investigator ? (
                    <AgentAvatar
                      agent={investigator}
                      isSpeaking={lastSpeakerId === investigator.agent_uuid}
                      bubble={
                        currentBubble?.agentId === investigator.agent_uuid
                          ? currentBubble.content
                          : null
                      }
                      isSearching={!!activeInvestigation}
                      searchQuery={activeInvestigation?.query}
                    />
                  ) : null}
                  {clerk ? (
                    <AgentAvatar
                      agent={clerk}
                      isSpeaking={lastSpeakerId === clerk.agent_uuid}
                      bubble={
                        currentBubble?.agentId === clerk.agent_uuid
                          ? currentBubble.content
                          : null
                      }
                    />
                  ) : null}
                </div>

                {/* Right: defender */}
                <div className="flex justify-center">
                  {defender ? (
                    <AgentAvatar
                      agent={defender}
                      isSpeaking={lastSpeakerId === defender.agent_uuid}
                      bubble={
                        currentBubble?.agentId === defender.agent_uuid
                          ? currentBubble.content
                          : null
                      }
                      optionB={session.option_b}
                    />
                  ) : null}
                </div>
              </div>
            </div>

            {agents.length === 0 && (
              <p className="text-slate-400 text-sm mt-4">未加载到 Agent 数据</p>
            )}
          </div>

          {/* Visualization section */}
          <ArgumentMap
            optionA={session?.option_a ?? ""}
            optionB={session?.option_b ?? ""}
            agents={agents}
          />

          {/* Evidence board */}
          <EvidenceBoard
            evidences={evidences}
            onSubmit={handleSubmitEvidence}
          />
        </div>

        {/* Message history sidebar */}
        <div className="w-80 hidden lg:flex border border-rule rounded-sm overflow-hidden h-full flex-col bg-white shadow-paper">
          {/* 右侧面板 Tab 切换：庭审记录 / 调查活动 / 策略笔记（v0.5 新增） */}
          <div role="tablist" className="flex border-b border-rule bg-paperDeep">
            <button
              role="tab"
              aria-selected={sidebarTab === "messages"}
              onClick={() => setSidebarTab("messages")}
              className={`flex-1 px-3 py-2 text-[11px] font-data tracking-[0.15em] uppercase flex items-center justify-center gap-1.5 border-b-2 ${
                sidebarTab === "messages"
                  ? "border-seal text-ink"
                  : "border-transparent text-inkFaint hover:text-ink"
              }`}
              data-sidebar-tab="messages"
            >
              <MessagesSquare className="w-3.5 h-3.5" />
              庭审记录
            </button>
            <button
              role="tab"
              aria-selected={sidebarTab === "investigator"}
              onClick={() => setSidebarTab("investigator")}
              className={`flex-1 px-3 py-2 text-[11px] font-data tracking-[0.15em] uppercase flex items-center justify-center gap-1.5 border-b-2 ${
                sidebarTab === "investigator"
                  ? "border-seal text-ink"
                  : "border-transparent text-inkFaint hover:text-ink"
              }`}
              data-sidebar-tab="investigator"
            >
              <SearchIcon className="w-3.5 h-3.5" />
              调查活动
            </button>
            <button
              role="tab"
              aria-selected={sidebarTab === "memory"}
              onClick={() => setSidebarTab("memory")}
              className={`flex-1 px-3 py-2 text-[11px] font-data tracking-[0.15em] uppercase flex items-center justify-center gap-1.5 border-b-2 ${
                sidebarTab === "memory"
                  ? "border-seal text-ink"
                  : "border-transparent text-inkFaint hover:text-ink"
              }`}
              data-sidebar-tab="memory"
              data-testid="memory-tab-button"
            >
              <Brain className="w-3.5 h-3.5" />
              策略笔记
              {memoryEntries.length > 0 && (
                <span className="ml-0.5 px-1 rounded-sm bg-paper text-inkFaint text-[9px] font-data tracking-normal">
                  {memoryEntries.length}
                </span>
              )}
            </button>
            <button
              role="tab"
              aria-selected={sidebarTab === "belief"}
              onClick={() => setSidebarTab("belief")}
              className={`flex-1 px-3 py-2 text-[11px] font-data tracking-[0.15em] uppercase flex items-center justify-center gap-1.5 border-b-2 ${
                sidebarTab === "belief"
                  ? "border-seal text-ink"
                  : "border-transparent text-inkFaint hover:text-ink"
              }`}
              data-sidebar-tab="belief"
              data-testid="belief-tab-button"
            >
              <Activity className="w-3.5 h-3.5" />
              信念轨迹
              {beliefDiffs.length > 0 && (
                <span className="ml-0.5 px-1 rounded-sm bg-paper text-inkFaint text-[9px] font-data tracking-normal">
                  {beliefDiffs.length}
                </span>
              )}
            </button>
          </div>
          <div className="flex-1 min-h-0 overflow-y-auto">
            {sidebarTab === "messages" ? (
              <MessageHistory messages={messages} />
            ) : sidebarTab === "investigator" ? (
              <InvestigatorPanel />
            ) : sidebarTab === "memory" ? (
              <MemoryAuditPanel
                entries={memoryEntries}
                redactedMode={realCourthouseMode}
                onToggleRedacted={toggleRealCourthouseMode}
              />
            ) : (
              <BeliefTrajectoryTab
                diffs={beliefDiffs}
                convergenceInfo={convergenceInfo}
              />
            )}
          </div>
        </div>
      </div>

      {/* Bottom input bar */}
      <div className="border-t border-rule bg-paperDeep px-6 py-4">
        <div className="container mx-auto max-w-6xl">
          <div className="flex items-center gap-2">
            <div className="flex items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  const el = document.querySelector(
                    "[data-evidence-toggle]",
                  ) as HTMLButtonElement | null;
                  el?.click();
                }}
                className="h-10 rounded-sm border-rule text-ink hover:bg-paper hover:border-inkSoft px-3 font-data tracking-wider text-xs"
              >
                <Plus className="w-3.5 h-3.5 mr-1" />
                归 档 证 据
              </Button>
              {waitingForNextRound && (
                <Button
                  size="sm"
                  onClick={() => {
                    setWaitingForNextRound(false);
                    if (session.current_phase === "opening") {
                      sendAction({ action: "start_cross_exam" });
                    } else {
                      sendAction({ action: "continue_cross_exam" });
                    }
                  }}
                  className="h-10 rounded-sm bg-seal text-paper hover:bg-seal-ink px-4 font-data tracking-wider text-xs"
                >
                  <Gavel className="w-3.5 h-3.5 mr-1.5" />
                  {session.current_phase === "opening"
                    ? "开 始 质 证"
                    : `进 入 第 ${nextRound} 轮`}
                </Button>
              )}
            </div>

            <div className="flex-1 relative">
              <Input
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                placeholder={phaseUI.placeholder}
                disabled={phaseUI.inputDisabled}
                className="inset-input h-10 pr-12 text-sm font-body"
                onKeyDown={(e) => e.key === "Enter" && handleSendInput()}
              />
              <Button
                size="icon"
                onClick={handleSendInput}
                disabled={phaseUI.inputDisabled}
                className="absolute right-1 top-1/2 -translate-y-1/2 h-8 w-8 rounded-sm bg-ink hover:bg-inkSoft text-paper"
              >
                <Send className="w-3.5 h-3.5" />
              </Button>
            </div>
          </div>
        </div>
      </div>

      {/* User action dialog */}
      <Dialog
        open={!!pendingUserAction}
        onOpenChange={() => setPendingUserAction(null)}
      >
        <DialogContent className="bg-white border-slate-200 text-slate-800">
          <DialogHeader>
            <DialogTitle className="text-slate-900">调查员提问</DialogTitle>
            <DialogDescription className="text-slate-500">
              {pendingUserAction?.purpose}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 pt-4">
            <p className="text-slate-800">{pendingUserAction?.question}</p>
            <div className="space-y-2">
              <Label className="text-slate-600">你的回答</Label>
              <Textarea
                value={answer}
                onChange={(e) => setAnswer(e.target.value)}
                placeholder="请输入你的回答…"
                className="inset-input min-h-[80px] resize-none"
              />
            </div>
            <div className="flex gap-2 justify-end">
              {pendingUserAction?.skip_allowed && (
                <Button
                  variant="outline"
                  onClick={handleSkipQuestion}
                  className="border-slate-200 text-slate-600 rounded-xl"
                >
                  跳过
                </Button>
              )}
              <Button
                onClick={handleAnswerQuestion}
                className="bg-slate-900 hover:bg-slate-800 text-white rounded-xl"
              >
                提交回答
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

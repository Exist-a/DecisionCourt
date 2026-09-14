"use client";

// v2.1 O-3.5: TrialReplay Dialog 内容重设计 —— 从「开发者 LLM trace 调试器」升级为「庭审用户回访」。
//
// v1.0.4 PR-C2 原始设计:
//   - 标题「庭审回放 (v1.0.4)」 + 默认 Tab 「LLM Trace」
//   - 左边 trace 列表 + 右边 trace tree / 信念曲线 / 反驳链
//   - 受众: backend 开发者调试 LLM API 调用
//
// v2.1 O-3.5 重设计:
//   - 标题「庭审回访 (Trial Review)」
//   - 默认 Tab 「庭审时轴」 = 5 色盘 + phase 时间轴 + 该 phase 对话流 + 法官信念快照
//   - Tab「信念轨迹」 = 复用 BeliefDiffTimeline
//   - Tab「技术 trace（高级）」 = 保留旧 trace tree,加「（高级）」标识避免用户误入
//   - 移除外层 [200px trace 列表 + 1fr detail] 网格 — 对庭审用户无意义
//
// 设计意图 (V2.1-OPTIMIZATION-PLAN.md §Phase O-3.5):
//   - 用户能一眼看出「这拨人是谁 (5 色盘)」「事情分几个阶段 (phase timeline)」「说了什么 (对话流)」
//   - 法官信念进度让用户理解「为什么法官这样判」= 决策可解释性
//   - 技术 trace 保留 opt-in,不删不破
//
// 不破坏:
//   - data-testid="trial-replay-button" / data-testid="trial-replay-date" (e2e 钩子保留)
//   - useTraces / fetchTrace API 调用方式
//   - AgentTraceNode / BeliefDiffTimeline / RebuttalTraceNode 组件契约
//
// 数据源 (store 读取):
//   - messages: 庭审对话流 (CourtroomStore.messages)
//   - beliefDiffs: 法官信念变化快照 (CourtroomStore.beliefDiffs)
//   - agents: 5 色盘静态渲染 (CourtroomStore.agents)

import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  History,
  TrendingUp,
  Swords,
  ScrollText,
  ChevronRight,
  Scale,
} from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { AgentTraceNode } from "./AgentTraceNode";
import { BeliefDiffTimeline } from "./BeliefDiffTimeline";
import { RebuttalTraceNode } from "./RebuttalTraceNode";
import { DotAvatar } from "@/components/courtroom/avatars/DotAvatar";
import { useCourtroomStore } from "@/store/courtroomStore";
import type { CourtPhase, Message } from "@/types";
import type { Trace } from "@/types";
import { useTraces, fetchTrace } from "@/lib/trace";

interface TrialReplayProps {
  sessionUUID: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

function formatDuration(startIso: string, endIso: string): string {
  const ms = new Date(endIso).getTime() - new Date(startIso).getTime();
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function todayStr(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// v2.1 O-3.5: phase 中文标签 (庭审用户视角)。
const PHASE_LABEL: Record<CourtPhase, string> = {
  idle: "待开始",
  clarification: "澄清提问",
  option_generation: "选项生成",
  opening: "开庭陈述",
  evidence: "举证质证",
  cross_exam: "交叉质证",
  closing: "结案陈词",
  deliberation: "法官审议",
  verdict: "判决",
  appeal: "上诉",
};

// v2.1 O-3.5: 庭审时轴上展示的 phase 顺序 (用户面向的「庭审重大阶段」,
// 跳过 idle/clarification/option_generation/deliberation 这些「非庭审现场」内部阶段)。
const REVIEW_PHASES: CourtPhase[] = [
  "opening",
  "evidence",
  "cross_exam",
  "closing",
  "verdict",
];

// v2.1 O-3.5: 角色中文短标签 (色盘下的标识 + Tab 内对话气泡的角色 chip)。
const AGENT_LABEL: Record<string, string> = {
  prosecutor: "控方",
  defender: "辩方",
  judge: "法官",
  investigator: "调查员",
  clerk: "书记员",
};

export function TrialReplay({ sessionUUID, open, onOpenChange }: TrialReplayProps) {
  const [date, setDate] = useState<string>(todayStr());
  const { traces, loading } = useTraces(sessionUUID, date);
  const [selectedTraceID, setSelectedTraceID] = useState<string | null>(null);
  const [traceDetail, setTraceDetail] = useState<Trace | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  // v2.1 O-3.5: 庭审时轴状态 — 当前选中的 phase(默认最后一个已发生 phase)。
  const [reviewPhase, setReviewPhase] = useState<CourtPhase>("opening");

  // v2.1 O-3.5: 从 store 拉庭审对话流 + 信念变化 + 5 agents。
  const messages = useCourtroomStore((s) => s.messages);
  const beliefDiffs = useCourtroomStore((s) => s.beliefDiffs);
  const agents = useCourtroomStore((s) => s.agents);
  const session = useCourtroomStore((s) => s.session);

  // v2.1 O-3.5: 根据 messages 自动推断「已发生 phase 列表」。
  const occurredPhases = useMemo(() => {
    const set = new Set<CourtPhase>();
    for (const m of messages) {
      if (m.phase) set.add(m.phase);
    }
    // 按 REVIEW_PHASES 顺序过滤
    return REVIEW_PHASES.filter((p) => set.has(p));
  }, [messages]);

  // v2.1 O-3.5: 如果当前选中 phase 没发生过,自动跳到最后一个已发生 phase。
  useEffect(() => {
    if (occurredPhases.length === 0) return;
    if (!occurredPhases.includes(reviewPhase)) {
      setReviewPhase(occurredPhases[occurredPhases.length - 1]);
    }
  }, [occurredPhases, reviewPhase]);

  // v1.0-patch-2: 切日期后默认选中第一条 trace。
  useEffect(() => {
    if (selectedTraceID === null && traces.length > 0) {
      setSelectedTraceID(traces[0].trace_id);
    }
  }, [traces, selectedTraceID]);

  // v1.0-patch-2: 选中 trace 后 fetch 详情。
  useEffect(() => {
    if (!selectedTraceID) return;
    let cancelled = false;
    setDetailLoading(true);
    void fetchTrace(sessionUUID, selectedTraceID).then((t) => {
      if (cancelled) return;
      setTraceDetail(t);
      setDetailLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [sessionUUID, selectedTraceID]);

  function handleSelectTrace(traceID: string) {
    setSelectedTraceID(traceID);
    setTraceDetail(null);
  }

  // v2.1 O-3.5: 提取当前 phase 的对话流(只显示 speak / submit_evidence / search 这类
  // 用户能理解的 action_type;phase_change / system / option_generated 跳过)。
  const phaseMessages: Message[] = useMemo(() => {
    return messages.filter(
      (m) =>
        m.phase === reviewPhase &&
        (m.action_type === "speak" ||
          m.action_type === "submit_evidence" ||
          m.action_type === "ask_question" ||
          m.action_type === "search"),
    );
  }, [messages, reviewPhase]);

  // v2.1 O-3.5 F2 修: 当前 phase 末的法官信念快照。
  //
  // 原实现直接返回 beliefDiffs 最后一条, 不区分 phase, 导致 opening /
  // evidence / cross_exam / closing 几个 tab 都显示最终 belief — 误导用户
  // 以为法官在每个 phase 都重新评估过。
  //
  // 修复策略 (按 BeliefDiff.phase 字段精确过滤 + 双层 fallback):
  //   1. beliefDiffs 中按 phase === reviewPhase 过滤, 取最后一条;
  //   2. 该 phase 无 diff (例如跨 phase 的 belief 推到 closing 才落)
  //      → 取 reviewPhase 之前 (按 round 升序累加) 的最后一条;
  //   3. 都失败 → 全局最后一条 (兜底, 单 phase 庭审时所有 tab 显示同一
  //      belief 是正确的)。
  //
  // 不改后端, 不改 types, 纯前端计算。
  const phaseBelief = useMemo(() => {
    if (beliefDiffs.length === 0) return null;

    // 1. 同 phase 的 diff (BeliefDiff.phase 字段由后端写入)
    const samePhase = beliefDiffs.filter((d) => d.phase === reviewPhase);
    if (samePhase.length > 0) return samePhase[samePhase.length - 1];

    // 2. 该 phase 之前 rounds 的最后一条 (round < reviewPhase 内最大 round)
    const phaseMsgMaxRound = messages
      .filter((m) => m.phase === reviewPhase)
      .reduce((max, m) => (m.round > max ? m.round : max), -1);
    if (phaseMsgMaxRound >= 0) {
      const priorRounds = beliefDiffs.filter((d) => d.round <= phaseMsgMaxRound);
      if (priorRounds.length > 0) return priorRounds[priorRounds.length - 1];
    }

    // 3. 全局兜底
    return beliefDiffs[beliefDiffs.length - 1];
  }, [beliefDiffs, messages, reviewPhase]);

  // v2.1 O-3.5: 5 色盘按标准顺序渲染。
  const orderedAgents = useMemo(() => {
    const order: Array<"judge" | "prosecutor" | "investigator" | "clerk" | "defender"> = [
      "judge",
      "prosecutor",
      "investigator",
      "clerk",
      "defender",
    ];
    return order
      .map((t) => agents.find((a) => a.agent_type === t))
      .filter((a): a is NonNullable<typeof a> => !!a);
  }, [agents]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl max-h-[85vh] bg-paper border-rule overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="text-ink flex items-center gap-2">
            <History className="w-4 h-4 text-prosecution-ink" />
            庭审回访 (Trial Review)
          </DialogTitle>
          <DialogDescription className="text-inkSoft">
            按 phase 时间轴复现刚才的庭审 · Tab「技术 trace」是高级调试入口
          </DialogDescription>
        </DialogHeader>

        <Tabs
          defaultValue="review"
          className="flex-1 min-h-0 flex flex-col"
          data-testid="trial-replay-tabs"
        >
          <TabsList className="bg-paperDeep border border-rule">
            <TabsTrigger value="review" className="text-xs">
              <ScrollText className="w-3 h-3 mr-1" />
              庭审时轴
            </TabsTrigger>
            <TabsTrigger value="belief" className="text-xs">
              <TrendingUp className="w-3 h-3 mr-1" />
              信念轨迹
            </TabsTrigger>
            <TabsTrigger value="trace" className="text-xs">
              <Activity className="w-3 h-3 mr-1" />
              技术 trace（高级）
            </TabsTrigger>
            <TabsTrigger value="rebuttal" className="text-xs">
              <Swords className="w-3 h-3 mr-1" />
              反驳链
            </TabsTrigger>
          </TabsList>

          {/* v2.1 O-3.5: 庭审时轴 (默认 Tab) — 用户视角的核心 */}
          <TabsContent
            value="review"
            className="mt-3 flex-1 min-h-0 flex flex-col gap-3"
            data-testid="trial-replay-review-tab"
          >
            {/* 1. 在场者 5 色盘缩略图 */}
            <div className="border border-rule rounded-sm bg-white px-3 py-3">
              <div className="flex items-baseline justify-between mb-2">
                <h3 className="text-display text-sm font-semibold text-ink">
                  在场者
                </h3>
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                  Participants
                </span>
              </div>
              <div className="flex items-center justify-around gap-2 flex-wrap">
                {orderedAgents.length === 0 ? (
                  <div className="text-xs text-inkFaint py-2">
                    庭审尚未开始,无在场者
                  </div>
                ) : (
                  orderedAgents.map((agent) => (
                    <div
                      key={agent.agent_uuid}
                      className="flex flex-col items-center gap-1"
                      data-testid={`review-participant-${agent.agent_type}`}
                    >
                      <DotAvatar
                        agentType={agent.agent_type}
                        size={36}
                      />
                      <span className="text-[10px] font-data text-ink uppercase tracking-wider">
                        {AGENT_LABEL[agent.agent_type] ?? agent.agent_type}
                      </span>
                    </div>
                  ))
                )}
              </div>
            </div>

            {/* 2. phase 时间轴 */}
            <div className="border border-rule rounded-sm bg-white px-3 py-3">
              <div className="flex items-baseline justify-between mb-2">
                <h3 className="text-display text-sm font-semibold text-ink">
                  phase 时间轴
                </h3>
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                  Trial Phases
                </span>
              </div>
              {occurredPhases.length === 0 ? (
                <div className="text-xs text-inkFaint py-3 text-center">
                  庭审尚未进入实质阶段,无可回顾 phase
                </div>
              ) : (
                <div className="flex items-center gap-1 overflow-x-auto pb-1">
                  {REVIEW_PHASES.map((p, idx) => {
                    const occurred = occurredPhases.includes(p);
                    const isActive = p === reviewPhase;
                    const isLastInOccurred =
                      occurred &&
                      occurredPhases[occurredPhases.length - 1] === p;
                    return (
                      <div key={p} className="flex items-center gap-1 shrink-0">
                        <button
                          type="button"
                          disabled={!occurred}
                          onClick={() => occurred && setReviewPhase(p)}
                          data-testid={`review-phase-${p}`}
                          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-sm border text-xs font-data tracking-wider transition-colors ${
                            isActive
                              ? "border-seal bg-seal/5 text-ink"
                              : occurred
                                ? "border-rule bg-paper text-inkSoft hover:border-inkSoft hover:bg-paperDeep"
                                : "border-transparent text-inkFaint/50 cursor-not-allowed"
                          }`}
                        >
                          <span
                            className={`inline-block w-2 h-2 rounded-full ${
                              isLastInOccurred
                                ? "bg-gold animate-pulse"
                                : isActive
                                  ? "bg-seal"
                                  : occurred
                                    ? "bg-inkSoft"
                                    : "bg-inkFaint/40"
                            }`}
                          />
                          {PHASE_LABEL[p] ?? p}
                        </button>
                        {idx < REVIEW_PHASES.length - 1 && (
                          <ChevronRight className="w-3 h-3 text-inkFaint shrink-0" />
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            {/* 3. 该 phase 对话流 */}
            <div className="border border-rule rounded-sm bg-white px-3 py-3 flex-1 min-h-0 flex flex-col">
              <div className="flex items-baseline justify-between mb-2 shrink-0">
                <h3 className="text-display text-sm font-semibold text-ink">
                  {PHASE_LABEL[reviewPhase] ?? reviewPhase} · 对话流
                </h3>
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                  {phaseMessages.length} messages
                </span>
              </div>
              <ScrollArea className="flex-1 pr-2">
                {phaseMessages.length === 0 ? (
                  <div className="text-xs text-inkFaint py-3 text-center">
                    {occurredPhases.length === 0
                      ? "庭审尚未开始"
                      : `${PHASE_LABEL[reviewPhase]} 阶段没有对话内容`}
                  </div>
                ) : (
                  <div className="space-y-2.5">
                    {phaseMessages.map((msg) => (
                      <div
                        key={msg.id}
                        className="flex gap-2 items-start"
                        data-testid={`review-message-${msg.action_type}`}
                      >
                        <Badge
                          variant="outline"
                          className="shrink-0 text-[10px] px-1.5 py-0 h-5 font-data tracking-wider border-rule"
                        >
                          {AGENT_LABEL[msg.agent_type ?? ""] ??
                            msg.agent_type ??
                            "系统"}
                        </Badge>
                        <p className="text-xs text-ink leading-relaxed flex-1 min-w-0 whitespace-pre-wrap break-words">
                          {msg.content}
                        </p>
                      </div>
                    ))}
                  </div>
                )}
              </ScrollArea>
            </div>

            {/* 4. 当前 phase 末法官信念快照 (v2.1 O-3.5.1 fallback: 无数据时显示 50/50 + 提示) */}
            <div className="border border-rule rounded-sm bg-white px-3 py-3 shrink-0">
              <div className="flex items-baseline justify-between mb-2">
                <h3 className="text-display text-sm font-semibold text-ink flex items-center gap-1.5">
                  <Scale className="w-3.5 h-3.5 text-judge-ink" />
                  法官信念快照
                </h3>
                <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
                  {session?.option_a ?? "选项A"} · {session?.option_b ?? "选项B"}
                </span>
              </div>
              {phaseBelief ? (
                <BeliefBar
                  optionA={session?.option_a ?? "选项A"}
                  optionB={session?.option_b ?? "选项B"}
                  beliefA={phaseBelief.posterior_belief_a ?? 0.5}
                  beliefB={1 - (phaseBelief.posterior_belief_a ?? 0.5)}
                />
              ) : (
                <div className="space-y-1.5">
                  <BeliefBar
                    optionA={session?.option_a ?? "选项A"}
                    optionB={session?.option_b ?? "选项B"}
                    beliefA={0.5}
                    beliefB={0.5}
                  />
                  <p className="text-[11px] text-inkFaint text-center pt-1">
                    尚无信念变化记录 ·证据提交后此处会更新
                  </p>
                </div>
              )}
            </div>
          </TabsContent>

          {/* v2.1 O-3.5: 信念轨迹 = 复用 BeliefDiffTimeline */}
          <TabsContent value="belief" className="mt-3 flex-1 min-h-0">
            <ScrollArea className="h-full">
              <BeliefDiffTimeline sessionUUID={sessionUUID} />
            </ScrollArea>
          </TabsContent>

          {/* v2.1 O-3.5: 技术 trace (高级) = 保留旧 trace tree + 日期选择器,加「高级」标识 */}
          <TabsContent
            value="trace"
            className="mt-3 flex-1 min-h-0 flex flex-col gap-3"
          >
            <div className="flex items-center gap-2 text-xs text-inkSoft px-1">
              <label
                htmlFor="trial-replay-date"
                className="font-data tracking-wider"
              >
                日期:
              </label>
              <input
                id="trial-replay-date"
                type="date"
                value={date}
                onChange={(e) => {
                  setDate(e.target.value);
                  setSelectedTraceID(null);
                  setTraceDetail(null);
                }}
                data-testid="trial-replay-date"
                className="border border-rule rounded px-2 py-1 text-xs font-data bg-paper"
              />
              <button
                type="button"
                onClick={() => setDate(todayStr())}
                className="text-inkFaint hover:text-ink text-xs font-data underline"
              >
                今天
              </button>
              <span className="text-[10px] text-inkFaint ml-auto font-data tracking-wider">
                开发者调试入口
              </span>
            </div>
            <div className="grid grid-cols-[200px_1fr] gap-3 flex-1 min-h-0">
              <ScrollArea className="border border-rule rounded">
                <div className="p-2 space-y-1">
                  {loading && (
                    <div className="text-xs text-inkFaint p-2">加载 trace 列表...</div>
                  )}
                  {!loading && traces.length === 0 && (
                    <div className="p-3 space-y-2">
                      <div className="text-xs text-ink font-medium">
                        暂无 trace 数据
                      </div>
                      <div className="text-[11px] text-inkSoft leading-relaxed">
                        {date === todayStr()
                          ? "今天还没产生 trace。"
                          : `${date} 当天没 trace 日志。`}
                      </div>
                      <div className="text-[11px] text-inkSoft leading-relaxed">
                        可能原因: ① trial 在 mock 模式;② backend 日志归档滞后;
                        ③ dev 路径错配 (检查{" "}
                        <code className="text-inkFaint">
                          internal/agent_gateway/logs
                        </code>
                        )。
                      </div>
                    </div>
                  )}
                  {traces.map((t) => {
                    const isSelected = t.trace_id === selectedTraceID;
                    return (
                      <button
                        key={t.trace_id}
                        onClick={() => handleSelectTrace(t.trace_id)}
                        className={`w-full text-left p-2 rounded text-xs hover:bg-paperDeep ${
                          isSelected ? "bg-paperDeep" : ""
                        }`}
                      >
                        <div className="text-ink font-mono truncate">
                          {t.trace_id.slice(0, 12)}
                        </div>
                        <div className="text-inkFaint mt-1">
                          {t.runs.length} runs ·{" "}
                          {formatDuration(t.started_at, t.ended_at)}
                        </div>
                        {t.runs.some((r) => r.status === "error") && (
                          <Badge
                            variant="outline"
                            className="mt-1 text-prosecution-ink border-prosecution/40"
                          >
                            error
                          </Badge>
                        )}
                      </button>
                    );
                  })}
                </div>
              </ScrollArea>
              <div className="min-w-0">
                <ScrollArea className="h-[500px] pr-3">
                  {detailLoading && (
                    <div className="text-xs text-inkFaint">加载 trace 详情...</div>
                  )}
                  {!detailLoading && !traceDetail && (
                    <Card className="p-4 text-xs text-inkFaint border-rule">
                      选中左侧 trace 查看详情
                    </Card>
                  )}
                  {!detailLoading && traceDetail && (
                    <AgentTraceNode node={traceDetail.tree} />
                  )}
                </ScrollArea>
              </div>
            </div>
          </TabsContent>

          {/* v2.1 O-3.5: 反驳链 (保留) */}
          <TabsContent value="rebuttal" className="mt-3 flex-1 min-h-0">
            <ScrollArea className="h-full">
              <RebuttalTraceNode sessionUUID={sessionUUID} />
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

// v2.1 O-3.5: 法官信念进度条 (option A vs option B 比例)。
//   - 复用现有 tailwind tokens (prosecution / defense / judge / paper / rule)
//   - 接受 belief_a + belief_b 在 0-1 区间 (归一化后显示)
function BeliefBar({
  optionA,
  optionB,
  beliefA,
  beliefB,
}: {
  optionA: string;
  optionB: string;
  beliefA: number;
  beliefB: number;
}) {
  const total = beliefA + beliefB || 1;
  const ratioA = (beliefA / total) * 100;
  const ratioB = 100 - ratioA;
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between text-[11px] font-data">
        <span className="text-prosecution-ink truncate max-w-[40%]">
          {optionA}
          <span className="ml-2 text-prosecution font-semibold">
            {ratioA.toFixed(0)}%
          </span>
        </span>
        <span className="text-inkFaint">vs</span>
        <span className="text-defense-ink truncate max-w-[40%] text-right">
          <span className="mr-2 text-defense font-semibold">
            {ratioB.toFixed(0)}%
          </span>
          {optionB}
        </span>
      </div>
      <div className="h-2 rounded-full bg-rule overflow-hidden flex">
        <div
          className="bg-prosecution transition-all"
          style={{ width: `${ratioA}%` }}
          data-testid="review-belief-bar-a"
        />
        <div
          className="bg-defense transition-all"
          style={{ width: `${ratioB}%` }}
          data-testid="review-belief-bar-b"
        />
      </div>
    </div>
  );
}
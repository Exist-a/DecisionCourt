"use client";

import { useEffect, useState } from "react";
import type { Agent } from "@/types";
import { useCourtroomStore } from "@/store/courtroomStore";
import { JudgeBiasMeter } from "./JudgeBiasMeter";
import { Search, Cloud } from "lucide-react";

interface AgentAvatarProps {
  agent: Agent;
  isSpeaking?: boolean;
  bubble?: string | null;
  showMeter?: boolean;
  optionA?: string;
  optionB?: string;
  /** 调查员专属：是否正在执行调查。开启后头像周围显示
   *  spinner 环 + 「正在调查："<query>"」气泡。 */
  isSearching?: boolean;
  /** isSearching=true 时，调查内容的简短描述（截断后显示）。 */
  searchQuery?: string;
}

/**
 * 案卷·印章 风格：每个角色像「卷宗签条」
 * - 圆形头像：克制本色（绛红/深青/棕金/米褐）
 * - 名字：Noto Serif SC，中文有"判决书"的庄重感
 * - 无 ripple 动效（避免 AI 默认的"杂多动效"感）
 * - 唯一保留：发言时头像描边变为金色
 */
const agentConfig = {
  prosecutor: {
    color: "bg-prosecution",
    inkColor: "text-prosecution-ink",
    ring: "ring-prosecution-soft",
    label: (option?: string) => `${option || "选项A"}代表`,
    roleName: "控方",
    quote: "呈堂",
  },
  defender: {
    color: "bg-defense",
    inkColor: "text-defense-ink",
    ring: "ring-defense-soft",
    label: (option?: string) => `${option || "选项B"}代表`,
    roleName: "辩方",
    quote: "辩护",
  },
  investigator: {
    color: "bg-neutral",
    inkColor: "text-inkSoft",
    ring: "ring-neutral-soft",
    label: () => "调查员",
    roleName: "调查员",
    quote: "勘验",
  },
  clerk: {
    color: "bg-neutral",
    inkColor: "text-inkSoft",
    ring: "ring-neutral-soft",
    label: () => "书记员",
    roleName: "书记员",
    quote: "记录",
  },
  judge: {
    color: "bg-judge",
    inkColor: "text-judge-ink",
    ring: "ring-judge-soft",
    label: () => "法官",
    roleName: "法官",
    quote: "判决",
  },
};

export function AgentAvatar({
  agent,
  isSpeaking,
  bubble,
  showMeter,
  optionA,
  optionB,
  isSearching,
  searchQuery,
}: AgentAvatarProps) {
  const config = agentConfig[agent.agent_type] ?? {
    color: "bg-inkFaint",
    inkColor: "text-ink",
    ring: "ring-rule",
    label: () => agent.agent_type ?? "未知",
    roleName: "未知",
    quote: "",
  };

  const agentLabel =
    agent.agent_type === "prosecutor"
      ? config.label(optionA)
      : agent.agent_type === "defender"
        ? config.label(optionB)
        : config.label();

  // 订阅思考状态。ReAct 起步时 store.activeThinking[agent_type] 被设上，
  // 结束时清空。Avatar 自身根据这个状态渲染头像上方的「思考中」云朵框——
  // 与发言气泡、调查气泡共用同一个 absolute 锚点，避免三个气泡互相覆盖。
  const thinking = useCourtroomStore((s) =>
    agent.agent_type ? s.activeThinking[agent.agent_type] : undefined,
  );
  const thinkingStepCount = useCourtroomStore((s) =>
    agent.agent_type ? (s.cotTrail[agent.agent_type]?.length ?? 0) : 0,
  );

  // 订阅流式发言状态。store.streamingContent 由后端 LLM 流式（agent.speak_chunk
  // 事件）驱动：每个 LLM token 到达时后端会用正则从 partial JSON 提取 content
  // 字段值并推送，前端 Avatar bubble 实时显示。这就是「降低首字延迟」的核
  // 心 —— 第一个 token 到达（~200-500ms）时首字即出现，而不是等几秒完整
  // LLM 调用结束。
  const streamingContent = useCourtroomStore((s) => {
    const sc = s.streamingContent;
    if (!sc) return null;
    if (sc.agentType && agent.agent_type && sc.agentType !== agent.agent_type) {
      return null;
    }
    return sc;
  });
  const isStreamingThisAgent =
    !!streamingContent && streamingContent.accumulated.length > 0;

  // thinking 离开时延迟 220ms 卸载，让淡出动画跑完。
  const [showThinking, setShowThinking] = useState(!!thinking);
  useEffect(() => {
    if (thinking) {
      setShowThinking(true);
    } else {
      const t = setTimeout(() => setShowThinking(false), 220);
      return () => clearTimeout(t);
    }
  }, [thinking]);

  // 四种气泡的优先级：调查 > 流式打字 > 思考 > 完整发言（搜索是调查员专属）。
  // 关键：流式（正在开口说话）优先级高于思考（还在想）—— 律师开始说第一句话
  // 时就应该显示"打字中"内容，而不是继续显示"思考中"云朵。后端 speak 流式
  // 期间 thinking_finished 还没发出（发在 runner 结束后），如果这里不调整
  // 优先级，thinkingBubble 会覆盖 streamingBubble，用户看到的就是"云朵 + 光标"
  // 重叠的怪样子。
  const searchBubble =
    isSearching && searchQuery
      ? `正在调查："${searchQuery.length > 24 ? `${searchQuery.slice(0, 24)}…` : searchQuery}"`
      : null;

  const streamingBubble = isStreamingThisAgent ? streamingContent.accumulated : null;

  const thinkingBubble =
    !isStreamingThisAgent && showThinking
      ? thinkingStepCount > 0
        ? `思考中（${thinkingStepCount} 步）`
        : "思考中…"
      : null;

  const visibleBubble = searchBubble ?? streamingBubble ?? thinkingBubble ?? bubble;

  // 决定 bubble kind：搜索框 vs 思考云朵 vs 流式 vs 完整发言
  const bubbleKind = searchBubble
    ? "searching"
    : thinkingBubble
      ? "thinking"
      : isStreamingThisAgent
        ? "streaming"
        : "speak";

  return (
    <div className="flex flex-col items-center gap-1.5 px-3 py-1 relative">
      {/* 气泡（搜索 / 思考 / 流式 / 发言）共用同一个锚点 */}
      {visibleBubble && (
        <div
          className={`absolute bottom-full mb-2 left-1/2 -translate-x-1/2 w-60 max-h-40 overflow-y-auto z-50 ${
            bubbleKind === "thinking"
              ? "thinking-bubble"
              : bubbleKind === "searching"
                ? "bg-paperDeep border border-inkSoft shadow-paper-lg"
                : "speech-bubble-paper"
          }`}
          data-bubble-kind={bubbleKind}
          data-streaming={isStreamingThisAgent ? "true" : "false"}
        >
          <p
            className={`text-[11px] text-display italic leading-relaxed flex items-start gap-1.5 px-2 py-1 ${
              bubbleKind === "thinking" ? "text-inkSoft" : "text-inkSoft"
            }`}
          >
            {bubbleKind === "thinking" && (
              <Cloud
                className="cloud w-3.5 h-3.5 mt-0.5 shrink-0 text-prosecution-ink"
                aria-hidden
              />
            )}
            {bubbleKind === "searching" && (
              <Search className="w-3 h-3 mt-0.5 shrink-0 animate-pulse" />
            )}
            {/* 流式时不截断前 100 字符，让用户看到完整累积文本 + 末尾光标 */}
            <span>
              「
              {isStreamingThisAgent
                ? visibleBubble
                : visibleBubble.length > 100
                  ? `${visibleBubble.slice(0, 100)}…`
                  : visibleBubble}
              {isStreamingThisAgent && (
                <span
                  className="inline-block w-[2px] h-[1em] align-middle ml-0.5 bg-ink animate-pulse"
                  aria-hidden
                  data-cursor="true"
                />
              )}
              」
            </span>
          </p>
          {/* 气泡尾部小三角 */}
          <div
            className={`absolute -bottom-1.5 left-1/2 -translate-x-1/2 w-3 h-3 border-b border-r rotate-45 ${
              bubbleKind === "thinking"
                ? "bg-paperDeep border-inkSoft"
                : "bg-white border-rule"
            }`}
          />
        </div>
      )}

      {/* 圆形头像：克制本色（无 ripple 动效） */}
      <div className="relative">
        <div
          className={`w-12 h-12 rounded-full ${config.color} ring-2 ${config.ring} flex items-center justify-center transition-all duration-300 ${
            isSpeaking
              ? "ring-gold ring-4 scale-105 shadow-paper-lg"
              : isSearching
                ? "ring-inkSoft ring-4 scale-105"
                : showThinking
                  ? `ring-4 scale-105 thinking-${agent.agent_type ?? "defender"}`
                  : "ring-2"
          }`}
        >
          {/* 角色名首字（serif） */}
          <span className="text-white font-serif text-base leading-none">
            {config.roleName.charAt(0)}
          </span>
        </div>
        {/* 发言时：金色印章点 */}
        {isSpeaking && (
          <span className="absolute -top-1 -right-1 w-3 h-3 rounded-full bg-gold border-2 border-paper" />
        )}
        {/* 调查时：旋转 spinner 环 */}
        {isSearching && (
          <span
            className="absolute inset-0 rounded-full border-2 border-transparent border-t-inkSoft animate-spin pointer-events-none"
            aria-label="调查员正在调查"
            data-searching-spinner="true"
          />
        )}
      </div>

      {/* 名字 + 角色（serif） */}
      <div className="text-center min-w-[80px]">
        <p className="text-display text-[13px] font-semibold text-ink leading-tight">
          {agentLabel}
        </p>
        <p className="text-[9px] uppercase tracking-[0.15em] text-inkFaint mt-0.5 font-data">
          {isSearching ? (
            <span className="text-prosecution-ink">调查员 · 工作中</span>
          ) : showThinking ? (
            <span className="text-prosecution-ink">
              {config.roleName} · 思考中
            </span>
          ) : (
            agent.agent_type
          )}
        </p>
      </div>

      {/* 法官偏倚仪表 */}
      {showMeter && agent.agent_type === "judge" && (
        <JudgeBiasMeter
          beliefA={agent.belief_a}
          beliefB={agent.belief_b}
          optionA={optionA || "选项A"}
          optionB={optionB || "选项B"}
        />
      )}
    </div>
  );
}

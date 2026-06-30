"use client";

import { useRef, useEffect } from "react";
import { formatEvidenceID } from "@/lib/utils";
import type { Message } from "@/types";
import { CotStepsPanel } from "./CotStepsPanel";

interface MessageHistoryProps {
  messages: Message[];
}

/**
 * 案卷·印章 风格：消息像"庭审记录"
 * - 每个发言 = 一条带"签条"的案卷条目
 * - 内容用 serif（判决书感）
 * - 不用 01/02/03 编号，用「一、二、三」或角色前缀
 */
const agentTypeLabels: Record<string, string> = {
  prosecutor: "控方",
  defender: "辩方",
  investigator: "调查员",
  clerk: "书记员",
  judge: "法官",
};

const agentSignatures: Record<string, { color: string; ink: string }> = {
  prosecutor: { color: "#B53A2E", ink: "#7A1F18" },
  defender: { color: "#2C5470", ink: "#16334A" },
  investigator: { color: "#A89F8E", ink: "#5C564F" },
  clerk: { color: "#7A5C3F", ink: "#3F2E1E" },
  judge: { color: "#7A5C3F", ink: "#3F2E1E" },
};

const phaseLabels: Record<string, string> = {
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

const ROUNDED_PHASES = new Set(["cross_exam"]);

function getRoundLabel(phase: string, round: number): string | null {
  if (!ROUNDED_PHASES.has(phase)) return null;
  if (!round || round < 1) return null;
  return `第 ${round} 轮`;
}

function getPhasePrefix(phase: string): string {
  // 把"质证阶段"简化为"质证"——更克制
  return phaseLabels[phase]?.replace("阶段", "") ?? phase;
}

export function MessageHistory({ messages }: MessageHistoryProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages.length]);

  return (
    <div className="bg-paper h-full flex flex-col">
      {/* 案卷标题 */}
      <div className="px-4 py-3 border-b border-rule flex items-baseline justify-between">
        <h3 className="text-display text-base font-semibold text-ink">
          庭审记录
        </h3>
        <span className="text-[10px] uppercase tracking-[0.2em] text-inkFaint font-data">
          Trial Record
        </span>
      </div>

      <div ref={scrollRef} className="flex-1 overflow-y-auto px-4 py-4 space-y-3">
        {messages.length === 0 ? (
          <div className="text-center py-10 text-sm text-inkFaint">
            尚无庭审记录 · 等待第一轮发言
          </div>
        ) : (
          messages.map((msg) => {
            const sig =
              (msg.agent_type && agentSignatures[msg.agent_type]) ||
              agentSignatures.clerk;
            const roleLabel = msg.agent_type
              ? agentTypeLabels[msg.agent_type] ?? msg.agent_type
              : "系统";
            const phasePrefix = getPhasePrefix(msg.phase);
            const roundSuffix = getRoundLabel(msg.phase, msg.round);

            return (
              <article
                key={msg.id}
                className="relative pl-4 pr-3 py-2.5 bg-white border border-rule rounded-sm shadow-paper"
              >
                {/* 左侧角色签条 */}
                <span
                  className="absolute left-0 top-0 bottom-0 w-1 rounded-l-sm"
                  style={{ backgroundColor: sig.color }}
                />

                {/* 头部：角色 + 阶段 + 轮次 */}
                <header className="flex items-baseline justify-between mb-1.5">
                  <div className="flex items-baseline gap-2">
                    <span
                      className="text-display text-sm font-semibold"
                      style={{ color: sig.ink }}
                    >
                      {roleLabel}
                    </span>
                    <span className="text-[10px] uppercase tracking-wider text-inkFaint font-data">
                      {phasePrefix}
                      {roundSuffix && ` · ${roundSuffix}`}
                    </span>
                  </div>
                </header>

                {/* 内容（serif 引用体） */}
                <p className="text-[13px] text-ink leading-relaxed text-display">
                  {msg.action_type === "system" ? (
                    <span className="text-inkSoft italic">— {msg.content}</span>
                  ) : msg.action_type === "submit_evidence" ? (
                    <span className="text-prosecution-ink">
                      提交证据：{msg.content}
                    </span>
                  ) : (
                    <span>「{msg.content}」</span>
                  )}
                </p>

                {/* 引用证据 */}
                {msg.evidence_refs && msg.evidence_refs.length > 0 && (
                  <div className="mt-2 flex flex-wrap gap-1">
                    {msg.evidence_refs.map((ref) => (
                      <span
                        key={ref}
                        className="text-[10px] font-data px-1.5 py-0.5 bg-paperDeep border border-rule rounded-sm text-inkSoft"
                      >
                        引 {formatEvidenceID(ref)}
                      </span>
                    ))}
                  </div>
                )}

                {/* ReAct 推理链：仅当本条发言携带 cot_steps 时渲染 */}
                {msg.cot_steps && msg.cot_steps.length > 0 && (
                  <CotStepsPanel steps={msg.cot_steps} />
                )}
              </article>
            );
          })
        )}
      </div>
    </div>
  );
}

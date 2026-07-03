"use client";

import { useEffect, useState } from "react";
import { useCourtroomStore } from "@/store/courtroomStore";
import { CotStepsPanel } from "./CotStepsPanel";
import type { AgentType, CotStep } from "@/types";

/**
 * ThinkingBubble 是庭审页里「律师在思考」的视觉占位：
 *   - 收到 agent.thinking_started 时立即挂载（云朵呼吸动画）
 *   - 收到 agent.cot_step 时把当前 trail 内容追加显示
 *   - 收到 agent.thinking_finished 时整块淡出，由父组件换成正式发言卡
 *
 * 实现为浮在消息流上方的一行/一列云朵，跟正式发言气泡的「签条 + 印章」
 * 视觉系统错开，避免误以为这是已经定稿的发言。
 */
export function ThinkingBubble() {
  const activeThinking = useCourtroomStore((s) => s.activeThinking);
  const _cotTrail = useCourtroomStore((s) => s.cotTrail);
  const agents = [
    activeThinking.prosecutor && ("prosecutor" as AgentType),
    activeThinking.defender && ("defender" as AgentType),
  ].filter(Boolean) as AgentType[];

  if (agents.length === 0) return null;

  return (
    <div className="space-y-2">
      {agents.map((agentType) => (
        <Bubble key={agentType} agentType={agentType} />
      ))}
    </div>
  );
}

interface BubbleProps {
  agentType: AgentType;
}

function Bubble({ agentType }: BubbleProps) {
  const thinking = useCourtroomStore((s) => s.activeThinking[agentType]);
  const trail = useCourtroomStore((s) => s.cotTrail[agentType] ?? []);
  const [showSteps, setShowSteps] = useState(false);

  // 当 thinking 切换成 undefined（finished），延迟 200ms 隐藏，让淡出动画跑完
  const [mounted, setMounted] = useState(true);
  useEffect(() => {
    if (thinking) {
      setMounted(true);
      setShowSteps(false);
    } else {
      const t = setTimeout(() => setMounted(false), 220);
      return () => clearTimeout(t);
    }
  }, [thinking]);

  if (!mounted || !thinking) return null;

  const startedAtMs = Date.parse(thinking.startedAt);
  const elapsedMs = Number.isNaN(startedAtMs)
    ? 0
    : Math.max(0, Date.now() - startedAtMs);

  return (
    <article
      className={`relative pl-4 pr-3 py-2.5 bg-paperDeep border border-rule rounded-sm shadow-paper thinking-bubble thinking-${agentType}`}
      data-thinking={agentType}
    >
      <header className="flex items-baseline justify-between mb-1.5">
        <div className="flex items-baseline gap-2">
          <span className="text-display text-sm font-semibold text-inkSoft">
            {agentType === "prosecutor" ? "控方" : "辩方"}
          </span>
          <span className="text-[10px] uppercase tracking-wider text-inkFaint font-data">
            思考中 · {(elapsedMs / 1000).toFixed(1)}s
          </span>
        </div>
      </header>

      <div className="flex items-center gap-2">
        {/* 云朵占位：用 CSS 动画模拟呼吸 */}
        <span className="cloud" aria-hidden>
          ☁
        </span>
        <span className="text-[12px] text-inkSoft text-display italic">
          正在思考
          {trail.length > 0
            ? `（已完成 ${trail.length} 步推理）`
            : "…"}
        </span>
        {trail.length > 0 && (
          <button
            type="button"
            onClick={() => setShowSteps((v) => !v)}
            className="text-[10px] text-inkSoft hover:text-ink underline-offset-2 hover:underline ml-auto"
          >
            {showSteps ? "收起" : "展开"}
          </button>
        )}
      </div>

      {showSteps && trail.length > 0 && (
        <div className="mt-2">
          <CotStepsPanel steps={trail as CotStep[]} />
        </div>
      )}
    </article>
  );
}
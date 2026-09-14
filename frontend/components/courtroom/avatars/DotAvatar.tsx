"use client";

// v2.1 DotAvatar — 抛弃人型剪影,改用纯几何抽象元素 + 6 状态动效
//
// 设计要点 (V2.1-OPTIMIZATION-PLAN.md §Phase A):
//   - 沿用 v1 小球视觉: 48px 色盘 + 角色汉字 (控/辩/法/调/书) + ring
//   - 抽象装饰层 (halo/orbit/scan/confront/seal) 互斥叠加在色盘外
//   - 状态机复用 AgentAvatar → deriveAnimationState 优先级链
//   - data-* 钩子保留 (Playwright 回归测试需要)
//
// 装饰层 ↔ 状态映射:
//   - speaking        → 金色 halo 呼吸 (dot-speak-ring)
//   - thinking        → 3 颗伴星轨道 (dot-orbit)
//   - searching       → conic-gradient 扫描环 (dot-search-ring)
//   - judging         → 法官金印冲击波 (dot-judge-shock) — 一次性触发
//   - confronting     → 双色外环挤压 (dot-confront-active) — 仅控辩
//   - listening/idle  → 静态 (色盘微浮由 <AvatarAnimations> 提供)
//
// 与现有 <AvatarAnimations> 协作:
//   - framer-motion 驱动色盘 (scale/rotate/translate)
//   - CSS keyframes 驱动外部装饰 (因为 framer 不擅 multi-class 切换)
//
// 不引入任何新依赖;不使用图片;不使用 3D / WebGL / Lottie。

import type { AgentType } from "@/types";

interface DotAvatarProps {
  agentType: AgentType;
  isSpeaking?: boolean;
  isThinking?: boolean;
  isSearching?: boolean;
  size?: number;
}

const AGENT_COLOR: Record<AgentType, { bg: string; ring: string }> = {
  prosecutor: { bg: "bg-prosecution", ring: "ring-prosecution-soft" },
  defender: { bg: "bg-defense", ring: "ring-defense-soft" },
  investigator: { bg: "bg-neutral", ring: "ring-neutral-soft" },
  clerk: { bg: "bg-neutral", ring: "ring-neutral-soft" },
  judge: { bg: "bg-judge", ring: "ring-judge-soft" },
};

const ROLE_GLYPH: Record<AgentType, string> = {
  prosecutor: "控",
  defender: "辩",
  investigator: "调",
  clerk: "书",
  judge: "法",
};

export function DotAvatar({
  agentType,
  isSpeaking,
  isThinking,
  isSearching,
  size = 48,
}: DotAvatarProps) {
  const { bg, ring } = AGENT_COLOR[agentType];
  const glyph = ROLE_GLYPH[agentType];

  // 控制 confrontation 配色 — 控方红/辩方青,其他角色不对峙,显示中性外环
  const confrontColor =
    agentType === "prosecutor"
      ? "#B53A2E"
      : agentType === "defender"
        ? "#2C5470"
        : "rgba(26,24,21,0.15)";

  return (
    <div
      className="relative inline-flex items-center justify-center"
      style={{ width: size, height: size, ["--dot-confront-color" as string]: confrontColor }}
      data-role={agentType}
    >
      {/* 装饰层 ─ 在色盘外,逐状态互斥叠加 */}
      {isSpeaking && <span className="dot-speak-ring" data-speaking="true" aria-hidden />}
      {isThinking && (
        <span className="dot-orbit" data-thinking="true" aria-hidden>
          <span className="dot-orbit-sat dot-orbit-sat-1" />
          <span className="dot-orbit-sat dot-orbit-sat-2" />
          <span className="dot-orbit-sat dot-orbit-sat-3" />
        </span>
      )}
      {isSearching && <span className="dot-search-ring" data-searching="true" aria-hidden />}
      {/* v2.1 F3 (ADR 0035): judge 不主动发言, isJudging 状态层废弃。
         保留 .dot-judge-shock / .dot-judge-shock-active CSS class 以备未来启用。 */}
      {/* 法官静止也有金色静态 ring,见 §Phase A 设计意图 →「法官的庄重底」 */}
      {agentType === "judge" && (
        <span
          className="absolute inset-0 rounded-full"
          style={{
            boxShadow: "inset 0 0 0 1px rgba(212,162,76,0.35)",
          }}
          aria-hidden
        />
      )}

      {/* 色盘本体 ── 继承 v1 CircleAvatarFallback 视觉
          v2.1 Bug-2 修:显式 transform-origin: center + 显式 transform:
          "rotate(0deg)" 防 GPU compositor 错误给色盘加 rotateZ */}
      <div
        className={`rounded-full ${bg} ring-2 ${ring} flex items-center justify-center select-none relative z-10`}
        style={{
          width: size,
          height: size,
          transformOrigin: "center",
          transform: "rotate(0deg)",
          willChange: "transform",
        }}
      >
        <span
          className="text-white font-serif leading-none"
          style={{
            fontSize: size * 0.42,
            transformOrigin: "center",
            transform: "rotate(0deg)",
            display: "inline-block",
          }}
        >
          {glyph}
        </span>
      </div>
    </div>
  );
}

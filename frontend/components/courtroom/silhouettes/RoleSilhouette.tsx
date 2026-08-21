"use client";

// v2.0 RoleSilhouette — 包装 Silhouette,提供向后兼容 fallback
//
// 设计要点 (V2.0-PLAN.md §1.5):
//   - mode="silhouette" (默认) → 渲染 SVG 剪影
//   - mode="circle" → 回退到旧圆形头像 (env var NEXT_PUBLIC_USE_CIRCLE_AVATAR=true 触发)
//   - 包装组件隔离 fallback 逻辑,避免污染 AgentAvatar.tsx
//
// 接入:
//   - AgentAvatar 默认使用 Silhouette
//   - 老 v1.0.x 用户升级想看圆形头像: 设 NEXT_PUBLIC_USE_CIRCLE_AVATAR=true 重启
//   - 未来 v2.1+ 可加 mode="lottie" 升级

import type { AgentType } from "@/types";
import { Silhouette } from "./Silhouette";

interface RoleSilhouetteProps {
  agentType: AgentType;
  isSpeaking?: boolean;
  isThinking?: boolean;
  isSearching?: boolean;
  isJudging?: boolean;
  size?: number;
  mode?: "silhouette" | "circle";
}

export function RoleSilhouette({
  agentType,
  isSpeaking,
  isThinking,
  isSearching,
  isJudging,
  size = 48,
  mode = "silhouette",
}: RoleSilhouetteProps) {
  // 旧圆形头像 fallback — 仅当 NEXT_PUBLIC_USE_CIRCLE_AVATAR=true
  if (mode === "circle") {
    return <CircleAvatarFallback agentType={agentType} size={size} />;
  }

  return (
    <Silhouette
      agentType={agentType}
      isSpeaking={isSpeaking}
      isThinking={isThinking}
      isSearching={isSearching}
      isJudging={isJudging}
      size={size}
    />
  );
}

/**
 * 旧圆形头像 fallback — 仅 mode="circle" 时渲染。
 * 与 v1.0.x AgentAvatar 圆形 div 视觉一致 (复用 CSS class 名)。
 */
function CircleAvatarFallback({ agentType, size }: { agentType: AgentType; size: number }) {
  const bgColor =
    agentType === "prosecutor"
      ? "bg-prosecution"
      : agentType === "defender"
        ? "bg-defense"
        : agentType === "judge"
          ? "bg-judge"
          : "bg-neutral";

  const ringColor =
    agentType === "prosecutor"
      ? "ring-prosecution-soft"
      : agentType === "defender"
        ? "ring-defense-soft"
        : agentType === "judge"
          ? "ring-judge-soft"
          : "ring-neutral-soft";

  const roleName =
    agentType === "prosecutor"
      ? "控"
      : agentType === "defender"
        ? "辩"
        : agentType === "judge"
          ? "法"
          : agentType === "investigator"
            ? "调"
            : "书";

  return (
    <div
      className={`rounded-full ${bgColor} ring-2 ${ringColor} flex items-center justify-center`}
      style={{ width: size, height: size }}
      data-role={agentType}
      data-mode="circle-fallback"
    >
      <span className="text-white font-serif text-base leading-none">{roleName}</span>
    </div>
  );
}

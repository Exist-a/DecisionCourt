"use client";

// v2.0 RoleSilhouette — 包装 Silhouette/DotAvatar,提供向后兼容 fallback
//
// v2.1 演进 (V2.1-OPTIMIZATION-PLAN.md §Phase A):
//   - mode="dot" → v2.1 默认值,继承 v1 小球视觉 + 6 状态动效装饰层
//   - mode="silhouette" → v2.0 剪影小人 (保留 opt-in)
//   - mode="circle" → v1 静态色盘 fallback (NEXT_PUBLIC_USE_CIRCLE_AVATAR=true)
//
// 设计意图:
//   - 默认从 silhouette → dot (用户决定抛弃人型剪影)
//   - 不删 silhouette 代码,老用户可经 mode="silhouette" 回滚
//   - AgentAvatar 仍 import { RoleSilhouette },不在 AgentAvatar 里分支,避免污染

import type { AgentType } from "@/types";
import { Silhouette } from "./Silhouette";
import { DotAvatar } from "../avatars/DotAvatar";

type RoleMode = "silhouette" | "circle" | "dot";

interface RoleSilhouetteProps {
  agentType: AgentType;
  isSpeaking?: boolean;
  isThinking?: boolean;
  isSearching?: boolean;
  size?: number;
  mode?: RoleMode;
}

export function RoleSilhouette({
  agentType,
  isSpeaking,
  isThinking,
  isSearching,
  size = 48,
  mode = "dot",
}: RoleSilhouetteProps) {
  // v2.1: 抛弃人型剪影,改用纯抽象 (色盘 + 状态装饰层)
  if (mode === "dot") {
    return (
      <DotAvatar
        agentType={agentType}
        isSpeaking={isSpeaking}
        isThinking={isThinking}
        isSearching={isSearching}
        size={size}
      />
    );
  }

  // 旧圆形头像 fallback — 仅 NEXT_PUBLIC_USE_CIRCLE_AVATAR=true 时由 AgentAvatar 触发
  if (mode === "circle") {
    return <CircleAvatarFallback agentType={agentType} size={size} />;
  }

  // mode="silhouette" — v2.0 剪影小人 (保留 opt-in)
  return (
    <Silhouette
      agentType={agentType}
      isSpeaking={isSpeaking}
      isThinking={isThinking}
      isSearching={isSearching}
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

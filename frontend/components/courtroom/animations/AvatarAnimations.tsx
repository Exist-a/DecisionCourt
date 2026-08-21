"use client";

// v1.0.4 PR-C3: AvatarAnimations — 包装头像元素,根据 agent 状态选 motion variant
//
// 设计要点:
//   - 6 个状态 → 6 个 variants (speak / think / listen / search / judge / confront)
//   - 状态优先级: search > judge > speak > think > confront > listen (互斥)
//   - 调查员专属 search,法官专属 judge,其余角色 speak/think/listen
//   - confront 是庭审级别 (cross_exam 阶段),由 CourtroomScene 控制 enableConfront
//
// 复用:
//   - AgentAvatar 用 <AvatarAnimations> 包裹圆形头像 div
//   - state prop 由 AgentAvatar 根据 props.isSpeaking / isSearching / 等计算

import { motion } from "framer-motion";
import type { ReactNode } from "react";
import {
  speakVariant,
  thinkVariant,
  listenVariant,
  searchVariant,
  judgeVariant,
  confrontVariant,
} from "@/lib/animations/variants.ts";

export type AvatarAnimationState =
  | "speaking"
  | "thinking"
  | "listening"
  | "searching"
  | "judging"
  | "confronting"
  | "idle";

interface AvatarAnimationsProps {
  state: AvatarAnimationState;
  children: ReactNode;
  className?: string;
}

/**
 * 根据 agent 当前状态选 motion variant 包装 children。
 * 纯 wrapper,不接管内容渲染 — 内容由 AgentAvatar 决定。
 */
export function AvatarAnimations({ state, children, className }: AvatarAnimationsProps) {
  const variant =
    state === "speaking"
      ? speakVariant
      : state === "thinking"
        ? thinkVariant
        : state === "searching"
          ? searchVariant
          : state === "judging"
            ? judgeVariant
            : state === "confronting"
              ? confrontVariant
              : listenVariant; // listening + idle 都用 listenVariant (微弱 y 浮动)

  return (
    <motion.div
      className={className}
      variants={variant}
      initial="initial"
      animate={state}
    >
      {children}
    </motion.div>
  );
}

/**
 * 从 AgentAvatar props 计算动画状态。
 *
 * 优先级:
 *   1. isSearching (调查员专属) → "searching"
 *   2. isJudging (法官敲锤, 未来接入) → "judging"
 *   3. isSpeaking → "speaking"
 *   4. showThinking → "thinking"
 *   5. enableConfront (cross_exam 阶段) → "confronting"
 *   6. else → "listening"
 *
 * 设计: 调查/说话/思考是互斥动作 (同一时刻只能一个),与现有 CSS 动画一致。
 */
export function deriveAnimationState(opts: {
  isSpeaking?: boolean;
  isSearching?: boolean;
  isJudging?: boolean;
  showThinking?: boolean;
  enableConfront?: boolean;
}): AvatarAnimationState {
  if (opts.isSearching) return "searching";
  if (opts.isJudging) return "judging";
  if (opts.isSpeaking) return "speaking";
  if (opts.showThinking) return "thinking";
  if (opts.enableConfront) return "confronting";
  return "listening";
}

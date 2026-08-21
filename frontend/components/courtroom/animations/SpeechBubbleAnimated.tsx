"use client";

// v1.0.4 PR-C3: SpeechBubbleAnimated — AnimatePresence 包裹气泡
//
// 设计要点:
//   - 切换 bubble 时强制 unmount/mount (key=bubbleId)
//   - 0.2s 淡入 + 0.15s 淡出 (比 CSS transition 快)
//   - 与 AgentAvatar 现有 .speech-bubble-paper 样式 100% 兼容 (包裹而非替换)
//
// 复用:
//   - AgentAvatar 把现有气泡 div 用 <SpeechBubbleAnimated> 包裹
//   - bubbleId 用 agent_type + bubbleKind 拼接保证切换时 key 变

import { AnimatePresence, motion } from "framer-motion";
import type { ReactNode } from "react";
import { bubbleEnterExit } from "@/lib/animations/variants.ts";

interface SpeechBubbleAnimatedProps {
  bubbleId: string;
  visible: boolean;
  children: ReactNode;
  className?: string;
}

export function SpeechBubbleAnimated({
  bubbleId,
  visible,
  children,
  className,
}: SpeechBubbleAnimatedProps) {
  return (
    <AnimatePresence mode="wait">
      {visible && (
        <motion.div
          key={bubbleId}
          variants={bubbleEnterExit}
          initial="initial"
          animate="animate"
          exit="exit"
          className={className}
        >
          {children}
        </motion.div>
      )}
    </AnimatePresence>
  );
}

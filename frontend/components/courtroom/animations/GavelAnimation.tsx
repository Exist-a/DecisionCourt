"use client";

// v1.0.4 PR-C3: GavelAnimation — 法官敲锤专用动画
//
// 设计要点:
//   - 在 AgentAvatar 内嵌入,法官 isJudging=true 时触发
//   - key={triggerCount} 强制 AnimatePresence 重置动画 (每次 verdict 重新敲)
//   - 用 judgeVariant 4 关键帧 (y: 0 → -8 → 0 → 0 + rotate 0 → -8 → 8 → 0)
//
// 接入:
//   - AgentAvatar 在法官头像右上角加 <GavelAnimation triggerCount={verdictCount}>
//   - verdictCount 来自 store (每裁决一次 +1) — 当前 v1.0.4 PR-C3 不强接 store,
//     只暴露 triggerKey 由父组件传入

import { AnimatePresence, motion } from "framer-motion";
import { Gavel } from "lucide-react";
import { judgeVariant } from "@/lib/animations/variants.ts";

interface GavelAnimationProps {
  triggerKey: number | string;
  size?: number;
}

export function GavelAnimation({ triggerKey, size = 20 }: GavelAnimationProps) {
  return (
    <AnimatePresence mode="wait">
      <motion.span
        key={triggerKey}
        variants={judgeVariant}
        initial="initial"
        animate="judging"
        className="absolute -top-2 -right-2 text-amber-700 pointer-events-none"
        aria-label="法官敲锤"
        data-gavel-trigger={triggerKey}
      >
        <Gavel style={{ width: size, height: size }} />
      </motion.span>
    </AnimatePresence>
  );
}

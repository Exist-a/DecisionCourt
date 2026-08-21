"use client";

// v1.0.4 PR-C3: ThinkingDots — 思考中 3 点循环动画
//
// 设计要点:
//   - 比 CSS animate-pulse 更精细: 3 点按顺序上升
//   - 替代 AgentAvatar 现有 "思考中…" 文字末尾的静态 "…"
//   - 0.6s 一个循环,3 个点间隔 0.1s 启动
//
// 复用:
//   - SpeechBubbleAnimated 内的 thinking bubble 可选嵌入 ThinkingDots

import { motion } from "framer-motion";

const dotVariants = {
  initial: { y: 0, opacity: 0.4 },
  animate: { y: -4, opacity: 1 },
};

export function ThinkingDots() {
  return (
    <span className="inline-flex items-center gap-0.5 ml-1 align-middle" aria-label="思考中">
      {[0, 1, 2].map((i) => (
        <motion.span
          key={i}
          className="inline-block w-1 h-1 rounded-full bg-inkFaint"
          variants={dotVariants}
          initial="initial"
          animate="animate"
          transition={{
            duration: 0.6,
            repeat: Infinity,
            repeatType: "reverse",
            delay: i * 0.15,
            ease: "easeInOut",
          }}
        />
      ))}
    </span>
  );
}

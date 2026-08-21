// v1.0.4 PR-C3: 共用 motion variants
//
// 设计要点 (与 V1.0.4-PLAN.md §1.4 对齐):
//   - 6 个核心 variants: speak / think / listen / search / judge / confront
//   - 用 framer-motion Variants 类型 (label + keyframes)
//   - 命名遵循 agent 状态语义,不是 "出场/退场",而是 "在做什么"
//   - 与现有 CSS transition-all / animate-pulse 并存 (motion 接管"头像+气泡",
//     CSS 处理"spinner ring"等纯装饰元素,避免双动画冲突)
//
// 复用:
//   - AvatarAnimations.tsx 包裹头像,根据 isSpeaking/isThinking/isSearching/isJudging 选 variant
//   - SpeechBubbleAnimated.tsx 用 ease + opacity/scale (AnimatePresence)

import type { Variants } from "framer-motion";
import { EASE_SMOOTH, EASE_STANDARD } from "./easing.ts";

// 1. 律师 "说话中" — 点头 + 缩放 + 上下浮动
//   设计: 轻微 y 浮动模拟"嘴里在发声",scale 微增 (1 → 1.06) 给视觉重点
//   持续: 短 cycle 0.6s 重复,模拟说话节奏
export const speakVariant: Variants = {
  initial: { scale: 1, y: 0 },
  speaking: {
    scale: 1.06,
    y: [0, -3, 0],
    transition: {
      scale: { duration: 0.3, ease: EASE_SMOOTH },
      y: { duration: 0.6, repeat: Infinity, repeatType: "reverse", ease: "easeInOut" },
    },
  },
};

// 2. 律师 "思考中" — pulse + 微旋转 (旋转 < 2° 不刺眼)
//   设计: scale 1 ↔ 1.04 + rotate -1° ↔ 1°,模拟"犹豫/反复权衡"
//   持续: 2s 慢 cycle,比说话慢表示"在思考而非说话"
export const thinkVariant: Variants = {
  initial: { scale: 1, rotate: 0 },
  thinking: {
    scale: [1, 1.04, 1],
    rotate: [-1, 1, -1],
    transition: { duration: 2, repeat: Infinity, ease: "easeInOut" },
  },
};

// 3. 律师 "倾听中" — 静止但有 "点头回应"
//   设计: 仅 y 微浮,模拟"在听对方说,偶尔点头"
//   持续: 3s 极慢,表示专注
export const listenVariant: Variants = {
  initial: { y: 0 },
  listening: { y: [0, 2, 0], transition: { duration: 3, repeat: Infinity } },
};

// 4. 调查员 "调查中" — 旋转 + 放大镜微闪
//   设计: rotate -15° ↔ 15°,模拟"摇头搜索证据"
//   持续: 1.5s 中等 cycle
export const searchVariant: Variants = {
  initial: { rotate: 0 },
  searching: {
    rotate: [0, 15, -15, 0],
    transition: { duration: 1.5, repeat: Infinity, ease: EASE_STANDARD },
  },
};

// 5. 法官 "敲锤" — 关键帧动画
//   设计: 4 关键帧 (y=0 → y=-8 → y=0 → y=0) + (rotate 0 → -8 → 8 → 0)
//   模拟"举锤"-"落锤"-"复位"节奏
//   持续: 0.4s 单次,触发后由父组件控制重播
export const judgeVariant: Variants = {
  initial: { y: 0, rotate: 0 },
  judging: {
    y: [0, -8, 0, 0],
    rotate: [0, -8, 8, 0],
    transition: { duration: 0.4, times: [0, 0.3, 0.6, 1], ease: EASE_STANDARD },
  },
};

// 6. 控辩 "交锋" — 两人相对移动
//   设计: x 0 → 20 → 0,模拟"互相靠近-退回"
//   持续: 1.5s,repeatType=reverse 让动作更顺滑
//   注意: confront 状态由法庭层 (cross_exam 阶段) 控制,Avatar 接收 prop 后 animate="confronting"
export const confrontVariant: Variants = {
  initial: { x: 0 },
  confronting: {
    x: [0, 20, 0],
    transition: { duration: 1.5, repeat: Infinity, repeatType: "reverse" },
  },
};

// 7. 气泡出现 / 消失 (AnimatePresence 用)
export const bubbleEnterExit: Variants = {
  initial: { opacity: 0, y: 10, scale: 0.9 },
  animate: { opacity: 1, y: 0, scale: 1, transition: { duration: 0.2, ease: EASE_SMOOTH } },
  exit: { opacity: 0, y: -10, scale: 0.9, transition: { duration: 0.15, ease: EASE_SMOOTH } },
};

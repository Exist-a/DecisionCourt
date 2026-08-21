// v1.0.4 PR-C3: 共用 easing constants
//
// 设计要点:
//   - 集中管理 4 类常用 easing 曲线,避免散落在各个 variant 里硬编码
//   - 与 framer-motion 11.x 的 Easing 类型对齐 ([number, number, number, number] 或 string)
//   - 命名遵循 CSS easing 关键字 (linear / easeInOut) 但底层用 cubic-bezier 数组
//     (更精确,浏览器 GPU 加速更友好)

// cubic-bezier(0.4, 0, 0.2, 1) — Material Design "standard" 曲线
export const EASE_STANDARD: [number, number, number, number] = [0.4, 0, 0.2, 1];

// cubic-bezier(0.4, 0, 1, 1) — 加速曲线 (元素出场)
export const EASE_ACCELERATE: [number, number, number, number] = [0.4, 0, 1, 1];

// cubic-bezier(0, 0, 0.2, 1) — 减速曲线 (元素入场)
export const EASE_DECELERATE: [number, number, number, number] = [0, 0, 0.2, 1];

// cubic-bezier(0.4, 0, 0.6, 1) — 平滑曲线 (气泡 fade in/out)
export const EASE_SMOOTH: [number, number, number, number] = [0.4, 0, 0.6, 1];

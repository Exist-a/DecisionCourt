// v0.8.3 重连退避算法：纯函数，无副作用，可独立测试。
// 整个模块只导出两个常量 + 两个函数，websocket.ts 引用它们。
//
// 设计原则：
//   - 不依赖任何 runtime（无 React、无 WebSocket 引用），方便 node:test
//   - 行为可预测：固定种子下输出固定结果
//   - 算法显式，注释说明每一步为什么

// 指数退避参数
export const INITIAL_RETRY_DELAY_MS = 1_000;
export const MAX_RETRY_DELAY_MS = 30_000;

/**
 * computeBackoff returns the delay (ms) for the *next* reconnect attempt
 * given the current delay. Exponential: 1s → 2s → 4s → 8s → 16s → 30s
 * (capped). Once the cap is hit, every subsequent call returns the cap.
 *
 * The caller is responsible for:
 *   - Resetting the delay back to INITIAL_RETRY_DELAY_MS on successful connect.
 *   - Calling this function on each reconnect (NOT on every render).
 */
export function computeBackoff(currentDelayMs: number): number {
  return Math.min(Math.max(currentDelayMs, INITIAL_RETRY_DELAY_MS) * 2, MAX_RETRY_DELAY_MS);
}

/**
 * resetBackoff returns the initial delay to use after a successful
 * reconnect. This is just INITIAL_RETRY_DELAY_MS but having a function
 * makes the call-site self-documenting.
 */
export function resetBackoff(): number {
  return INITIAL_RETRY_DELAY_MS;
}

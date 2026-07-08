// v0.10 前端埋点 (ADR 0020):运行时单例
//
// 设计动机：
//   每个组件都不应该自己造 transport/createAnalytics,而是共享一个绑定当前
//   sessionUUID 的实例。这避免:
//     - 多个 transport 实例 → 多个 batch timer → 多份并发请求
//     - 重复装配 deps → 状态不一致(useMock / baseUrl 可能不同)
//
// 设计选择：模块级单例(不是 React Context),原因：
//   1. 埋点是横切关注点,不参与 React 渲染(不应该 re-render 触发 flush)
//   2. SSR 友好：模块加载顺序确定,window 检测更简单
//   3. sessionUUID 通过 setSessionUUID() 切换,组件不持有引用
//
// 切换 sessionUUID 时机：
//   - 用户从首页点进具体庭审 → setSessionUUID(newUuid)
//   - 用户重开庭审 → setSessionUUID(newUuid)
//   - 退出庭审回首页 → setSessionUUID("")  (空串触发后续 track 自动 noop)

import { createAnalytics, type Analytics } from "./index.ts";
import {
  createTransport,
  defaultDeps,
  type TransportConfig,
} from "../transport.ts";

interface RuntimeState {
  analytics: Analytics;
  transport: ReturnType<typeof createTransport>;
}

let state: RuntimeState | null = null;
let currentSessionUUID = "";

/**
 * initAnalytics 初始化（或重置）单例。
 * 每次切换 sessionUUID 时调用,绑新的 sessionUUID 到 analytics 单例。
 * 不传 sessionUUID 时,使用当前全局 sessionUUID（保持不变）。
 */
export function initAnalytics(sessionUUID?: string): Analytics {
  if (sessionUUID !== undefined) {
    currentSessionUUID = sessionUUID;
  }

  // 不重置 state —— transport / analytics 已经是稳定的依赖图。
  // 只更新 analytics 的 sessionUUID 通过 defaultDeps 重新构建。
  // 为简化实现,每次 init 都重新构造(避免实现 setSessionUUID 双向同步)。
  const config: TransportConfig = {
    // v0.10.1 修复:dev 模式下 baseUrl 必须读 NEXT_PUBLIC_API_URL,
    // 不写死 "/api"。原因:硬编码 "/api" 是相对路径,浏览器 fetch 时拼到当前
    // origin (localhost:3000 frontend),frontend 没有 /api/* 路由 → 404。
    //
    // 通过 env 读取的好处:
    //   - dev 模式 NEXT_PUBLIC_API_URL=http://localhost:8080 → 直连 backend ✓
    //   - prod 模式 NEXT_PUBLIC_API_URL=https://decisioncourt.cn → 经 nginx 反代 ✓
    //   - prod 同源部署 NEXT_PUBLIC_API_URL="" → /api/v1/... 相对路径,经前端反代 ✓
    // 这与 production Dockerfile 里的 NEXT_PUBLIC_API_URL 配置兼容,
    // 不影响真实项目上线时的运行。
    baseUrl: process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080",
    batchIntervalMs: 5000,
    maxQueueSize: 100,
    maxPayloadBytes: 32 * 1024,
  };
  const deps = defaultDeps(config.batchIntervalMs!);
  const transport = createTransport(config, deps);
  const analytics = createAnalytics({
    transport,
    sessionUUID: currentSessionUUID,
    useMock: deps.useMock,
    onWarn: deps.onWarn,
  });
  state = { analytics, transport };
  return analytics;
}

/**
 * getAnalytics 返回当前绑定的 analytics 实例。未 init 时 lazy init。
 * 调用方惯例：直接调 track() 等方法,不必关心是否 init。
 */
export function getAnalytics(): Analytics {
  if (!state) {
    return initAnalytics();
  }
  return state.analytics;
}

/**
 * getTransport 返回 transport,便于测试和 page unload 时手动 flush。
 */
export function getTransport(): ReturnType<typeof createTransport> | null {
  return state?.transport ?? null;
}

/**
 * flushNow 触发一次 flush,主要用于 page unload 期间。
 * 使用 sendBeacon 的替代方案:fetch + keepalive 已经能让请求在 unload 期间
 * 完成。SDK 主动调用 flush 确保 batch window 内的事件不丢失。
 */
export function flushNow(): Promise<void> {
  return state?.transport.flush() ?? Promise.resolve();
}

/**
 * _resetForTesting 仅供单元测试使用,重置模块状态。
 * 不能通过 setSessionUUID 单独切换：state 持有 transport 等可变状态，
 * 完整 reset 比单独字段更新更不容易出 bug。
 */
export function _resetForTesting(): void {
  state = null;
  currentSessionUUID = "";
}

/**
 * _currentSessionUUIDForTesting 仅供单元测试读取当前 sessionUUID。
 */
export function _currentSessionUUIDForTesting(): string {
  return currentSessionUUID;
}
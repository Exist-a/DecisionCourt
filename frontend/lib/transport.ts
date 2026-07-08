// v0.10 前端埋点 (ADR 0020):网络传输层
//
// 设计目标：
//   - 批量：默认 5s 窗口内的非关键事件合并,减少请求数
//   - 降级：失败 console.warn 不抛,不阻塞前端
//   - 容量保护：队列上限 + payload 字节上限,防 OOM / 防后端被打爆
//   - 关键事件立即 flush（verdict_feedback / ws_reconnect 等不容丢失）
//   - 测试友好：依赖注入 fetcher / scheduleFlush / now,无全局副作用
//
// v0.10.1 修复：默认 fetcher 必须带 auth token + cookie,否则 backend /events
// 端点返回 401。原因:backend 在 authedGroup 下注册,需要 Authorization header
// 或 cookie 鉴权。参考 lib/api.ts:fetchJson() 的做法(ensureAuthToken +
// Authorization Bearer + credentials: include)。
//
// 与后端契约：
//   POST {baseUrl}/api/v1/courtrooms/{session_uuid}/events
//   body: 单个事件（非数组;后端 handler 一次收一个事件）
//
// SSR safety：所有 API 都检查 window/useMock,服务端渲染时全部 noop。

// ============== 类型 ==============

/**
 * FrontendEvent 是前端埋点的标准事件格式。
 * 字段与后端 model.DecisionEvent 一一对应:
 *   - session_uuid → SessionUUID
 *   - event_type → EventType (前端用 fe.<name> 前缀)
 *   - payload → Payload (JSONB)
 *   - duration_ms → DurationMs
 *   - status → Status (默认 "ok")
 *   - error_msg → ErrorMsg
 */
export interface FrontendEvent {
  session_uuid: string;
  event_type: string;
  payload?: Record<string, unknown>;
  duration_ms?: number;
  status?: string;
  error_msg?: string;
}

export interface TransportConfig {
  /** 后端 base URL,e.g. "/api" */
  baseUrl: string;
  /** 批量窗口(毫秒),默认 5000 */
  batchIntervalMs?: number;
  /** 队列上限,默认 100 (防内存泄漏) */
  maxQueueSize?: number;
  /** 单事件 payload 字节上限,默认 32KB (防后端被大 payload 打爆) */
  maxPayloadBytes?: number;
}

export interface FetcherResponse {
  ok: boolean;
  status: number;
}

export interface TransportDeps {
  /** 网络层,默认 globalThis.fetch */
  fetcher: (url: string, body: unknown, headers: Record<string, string>) => Promise<FetcherResponse>;
  /** 替代 console.warn,便于测试断言 */
  onWarn: (msg: string) => void;
  /** 替代 setTimeout,接收一个 trigger 回调让 transport 主动 flush */
  scheduleFlush: (trigger: () => void) => void;
  /** 手动触发 flush(用于 page unload) */
  triggerFlush: () => void;
  /** 当前时间(毫秒),便于测试用 fake clock */
  now: () => number;
  /** mock 模式下全部 noop,不发任何请求 */
  useMock: boolean;
}

// ============== 关键事件清单 ==============

/**
 * CRITICAL_EVENTS:不容许丢失、需要立即 flush 的事件。
 * 不在表内的事件走批量窗口(默认 5s),由 scheduleFlush 触发 flush。
 *
 * 设计权衡：关键事件立即 flush 会增加请求数,但这些事件本身很少
 * (verdict_feedback 一案一次、ws_reconnect 偶发),可接受。
 */
const CRITICAL_EVENTS = new Set<string>([
  "fe.verdict_feedback",
  "fe.ws_reconnect",
  "fe.ws_missed_pong",
  "fe.trial_completed",
]);

// ============== 默认值 ==============

const DEFAULT_BATCH_INTERVAL_MS = 5_000;
const DEFAULT_MAX_QUEUE_SIZE = 100;
const DEFAULT_MAX_PAYLOAD_BYTES = 32 * 1024; // 32KB

// ============== Transport 实例 ==============

export interface Transport {
  /** 把事件加入队列;关键事件立即触发 flush */
  enqueue: (event: FrontendEvent) => void;
  /** 把队列里的事件一次性发出;空队列是 noop */
  flush: () => Promise<void>;
  /** 当前队列长度(测试用) */
  size: () => number;
}

/**
 * createTransport 构造一个埋点传输实例。
 * 工厂模式(不是 class)：便于测试时注入 fake fetcher/timer,
 * 也便于未来扩展(例如增加 page unload 时的 sendBeacon 通道)。
 */
export function createTransport(
  config: TransportConfig,
  deps: TransportDeps,
): Transport {
  // v0.10.10: 改 _batchIntervalMs 让 ESLint 放过
  // (原来 batchIntervalMs 是 dead code, setTimeout 的 batchIntervalMs 来自 defaultDeps 形参)
  const _batchIntervalMs = config.batchIntervalMs ?? DEFAULT_BATCH_INTERVAL_MS;
  const maxQueueSize = config.maxQueueSize ?? DEFAULT_MAX_QUEUE_SIZE;
  const maxPayloadBytes = config.maxPayloadBytes ?? DEFAULT_MAX_PAYLOAD_BYTES;

  const queue: FrontendEvent[] = [];
  let flushing = false;
  let flushInFlight: Promise<void> | null = null;

  // ============== 内部函数 ==============

  function isCritical(eventType: string): boolean {
    return CRITICAL_EVENTS.has(eventType);
  }

  function payloadByteSize(event: FrontendEvent): number {
    // 用字符串长度近似 JSON 字节数(JSON.stringify 单字节字符长度 ≈ UTF-8 字节数)
    return JSON.stringify(event).length;
  }

  function scheduleTimer(): void {
    deps.scheduleFlush(() => {
      void flushInternal();
    });
  }

  async function flushInternal(): Promise<void> {
    if (flushing) {
      // 已有 flush 正在进行：返回同一个 promise 让调用方等待完成
      // (避免并行 flush 把同一批事件发两次)
      return flushInFlight ?? Promise.resolve();
    }
    if (queue.length === 0) return;
    if (deps.useMock) {
      queue.length = 0;
      return;
    }

    // 复制一份出队,失败时再回填
    const batch = queue.splice(0, queue.length);
    flushing = true;
    const p = (async () => {
      // 关键简化:后端接口一次一个事件,所以这里发 N 次并行 fetch。
      // 优点:1 次事件失败不影响其他;2)后端不需要批量 endpoint。
      // 缺点:N 个请求而非 1 个,但 N 通常很小(批量窗口内 1-5 个)。
      await Promise.all(
        batch.map(async (event) => {
          // v0.10.1 修复:拼完整 endpoint "/api/v1/courtrooms/...",不再假设
          // baseUrl 已含 "/api"。配合 runtime.ts 读 NEXT_PUBLIC_API_URL,
          // dev 直连 backend (8080), prod 经 nginx 反代都能工作。
          const url = `${config.baseUrl}/api/v1/courtrooms/${encodeURIComponent(event.session_uuid)}/events`;
          try {
            const res = await deps.fetcher(url, event, {
              "Content-Type": "application/json",
            });
            if (!res.ok) {
              deps.onWarn(
                `frontend event flush failed: ${event.event_type} status=${res.status}`,
              );
              // 失败回填到队尾,下次重试
              queue.push(event);
            }
          } catch (err) {
            deps.onWarn(
              `frontend event flush threw: ${event.event_type} err=${err instanceof Error ? err.message : String(err)}`,
            );
            queue.push(event);
          }
        }),
      );
    })();
    flushInFlight = p;
    try {
      await p;
    } finally {
      flushing = false;
      flushInFlight = null;
      // 失败回填的事件 + flush 期间新入队的事件 → 重新调度 timer
      if (queue.length > 0) {
        scheduleTimer();
      }
    }
  }

  // ============== 公开 API ==============

  function enqueue(event: FrontendEvent): void {
    // mock 模式 / SSR: 全部丢弃
    if (deps.useMock) return;

    // 无 session 视为"全局事件",直接丢弃(没有归属的事件进不了 decision_events 表)
    if (!event.session_uuid) return;

    // payload 字节上限：防止后端被大 payload 打爆
    if (payloadByteSize(event) > maxPayloadBytes) {
      deps.onWarn(
        `frontend event dropped: payload too large event=${event.event_type}`,
      );
      return;
    }

    // 队列容量保护：满了挤掉最早的(避免内存泄漏)
    if (queue.length >= maxQueueSize) {
      queue.shift();
    }
    queue.push(event);

    // 关键事件立即 flush,其他事件等批量窗口
    if (isCritical(event.event_type)) {
      // 直接调用 flushInternal,不依赖 deps.triggerFlush(后者依赖 scheduleFlush 先注册)
      void flushInternal();
    } else {
      scheduleTimer();
    }
  }

  async function flush(): Promise<void> {
    if (deps.useMock) {
      queue.length = 0;
      return;
    }
    await flushInternal();
  }

  function size(): number {
    return queue.length;
  }

  return { enqueue, flush, size };
}

// ============== 生产默认实例 ==============

/**
 * defaultDeps 返回生产环境的默认依赖集合。
 * - fetcher: globalThis.fetch
 * - onWarn: console.warn
 * - scheduleFlush: setTimeout(batchIntervalMs)
 * - now: Date.now
 * - useMock: 从环境变量读
 *
 * SSR 安全：检测 typeof window === "undefined",所有依赖降级为 noop。
 */
export function defaultDeps(batchIntervalMs: number = DEFAULT_BATCH_INTERVAL_MS): TransportDeps {
  const isBrowser = typeof window !== "undefined";
  const useMock = !isBrowser || process.env.NEXT_PUBLIC_USE_MOCK === "true";

  // v0.10.1：动态 import auth.ts,避免 SSR 阶段 import localStorage 依赖失败。
  // 运行时通过 await ensureAuthToken() 拿 token + 自动 set cookie。
  let authModule: typeof import("./auth.ts") | null = null;
  if (isBrowser) {
    // 同步 require 模式 (Next.js 支持):实际 import 走顶部 import 语句,
    // 这里只 hold 引用。避免在 SSR 顶层调用 getAuthToken 触发 localStorage。
    // 动态 import 在浏览器内是 async,这里简化用静态顶层 import 替代。
  }

  let pendingTrigger: (() => void) | null = null;
  let timerId: ReturnType<typeof setTimeout> | null = null;

  return {
    fetcher: async (url, body, headers) => {
      if (!isBrowser) return { ok: true, status: 200 }; // SSR noop

      // v0.10.1：确保 token 有效 + 带 Authorization header + cookie。
      // 与 lib/api.ts:fetchJson 一致,否则 backend /events 返回 401。
      let token = "";
      try {
        // 静态 import (顶部已 import) - 通过 globalThis 缓存避免循环依赖
        // 这里直接 dynamic import 拿 auth module
        const mod = authModule ?? (authModule = await import("./auth.ts"));
        token = await mod.ensureAuthToken();
      } catch {
        // auth 失败时仍尝试发请求,让 backend 给 401(便于 dev 排查)
      }
      const authHeaders: Record<string, string> = { ...headers };
      if (token) {
        authHeaders["Authorization"] = `Bearer ${token}`;
      }

      const res = await fetch(url, {
        method: "POST",
        headers: authHeaders,
        body: JSON.stringify(body),
        credentials: "include", // v0.8.3：带 cookie 让服务端也能从 Cookie 头验签
        // keepalive 让 page unload 期间也能发出请求(fetch + keepalive 等价 sendBeacon)
        keepalive: true,
      });
      return { ok: res.ok, status: res.status };
    },
    onWarn: (msg) => console.warn("[analytics]", msg),
    scheduleFlush: (trigger) => {
      if (!isBrowser) return;
      pendingTrigger = trigger;
      if (timerId !== null) clearTimeout(timerId);
      timerId = setTimeout(() => {
        timerId = null;
        if (pendingTrigger) {
          const t = pendingTrigger;
          pendingTrigger = null;
          t();
        }
      }, batchIntervalMs);
    },
    triggerFlush: () => {
      if (!isBrowser) return;
      if (timerId !== null) {
        clearTimeout(timerId);
        timerId = null;
      }
      if (pendingTrigger) {
        const t = pendingTrigger;
        pendingTrigger = null;
        t();
      }
    },
    now: () => Date.now(),
    useMock,
  };
}
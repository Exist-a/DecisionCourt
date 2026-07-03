import type { CourtEvent, UserActionRequest } from "@/types";
import { getMockWebSocket } from "./mock/mockWebSocket";
import { computeBackoff, resetBackoff, INITIAL_RETRY_DELAY_MS, MAX_RETRY_DELAY_MS } from "./reconnect";

export type CourtEventHandler = (event: CourtEvent) => void;

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

// 心跳：每 25s 一次（早于 nginx/ALB 默认的 60s idle 超时），让中间代理
// 不会把连接当成 idle 杀掉。服务端会在收到 {type:"ping"} 时立即回 pong。
const HEARTBEAT_INTERVAL_MS = 25_000;

export class CourtWebSocket {
  private socket: ReturnType<typeof getMockWebSocket> | null = null;
  private realSocket: WebSocket | null = null;
  private handlers: Map<string, CourtEventHandler[]> = new Map();
  private url: string;

  // === v0.8.3 新增：自动重连 + 心跳状态 ===
  // closedByUser 区分"用户主动 disconnect"和"网络断开" —— 前者不重连。
  private closedByUser = false;
  private retryDelayMs = INITIAL_RETRY_DELAY_MS;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private missedPongs = 0;
  // 客户端监听重连事件，前端可显示"网络恢复中..." toast。
  private readonly onReconnectAttempt?: (attempt: number, delayMs: number) => void;
  private readonly onConnectionStateChange?: (state: "connected" | "reconnecting" | "closed") => void;

  constructor(
    sessionId: string,
    options?: {
      onReconnectAttempt?: (attempt: number, delayMs: number) => void;
      onConnectionStateChange?: (state: "connected" | "reconnecting" | "closed") => void;
    },
  ) {
    const baseUrl = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080";
    this.url = `${baseUrl}/ws/courtrooms/${sessionId}`;
    this.onReconnectAttempt = options?.onReconnectAttempt;
    this.onConnectionStateChange = options?.onConnectionStateChange;

    if (useMock) {
      this.socket = getMockWebSocket();
    } else {
      this.connectRealSocket();
    }
  }

  /**
   * connectRealSocket opens a new WebSocket and wires up its event handlers.
   * Called both for the initial connect AND for every reconnect attempt.
   */
  private connectRealSocket() {
    if (this.closedByUser) return;

    this.onConnectionStateChange?.("reconnecting");
    try {
      this.realSocket = new WebSocket(this.url);
    } catch (err) {
      console.error("[WebSocket] failed to construct:", err);
      this.scheduleReconnect();
      return;
    }

    this.realSocket.onopen = () => {
      console.log("[WebSocket] connected to", this.url);
      this.retryDelayMs = resetBackoff(); // 重置退避（重连成功 → 回到 1s）
      this.missedPongs = 0;
      this.onConnectionStateChange?.("connected");
      this.startHeartbeat();
    };
    this.realSocket.onmessage = (msg) => {
      try {
        const event = JSON.parse(msg.data) as CourtEvent;
        // 收到服务端 pong → 标记这次心跳成功。如果连续多个周期没收到
        // pong（mock 模式永远不会发），我们自己主动关闭重连，避免
        // TCP 半连接挂死。
        if (event.type === "pong") {
          this.missedPongs = 0;
          return;
        }
        this.dispatch(event);
      } catch {
        // ignore invalid messages
      }
    };
    this.realSocket.onerror = (err) => {
      console.error("[WebSocket] error:", err);
      // 错误不直接重连：onclose 一定会随后触发，统一在那里 schedule。
    };
    this.realSocket.onclose = () => {
      console.log("[WebSocket] closed");
      this.stopHeartbeat();
      this.onConnectionStateChange?.("closed");
      if (this.closedByUser) return;
      this.scheduleReconnect();
    };
  }

  /**
   * scheduleReconnect applies exponential backoff and re-opens the socket.
   * Called both after onclose (server-driven) and after repeated missed
   * heartbeats (client-driven).
   */
  private scheduleReconnect() {
    if (this.closedByUser) return;
    if (this.reconnectTimer) return; // 已经在排队了

    const delay = this.retryDelayMs;
    this.onReconnectAttempt?.(this.getReconnectAttemptCount(), delay);
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.retryDelayMs = computeBackoff(this.retryDelayMs);
      this.connectRealSocket();
    }, delay);
  }

  // 简单计数：每次重连都 +1（只在内存里，不持久化）。
  private reconnectAttempts = 0;
  private getReconnectAttemptCount(): number {
    return this.reconnectAttempts++;
  }

  /**
   * startHeartbeat fires every HEARTBEAT_INTERVAL_MS. We track how many
   * pings we've sent without receiving a matching pong. If two in a row
   * get missed we treat the socket as dead (half-open TCP) and force a
   * reconnect — this is how we detect "服务端 nginx 偷偷 close 了连接"
   * scenarios that wouldn't trigger onclose on our side.
   */
  private startHeartbeat() {
    this.stopHeartbeat();
    this.missedPongs = 0;
    this.heartbeatTimer = setInterval(() => {
      if (this.realSocket?.readyState === WebSocket.OPEN) {
        try {
          this.realSocket.send(JSON.stringify({ type: "ping" }));
          this.missedPongs++;
          // 发了 ping 但下一周期还没收到 pong（即使 pong 也算本周期成功）
          if (this.missedPongs >= 2) {
            console.warn("[WebSocket] no pong received — forcing reconnect");
            this.realSocket.close(); // 会触发 onclose → scheduleReconnect
          }
        } catch (err) {
          console.error("[WebSocket] heartbeat send failed:", err);
          this.realSocket.close();
        }
      } else if (this.realSocket?.readyState === WebSocket.CLOSED) {
        // 已经死了，主动触发 onclose 路径
        this.scheduleReconnect();
      }
    }, HEARTBEAT_INTERVAL_MS);
  }

  private stopHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  connect() {
    if (this.socket) {
      this.socket.connect();
      this.socket.on("*", (event) => this.dispatch(event));
    }
    return this;
  }

  on(event: string, handler: CourtEventHandler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, []);
    }
    this.handlers.get(event)!.push(handler);
    return this;
  }

  off(event: string, handler: CourtEventHandler) {
    const list = this.handlers.get(event);
    if (list) {
      this.handlers.set(
        event,
        list.filter((h) => h !== handler)
      );
    }
    return this;
  }

  send(action: UserActionRequest) {
    if (this.socket) {
      this.socket.send({ type: "user.action", payload: action });
    } else if (this.realSocket && this.realSocket.readyState === WebSocket.OPEN) {
      this.realSocket.send(JSON.stringify({ type: "user.action", payload: action }));
    }
  }

  disconnect() {
    // v0.8.3 修复：disconnect 必须清理所有 timer，否则 setTimeout 会
    // 持续触发 scheduleReconnect，组件卸载后还在重连（内存泄漏）。
    this.closedByUser = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.stopHeartbeat();
    this.onConnectionStateChange?.("closed");

    if (this.socket) {
      this.socket.disconnect();
    }
    if (this.realSocket) {
      const { CONNECTING, OPEN } = WebSocket;
      if (this.realSocket.readyState === CONNECTING) {
        // Abort the in-flight handshake gracefully to avoid
        // "closed before the connection is established" console noise.
        this.realSocket.onerror = null;
        this.realSocket.onopen = () => this.realSocket?.close();
      } else if (this.realSocket.readyState === OPEN) {
        this.realSocket.close();
      }
      this.realSocket = null;
    }
  }

  private dispatch(event: CourtEvent) {
    const list = this.handlers.get(event.type) || [];
    const wildcard = this.handlers.get("*") || [];
    [...list, ...wildcard].forEach((handler) => handler(event));
  }
}

export function createCourtWebSocket(
  sessionId: string,
  options?: ConstructorParameters<typeof CourtWebSocket>[1],
): CourtWebSocket {
  return new CourtWebSocket(sessionId, options).connect();
}
import type { CourtEvent, UserActionRequest } from "@/types";
import { getMockWebSocket } from "./mock/mockWebSocket";

export type CourtEventHandler = (event: CourtEvent) => void;

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

export class CourtWebSocket {
  private socket: ReturnType<typeof getMockWebSocket> | null = null;
  private realSocket: WebSocket | null = null;
  private handlers: Map<string, CourtEventHandler[]> = new Map();
  private url: string;

  constructor(sessionId: string) {
    const baseUrl = process.env.NEXT_PUBLIC_WS_URL || "ws://localhost:8080";
    this.url = `${baseUrl}/ws/courtrooms/${sessionId}`;

    if (useMock) {
      this.socket = getMockWebSocket();
    } else {
      this.realSocket = new WebSocket(this.url);
      this.realSocket.onmessage = (msg) => {
        try {
          const event = JSON.parse(msg.data) as CourtEvent;
          this.dispatch(event);
        } catch {
          // ignore invalid messages
        }
      };
      this.realSocket.onopen = () => {
        console.log("[WebSocket] connected to", this.url);
      };
      this.realSocket.onerror = (err) => {
        console.error("[WebSocket] error:", err);
      };
      this.realSocket.onclose = () => {
        console.log("[WebSocket] closed");
      };
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

export function createCourtWebSocket(sessionId: string): CourtWebSocket {
  return new CourtWebSocket(sessionId).connect();
}

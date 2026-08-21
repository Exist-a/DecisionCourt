// v1.0.4 PR-C2: Trace 前端可视化 helper
//
// 设计原则 (与 lib/rebuttal.ts 同源):
//   - SSR-safe (typeof window 检查)
//   - useMock 退化 (返回空数组 / null)
//   - 失败 console.warn 不抛 (UI 降级到"无数据"渲染)
//
// 与后端契约 (handler_trace.go):
//   GET /api/v1/courtrooms/:session_uuid/traces?date=YYYY-MM-DD
//     → { code: 0, data: { traces: Trace[], count: N, date: "..." } }
//   GET /api/v1/courtrooms/:session_uuid/traces/:trace_id
//     → { code: 0, data: Trace | null } — null 表示 trace 不存在

import { useEffect, useState } from "react";
import type { Trace } from "@/types";
import { getAuthToken } from "./auth.ts";

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

interface TraceListResponse {
  code: number;
  data: {
    traces: Trace[];
    count: number;
    date: string;
  };
}

interface TraceDetailResponse {
  code: number;
  data: Trace | null;
}

/**
 * Fetch all traces for a session on a given date.
 * date 空字符串 = 查今天 (后端默认逻辑)。
 *
 * Returns empty array on useMock / SSR / fetch failure (UI degradation).
 */
export async function fetchTraces(
  sessionUUID: string,
  date: string = "",
): Promise<Trace[]> {
  if (typeof window === "undefined" || useMock) {
    return [];
  }
  const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
  const token = getAuthToken();
  const url = date
    ? `${baseUrl}/api/v1/courtrooms/${sessionUUID}/traces?date=${date}`
    : `${baseUrl}/api/v1/courtrooms/${sessionUUID}/traces`;
  try {
    const res = await fetch(url, {
      method: "GET",
      credentials: "include",
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) {
      console.warn(`[trace] fetch list failed: ${res.status} ${res.statusText}`);
      return [];
    }
    const json = (await res.json()) as TraceListResponse;
    if (json.code !== 0 || !json.data) {
      console.warn(`[trace] unexpected list response: code=${json.code}`);
      return [];
    }
    return json.data.traces ?? [];
  } catch (err) {
    console.warn("[trace] fetch list error:", err);
    return [];
  }
}

/**
 * Fetch a single trace with full tree.
 *
 * Returns null when trace not found (后端 200 + data:null 语义,不是 404)。
 * Returns null on useMock / SSR / fetch failure (UI 容错渲染)。
 */
export async function fetchTrace(
  sessionUUID: string,
  traceID: string,
): Promise<Trace | null> {
  if (typeof window === "undefined" || useMock) {
    return null;
  }
  const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
  const token = getAuthToken();
  try {
    const res = await fetch(
      `${baseUrl}/api/v1/courtrooms/${sessionUUID}/traces/${traceID}`,
      {
        method: "GET",
        credentials: "include",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      },
    );
    if (!res.ok) {
      console.warn(`[trace] fetch detail failed: ${res.status} ${res.statusText}`);
      return null;
    }
    const json = (await res.json()) as TraceDetailResponse;
    if (json.code !== 0) {
      console.warn(`[trace] unexpected detail response: code=${json.code}`);
      return null;
    }
    return json.data;
  } catch (err) {
    console.warn("[trace] fetch detail error:", err);
    return null;
  }
}

/**
 * Hook: 拉 session 的所有 traces (按 date)。
 * 用于 TrialReplay 主组件初始渲染。
 */
export function useTraces(
  sessionUUID: string | null | undefined,
  date: string = "",
): { traces: Trace[]; loading: boolean } {
  const [traces, setTraces] = useState<Trace[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!sessionUUID) {
      setTraces([]);
      return;
    }
    let cancelled = false;
    setLoading(true);
    void fetchTraces(sessionUUID, date).then((result) => {
      if (cancelled) return;
      setTraces(result);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [sessionUUID, date]);

  return { traces, loading };
}

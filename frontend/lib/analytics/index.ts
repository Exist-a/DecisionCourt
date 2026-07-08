// v0.10 前端埋点 (ADR 0020):公开 API + PII 守卫
//
// 设计目标：
//   - track(eventType, payload) 公共 API,自动注入 sessionUUID
//   - PII 守卫：递归扫描 payload,含敏感字段直接拒绝 + 警告
//     (敏感字段：content / message / messages / raw_results / summary 等
//      任何含庭审原文 / 用户证据 / 判决正文的字段)
//   - SSR safe：useMock 或 window undefined 时全部 noop
//   - 便捷函数：trackPhaseChange / trackVerdictFeedback / trackEvidenceSubmitted
//
// 调用方惯例：
//   import { analytics } from "@/lib/analytics";
//   analytics.trackPhaseChange("opening", "cross_exam", 12_000);
//
// PII 黑名单策略（保守）：
//   顶层 + 嵌套递归扫描,任何一层命中就拒绝。理由：埋点维度已经够用,
//   真要记"用户证据"应该用后端的事件 + 鉴权,前端埋点不该带内容。

import type { Transport, FrontendEvent } from "../transport.ts";

// ============== PII 守卫 ==============

/**
 * PII_FIELDS：禁止出现在埋点 payload 中的字段名集合。
 * 大小写不敏感,递归扫描整个 payload。
 *
 * 来源（v0.10 决策）：
 *   - content / message / messages:律师发言、用户证据内容
 *   - raw_results / summary:搜索结果 + 总结(含第三方内容片段)
 *   - verdict / verdict_content / trial_summary:判决书正文 / 庭审纪要
 *   - session_uuid 是 session 标识,不算 PII,但禁止套娃
 *
 * 新增敏感字段时：append 到这里,无需改其他代码。
 */
const PII_FIELDS = new Set<string>([
  "content",
  "message",
  "messages",
  "raw_results",
  "summary",
  "verdict",
  "verdict_content",
  "trial_summary",
  "context", // 用户提交案情描述
  "title",   // 案件标题(可能含敏感信息)
]);

/**
 * containsPII 递归扫描 payload,任意一层包含 PII 字段 → true。
 * 不抛错,纯查询。
 */
export function containsPII(payload: unknown): boolean {
  if (payload === null || payload === undefined) return false;
  if (typeof payload !== "object") return false;
  if (Array.isArray(payload)) {
    return payload.some(containsPII);
  }
  for (const key of Object.keys(payload as Record<string, unknown>)) {
    if (PII_FIELDS.has(key.toLowerCase())) return true;
    const value = (payload as Record<string, unknown>)[key];
    if (value !== null && typeof value === "object") {
      if (containsPII(value)) return true;
    }
  }
  return false;
}

// ============== 类型 ==============

export interface AnalyticsDeps {
  /** 底层传输实例(transport.ts 的工厂产物) */
  transport: Transport;
  /** 当前 session UUID;空字符串视为全局事件 → 丢弃 */
  sessionUUID: string;
  /** mock 模式(演示前端时不发请求) */
  useMock: boolean;
  /** 警告回调(默认 console.warn) */
  onWarn: (msg: string) => void;
}

export interface TrackOptions {
  duration_ms?: number;
  status?: string;
  error_msg?: string;
}

export interface Analytics {
  /** 通用埋点 API */
  track: (eventType: string, payload?: Record<string, unknown>, options?: TrackOptions) => void;
  /** 阶段切换 */
  trackPhaseChange: (fromPhase: string, toPhase: string, durationMs: number) => void;
  /** 判决反馈 */
  trackVerdictFeedback: (helpful: boolean, scoreA: number, scoreB: number) => void;
  /** 证据提交 */
  trackEvidenceSubmitted: (evidenceType: string, charCount: number) => void;
}

// ============== 工厂 ==============

export function createAnalytics(deps: AnalyticsDeps): Analytics {
  function track(
    eventType: string,
    payload?: Record<string, unknown>,
    options?: TrackOptions,
  ): void {
    // mock / SSR noop
    if (deps.useMock) return;
    // 无 session → 丢弃(全局事件无归属,后端无处落)
    if (!deps.sessionUUID) return;

    // PII 守卫：递归扫描,含敏感字段直接拒绝
    if (payload && containsPII(payload)) {
      deps.onWarn(
        `analytics: dropped event with PII payload event=${eventType}`,
      );
      return;
    }

    const event: FrontendEvent = {
      session_uuid: deps.sessionUUID,
      event_type: eventType,
      payload,
      duration_ms: options?.duration_ms ?? 0,
      status: options?.status ?? "ok",
      error_msg: options?.error_msg ?? "",
    };
    deps.transport.enqueue(event);
  }

  function trackPhaseChange(fromPhase: string, toPhase: string, durationMs: number): void {
    track("fe.phase_entered", {
      from_phase: fromPhase,
      to_phase: toPhase,
      duration_ms: durationMs,
    });
  }

  function trackVerdictFeedback(helpful: boolean, scoreA: number, scoreB: number): void {
    track("fe.verdict_feedback", {
      helpful,
      score_a: scoreA,
      score_b: scoreB,
    });
  }

  function trackEvidenceSubmitted(evidenceType: string, charCount: number): void {
    track("fe.evidence_submitted", {
      type: evidenceType,
      char_count: charCount,
    });
  }

  return {
    track,
    trackPhaseChange,
    trackVerdictFeedback,
    trackEvidenceSubmitted,
  };
}
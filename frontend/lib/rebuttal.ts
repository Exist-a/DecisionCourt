// v1.0.2 候选 4: 已反驳证据集合跟踪 (PRD §4.3.3)
//
// 前端 helper:
//   - fetchRebuttalLinks(sessionUUID): 拉 session 全部 rebuttal links
//   - fetchRebuttalLinksForEvidence(sessionUUID, evidenceUUID): filter by evidence
//   - useRebuttalCounts(evidences, sessionUUID): hook 拉每条 evidence 的反驳计数
//
// 设计原则 (与 lib/transport.ts 同源):
//   - SSR-safe (window 检查)
//   - useMock 退化 (返回空列表)
//   - 失败 console.warn 不抛 (UI 降级到空 chip)
//
// 与后端契约 (handler.go GetRebuttalLinks):
//   GET /api/v1/courtrooms/:session_uuid/rebuttal-links
//     → { code: 0, data: { links: RebuttalLink[], count: N } }
//   GET /api/v1/courtrooms/:session_uuid/rebuttal-links?evidence_id=<UUID>
//     → 过滤后的 links (PR-4 简化为只接 UUID, display_id 暂不支持)

import { useEffect, useState } from "react";
import type { RebuttalLink, Evidence } from "@/types";
import { getAuthToken } from "./auth.ts";

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

interface RebuttalLinksResponse {
  code: number;
  data: {
    links: RebuttalLink[];
    count: number;
  };
}

/**
 * Fetch all rebuttal links for a session.
 * Returns empty array on useMock / SSR / fetch failure (UI degradation).
 */
export async function fetchRebuttalLinks(
  sessionUUID: string,
): Promise<RebuttalLink[]> {
  if (typeof window === "undefined" || useMock) {
    return [];
  }
  const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
  const token = getAuthToken();
  try {
    const res = await fetch(
      `${baseUrl}/api/v1/courtrooms/${sessionUUID}/rebuttal-links`,
      {
        method: "GET",
        credentials: "include",
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      },
    );
    if (!res.ok) {
      console.warn(
        `[rebuttal] fetch failed: ${res.status} ${res.statusText}`,
      );
      return [];
    }
    const json = (await res.json()) as RebuttalLinksResponse;
    if (json.code !== 0 || !json.data) {
      console.warn(`[rebuttal] unexpected response: code=${json.code}`);
      return [];
    }
    return json.data.links ?? [];
  } catch (err) {
    console.warn(`[rebuttal] fetch error:`, err);
    return [];
  }
}

/**
 * Hook: 拉 session 全部 rebuttal links + 按 evidence_id 分组成 map.
 * 返回的 map<evidence_id, count> 供 EvidenceBoard 显示 chip "已反驳 X 次".
 *
 * SSR-safe: window check + empty fallback.
 */
export function useRebuttalCounts(
  sessionUUID: string | null | undefined,
  evidences: Evidence[],
): Map<string, number> {
  const [counts, setCounts] = useState<Map<string, number>>(new Map());

  useEffect(() => {
    if (!sessionUUID || evidences.length === 0) {
      setCounts(new Map());
      return;
    }

    let cancelled = false;
    void fetchRebuttalLinks(sessionUUID).then((links) => {
      if (cancelled) return;
      // Build UUID → display_id map (v1.0.2: backend evidence.id 是 UUID,
      // evidence.evidence_id 是 display_id). link.rebutted_evidence_id 是 UUID.
      const uuidToDisplay = new Map<string, string>();
      for (const e of evidences) {
        uuidToDisplay.set(e.id, e.evidence_id);
      }
      // Count standing rebuttal per display_id.
      const m = new Map<string, number>();
      for (const link of links) {
        // 只计 standing 状态 (被翻盘的不算 chip)
        if (link.status !== "standing") continue;
        const display = uuidToDisplay.get(link.rebutted_evidence_id);
        if (display) {
          m.set(display, (m.get(display) ?? 0) + 1);
        }
      }
      setCounts(m);
    });

    return () => {
      cancelled = true;
    };
  }, [sessionUUID, evidences]);

  return counts;
}
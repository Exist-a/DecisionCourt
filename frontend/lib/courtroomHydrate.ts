// v1.0-patch: 共享 hydrate 函数
//
// 触发: 用户 2026-08-22 反馈"判决书浏览器 back 按钮返回庭审报错"。
// 根因: /court/[id] 页 mount 时无 store 填充逻辑,假设 store 已被实时 WS 填好。
//       浏览器 back 不重发 WS,导致 store 为空 → 所有 GET "record not found"。
//
// 修复: 抽出此函数, court + verdict 两个页面共用同一套 hydrate 序列:
//
//   1. session       (庭基本信息)
//   2. agents        (5 角色 metadata)
//   3. evidences     (用户提交 + 调查员补充)
//   4. investigations (调查活动记录)
//   5. belief_diffs   (信念变化时间线)
//   6. memory         (episodic memory 时间线)
//
// 6 个独立 try/catch, 任一失败 console.warn 不阻断 (verdict 页已经用同样模式)。
// `mounted` 守门防止 unmount 后 setState。

import { api } from "./api";
import { applyCourtEvent } from "@/store/courtroomStore";
import type {
  Agent,
  BeliefDiff,
  CourtSession,
  Evidence,
  InvestigationFinding,
  MemoryEntry,
} from "@/types";

export interface CourtroomHydrateActions {
  setSession: (s: CourtSession) => void;
  setAgents: (a: Agent[]) => void;
  addEvidence: (e: Evidence) => void;
  // v1.0-patch (2026-08-22): hydrate 时整体替换 evidence, 避免跨 session 累积。
  setEvidences: (e: Evidence[]) => void;
  setInvestigationFindings: (f: InvestigationFinding[]) => void;
  setBeliefDiffs: (d: BeliefDiff[]) => void;
  getStoredEvidences: () => Evidence[];
  setMemoryEntries: (m: MemoryEntry[]) => void;
}

/**
 * Hydrate courtroom store from REST APIs.
 *
 * v1.0-patch (2026-08-22): 抽自 verdict/[id]/page.tsx,供 court/[id]/page.tsx 复用。
 * 触发 bug: 浏览器 back 从 /verdict 跳 /court → 庭审现场空 store 显示。
 *
 * @param sessionUUID - 要 hydrate 的 session id
 * @param actions - 来自 courtroomStore 的 setter 集合
 * @returns Promise<void> — 不抛错,失败仅 console.warn
 */
export async function hydrateCourtroomStore(
  sessionUUID: string,
  actions: CourtroomHydrateActions,
): Promise<void> {
  const {
    setSession,
    setAgents,
    addEvidence,
    setEvidences,
    setInvestigationFindings,
    setBeliefDiffs,
    getStoredEvidences,
    setMemoryEntries,
  } = actions;

  // 1. Session
  try {
    const sessRes = await api.getSession(sessionUUID);
    if (sessRes.code === 0) setSession(sessRes.data);
  } catch (err) {
    console.warn("[hydrate] session failed:", err);
  }

  // 2. Agents
  try {
    const agentsRes = await api.getAgents(sessionUUID);
    if (agentsRes.code === 0) {
      // 后端返回可能是 { agents: [...] } 或直接 [...] (历史兼容)
      const data = agentsRes.data as { agents?: Agent[] } | Agent[];
      const list = Array.isArray(data) ? data : (data.agents ?? []);
      if (list.length > 0) setAgents(list);
    }
  } catch (err) {
    console.warn("[hydrate] agents failed:", err);
  }

  // 3. Evidences (整体替换 — v1.0-patch 修跨 session 证据污染 bug)
  //
  // 之前用 addEvidence + 去重, 但 store 是全局的, 切 session 时:
  //   trial A 跑完 → store.evidences = A 的 evidence
  //   trial B 回看 → addEvidence(B) 因 evidence_id 不同追加成功
  //   用户看到 A + B 的 evidence 混合 (你反馈的"点开不一样的庭审看到相同证据")
  //
  // 修复: hydrate 时 setEvidences (整体覆盖) 替换整个数组,
  // 与 setSession / setAgents / setInvestigationFindings / setBeliefDiffs
  // 行为一致 (hydrate 时整体覆盖, 不是增量)
  try {
    const evRes = await api.getEvidences(sessionUUID);
    if (evRes.code === 0 && Array.isArray(evRes.data.evidences)) {
      setEvidences(evRes.data.evidences);
    } else {
      // 0 evidence 时清空 (防止上次 session 残留)
      setEvidences([]);
    }
  } catch (err) {
    console.warn("[hydrate] evidences failed:", err);
  }

  // 4. Investigations
  try {
    const invRes = await api.getInvestigations(sessionUUID);
    if (
      invRes.code === 0 &&
      Array.isArray((invRes.data as { findings?: InvestigationFinding[] }).findings)
    ) {
      const findings = (invRes.data as { findings: InvestigationFinding[] }).findings;
      setInvestigationFindings(findings);
    }
  } catch (err) {
    console.warn("[hydrate] investigations failed:", err);
  }

  // 5. Belief diffs
  try {
    const diffRes = await api.getBeliefDiffs(sessionUUID);
    if (
      diffRes.code === 0 &&
      Array.isArray((diffRes.data as { diffs?: BeliefDiff[] }).diffs)
    ) {
      const diffs = (diffRes.data as { diffs: BeliefDiff[] }).diffs;
      setBeliefDiffs(diffs);
    }
  } catch (err) {
    console.warn("[hydrate] belief_diffs failed:", err);
  }

  // 6. Memory entries (a2a.message type)
  try {
    const memRes = await api.getVisibleMemory(sessionUUID);
    if (
      memRes.code === 0 &&
      Array.isArray((memRes.data as { memory?: unknown[] }).memory)
    ) {
      const memory = (memRes.data as { memory: Array<Record<string, unknown>> }).memory;
      // 复用 verdict 页的 a2a.message 转换路径 (applyCourtEvent 已处理)
      for (const row of memory) {
        applyCourtEvent({
          type: "a2a.message",
          payload: row as never,
          timestamp:
            (row.created_at as string | undefined) ?? new Date().toISOString(),
        });
      }
      setMemoryEntries(
        memory.map((r) => ({
          id: (r.id as string) ?? "",
          kind: ((r.message_type as string) ?? "strategy_note") as MemoryEntry["kind"],
          agentType: ((r.from as string) ?? "prosecutor") as MemoryEntry["agentType"],
          round: (r.round as number) ?? 0,
          phase: (r.phase as string) ?? "",
          content: (r.content as string) ?? "",
          // 兼容老数据: memory rows 可能不含 linked_evidence_ids, 默认为空数组
          linkedEvidenceIds: Array.isArray(r.linked_evidence_ids)
            ? (r.linked_evidence_ids as string[])
            : [],
          createdAt: (r.created_at as string) ?? new Date().toISOString(),
        })),
      );
    }
  } catch (err) {
    console.warn("[hydrate] memory failed:", err);
  }
}

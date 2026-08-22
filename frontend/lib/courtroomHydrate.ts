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
  Message,
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
  // v1.0-patch (2026-08-22): hydrate 补 messages + activeInvestigation,
  // 之前抽函数时漏了, 导致 court page 庭审记录 / 调查记录 Tab 没数据。
  // 可选: verdict 页面用本地 useState<Message[]> 管 messages (不靠 store),
  // verdict 页面也不需要重置 activeInvestigation (独立逻辑)。
  // 所以这两个 setter 是 optional — court 页面传, verdict 不传。
  setMessages?: (m: Message[]) => void;
  setActiveInvestigation?: (
    info: { dispatcher: string; query: string; startedAt: string } | null,
  ) => void;
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
    setMemoryEntries, // 保留以备 verdict page 整体替换使用, 当前 applyCourtEvent 路径走 store.appendMemoryEntry 已正确写入
    setMessages,
    setActiveInvestigation,
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

  // 4. Investigations (setInvestigationFindings 整体替换 — v1.0-patch 与 evidences 同)
  // 之前 verdict 老代码在这里调, 但搬到 hydrate 函数时漏写了一段 try/catch。
  // 现状: court page 庭审调查活动 Tab 没数据。
  try {
    const invRes = await api.getInvestigations(sessionUUID);
    if (invRes.code === 0 && Array.isArray(invRes.data.findings)) {
      setInvestigationFindings(invRes.data.findings);
    } else {
      setInvestigationFindings([]);
    }
  } catch (err) {
    console.warn("[hydrate] investigations failed:", err);
  }

  // 4. Investigations (已被 line 117 取代, 删除重复 block)

  // 5. Messages (setMessages 整体替换 — v1.0-patch 防止跨 session 累积)
  // 之前 verdict 页老代码调 api.getMessages + setMessages, court page 没调,
  // 抽到 hydrate 时漏了。修: hydrate 也调, 与 setEvidences 同样覆盖语义。
  // setMessages 是 optional (verdict 页面用本地 useState 不传)。
  if (setMessages) {
    try {
      const msgRes = await api.getMessages(sessionUUID);
      if (msgRes.code === 0 && Array.isArray(msgRes.data.messages)) {
        setMessages(msgRes.data.messages);
      } else {
        setMessages([]);
      }
    } catch (err) {
      console.warn("[hydrate] messages failed:", err);
    }
  }

  // 5b. Active investigation (v0.9 调查员跑状态)
  // hydrate 时重置为 null — 历史 session 不会有 "正在跑" 的 investigation,
  // 防止旧 session 的 activeInvestigation 状态污染新 session。
  // setActiveInvestigation 是 optional (verdict 页面不传)。
  if (setActiveInvestigation) {
    setActiveInvestigation(null);
  }

  // 6. Belief diffs
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
  //
  // v1.0-patch (2026-08-23, fix U1/U2/U3): 唯一写路径改为 applyCourtEvent 循环。
  //
  // 旧实现额外调一次 setMemoryEntries(memory.map(...)) 用顶层 r.content 整体覆盖,
  // 但后端 REST 返回结构里 content 在 row.payload.content 嵌套里 (见
  // backend/internal/api/handler.go:908-921), 顶层 r.content 永远是 undefined
  // → 全部 entry.content="" → 判决书 / 历史庭审策略笔记全空 (U1/U2)。
  // 且 setMemoryEntries 整体替换, 在 applyCourtEvent 之后执行,
  // 用空内容覆盖了正确数据; appendMemoryEntry 按 id 幂等 (store L499)
  // 又阻断了 CourtroomScene 的二次补救, 救不回。
  //
  // 现在只走 applyCourtEvent 一条路径: store 的 a2a.message handler (L749-815)
  // 已正确解析 payload.content / payload.stance / payload.confidence /
  // payload.reasoning / payload.linked_evidence_ids, appendMemoryEntry (L496-509)
  // 按 id 幂等 + 排序, 多次 hydrate 安全。
  try {
    const memRes = await api.getVisibleMemory(sessionUUID);
    if (
      memRes.code === 0 &&
      Array.isArray((memRes.data as { memory?: unknown[] }).memory)
    ) {
      const memory = (memRes.data as { memory: Array<Record<string, unknown>> }).memory;
      for (const row of memory) {
        applyCourtEvent({
          type: "a2a.message",
          payload: row as never,
          timestamp:
            (row.created_at as string | undefined) ?? new Date().toISOString(),
        });
      }
    }
  } catch (err) {
    console.warn("[hydrate] memory failed:", err);
  }
}

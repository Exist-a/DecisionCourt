import type {
  Agent,
  BeliefDiff,
  CourtSession,
  CreateSessionRequest,
  Evidence,
  InvestigationFinding,
  Message,
  SubmitEvidenceRequest,
  UserActionRequest,
  Verdict,
} from "@/types";
import { mockApi } from "./mock/mockApi";

const useMock = process.env.NEXT_PUBLIC_USE_MOCK === "true";

export const api = {
  createSession: (req: CreateSessionRequest) =>
    useMock
      ? mockApi.createSession(req)
      : fetchJson<CreateSessionRequest, { code: number; data: CourtSession }>(
          "/api/v1/courtrooms",
          req
        ),

  getSession: (sessionUUID: string) =>
    useMock
      ? mockApi.getSession(sessionUUID)
      : fetchJson<never, { code: number; data: CourtSession }>(
          `/api/v1/courtrooms/${sessionUUID}`
        ),

  startTrial: (sessionUUID: string) =>
    useMock
      ? mockApi.startTrial(sessionUUID)
      : fetchJson<Record<string, never>, { code: number; data: { session_uuid: string; message: string } }>(
          `/api/v1/courtrooms/${sessionUUID}/start`,
          {}
        ),

  getAgents: (sessionUUID: string) =>
    useMock
      ? mockApi.getAgents(sessionUUID)
      : fetchJson<never, { code: number; data: { agents: Agent[] } }>(
          `/api/v1/courtrooms/${sessionUUID}/agents`
        ),

  getEvidences: (sessionUUID: string) =>
    useMock
      ? mockApi.getEvidences(sessionUUID)
      : fetchJson<never, { code: number; data: { evidences: Evidence[] } }>(
          `/api/v1/courtrooms/${sessionUUID}/evidences`
        ),

  getMessages: (sessionUUID: string) =>
    useMock
      ? mockApi.getMessages(sessionUUID)
      : fetchJson<
          never,
          { code: number; data: { total: number; messages: Message[] } }
        >(`/api/v1/courtrooms/${sessionUUID}/messages`),

  submitEvidence: (sessionUUID: string, req: SubmitEvidenceRequest) =>
    useMock
      ? mockApi.submitEvidence(sessionUUID, req)
      : fetchJson<SubmitEvidenceRequest, { code: number; data: Evidence }>(
          `/api/v1/courtrooms/${sessionUUID}/evidences`,
          req
        ),

  sendAction: (sessionUUID: string, req: UserActionRequest) =>
    useMock
      ? mockApi.sendAction(sessionUUID, req)
      : fetchJson<UserActionRequest, { code: number; data: Record<string, unknown> }>(
          `/api/v1/courtrooms/${sessionUUID}/actions`,
          req
        ),

  getVerdict: (sessionUUID: string) =>
    useMock
      ? mockApi.getVerdict(sessionUUID)
      : fetchJson<never, { code: number; data: Verdict }>(
          `/api/v1/courtrooms/${sessionUUID}/verdict`
        ),

  // 导出庭审完整数据（JSON / PDF）。v0.5+ 增量功能。
  // - json: 返回完整 JSON dump（含 verdict、evidence、messages、a2a_messages）。
  //   服务端 Content-Disposition 强制浏览器下载。
  // - pdf: 客户端用 window.print() 触发，不走后端。
  exportSession: async (sessionUUID: string): Promise<void> => {
    if (useMock) {
      // mock 模式：弹个 alert 提示用户
      window.alert("Mock 模式暂不支持导出，请连接真实后端。");
      return;
    }
    const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
    const res = await fetch(`${baseUrl}/api/v1/courtrooms/${sessionUUID}/export`, {
      method: "GET",
    });
    if (!res.ok) {
      throw new Error(`Export failed: ${res.status} ${res.statusText}`);
    }
    // 服务端已设 Content-Disposition: attachment，浏览器会自动下载。
    // 但用 fetch 走的话需要手动处理 blob。
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    // 从 Content-Disposition 提取 filename，缺省用 sessionUUID
    const disp = res.headers.get("Content-Disposition") || "";
    const match = disp.match(/filename="?([^";]+)"?/);
    a.download = match?.[1] || `decisioncourt-${sessionUUID}.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  },

  // PDF 导出：客户端 window.print() 配合 print stylesheet。
  // 后端零改动，浏览器原生 PDF 保存对话框。
  printVerdictAsPDF: () => window.print(),

  // 调查发现列表（区别于用户证据）。后端 GET /investigations 接口
  // 返回 InvestigationFinding[]；不在用户证据列表出现。
  getInvestigations: (sessionUUID: string) =>
    useMock
      ? Promise.resolve({ code: 0, data: { findings: [] as InvestigationFinding[] } })
      : fetchJson<
          never,
          {
            code: number;
            data: { findings: InvestigationFinding[] };
          }
        >(`/api/v1/courtrooms/${sessionUUID}/investigations`),

  // v0.6 belief engine audit trail. Supports two optional filters:
  //   - agent: "prosecutor" | "defender" | "investigator" | ...
  //   - round: 1..maxRounds
  // Backend returns the structured BeliefDiff list (one row per
  // (evidence, agent) update step). The frontend hydrates this into
  // the store on session restore so a reconnection mid-trial shows the
  // full history without losing anything.
  getBeliefDiffs: (
    sessionUUID: string,
    filter?: { agent?: string; round?: number },
  ) =>
    useMock
      ? Promise.resolve({ code: 0, data: { diffs: [] as BeliefDiff[], count: 0 } })
      : fetchJson<
          never,
          { code: number; data: { diffs: BeliefDiff[]; count: number } }
        >(`/api/v1/courtrooms/${sessionUUID}/belief-diffs${
          filter?.agent
            ? `?agent=${encodeURIComponent(filter.agent)}`
            : filter?.round
              ? `?round=${filter.round}`
              : ""
        }`),

  // v0.5 episodic-memory REST hydration. Returns the full v0.5 private
  // memory timeline (strategy_note / opponent_weakness / self_correction /
  // evidence_eval) for both sides. See backend
  // internal/api/handler.go::GetVisibleMemory.
  //
  // Frontend hydrates these into the store by replaying each row as an
  // `a2a.message` court-event (the envelope shape is identical to what
  // the WebSocket broadcasts). This means there is only ONE parser path
  // (applyCourtEvent → store.appendMemoryEntry) for memory rows whether
  // they arrive live or via rehydration.
  getVisibleMemory: (sessionUUID: string) =>
    useMock
      ? Promise.resolve({ code: 0, data: { memory: [], count: 0 } })
      : fetchJson<
          never,
          {
            code: number;
            data: {
              memory: Array<{
                id: string;
                message_uuid: string;
                round: number;
                phase: string;
                from: string;
                to: string;
                message_type: string;
                visibility: string;
                payload: Record<string, unknown>;
                created_at: string;
              }>;
              count: number;
            };
          }
        >(`/api/v1/courtrooms/${sessionUUID}/memory`),
};

async function fetchJson<Req, Res>(
  path: string,
  body?: Req,
  method?: string
): Promise<Res> {
  // 端口配置完全由 .env.local 决定（v0.8.3 修复：不要硬编码默认值覆盖用户配置）
  const baseUrl = process.env.NEXT_PUBLIC_API_URL || "";
  const res = await fetch(`${baseUrl}${path}`, {
    method: method || (body ? "POST" : "GET"),
    headers: { "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    throw new Error(`API error: ${res.status} ${res.statusText}`);
  }
  return res.json() as Promise<Res>;
}

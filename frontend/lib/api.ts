import type {
  Agent,
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
};

async function fetchJson<Req, Res>(
  path: string,
  body?: Req,
  method?: string
): Promise<Res> {
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

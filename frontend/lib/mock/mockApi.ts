import type {
  Agent,
  CourtSession,
  CreateSessionRequest,
  CreateSessionResponse,
  Evidence,
  Message,
  SubmitEvidenceRequest,
  UserActionRequest,
  Verdict,
} from "@/types";
import {
  mockAgents,
  mockEvidences,
  mockMessages,
  mockSession,
  mockVerdict,
} from "./mockData";

function delay(ms = 500) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function generateUUID() {
  return "xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx".replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === "x" ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export const mockApi = {
  async createSession(
    req: CreateSessionRequest
  ): Promise<CreateSessionResponse> {
    await delay(600);
    const session: CourtSession = {
      ...mockSession,
      session_uuid: `court_${generateUUID().slice(0, 8)}`,
      title: req.title || mockSession.title,
      option_a: req.option_a || mockSession.option_a,
      option_b: req.option_b || mockSession.option_b,
      context: req.context || mockSession.context,
      mode: req.mode || "standard",
      max_rounds:
        req.mode === "quick" ? 2 : req.mode === "deep" ? 5 : 3,
      current_phase:
        req.option_a && req.option_b ? "idle" : "clarification",
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    return { code: 0, data: session };
  },

  async getSession(_sessionUUID: string): Promise<{ code: number; data: CourtSession }> {
    await delay(400);
    return {
      code: 0,
      data: { ...mockSession, session_uuid: _sessionUUID },
    };
  },

  async startTrial(
    sessionUUID: string
  ): Promise<{ code: number; data: { session_uuid: string; message: string } }> {
    await delay(400);
    return {
      code: 0,
      data: { session_uuid: sessionUUID, message: "庭审开始" },
    };
  },

  async getAgents(_sessionUUID: string): Promise<{ code: number; data: Agent[] }> {
    await delay(300);
    return {
      code: 0,
      data: mockAgents.map((a) => ({ ...a })),
    };
  },

  async getEvidences(
    __sessionUUID: string
  ): Promise<{ code: number; data: { evidences: Evidence[] } }> {
    await delay(300);
    return {
      code: 0,
      data: { evidences: mockEvidences.map((e) => ({ ...e })) },
    };
  },

  async getMessages(
    _sessionUUID: string
  ): Promise<{ code: number; data: { total: number; messages: Message[] } }> {
    await delay(400);
    return {
      code: 0,
      data: { total: mockMessages.length, messages: [...mockMessages] },
    };
  },

  async submitEvidence(
    sessionUUID: string,
    req: SubmitEvidenceRequest
  ): Promise<{ code: number; data: Evidence }> {
    await delay(500);
    const evidence: Evidence = {
      evidence_id: `E${String(mockEvidences.length + 1).padStart(3, "0")}`,
      type: req.type || "fact",
      source: req.source || "user",
      content: req.content,
      submitted_by: "user",
      credibility_score: 0.85,
      relevance_score: 0.8,
      impact_on_option_a: 0.3,
      impact_on_option_b: -0.1,
      status: "admitted",
      created_at: new Date().toISOString(),
    };
    return { code: 0, data: evidence };
  },

  async sendAction(
    sessionUUID: string,
    req: UserActionRequest
  ): Promise<{ code: number; data: Record<string, unknown> }> {
    await delay(400);
    return { code: 0, data: { session_uuid: sessionUUID, action: req.action } };
  },

  async getVerdict(
    sessionUUID: string
  ): Promise<{ code: number; data: Verdict }> {
    await delay(600);
    return { code: 0, data: { ...mockVerdict, session_uuid: sessionUUID } };
  },
};

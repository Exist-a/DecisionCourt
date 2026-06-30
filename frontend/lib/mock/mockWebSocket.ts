import type { CourtEvent, UserActionRequest } from "@/types";
import {
  buildClosingEvents,
  buildCrossExamEvents,
  buildMockEventTimeline,
} from "./mockData";

type EventHandler = (event: CourtEvent) => void;

export class MockWebSocket {
  private handlers: Map<string, EventHandler[]> = new Map();
  private timers: NodeJS.Timeout[] = [];
  private connected = false;
  private pendingEvents: CourtEvent[] = [];
  private currentRound = 0;
  private maxRounds = 3;
  private isDirectVerdict = false;

  connect() {
    this.connected = true;
    this.scheduleEvents(buildMockEventTimeline(), 0);
    return this;
  }

  disconnect() {
    this.connected = false;
    this.timers.forEach((t) => clearTimeout(t));
    this.timers = [];
    this.pendingEvents = [];
  }

  on(event: string, handler: EventHandler) {
    if (!this.handlers.has(event)) {
      this.handlers.set(event, []);
    }
    this.handlers.get(event)!.push(handler);
    return this;
  }

  off(event: string, handler: EventHandler) {
    const list = this.handlers.get(event);
    if (list) {
      this.handlers.set(
        event,
        list.filter((h) => h !== handler)
      );
    }
    return this;
  }

  send(data: { type: string; payload: UserActionRequest }) {
    if (!this.connected) return;

    if (data.type === "user.action") {
      const action = data.payload;

      if (action.action === "submit_evidence") {
        this.emit({
          type: "evidence.added",
          payload: {
            evidence_id: `E${String(Math.floor(Math.random() * 900) + 100)}`,
            type: action.type || "fact",
            source: "user",
            content: action.content || "",
            submitted_by: "user",
            credibility_score: 0.85,
            relevance_score: 0.8,
            impact_on_option_a: 0.4,
            impact_on_option_b: -0.2,
            status: "admitted",
            created_at: new Date().toISOString(),
          },
          timestamp: new Date().toISOString(),
        });

        this.timers.push(
          setTimeout(() => {
            this.emit({
              type: "agent.speak",
              payload: {
                agent_id: "agent_pro_001",
                agent_type: "prosecutor",
                name: "控方律师",
                phase: "evidence",
                round: this.currentRound,
                content: "这份新证据进一步支持我方观点，请求法庭采纳。",
                evidence_refs: [],
                belief_a: 0.8,
                belief_b: 0.2,
              },
              timestamp: new Date().toISOString(),
            });
          }, 800)
        );
      }

      if (action.action === "request_search") {
        this.emit({
          type: "search.started",
          payload: {
            agent_id: "agent_inv_001",
            query: action.query || "",
          },
          timestamp: new Date().toISOString(),
        });

        this.timers.push(
          setTimeout(() => {
            this.emit({
              type: "search.completed",
              payload: {
                agent_id: "agent_inv_001",
                query: action.query || "",
                result_count: 5,
                evidence_ids: ["E005"],
              },
              timestamp: new Date().toISOString(),
            });

            this.emit({
              type: "evidence.added",
              payload: {
                evidence_id: "E005",
                type: "data",
                source: "web_search",
                content: `关于"${action.query}"的搜索结果：行业报告显示相关趋势向好`,
                submitted_by: "investigator",
                credibility_score: 0.75,
                relevance_score: 0.8,
                impact_on_option_a: 0.5,
                impact_on_option_b: -0.2,
                status: "admitted",
                created_at: new Date().toISOString(),
              },
              timestamp: new Date().toISOString(),
            });
          }, 1500)
        );
      }

      if (action.action === "direct_verdict") {
        this.isDirectVerdict = true;
        this.timers.forEach((t) => clearTimeout(t));
        this.timers = [];
        this.scheduleEvents(buildClosingEvents(), 0);
      }

      if (action.action === "answer_question") {
        this.emit({
          type: "evidence.added",
          payload: {
            evidence_id: `E${String(Math.floor(Math.random() * 900) + 100)}`,
            type: "fact",
            source: "agent_question",
            content: action.answer || "用户回答",
            submitted_by: "user",
            credibility_score: 0.9,
            relevance_score: 0.85,
            impact_on_option_a: 0.2,
            impact_on_option_b: 0.1,
            status: "admitted",
            created_at: new Date().toISOString(),
          },
          timestamp: new Date().toISOString(),
        });

        this.advanceToCrossExam();
      }

      if (action.action === "skip_agent") {
        // no-op in mock
      }
    }
  }

  private advanceToCrossExam() {
    if (this.isDirectVerdict) return;
    this.currentRound += 1;
    if (this.currentRound <= this.maxRounds) {
      this.scheduleEvents(buildCrossExamEvents(this.currentRound), 1000);
      if (this.currentRound === this.maxRounds) {
        this.timers.push(
          setTimeout(() => {
            this.scheduleEvents(buildClosingEvents(), 500);
          }, 6000)
        );
      } else {
        this.timers.push(
          setTimeout(() => {
            this.advanceToCrossExam();
          }, 6000)
        );
      }
    }
  }

  private scheduleEvents(events: CourtEvent[], baseDelay: number) {
    events.forEach((event, index) => {
      const delay = baseDelay + index * 1200;
      const timer = setTimeout(() => {
        this.emit(event);
      }, delay);
      this.timers.push(timer);
    });

    // If no user question, auto advance after opening evidence phase
    const hasUserQuestion = events.some(
      (e) =>
        e.type === "user.action.required" &&
        ((e.payload as unknown) as { action: string }).action === "answer_question"
    );
    if (!hasUserQuestion && !this.isDirectVerdict) {
      const lastDelay = baseDelay + events.length * 1200 + 2000;
      const timer = setTimeout(() => {
        this.advanceToCrossExam();
      }, lastDelay);
      this.timers.push(timer);
    }
  }

  private emit(event: CourtEvent) {
    if (!this.connected) return;
    const list = this.handlers.get(event.type) || [];
    list.forEach((handler) => handler(event));

    // Also emit to wildcard listeners
    const all = this.handlers.get("*") || [];
    all.forEach((handler) => handler(event));
  }
}

let instance: MockWebSocket | null = null;

export function getMockWebSocket(): MockWebSocket {
  if (!instance) {
    instance = new MockWebSocket();
  }
  return instance;
}

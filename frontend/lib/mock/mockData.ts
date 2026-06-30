import type {
  Agent,
  CourtSession,
  CourtEvent,
  Evidence,
  Message,
  Verdict,
} from "@/types";

export const mockSession: CourtSession = {
  session_uuid: "court_demo_001",
  title: "跳槽 vs 留下",
  option_a: "接受创业公司 offer",
  option_b: "留在现在的大厂",
  context: "工作三年，目前在大厂做后端开发，有一家 AI 创业公司 offer。",
  mode: "standard",
  max_rounds: 3,
  current_phase: "idle",
  current_round: 0,
  status: "active",
  converged: false,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
};

export const mockAgents: Agent[] = [
  {
    agent_uuid: "agent_pro_001",
    agent_type: "prosecutor",
    name: "选项A代表",
    role: "支持选项 A，证明接受创业公司 offer 是更优选择。",
    belief_a: 0.75,
    belief_b: 0.25,
    model: "deepseek-chat",
    temperature: 0.8,
    status: "active",
  },
  {
    agent_uuid: "agent_def_001",
    agent_type: "defender",
    name: "选项B代表",
    role: "支持选项 B，证明留在大厂是更优选择。",
    belief_a: 0.25,
    belief_b: 0.75,
    model: "deepseek-chat",
    temperature: 0.8,
    status: "active",
  },
  {
    agent_uuid: "agent_inv_001",
    agent_type: "investigator",
    name: "调查员",
    role: "检索外部信息、澄清问题、生成候选选项、提交证据。",
    belief_a: 0.5,
    belief_b: 0.5,
    model: "deepseek-chat",
    temperature: 0.3,
    status: "active",
  },
  {
    agent_uuid: "agent_clk_001",
    agent_type: "clerk",
    name: "书记员",
    role: "记录庭审、整理证据、生成判决书。",
    belief_a: 0.5,
    belief_b: 0.5,
    model: "deepseek-reasoner",
    temperature: 0.2,
    status: "active",
  },
];

export const mockEvidences: Evidence[] = [
  {
    evidence_id: "E001",
    type: "fact",
    source: "user",
    content: "我已存下 18 个月生活费的应急基金",
    submitted_by: "user",
    credibility_score: 0.9,
    relevance_score: 0.85,
    impact_on_option_a: 0.6,
    impact_on_option_b: -0.2,
    status: "admitted",
    created_at: new Date().toISOString(),
  },
  {
    evidence_id: "E002",
    type: "data",
    source: "web_search",
    content: "2026 年 AI 创业公司早期员工平均期权回报率约为 3-5 倍",
    url: "https://example.com/ai-startup-equity",
    submitted_by: "investigator",
    credibility_score: 0.7,
    relevance_score: 0.8,
    impact_on_option_a: 0.7,
    impact_on_option_b: -0.3,
    status: "admitted",
    created_at: new Date().toISOString(),
  },
  {
    evidence_id: "E003",
    type: "constraint",
    source: "user",
    content: "家庭需要我每月至少 2 万元稳定现金流",
    submitted_by: "user",
    credibility_score: 0.95,
    relevance_score: 0.9,
    impact_on_option_a: -0.5,
    impact_on_option_b: 0.4,
    status: "admitted",
    created_at: new Date().toISOString(),
  },
];

export const mockMessages: Message[] = [
  {
    id: "msg_001",
    agent_id: "agent_pro_001",
    agent_type: "prosecutor",
    name: "选项A代表",
    phase: "opening",
    round: 0,
    action_type: "speak",
    content:
      "尊敬的法官，我方将证明接受创业公司 offer 是更优选择。原告具备充足财务缓冲，且 AI 赛道正处于爆发期。",
    evidence_refs: ["E001"],
    created_at: new Date().toISOString(),
  },
  {
    id: "msg_002",
    agent_id: "agent_def_001",
    agent_type: "defender",
    name: "选项B代表",
    phase: "opening",
    round: 0,
    action_type: "speak",
    content:
      "尊敬的法官，我方认为留在现有大厂更为稳妥。创业公司成功率低，且家庭有稳定现金流硬约束。",
    evidence_refs: ["E003"],
    created_at: new Date().toISOString(),
  },
];

export const mockVerdict: Verdict = {
  verdict_id: "ver_001",
  session_uuid: "court_demo_001",
  summary:
    "建议接受创业公司 offer，但需设定 12 个月评估节点，并保留家庭财务安全垫。",
  option_a_score: 0.68,
  option_b_score: 0.52,
  consensus_points: [
    "财务缓冲充足，可支撑过渡期",
    "AI 赛道前景与个人技术栈匹配",
  ],
  divergence_points: [
    "收入稳定性与家庭现金流约束",
    "长期职业风险与期权回报不确定性",
  ],
  recommendation:
    "接受 offer，但签订合同时明确绩效评估节点；保持与大厂前同事联系；每月留足家庭最低现金流后再做投资。",
  content: `# 决策判决书

## 一、双方主张

| 控方（接受创业公司 offer） | 辩方（留在大厂） |
|---|---|
| 财务缓冲充足，可承受 18 个月无收入 | 家庭需要每月 2 万稳定现金流 |
| AI 赛道爆发，早期员工期权回报高 | 创业公司成功率低，期权兑现不确定 |
| 小团队核心岗位成长快 | 大厂稳定性高，技术风险低 |

## 二、已采纳证据

- **E001**: 原告已存 18 个月应急基金 ✅
- **E002**: 2026 年 AI 创业公司早期员工期权回报率 3-5 倍 ⚠️（数据来源需进一步核实）
- **E003**: 家庭每月需 2 万稳定现金流 ✅

## 三、争议焦点

1. 收入稳定性 vs 成长空间
2. 家庭现金流约束能否被应急基金覆盖
3. 期权回报概率与兑现周期

## 四、最终裁决

建议接受创业公司 offer，但设定 12 个月评估节点，并保留家庭最低现金流安全垫。
`,
  user_feedback: "none",
  created_at: new Date().toISOString(),
};

export function buildMockEventTimeline(): CourtEvent[] {
  const now = Date.now;
  const events: CourtEvent[] = [];

  events.push({
    type: "phase.changed",
    payload: {
      previous_phase: "idle",
      current_phase: "opening",
      current_round: 0,
      message: "庭审开始，进入开庭陈述阶段",
    },
    timestamp: new Date(now() + 500).toISOString(),
  });

  events.push({
    type: "agent.speak",
    payload: {
      agent_id: "agent_pro_001",
      agent_type: "prosecutor",
      name: "选项A代表",
      phase: "opening",
      round: 0,
      content:
        "尊敬的法官，我方将证明接受创业公司 offer 是更优选择。原告具备 18 个月应急基金，足以覆盖过渡期风险。",
      evidence_refs: ["E001"],
      belief_a: 0.75,
      belief_b: 0.25,
    },
    timestamp: new Date(now() + 1500).toISOString(),
  });

  events.push({
    type: "agent.speak",
    payload: {
      agent_id: "agent_def_001",
      agent_type: "defender",
      name: "选项B代表",
      phase: "opening",
      round: 0,
      content:
        "尊敬的法官，我方认为应留在大厂。家庭每月需要 2 万元稳定现金流，这是不可妥协的硬约束。",
      evidence_refs: ["E003"],
      belief_a: 0.25,
      belief_b: 0.75,
    },
    timestamp: new Date(now() + 3500).toISOString(),
  });

  events.push({
    type: "phase.changed",
    payload: {
      previous_phase: "opening",
      current_phase: "evidence",
      current_round: 0,
      message: "进入举证阶段，请提交证据或传唤调查员",
    },
    timestamp: new Date(now() + 4500).toISOString(),
  });

  events.push({
    type: "user.action.required",
    payload: {
      action: "answer_question",
      question_id: "q_001",
      question: "你目前的月收入范围是多少？",
      purpose: "评估你的财务风险承受能力",
      skip_allowed: true,
    },
    timestamp: new Date(now() + 5000).toISOString(),
  });

  return events;
}

export function buildCrossExamEvents(round: number): CourtEvent[] {
  const now = Date.now;
  const events: CourtEvent[] = [];

  events.push({
    type: "phase.changed",
    payload: {
      previous_phase: round === 1 ? "evidence" : "cross_exam",
      current_phase: "cross_exam",
      current_round: round,
      message: `进入第 ${round} 轮质证`,
    },
    timestamp: new Date(now() + 100).toISOString(),
  });

  if (round === 1) {
    events.push({
      type: "evidence.added",
      payload: {
        evidence_id: "E002",
        type: "data",
        source: "web_search",
        content: "2026 年 AI 创业公司早期员工平均期权回报率约为 3-5 倍",
        url: "https://example.com/ai-startup-equity",
        submitted_by: "investigator",
        credibility_score: 0.7,
        relevance_score: 0.8,
        impact_on_option_a: 0.7,
        impact_on_option_b: -0.3,
        status: "admitted",
        created_at: new Date(now() + 200).toISOString(),
      },
      timestamp: new Date(now() + 200).toISOString(),
    });

    events.push({
      type: "belief.updated",
      payload: {
        round,
        agent_id: "agent_pro_001",
        agent_type: "prosecutor",
        belief_a: 0.82,
        belief_b: 0.18,
        delta: 0.07,
      },
      timestamp: new Date(now() + 300).toISOString(),
    });

    events.push({
      type: "agent.speak",
      payload: {
        agent_id: "agent_pro_001",
        agent_type: "prosecutor",
        name: "选项A代表",
        phase: "cross_exam",
        round,
        content:
          "调查员提交的数据显示，AI 创业公司早期员工期权回报率高达 3-5 倍。这强力支持我方观点。",
        evidence_refs: ["E002"],
        belief_a: 0.82,
        belief_b: 0.18,
      },
      timestamp: new Date(now() + 1500).toISOString(),
    });

    events.push({
      type: "evidence.challenged",
      payload: {
        evidence_id: "E002",
        agent_id: "agent_def_001",
        agent_type: "defender",
        reason: "该数据来源不明，且未说明统计口径，不能作为可靠证据。",
      },
      timestamp: new Date(now() + 2500).toISOString(),
    });

    events.push({
      type: "agent.speak",
      payload: {
        agent_id: "agent_def_001",
        agent_type: "defender",
        name: "选项B代表",
        phase: "cross_exam",
        round,
        content:
          "我方质疑 E002 的可信度。期权回报 3-5 倍只是平均值，未考虑失败公司归零的情况，不能说明问题。",
        evidence_refs: ["E002"],
        belief_a: 0.28,
        belief_b: 0.72,
      },
      timestamp: new Date(now() + 3500).toISOString(),
    });
  } else if (round === 2) {
    events.push({
      type: "evidence.added",
      payload: {
        evidence_id: "E004",
        type: "data",
        source: "web_search",
        content: "2026 年国内 AI 创业公司 B 轮前存活率约为 35%",
        url: "https://example.com/startup-survival",
        submitted_by: "investigator",
        credibility_score: 0.75,
        relevance_score: 0.85,
        impact_on_option_a: -0.6,
        impact_on_option_b: 0.4,
        status: "admitted",
        created_at: new Date(now() + 200).toISOString(),
      },
      timestamp: new Date(now() + 200).toISOString(),
    });

    events.push({
      type: "belief.updated",
      payload: {
        round,
        agent_id: "agent_def_001",
        agent_type: "defender",
        belief_a: 0.2,
        belief_b: 0.8,
        delta: 0.08,
      },
      timestamp: new Date(now() + 300).toISOString(),
    });

    events.push({
      type: "agent.speak",
      payload: {
        agent_id: "agent_def_001",
        agent_type: "defender",
        name: "选项B代表",
        phase: "cross_exam",
        round,
        content:
          "E004 显示 B 轮前存活率仅 35%。这意味着接受 offer 有 65% 概率面临公司失败或裁员风险。",
        evidence_refs: ["E004"],
        belief_a: 0.2,
        belief_b: 0.8,
      },
      timestamp: new Date(now() + 1500).toISOString(),
    });

    events.push({
      type: "agent.speak",
      payload: {
        agent_id: "agent_pro_001",
        agent_type: "prosecutor",
        name: "选项A代表",
        phase: "cross_exam",
        round,
        content:
          "我方承认存活率数据，但原告已有 18 个月应急基金，即使公司失败也能从容找下家。风险可控。",
        evidence_refs: ["E001", "E004"],
        belief_a: 0.75,
        belief_b: 0.25,
      },
      timestamp: new Date(now() + 3500).toISOString(),
    });
  } else {
    events.push({
      type: "agent.speak",
      payload: {
        agent_id: "agent_pro_001",
        agent_type: "prosecutor",
        name: "选项A代表",
        phase: "cross_exam",
        round,
        content:
          "综合来看，原告财务缓冲充足、AI 赛道匹配、期权回报潜力大，接受 offer 是理性选择。",
        evidence_refs: ["E001", "E002"],
        belief_a: 0.78,
        belief_b: 0.22,
      },
      timestamp: new Date(now() + 1500).toISOString(),
    });

    events.push({
      type: "agent.speak",
      payload: {
        agent_id: "agent_def_001",
        agent_type: "defender",
        name: "选项B代表",
        phase: "cross_exam",
        round,
        content:
          "我方坚持认为家庭现金流约束优先。如果创业公司在 12 个月内无法提供稳定收入，家庭将面临压力。",
        evidence_refs: ["E003"],
        belief_a: 0.22,
        belief_b: 0.78,
      },
      timestamp: new Date(now() + 3500).toISOString(),
    });
  }

  return events;
}

export function buildClosingEvents(): CourtEvent[] {
  const now = Date.now;
  const events: CourtEvent[] = [];

  events.push({
    type: "phase.changed",
    payload: {
      previous_phase: "cross_exam",
      current_phase: "closing",
      message: "进入结案陈词阶段",
    },
    timestamp: new Date(now() + 100).toISOString(),
  });

  events.push({
    type: "agent.speak",
    payload: {
      agent_id: "agent_pro_001",
      agent_type: "prosecutor",
      name: "选项A代表",
      phase: "closing",
      round: 3,
      content:
        "综上所述，我方请求法官裁定接受创业公司 offer。原告已做好财务与心理准备，未来回报值得期待。",
      evidence_refs: ["E001", "E002"],
    },
    timestamp: new Date(now() + 1500).toISOString(),
  });

  events.push({
    type: "agent.speak",
    payload: {
      agent_id: "agent_def_001",
      agent_type: "defender",
      name: "选项B代表",
      phase: "closing",
      round: 3,
      content:
        "综上所述，我方请求法官裁定留在大厂。稳定收入与家庭责任不容忽视，冒险需慎之又慎。",
      evidence_refs: ["E003", "E004"],
    },
    timestamp: new Date(now() + 3500).toISOString(),
  });

  events.push({
    type: "phase.changed",
    payload: {
      previous_phase: "closing",
      current_phase: "deliberation",
      message: "书记员正在整理庭审记录，生成判决书",
    },
    timestamp: new Date(now() + 4500).toISOString(),
  });

  events.push({
    type: "phase.changed",
    payload: {
      previous_phase: "deliberation",
      current_phase: "verdict",
      message: "判决书已生成",
    },
    timestamp: new Date(now() + 6500).toISOString(),
  });

  events.push({
    type: "verdict.ready",
    payload: {
      verdict_id: "ver_001",
      summary: mockVerdict.summary,
      option_a_score: mockVerdict.option_a_score,
      option_b_score: mockVerdict.option_b_score,
    },
    timestamp: new Date(now() + 7000).toISOString(),
  });

  return events;
}

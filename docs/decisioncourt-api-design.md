# 决策庭（DecisionCourt）API 接口设计文档

> **版本**：v0.8
> **状态**：v0.5 增补 4 个 private MessageType（strategy_note / opponent_weakness / self_correction / evidence_eval）+ MemoryAuditPanel REST 端点；v0.6 增补 `GET /api/v1/courtrooms/:uuid/belief-diffs` + WS `belief.diff` / `belief.convergence` 事件；v0.7 整合文档结构 + ADR 提炼；**v0.8 新增 `GET /metrics` 端点（白盒化）+ HTTP `X-Request-ID` 头 / `trace_id` 字段（端到端 trace 串联）**。
> **目标**：定义决策庭前后端交互的 RESTful API 和 WebSocket 事件协议。
> **设计演进（已归档）**：[`docs/archive/memory-a2a-redesign-v1.2.md`](./archive/memory-a2a-redesign-v1.2.md)
> **2026-07-02 整理时同步 + 2026-07-02 v0.8 白盒化升级同步**：本版本号对齐后端代码实装现状（参见 [`docs/README.md`](./README.md)）。

## 1. 设计原则

1. **RESTful + WebSocket 混合**：状态查询用 REST，实时事件用 WebSocket。
2. **以庭审会话为核心**：所有接口路径以 `/courtrooms/:id` 开头。
3. **无认证设计**：MVP 匿名会话，通过 `session_uuid` 访问。
4. **事件驱动**：庭审阶段变化、Agent 发言、证据提交都通过 WebSocket 广播。
5. **幂等性**：用户操作接口支持幂等，避免重复触发。

---

## 2. 基础信息

| 项 | 内容 |
|---|---|
| Base URL | `http://localhost:8080/api/v1` |
| WebSocket URL | `ws://localhost:8080/ws/courtrooms/:id` |
| 数据格式 | JSON |
| 时区 | UTC |

---

## 3. RESTful API

### 3.1 庭审管理

#### 3.1.1 创建庭审

```http
POST /api/v1/courtrooms
```

**请求体**：

```json
{
  "title": "跳槽 vs 留下",
  "option_a": "接受创业公司 offer",
  "option_b": "留在现在的大厂",
  "context": "工作三年，目前在大厂做后端开发，创业公司是小团队核心岗位",
  "mode": "standard"
}
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "title": "跳槽 vs 留下",
    "option_a": "接受创业公司 offer",
    "option_b": "留在现在的大厂",
    "mode": "standard",
    "max_rounds": 3,
    "current_phase": "idle",
    "current_round": 0,
    "status": "active",
    "created_at": "2026-06-24T12:00:00Z"
  }
}
```

**字段说明**：

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `title` | string | 是 | 庭审标题 |
| `option_a` | string | 否 | 选项 A。如果未提供，进入问题澄清与选项生成阶段 |
| `option_b` | string | 否 | 选项 B。如果未提供，进入问题澄清与选项生成阶段 |
| `context` | string | 否 | 背景信息 |
| `mode` | string | 否 | `quick` / `standard` / `deep`，默认 `standard` |

**说明**：
- 如果用户提供了 `option_a` 和 `option_b`，直接进入庭审。
- 如果只提供了问题描述，系统会先进入 `clarification` 阶段，由 Agent 提问澄清，然后生成候选选项。

---

#### 3.1.2 获取庭审信息

```http
GET /api/v1/courtrooms/:session_uuid
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "title": "跳槽 vs 留下",
    "option_a": "接受创业公司 offer",
    "option_b": "留在现在的大厂",
    "mode": "standard",
    "max_rounds": 3,
    "current_phase": "cross_exam",
    "current_round": 2,
    "status": "active",
    "converged": false,
    "agents": [
      {
        "agent_uuid": "agent_pro_xxx",
        "agent_type": "prosecutor",
        "name": "控方律师",
        "belief_a": 0.72,
        "belief_b": 0.28
      }
    ],
    "created_at": "2026-06-24T12:00:00Z",
    "updated_at": "2026-06-24T12:05:00Z"
  }
}
```

---

#### 3.1.3 获取澄清问题

```http
GET /api/v1/courtrooms/:session_uuid/clarification
```

**说明**：当用户未提供两个明确选项时，系统返回需要澄清的问题列表。

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "current_phase": "clarification",
    "questions": [
      {
        "question_id": "q_001",
        "question": "你对现在工作最不满意的地方是什么？",
        "purpose": "识别当前工作的核心痛点"
      },
      {
        "question_id": "q_002",
        "question": "你有没有具体的跳槽目标或 offer？",
        "purpose": "判断是否有现实选择"
      }
    ]
  }
}
```

---

#### 3.1.4 提交澄清回答

```http
POST /api/v1/courtrooms/:session_uuid/clarification
```

**请求体**：

```json
{
  "answers": [
    {
      "question_id": "q_001",
      "answer": "晋升空间小，技术栈老化"
    },
    {
      "question_id": "q_002",
      "answer": "有一家创业公司 offer 和另一家大厂 offer"
    }
  ]
}
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "message": "澄清回答已提交"
  }
}
```

---

#### 3.1.5 获取候选选项

```http
GET /api/v1/courtrooms/:session_uuid/options
```

**说明**：基于用户问题和澄清回答，系统生成 2-3 个候选选项。

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "current_phase": "option_generation",
    "options": [
      {
        "option_id": "opt_a",
        "label": "留在现公司，争取内部晋升或转岗",
        "rationale": "风险最低，适合当前环境不确定的情况"
      },
      {
        "option_id": "opt_b",
        "label": "跳槽到同行业成熟公司",
        "rationale": "收入和稳定性都有保障，成长空间有限"
      },
      {
        "option_id": "opt_c",
        "label": "加入创业公司核心团队",
        "rationale": "高风险高回报，适合追求快速成长"
      }
    ]
  }
}
```

---

#### 3.1.6 确认选项

```http
POST /api/v1/courtrooms/:session_uuid/options
```

**请求体**：

```json
{
  "option_a": {
    "option_id": "opt_c",
    "label": "加入创业公司核心团队"
  },
  "option_b": {
    "option_id": "opt_a",
    "label": "留在现公司，争取内部晋升或转岗"
  }
}
```

**说明**：用户从候选选项中选择两个，或手动输入自己的选项。

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "option_a": "加入创业公司核心团队",
    "option_b": "留在现公司，争取内部晋升或转岗",
    "current_phase": "idle",
    "message": "选项已确认，可以开始庭审"
  }
}
```

---

#### 3.1.7 开始庭审

```http
POST /api/v1/courtrooms/:session_uuid/start
```

**说明**：从 `idle` 阶段进入 `opening` 阶段，触发控辩双方开场陈述。

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "current_phase": "opening",
    "message": "庭审已开始"
  }
}
```

---

#### 3.1.8 进入下一阶段

```http
POST /api/v1/courtrooms/:session_uuid/next
```

**说明**：在当前阶段完成后，手动推进到下一阶段。系统会自动判断是否可以推进。

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "current_phase": "cross_exam",
    "current_round": 1
  }
}
```

---

### 3.2 证据管理

#### 3.2.1 提交证据

```http
POST /api/v1/courtrooms/:session_uuid/evidences
```

**请求体**：

```json
{
  "content": "我已存下 18 个月生活费的应急基金",
  "type": "fact",
  "source": "user"
}
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "evidence_id": "E001",
    "content": "我已存下 18 个月生活费的应急基金",
    "type": "fact",
    "source": "user",
    "submitted_by": "user",
    "credibility_score": 0.9,
    "relevance_score": 0.85,
    "impact_on_option_a": 0.7,
    "impact_on_option_b": -0.3,
    "status": "admitted",
    "created_at": "2026-06-24T12:06:00Z"
  }
}
```

---

#### 3.2.2 获取证据列表

```http
GET /api/v1/courtrooms/:session_uuid/evidences
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "evidences": [
      {
        "evidence_id": "E001",
        "content": "我已存下 18 个月生活费的应急基金",
        "type": "fact",
        "status": "admitted"
      }
    ]
  }
}
```

---

### 3.3 用户操作

#### 3.3.1 执行用户操作

```http
POST /api/v1/courtrooms/:session_uuid/actions
```

**请求体**：

```json
{
  "action": "direct_verdict"
}
```

**支持的 action 类型**：

| action | 说明 | 请求体示例 |
|---|---|---|
| `direct_verdict` | 直接判决，跳过剩余轮次 | `{}` |
| `skip_agent` | 跳过当前 Agent 发言 | `{}` |
| `request_search` | 用户要求调查员搜索（中立搜证，结果进入 InvestigationFinding） | `{"query": "2026 年 AI 创业公司融资情况"}` |
| `answer_question` | 回答 Agent 主动提问 | `{"question_id": "q_001", "answer": "月收入 3 万"}` |
| `pause` | 暂停庭审 | `{}` |
| `resume` | 恢复庭审 | `{}` |

> **v0.2 修订**：`dispatch_investigator` 不再作为用户 action。它是控辩方 LLM 通过 `agent.cot_step(tool_call, tool=investigator_search)` 内部决策触发的，由后端 `courtroom.Service.DispatchInvestigator` 处理，**不**走 `/actions` REST 端点。

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "action": "direct_verdict",
    "current_phase": "closing"
  }
}
```

---

### 3.4 消息与历史

#### 3.4.1 获取庭审消息历史

```http
GET /api/v1/courtrooms/:session_uuid/messages?limit=50&offset=0
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "total": 15,
    "messages": [
      {
        "id": "msg_001",
        "agent_id": "agent_pro_xxx",
        "agent_type": "prosecutor",
        "phase": "opening",
        "round": 0,
        "action_type": "speak",
        "content": "尊敬的法官，我方将证明接受创业公司 offer 是更优选择。",
        "evidence_refs": [],
        "created_at": "2026-06-24T12:01:00Z"
      }
    ]
  }
}
```

---

### 3.5 调查发现

> v0.2 新增。调查员被控辩方派遣后的搜索结果，**不**写入 `evidences` 表，独立成 `InvestigationFinding`（详见 `docs/decisioncourt-db-design.md` §3.9）。

#### 3.5.1 获取调查发现列表

```http
GET /api/v1/courtrooms/:session_uuid/investigations
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "session_uuid": "court_abc123",
    "findings": [
      {
        "finding_id": "f-xxx",
        "dispatcher": "prosecutor",
        "investigator": "investigator",
        "query": "睡眠对健康的好处 科学证据",
        "summary": "前 3 条搜索结果摘要",
        "raw_results": [
          { "title": "睡眠不足的 7 大危害", "url": "https://...", "content": "..." }
        ],
        "result_count": 10,
        "source_provider": "bocha",
        "created_at": "2026-06-29T12:01:00Z"
      }
    ]
  }
}
```

**说明**：
- 按 `created_at` 升序（旧 → 新），前端 InvestigatorPanel 追加展示
- `findings` 数组为空时返回 `[]`，**不**返回 `null`
- 找不到庭审时返回 404 `{"code":1002,...}`

#### 3.5.2 获取信念变化审计（v0.6）

```http
GET /api/v1/courtrooms/:session_uuid/belief-diffs
GET /api/v1/courtrooms/:session_uuid/belief-diffs?agent=prosecutor
GET /api/v1/courtrooms/:session_uuid/belief-diffs?round=2
```

**Query 参数**（均可选）：
- `agent`：按 agent_type 过滤，取值 `prosecutor` | `defender` | `investigator` | `clerk` | `judge`
- `round`：按 round 过滤（正整数）

**响应**：

```json
{
  "code": 0,
  "data": {
    "diffs": [
      {
        "id": "diff-uuid-1",
        "round": 2,
        "phase": "evidence",
        "agent_type": "prosecutor",
        "evidence_id": "ev-uuid-1",
        "source": "evidence",
        "direction": "supports_a",
        "prior_belief_a": 0.75,
        "posterior_belief_a": 0.78,
        "delta_belief_a": 0.03,
        "prior_logit": 1.0986,
        "posterior_logit": 1.2736,
        "evidence_weight": 0.504,
        "weaken_factor": 1.0,
        "reason": "新证据：选项 A 的关键优势",
        "created_at": "2026-07-01T10:15:30Z"
      }
    ],
    "count": 1
  }
}
```

**说明**：
- `diffs` 数组按 `round, created_at` 升序（旧 → 新）
- `weaken_factor` < 1.0 表示该条证据被对方律师通过 weaken 边削弱过
- `source` 为 `evidence` / `weaken` / `anchor_pull`，锚定回拉是 v0.6 引擎自带特性
- `diffs` 数组为空时返回 `[]`，**不**返回 `null`
- 找不到庭审时返回 404 `{"code":1002,...}`
- 不存在的 `round` 过滤值返回 400 `{"code":1001,...}`
- 后端 belief 引擎未启用（diffRepo == nil）时返回 200 + 空数组（向后兼容老部署）

#### 3.6 判决书

#### 3.6.1 获取判决书

```http
GET /api/v1/courtrooms/:session_uuid/verdict
```

**响应**：

```json
{
  "code": 0,
  "data": {
    "verdict_id": "ver_001",
    "session_uuid": "court_abc123",
    "summary": "建议接受创业公司 offer，但需设定 12 个月评估节点。",
    "option_a_score": 0.68,
    "option_b_score": 0.52,
    "consensus_points": ["财务缓冲充足", "创业方向与个人兴趣匹配"],
    "divergence_points": ["收入稳定性", "长期职业风险"],
    "recommendation": "接受 offer，但保留与大厂前同事的联系，并明确退出机制。",
    "content": "# 决策判决书\n\n## 一、双方主张...",
    "created_at": "2026-06-24T12:15:00Z"
  }
}
```

---

#### 3.6.2 反馈判决书

```http
POST /api/v1/courtrooms/:session_uuid/verdict/feedback
```

**请求体**：

```json
{
  "feedback": "helpful"
}
```

**说明**：`feedback` 可选 `helpful` / `not_helpful`。

#### 3.6.3 庭审导出（v0.5+ 新增）

```http
GET /api/v1/courtrooms/:session_uuid/export
```

**说明**：返回自包含的庭审 JSON 快照，verdict 页面"导出 JSON"按钮调用。`Content-Disposition: attachment` 强制浏览器下载。

**响应 Payload**（节选）：

```json
{
  "export_version": "v1",
  "exported_at": "2026-07-01T10:30:00Z",
  "session": {
    "session_uuid": "court_abc123",
    "title": "跳槽 vs 留下",
    "option_a": "接受创业公司 offer",
    "option_b": "留在现在的大厂",
    "context": "...",
    "mode": "standard",
    "max_rounds": 3,
    "current_phase": "verdict",
    "current_round": 3,
    "status": "completed",
    "converged": false,
    "created_at": "2026-07-01T10:00:00Z",
    "updated_at": "2026-07-01T10:25:00Z"
  },
  "verdict": {
    "summary": "建议接受创业公司 offer...",
    "trial_summary": "控方在第 2 轮抛出 E001 的数据来源质疑...",
    "option_a_score": 0.68,
    "option_b_score": 0.52,
    "consensus_points": "[\"财务缓冲充足\"]",
    "divergence_points": "[\"收入稳定性\"]",
    "recommendation": "接受 offer...",
    "content": "# 决策判决书\n\n## 一、双方主张...",
    "created_at": "2026-07-01T10:25:00Z"
  },
  "evidences": [
    {
      "evidence_id": "E001",
      "type": "fact",
      "source": "user",
      "content": "...",
      "credibility_score": 0.9,
      "impact_on_option_a": 0.7,
      "impact_on_option_b": -0.3,
      "status": "admitted",
      "created_at": "2026-07-01T10:05:00Z"
    }
  ],
  "messages": [
    {
      "agent_type": "prosecutor",
      "phase": "opening",
      "round": 0,
      "action_type": "speak",
      "content": "...",
      "created_at": "2026-07-01T10:01:00Z"
    }
  ],
  "a2a_messages": [
    {
      "message_uuid": "msg_xxx",
      "round": 2,
      "phase": "cross_exam",
      "from_agent": "prosecutor",
      "to_agent": "prosecutor",
      "message_type": "strategy_note",
      "visibility": "private",
      "payload": "{...}",
      "created_at": "2026-07-01T10:15:00Z"
    }
  ]
}
```

**关键约束**：
1. **`a2a_messages` 字段只含 `ListVisibleTo("user")` 能看到的** —— 通过 `a2a.Bus.ListVisibleTo(sessionID, "user")` 复用 SQL 隔离，**对家 private memory（辩方/控方发给自己的）不会泄漏到导出文件**
2. **`Content-Disposition: attachment; filename="decisioncourt-<uuid>-<ts>.json"`** —— 浏览器自动下载
3. **失败返回 5xx**（code 1500）—— 用户在 verdict 页面看到"导出失败"提示

**为什么不后端生成 PDF**：
- 后端加 PDF 库（`gofpdf` / `unidoc`）= ~30MB 依赖 + 维护成本
- 浏览器原生 PDF 质量足够（用户打印用）
- 当前用户量下，PDF 导出使用率 < 5%，不值得优化
- 客户端 `window.print()` 配合 `@media print` 样式即可

---

---

## 4. WebSocket 事件协议

### 4.1 连接方式

```
ws://localhost:8080/ws/courtrooms/:session_uuid
```

连接成功后，后端会自动将客户端加入该庭审的 room。

### 4.2 通用事件格式

```json
{
  "type": "agent.speak",
  "payload": {},
  "timestamp": "2026-06-24T12:01:00Z"
}
```

### 4.3 后端 → 前端事件

#### 4.3.1 clarification.questions

需要用户澄清的问题。

```json
{
  "type": "clarification.questions",
  "payload": {
    "questions": [
      {
        "question_id": "q_001",
        "question": "你对现在工作最不满意的地方是什么？",
        "purpose": "识别当前工作的核心痛点"
      }
    ]
  },
  "timestamp": "2026-06-24T12:00:30Z"
}
```

---

#### 4.3.2 options.generated

系统生成候选选项。

```json
{
  "type": "options.generated",
  "payload": {
    "options": [
      {
        "option_id": "opt_a",
        "label": "留在现公司，争取内部晋升或转岗",
        "rationale": "风险最低"
      },
      {
        "option_id": "opt_b",
        "label": "跳槽到同行业成熟公司",
        "rationale": "稳定且有成长"
      }
    ]
  },
  "timestamp": "2026-06-24T12:01:00Z"
}
```

---

#### 4.3.3 agent.speak

Agent 发言事件。

```json
{
  "type": "agent.speak",
  "payload": {
    "agent_id": "agent_pro_xxx",
    "agent_type": "prosecutor",
    "name": "控方律师",
    "phase": "cross_exam",
    "round": 1,
    "content": "这份证据强力支持我方观点...",
    "evidence_refs": ["E001"],
    "belief_a": 0.72,
    "belief_b": 0.28
  },
  "timestamp": "2026-06-24T12:01:00Z"
}
```

---

#### 4.3.2 evidence.added

新证据加入证据板。

```json
{
  "type": "evidence.added",
  "payload": {
    "evidence_id": "E002",
    "content": "该赛道 2026 年增长率预计 30%",
    "type": "data",
    "source": "web_search",
    "submitted_by": "investigator",
    "impact_on_option_a": 0.6,
    "impact_on_option_b": -0.2
  },
  "timestamp": "2026-06-24T12:02:00Z"
}
```

---

#### 4.3.3 evidence.challenged

证据被对方质疑。

```json
{
  "type": "evidence.challenged",
  "payload": {
    "evidence_id": "E002",
    "agent_id": "agent_def_xxx",
    "agent_type": "defender",
    "reason": "该数据来源不明，且未说明统计口径。"
  },
  "timestamp": "2026-06-24T12:02:30Z"
}
```

---

#### 4.3.4 belief.updated

信念度更新，用于绘制立场变化曲线。

```json
{
  "type": "belief.updated",
  "payload": {
    "round": 1,
    "agent_id": "agent_pro_xxx",
    "agent_type": "prosecutor",
    "belief_a": 0.72,
    "belief_b": 0.28,
    "delta": 0.05
  },
  "timestamp": "2026-06-24T12:02:00Z"
}
```

#### 4.3.4a belief.diff（v0.6 新增）

单条信念变化的结构化 diff，每条证据被引擎应用到某 agent 时广播一次。
前端 BeliefDiffCard 渲染成时间线条目；可重放/审计。

```json
{
  "type": "belief.diff",
  "payload": {
    "id": "diff-uuid-1",
    "session_id": "session-uuid",
    "round": 2,
    "phase": "evidence",
    "agent_type": "prosecutor",
    "evidence_id": "ev-uuid-1",
    "source": "evidence",
    "direction": "supports_a",
    "prior_belief_a": 0.75,
    "posterior_belief_a": 0.78,
    "delta_belief_a": 0.03,
    "prior_logit": 1.0986,
    "posterior_logit": 1.2736,
    "evidence_weight": 0.504,
    "weaken_factor": 1.0,
    "reason": "新证据：选项 A 的关键优势",
    "created_at": "2026-07-01T10:15:30Z"
  },
  "timestamp": "2026-07-01T10:15:30Z"
}
```

字段语义：
- `source`: `evidence` / `weaken` / `anchor_pull`
- `direction`: `supports_a` / `supports_b` / `neutral`
- `weaken_factor` < 1.0 表示该条证据被对方 weaken 边削弱
- `prior_logit` / `posterior_logit` 是 log-odds 域值，可用于离线重放

#### 4.3.4b belief.convergence（v0.6 新增）

当 v0.6 多信号收敛判断触发时广播一次（之后 trial 结束）。
前端 ConvergenceBadge 渲染四种原因对应的颜色/图标。

```json
{
  "type": "belief.convergence",
  "payload": {
    "reason": "reasoning_oscillation",
    "round": 3,
    "converged": true,
    "reason_message": "第 3 轮检测到律师发言高度重复，辩论已陷入循环，触发提前判决"
  },
  "timestamp": "2026-07-01T10:18:00Z"
}
```

`reason` 取值（按优先级）：
1. `reasoning_oscillation` — 律师发言高度重复
2. `consensus` — 控辩双方都偏向同一侧
3. `belief_stable` — 连续 N 轮单轮最大 Δ < 0.05
4. `max_rounds` — 达到最大轮次兜底

---

#### 4.3.5 phase.changed

庭审阶段变化。

```json
{
  "type": "phase.changed",
  "payload": {
    "previous_phase": "evidence",
    "current_phase": "cross_exam",
    "current_round": 1,
    "message": "进入第 1 轮质证"
  },
  "timestamp": "2026-06-24T12:02:00Z"
}
```

---

#### 4.3.6 user.action.required

需要用户介入，如回答 Agent 提问。

```json
{
  "type": "user.action.required",
  "payload": {
    "action": "answer_question",
    "question_id": "q_001",
    "question": "你的月收入范围是多少？",
    "purpose": "评估你的财务风险承受能力",
    "skip_allowed": true
  },
  "timestamp": "2026-06-24T12:03:00Z"
}
```

---

#### 4.3.7 search.started / search.completed

> v0.2 修订：dispatch/completed 不再以 `evidence_ids` 为 payload —— 调查发现独立于证据表。

调查员搜索状态。`search.started` 在 LLM 派遣调查员时立刻推送；`search.completed` 用 `defer` 包裹保证成功/失败都发出。

```json
{
  "type": "search.started",
  "payload": {
    "dispatcher": "prosecutor",
    "query": "2026 年 AI 创业公司融资情况",
    "started_at": "2026-06-29T12:01:00Z"
  }
}
```

```json
{
  "type": "search.completed",
  "payload": {
    "dispatcher": "prosecutor",
    "query": "2026 年 AI 创业公司融资情况",
    "success": true,
    "finding_id": "f-xxx",
    "result_count": 10,
    "summary": "前 3 条搜索结果摘要",
    "source_provider": "bocha",
    "raw_results": [
      { "title": "...", "url": "https://...", "content": "..." }
    ]
  }
}
```

```json
{
  "type": "search.completed",
  "payload": {
    "dispatcher": "prosecutor",
    "query": "...",
    "success": false,
    "error": "search provider timeout"
  }
}
```

---

#### 4.3.8 a2a.message

A2A 消息总线广播事件，用于审计与可视化。**公开消息**会携带 payload，**私有消息**仅广播 envelope（不暴露 payload，避免泄漏）。

```json
{
  "type": "a2a.message",
  "payload": {
    "message_uuid": "msg_xxx",
    "from": "prosecutor",
    "to": "defender",
    "message_type": "speech",            // 见下方 v0.5 MessageType 全量列表
    "round": 1,
    "phase": "cross_exam",
    "visibility": "public",              // public | private
    "payload": { ... }                   // 仅 visibility=public 时出现
  }
}
```

**v0.5 MessageType 全量列表**（v0.5 新增 4 个 private 类型）：

| MessageType | Visibility | 说明 | v0.5 来源 |
|---|---|---|---|
| `speech` | public | 开庭/质证/结案发言 | v0.1 |
| `evidence` | public | 新证据提交 | v0.1 |
| `challenge` | public | 反驳对方论证 | v0.1 |
| `inquiry` | public | 提问 | v0.1 |
| `verdict_task` | public | 判决任务派发 | v0.1 |
| `dispatch` | public | 调查员派遣 | v0.1 |
| `report` | public | 调查员回报 | v0.1 |
| `dispatch_investigator` | public | 调查员派遣（v0.2 改名为更具体的语义） | v0.2 |
| `investigation_report` | public | 调查员回报（v0.2 改名） | v0.2 |
| **`strategy_note`** 🆕 | **private** | 私有策略笔记 | **v0.5 PR 1** |
| **`opponent_weakness`** 🆕 | **private** | 私有：对方弱点 | **v0.5 PR 1** |
| **`self_correction`** 🆕 | **private** | 私有：自我修正 | **v0.5 PR 1** |
| **`evidence_eval`** 🆕 | **private** | 私有：证据内部评估 | **v0.5 PR 1** |

调查员派遣/回报的 A2A 消息（**v0.2 修订：可见性为 public**——类比正常庭审记录）：

```json
{
  "type": "a2a.message",
  "payload": {
    "message_uuid": "msg_yyy",
    "from": "prosecutor",
    "to": "investigator",
    "message_type": "dispatch_investigator",
    "round": 1,
    "phase": "cross_exam",
    "visibility": "public",
    "payload": { "query": "睡眠对健康的好处" }
  }
}
```

```json
{
  "type": "a2a.message",
  "payload": {
    "message_uuid": "msg_zzz",
    "from": "investigator",
    "to": "prosecutor",
    "message_type": "investigation_report",
    "round": 1,
    "phase": "cross_exam",
    "visibility": "public",
    "payload": { "finding_id": "f-xxx", "result_count": 10, "summary": "..." }
  }
}
```

**v0.5+ 私有记忆广播示例**（用于前端 MemoryAuditPanel，含结构化字段）：

```json
{
  "type": "a2a.message",
  "payload": {
    "id": "uuid-a2a-msg-xxx",
    "message_uuid": "mem_pro_001",
    "session_uuid": "ws-room-key-xxx",
    "from": "prosecutor",
    "to": "prosecutor",
    "message_type": "strategy_note",
    "round": 1,
    "phase": "cross_exam",
    "visibility": "private",
    "created_at": "2026-06-30T12:00:00Z",
    "payload": {
      "memory_type": "strategy_note",
      "stance": "pro_a",
      "confidence": 0.82,
      "reasoning": "辩方没反驳 E001 的数据来源，是核心弱点",
      "content": "立场 支持选项A · 置信度 82% · 辩方没反驳 E001 的数据来源...",
      "linked_evidence_ids": ["E001"]
    }
  }
}
```

**v0.5+ 字段说明**：

| 字段 | 必填 | 说明 |
|---|---|---|
| `session_uuid` | ✅ | 必须是 `court_sessions.session_uuid`（字符串列）。Bus.Send 用它当 WebSocket hub room key。**与 DB 主键 `session_id` 是不同字段** |
| `from` | ✅ | Agent 类型字符串（`prosecutor` / `defender` / `investigator`），**不是 Agent UUID** |
| `payload.stance` | 🟡 | strategy_note 类型必填；4 种 kind 的视觉差异化靠这个 |
| `payload.confidence` | 🟡 | 0..1；置信度条渲染依据 |
| `payload.reasoning` | 🟡 | LLM 推理链，独立于 fallback content |

**v0.5+ 安全边界**：

1. **`visibility=private` 时 WebSocket 广播保留 payload**（v0.5+ 行为变更）：前端 MemoryAuditPanel 通过 WebSocket 实时 hydrate，不再走 REST 端点拉取。这是因为 PR 4 实装时 REST 端点被撤销，改为走 WebSocket 实时推送。
2. **前端有 `mapFromToAgentType()` 归一化**：若 `from` 是 agent UUID（早期版本残留），落到 investigator 兜底，不报错。
3. **`bus_test.go` schema freeze**：`TestBus_Send_BroadcastEnvelopeMatchesFrontendContract` 锁住所有字段名（`from` / `to` / `round` / `phase` / `id` / `created_at` / `payload`），任何人改广播字段名都会立刻爆红。

**前端渲染路径**（`MemoryTimeline.tsx`）：

| 数据形态 | 渲染 |
|---|---|
| `stance` / `confidence` / `reasoning` 齐全 | **结构化卡片**：立场 chip + 置信度条 + 推理段 + 引用证据 |
| 只 `content`（老数据） | **纯文本 fallback** |
| `redacted=true`（真实法庭模式） | 隐藏 content，仅显示 kind chip + 计数 |

#### 4.3.8.1 agent.thinking_started / agent.thinking_finished（v0.2 新增）

> ReAct 实时性：律师开始思考时**立即**推送 `thinking_started`，前端立刻显示云朵；ReAct runner 完成后推送 `thinking_finished`，前端延迟 220ms 卸载云朵。

```json
{
  "type": "agent.thinking_started",
  "payload": { "agent_id": "agent_pro_xxx", "agent_type": "prosecutor" }
}
```

```json
{
  "type": "agent.thinking_finished",
  "payload": { "agent_id": "agent_pro_xxx", "agent_type": "prosecutor" }
}
```

#### 4.3.8.2 agent.cot_step（v0.2 新增）

> ReAct 思维链每步推送。前端 CoT 折叠面板累积展示。`tool_call` 类型的 step 含 tool/input/output。

```json
{
  "type": "agent.cot_step",
  "payload": {
    "agent_id": "agent_pro_xxx",
    "agent_type": "prosecutor",
    "step": {
      "kind": "thought",
      "content": "我需要先搜索一下对方行业相关的数据"
    }
  }
}
```

```json
{
  "type": "agent.cot_step",
  "payload": {
    "agent_id": "agent_pro_xxx",
    "agent_type": "prosecutor",
    "step": {
      "kind": "tool_call",
      "tool": "investigator_search",
      "input": { "query": "..." },
      "output": "搜索完成：新增调查发现 finding_id=..."
    }
  }
}
```

#### 4.3.8.3 agent.speak_chunk（v0.2 新增）

> LLM 流式发言每个 token 推送。前端用 `flushSync` 强制同步 commit 实现逐字渲染。

```json
{
  "type": "agent.speak_chunk",
  "payload": {
    "agent_id": "agent_pro_xxx",
    "agent_type": "prosecutor",
    "chunk": "尊敬的审判",
    "accumulated": "尊敬的审判长、审判员：..."
  }
}
```

---

#### 4.3.9 verdict.ready

判决书已生成。

```json
{
  "type": "verdict.ready",
  "payload": {
    "verdict_id": "ver_001",
    "summary": "建议接受创业公司 offer...",
    "option_a_score": 0.68,
    "option_b_score": 0.52
  }
}
```

---

#### 4.3.9 error

错误事件。

```json
{
  "type": "error",
  "payload": {
    "code": "LLM_TIMEOUT",
    "message": "模型调用超时，请稍后重试"
  }
}
```

---

### 4.4 前端 → 后端事件

前端通过 WebSocket 发送用户操作，替代部分 REST API 请求。

#### 4.4.1 user.action

```json
{
  "type": "user.action",
  "payload": {
    "action": "submit_evidence",
    "content": "我有 18 个月应急基金",
    "type": "fact"
  }
}
```

支持的 action：
- `submit_evidence`
- `answer_question`
- `request_search`
- `direct_verdict`
- `skip_agent`
- `pause`
- `resume`

---

## 5. 错误码设计

| 错误码 | HTTP 状态码 | 说明 |
|---|---|---|
| `0` | 200 | 成功 |
| `1001` | 400 | 请求参数错误 |
| `1002` | 404 | 庭审不存在 |
| `1003` | 409 | 当前阶段不允许该操作 |
| `2001` | 500 | LLM 调用失败 |
| `2002` | 500 | 搜索服务不可用 |

---

## 6. 幂等性设计

- 所有 `POST /actions` 请求需要携带 `client_request_id`（UUID）。
- 后端记录已处理的 `client_request_id`，重复请求返回相同结果。
- 防止用户快速点击导致的重复触发。

---

## 7. 安全性考虑

1. **无认证但有 rate limit**：匿名用户通过 IP + session 限流。
2. **输入校验**：所有用户输入经过 validator 校验长度和内容。
3. **WebSocket room 隔离**：用户只能加入自己创建的庭审 room。
4. **LLM prompt injection 防护**：用户输入不会直接拼接到 system prompt 中。

---

## 8. 下一步

API 接口设计已完成。接下来可以进入：

1. **Agent 状态机设计文档**：庭审阶段流转、Agent 调用时序
2. **Prompt 设计文档**：控方、辩方、调查员、书记员的 system prompt

是否继续写 **Agent 状态机与 Prompt 设计文档**？

---

## 9. v0.8 白盒化端点（Observability）

### 9.1 `GET /metrics`

返回当前所有指标快照（JSON 格式）。

**请求**：

```http
GET /metrics HTTP/1.1
Host: localhost:8080
```

**响应（200 OK）**：

```json
{
  "code": 0,
  "data": {
    "timestamp": "2026-07-02T00:35:21.123Z",
    "counters": {
      "courtroom_state_transition_total": [
        {"labels": {"from": "idle", "to": "opening"}, "value": 12},
        {"labels": {"from": "opening", "to": "cross_exam"}, "value": 11}
      ],
      "a2a_message_throughput_total": [
        {"labels": {"event_type": "agent.speak"}, "value": 256},
        {"labels": {"event_type": "phase.changed"}, "value": 32}
      ]
    },
    "gauges": {
      "budget_ratio": [
        {"labels": {"session_uuid": "abc-123"}, "value": 0.42}
      ]
    },
    "histograms": {
      "llm_call_duration_seconds": [
        {
          "labels": {"agent_type": "prosecutor", "model": "deepseek-chat"},
          "count": 45,
          "sum": 32.4,
          "buckets": [
            {"le": 0.5, "count": 12},
            {"le": 1.0, "count": 28},
            {"le": 5.0, "count": 42},
            {"le": 30.0, "count": 45}
          ]
        }
      ],
      "http_request_duration_seconds": [
        {
          "labels": {"path": "/api/v1/courtrooms/:session_uuid", "method": "POST", "status": "200"},
          "count": 12,
          "sum": 1.234,
          "buckets": [...]
        }
      ]
    }
  }
}
```

**错误响应（503 Service Unavailable）**：

```json
{"code": 1500, "message": "metrics not configured"}
```

> 当前为 JSON 格式输出。**未来可加 `?format=prometheus` 切换为 `text/plain; version=0.0.4`** 格式对接 Prometheus。

### 9.2 `X-Request-ID` 头

所有 HTTP 响应都包含 `X-Request-ID` 响应头：

- **客户端可带**：前端 / curl 可在请求时带 `X-Request-ID: <uuid>`，服务端会原样回写
- **缺失时生成**：服务端自动生成 36 字符伪 UUID
- **用途**：通过 `X-Request-ID` 在 `decision_events` 表 / `agent_gateway` 文件日志中检索全链路 trace

```http
GET /api/v1/courtrooms/abc/evidences HTTP/1.1
X-Request-ID: 7f3a-bc12-...

HTTP/1.1 200 OK
X-Request-ID: 7f3a-bc12-...
Content-Type: application/json
```

### 9.3 WebSocket `trace_id` 字段

WebSocket 消息 payload 中可携带 `trace_id` 字段（v0.8 起）：

```json
{
  "type": "user.action",
  "payload": {
    "action": "submit_evidence",
    "content": "...",
    "trace_id": "7f3a-bc12-..."
  }
}
```

服务端会优先使用此 `trace_id` 作为 ctx.Trace.RequestID，使后端业务事件的 trace 串联到客户端。如果缺失则生成新 trace_id。

### 9.4 业务事件落库（`decision_events` 表）

`decision_events` 表记录所有业务级 span 关闭事件（v0.8 起）。可按以下维度查询：

- `SELECT * FROM decision_events WHERE session_uuid = ? ORDER BY created_at` —— 单 session 全链路
- `SELECT * FROM decision_events WHERE request_id = ?` —— 单 trace
- `SELECT * FROM decision_events WHERE event_type LIKE 'state_transition%'` —— 仅状态机迁移

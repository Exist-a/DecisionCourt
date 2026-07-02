# 决策庭（DecisionCourt）数据库设计文档

> **版本**：v0.8
> **状态**：v0.2 新增 `investigation_findings` 表（调查发现与证据严格分离）；v0.5+ 新增 `verdicts.trial_summary` 字段；v0.6 新增 `belief_diffs` 表（信念变化审计 trail）+ `evidence_weaken_links` 表（异构论辩图谱 weaken 边）+ A2A `Message.SessionUUID` 字段区分 DB 主键与 WS room key；**v0.8 新增 `decision_events` 表（业务级 span / 状态机迁移审计）**。
> **目标**：定义决策庭 MVP 所需的数据表结构、字段含义和关联关系。
> **2026-07-02 整理时同步 + 2026-07-02 v0.8 白盒化升级同步**：本版本号对齐后端 GORM model 实装现状（参见 [`docs/README.md`](./README.md)）。

---

## 1. 设计原则

1. **以庭审为中心**：所有表都围绕 `court_sessions` 展开。
2. **可审计**：每条 Agent 发言、每个证据、每次 LLM 调用都记录。
3. **支持实时状态恢复**：庭审状态以数据库为准，Redis 仅做缓存。
4. **为 Agent Gateway 预留字段**：`llm_calls` 表提前记录 Token 和成本，为第二阶段优化做准备。
5. **避免过度设计**：MVP 不需要用户表、权限表、组织表。

---

## 2. ER 关系图

```
┌─────────────────┐
│ court_sessions  │
└────────┬────────┘
         │ 1:N
    ┌────┴────┬────────┬────────┬────────────┬───────────┬────────────┬───────────────────┐
    ▼         ▼        ▼        ▼            ▼           ▼            ▼                   ▼
┌───────┐ ┌─────────┐ ┌──────┐ │belief_      │ │  verdicts  │ │ llm_calls │ ┌──────────────────────┐
│agents │ │evidences│ │ mess.│ │snapshots    │ └────────────┘ └───────────┘ │investigation_findings│
└───────┘ └─────────┘ └──────┘ └─────────────┘                             └──────────────────────┘
                                  │
                                  │ 1:N
                                  ▼
                          ┌──────────────┐
                          │  search_logs │  ← 历史；MVP 起调查发现走 investigation_findings
                          └──────────────┘
```

---

## 3. 表结构设计

### 3.1 court_sessions（庭审会话）

存储每场庭审的基本信息和当前状态。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_uuid` | VARCHAR(36) UNIQUE | 对外暴露的会话 ID |
| `title` | VARCHAR(255) | 庭审标题，如"跳槽 vs 留下" |
| `option_a` | VARCHAR(255) | 选项 A，如"接受创业公司 offer" |
| `option_b` | VARCHAR(255) | 选项 B，如"留在现在大厂" |
| `context` | TEXT | 用户提供的背景信息 |
| `mode` | VARCHAR(20) | 庭审模式：`quick` / `standard` / `deep` |
| `max_rounds` | INT | 最大质证轮数 |
| `current_phase` | VARCHAR(50) | 当前阶段：`idle` / `clarification` / `option_generation` / `opening` / `evidence` / `cross_exam` / `closing` / `deliberation` / `verdict` |
| `current_round` | INT | 当前质证轮数，默认 0 |
| `status` | VARCHAR(20) | 会话状态：`active` / `paused` / `completed` / `aborted` |
| `converged` | BOOLEAN | 是否已触发智能收敛 |
| `created_at` | TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | 更新时间 |

**索引**：
- `session_uuid`（唯一查询）
- `status` + `updated_at`（清理过期会话）

---

### 3.2 agents（Agent 配置与状态）

存储每场庭审中的 Agent 角色和当前信念状态。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `agent_uuid` | VARCHAR(36) UNIQUE | Agent 唯一标识 |
| `agent_type` | VARCHAR(50) | 类型：`prosecutor` / `defender` / `investigator` / `clerk` |
| `name` | VARCHAR(100) | 显示名称，如"控方律师" |
| `role` | TEXT | 角色描述 |
| `belief_a` | FLOAT | 对选项 A 的信念度，范围 [0, 1] |
| `belief_b` | FLOAT | 对选项 B 的信念度，范围 [0, 1] |
| `model` | VARCHAR(50) | 使用的模型，如 `deepseek-chat` |
| `temperature` | FLOAT | 采样温度 |
| `system_prompt` | TEXT | system prompt 内容 |
| `status` | VARCHAR(20) | 状态：`active` / `inactive` |
| `created_at` | TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | 更新时间 |

**约束**：
- `belief_a + belief_b = 1`
- 每个 session 每种 `agent_type` 最多一条记录（MVP 设计）

**索引**：
- `session_id` + `agent_type`（联合唯一）

---

### 3.3 evidences（证据）

存储庭审中的所有证据。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `evidence_id` | VARCHAR(50) | 展示 ID，如 `E001` |
| `type` | VARCHAR(30) | 类型：`fact` / `data` / `expert_opinion` / `preference` / `constraint` |
| `source` | VARCHAR(30) | 来源：`user` / `web_search` / `agent_question` / `clarification_answer` |
| `content` | TEXT | 证据内容 |
| `url` | VARCHAR(500) | 来源链接（WebSearch 时有） |
| `submitted_by` | VARCHAR(50) | 提交者：`user` / `prosecutor` / `defender` / `investigator` |
| `credibility_score` | FLOAT | 可信度 [0, 1] |
| `relevance_score` | FLOAT | 相关性 [0, 1] |
| `impact_on_option_a` | FLOAT | 对选项 A 的影响 [-1, 1] |
| `impact_on_option_b` | FLOAT | 对选项 B 的影响 [-1, 1] |
| `status` | VARCHAR(20) | 状态：`admitted` / `challenged` / `rejected` |
| `challenge_reason` | TEXT | 被质疑原因 |
| `created_at` | TIMESTAMP | 创建时间 |

**索引**：
- `session_id` + `evidence_id`（联合唯一）
- `session_id` + `status`

---

### 3.4 messages（庭审消息记录）

存储 Agent 发言、用户操作、系统事件等。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `agent_id` | UUID FK | 发言 Agent（系统事件可为 NULL） |
| `phase` | VARCHAR(50) | 当前阶段 |
| `round` | INT | 当前轮数 |
| `content` | TEXT | 消息内容 |
| `evidence_refs` | JSONB | 引用的证据 ID 列表 |
| `action_type` | VARCHAR(50) | 动作类型：`speak` / `submit_evidence` / `ask_question` / `search` / `phase_change` / `clarification_question` / `clarification_answer` / `option_generated` / `system` |
| `metadata` | JSONB | 额外元数据 |
| `created_at` | TIMESTAMP | 创建时间 |

**索引**：
- `session_id` + `created_at`（按时间顺序拉取庭审记录）
- `session_id` + `phase` + `round`

---

### 3.5 belief_snapshots（信念状态快照）

每轮质证后记录各 Agent 的信念度，用于智能收敛判断和可视化。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `agent_id` | UUID FK | Agent |
| `round` | INT | 轮数 |
| `belief_a` | FLOAT | 本轮对选项 A 的信念度 |
| `belief_b` | FLOAT | 本轮对选项 B 的信念度 |
| `delta` | FLOAT | 相比上轮的变化 |
| `trigger_event` | VARCHAR(50) | 触发变化的事件：`evidence_added` / `cross_exam` / `rebuttal` |
| `created_at` | TIMESTAMP | 创建时间 |

**索引**：
- `session_id` + `agent_id` + `round`（联合唯一）
- `session_id` + `round`（拉取整轮快照）

### 3.5a belief_diffs（信念变化 diff，v0.6 新增）

每次贝叶斯 log-odds 引擎把一条 evidence 应用到某个 agent 时，生成一行。
用于审计、可视化（BeliefDiffCard）、离线回放。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `round` | INT | 轮数 |
| `phase` | VARCHAR(50) | 阶段（`evidence` / `cross_exam` / `closing`） |
| `agent_type` | VARCHAR(50) | Agent 类型 |
| `evidence_id` | UUID NULL | 触发该 diff 的 evidence（anchor_pull 类型时为 NULL） |
| `source` | VARCHAR(20) | `evidence` / `weaken` / `anchor_pull` |
| `direction` | VARCHAR(20) | `supports_a` / `supports_b` / `neutral` |
| `prior_belief_a` | DECIMAL(5,4) | 更新前 belief_a |
| `posterior_belief_a` | DECIMAL(5,4) | 更新后 belief_a |
| `delta_belief_a` | DECIMAL(5,4) | posterior − prior |
| `prior_logit` | DECIMAL(8,4) | 更新前 logit（用于离线重放） |
| `posterior_logit` | DECIMAL(8,4) | 更新后 logit |
| `evidence_weight` | DECIMAL(5,4) | 该 evidence 对该 agent 的有效权重（cred·rel·|impact|·(1−weaken)） |
| `weaken_factor` | DECIMAL(5,4) | 1 − max(weaken strength targeting that agent) |
| `reason` | TEXT | 触发原因（默认截取 evidence.content 80 字） |
| `created_at` | TIMESTAMP | 默认 `now()` |

**索引**：
- `session_id`（按庭审拉时间线）
- `session_id` + `agent_type`（按角色过滤）
- `session_id` + `round`（按轮次过滤）

**为什么是 DECIMAL(5,4) 而不是 FLOAT**：
- belief_a 范围 [0, 1]，4 位小数与 UI 显示一致
- DECIMAL 保证 0.0500 == 0.0500 严格比较（前端 BeliefDiffCard 按 prior/posterior 相等跳过重复行）

### 3.5b evidence_weaken_links（质疑边，v0.6 新增）

异构论辩图谱专利（CN202610034750）的实现：律师可以主动质疑某条 evidence 对某个目标 agent 的影响。
每次质疑写入一行，多个 weaken 边叠加时取 max。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `evidence_id` | UUID FK | 被质疑的 evidence |
| `aggressor_msg` | UUID NULL | 发起质疑的消息（可空） |
| `aggressor_agent` | VARCHAR(32) | 质疑方 agent_type（`prosecutor` / `defender`） |
| `target_agent` | VARCHAR(32) | 目标 agent_type（被削弱的 agent） |
| `weaken_strength` | DECIMAL(4,2) | 削弱强度 [0, 1] |
| `rationale` | TEXT | 质疑理由 |
| `created_at` | TIMESTAMP | 默认 `now()` |

**索引**：
- `session_id` + `evidence_id`（按证据查所有 weaken）
- `session_id`（按庭审全量审计）

**与 belief_diffs 的关系**：
- weaken 边写入是"声明"，不直接生成 belief_diff
- 下次 engine.UpdateWithDiff 跑 evidence 时，会读所有 `evidence_weaken_links WHERE evidence_id = X AND target_agent = Y` 取 max，然后把 `w *= (1 - max)`
- weaken 边本身可以通过 REST 端点写入（v0.7+ UI）

---

### 3.6 verdicts（判决书）

每场庭审最终生成一份判决书。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK UNIQUE | 所属庭审（一对一） |
| `content` | TEXT | 完整判决书 Markdown |
| `summary` | TEXT | 一句话总结（采纳建议） |
| **`trial_summary`** | **TEXT** | **v0.5+ 新增：1-2 句庭审过程纪要（双方核心攻防 + 关键转折点）。与 `summary` 不同：summary 给"该选什么"，trial_summary 给"庭审里发生了什么"。老 verdict 此列为空字符串** |
| `option_a_score` | FLOAT | 选项 A 综合得分 [0, 1] |
| `option_b_score` | FLOAT | 选项 B 综合得分 [0, 1] |
| `consensus_points` | JSONB | 共识点列表 |
| `divergence_points` | JSONB | 分歧点列表 |
| `recommendation` | TEXT | 可执行建议 |
| `user_feedback` | VARCHAR(20) | 用户反馈：`helpful` / `not_helpful` / `none` |
| `created_at` | TIMESTAMP | 创建时间 |

**v0.5+ 迁移说明**：
- `trial_summary` 列由 GORM AutoMigrate 自动添加（`type:text`），无需手写 migration
- 老 verdict 行 `trial_summary = ''`，前端 `v-if` 不渲染"庭审纪要"卡片
- 重新生成 verdict 时（用户点"上诉/再审"）自动填充新字段

---

### 3.7 llm_calls（LLM 调用日志）

记录每次 LLM 调用，为 Agent Gateway 的成本优化做准备。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `agent_id` | UUID FK | 调用 Agent |
| `task_type` | VARCHAR(50) | 任务类型：`opening` / `rebuttal` / `verdict` / `question` |
| `model` | VARCHAR(50) | 实际调用模型 |
| `prompt_tokens` | INT | prompt token 数 |
| `completion_tokens` | INT | 输出 token 数 |
| `total_tokens` | INT | 总 token 数 |
| `cost_usd` | DECIMAL(10,6) | 估算成本（美元） |
| `cost_cny` | DECIMAL(10,6) | 估算成本（人民币） |
| `latency_ms` | INT | 调用延迟（毫秒） |
| `status` | VARCHAR(20) | 状态：`success` / `failed` / `cached` |
| `error_msg` | TEXT | 错误信息 |
| `created_at` | TIMESTAMP | 创建时间 |

**索引**：
- `session_id` + `created_at`
- `agent_id` + `created_at`

---

### 3.8 search_logs（搜索日志）

> v0.2 修订：本表保留为**只读历史**，新搜索产生的发现写入 §3.9 `investigation_findings`。`search_logs` 不会被新代码读写。

记录调查员 Agent 的每次搜索。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `session_id` | UUID FK | 所属庭审 |
| `agent_id` | UUID FK | 调查员 Agent |
| `provider` | VARCHAR(30) | 搜索提供商：`bocha` / `searxng` / `tavily` |
| `query` | TEXT | 搜索 query |
| `result_count` | INT | 返回结果数 |
| `latency_ms` | INT | 搜索延迟 |
| `created_at` | TIMESTAMP | 创建时间 |

---

### 3.9 investigation_findings（调查发现）— v0.2 新增

> 控辩方派遣调查员的搜索结果。**不**写入 `evidences` 表，与用户证据严格分离。前端通过 `GET /api/v1/courtrooms/:uuid/investigations` 读取，`InvestigatorPanel` 单独展示。

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | UUID PK | 自增主键 |
| `finding_uuid` | VARCHAR(36) UNIQUE | 对外暴露的调查发现 ID（如 `f-xxx`） |
| `session_id` | UUID FK | 所属庭审 |
| `dispatcher` | VARCHAR(32) | 派遣方：`prosecutor` / `defender` |
| `investigator` | VARCHAR(32) | 调查员身份，默认 `investigator` |
| `query` | TEXT | 派遣时的搜索 query（tool_input.query） |
| `summary` | TEXT | 搜索结果摘要（截断 ≤ 1KB，前 5 条结果的标题 + URL） |
| `raw_result` | JSONB | 完整搜索结果（标题/URL/content），便于审计 + 前端可点击展开 |
| `source_provider` | VARCHAR(32) | 搜索提供商：`bocha` / `duckduckgo` / `mock` |
| `result_count` | INT | 本次搜索返回的结果数 |
| `a2a_message_id` | UUID | 对应的 A2A 消息 id（可追溯） |
| `created_at` | TIMESTAMP | 创建时间 |

**索引**：
- `session_id` + `created_at`（按时间顺序展示）
- `finding_uuid`（外部查询）

**与 evidences 表的关系**：
- 完全独立，不互引
- `evidences` 表仅由用户 `POST /evidences` 端点写入
- `investigation_findings` 表仅由 `investigation.Service.RecordFinding` 写入
- 前端两套列表 UI 完全分离

---

## 4. 关键业务流程与表操作

### 4.1 立案流程

1. 用户提交决策问题、选项、背景、模式。
2. 创建 `court_sessions` 记录。
3. 创建 4 条 `agents` 记录，初始化信念度。
4. 返回 `session_uuid`。

### 4.2 举证流程

1. 用户提交证据 / Agent 主动提问得到回答 → `evidences` 表
2. 调查员被控辩方派遣 → `investigation_findings` 表（**不**写入 evidences）
3. 更新相关 Agent 的 `belief_a` / `belief_b`。
4. 插入 `belief_snapshots` 记录本轮信念度。
5. 发送 WebSocket 事件通知前端：
   - 用户证据 → `evidence.added`
   - 调查发现 → `a2a.message(investigation_report)` + `search.completed`

### 4.3 质证流程

1. Agent 生成发言。
2. 插入 `messages` 记录，`action_type = speak`。
3. 如果对方质疑证据，更新 `evidences.status = challenged`。
4. 每轮结束后，插入 `belief_snapshots`。
5. 检查是否满足智能收敛条件。

### 4.4 判决流程

1. 触发 `closing` 阶段。
2. 控辩双方做结案陈词。
3. 书记员生成判决书。
4. 插入 `verdicts` 记录。
5. 更新 `court_sessions.status = completed`。

---

## 5. 智能收敛判断逻辑

```sql
-- 查询最近两轮所有 Agent 的信念度变化
SELECT agent_id, round, delta
FROM belief_snapshots
WHERE session_id = ? AND round IN (?, ?)
ORDER BY agent_id, round;
```

收敛条件：
- 当前轮数 ≥ 2
- 所有 Agent 在最近两轮中的 `|delta| < 0.05`
- 当前轮数 ≥ `max_rounds` 的 50%（避免过早收敛）

满足条件则设置 `court_sessions.converged = true`，提前进入 `closing` 阶段。

---

## 6. 第二阶段扩展预留

以下设计为后续扩展预留：

| 扩展 | 涉及表/字段 |
|---|---|
| **用户系统** | 新增 `users` 表，`court_sessions.user_id` |
| **历史庭审** | `court_sessions` 增加 `is_template`、`parent_session_id` |
| **专家证人** | `agents.agent_type` 增加 `expert_witness` |
| **陪审团** | 新增 `jury_votes` 表 |
| **Agent Gateway** | `llm_calls` 表已预留 cost/latency/status 字段 |
| **分布式会话** | Redis 缓存 `court_sessions` 实时状态，PG 持久化 |
| **消息队列** | 新增异步任务表 `async_tasks`（或直接用 Redis 队列）|

---

## 7. 数据库选型确认

| 项 | 选择 |
|---|---|
| 主数据库 | PostgreSQL 15+ |
| ORM | GORM |
| 迁移工具 | golang-migrate / GORM AutoMigrate |
| 缓存 | Redis 7+ |
| 时区 | UTC 存储，前端转换 |

---

## 8. 下一步

数据库设计已完成。接下来可以进入：

1. **API 接口设计文档**：RESTful API + WebSocket 事件定义
2. **Agent 状态机设计**：庭审阶段流转、Agent 调用时序
3. **Prompt 设计**：控方、辩方、调查员、书记员的 system prompt

是否继续写 **API 接口设计文档**？

---

## 9. v0.8 业务事件审计表（`decision_events`）

### 9.1 用途

`decision_events` 表记录**业务级 span**的关闭事件，是 v0.8 白盒化的核心可观测性表。区别于：
- `llm_calls` —— 仅 LLM 调用事件
- `belief_diffs` —— 仅信念变化事件
- `a2a_messages` —— 仅 A2A 消息路由事件

`decision_events` 覆盖**所有业务事件**：
- 状态机迁移（`event_type = "state_transition"`，如 `idle → opening`）
- 业务级 span 关闭（`event_type = "span.RunCrossExamRound"` / `"span.DispatchInvestigator"` / `"span.GenerateVerdict"` 等）
- 信念收敛触发 / 强制中断 / 重试

### 9.2 Schema

```sql
CREATE TABLE decision_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_uuid  VARCHAR(36) NOT NULL,
    request_id    VARCHAR(36),
    event_type    VARCHAR(50) NOT NULL,
    agent_type    VARCHAR(50),
    payload       JSONB DEFAULT '{}'::jsonb,
    duration_ms   BIGINT DEFAULT 0,
    status        VARCHAR(20) DEFAULT 'ok',
    error_msg     TEXT,
    created_at    TIMESTAMP,
    INDEX idx_decision_events_session (session_uuid),
    INDEX idx_decision_events_request (request_id),
    INDEX idx_decision_events_type (event_type),
    INDEX idx_decision_events_agent (agent_type),
    INDEX idx_decision_events_created (created_at)
);
```

### 9.3 字段说明

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `id` | UUID | ✅ | 主键 |
| `session_uuid` | VARCHAR(36) | ✅ | 所属庭审 session；indexed |
| `request_id` | VARCHAR(36) | ❌ | 关联 HTTP / WS trace_id；indexed |
| `event_type` | VARCHAR(50) | ✅ | 事件类型；indexed。约定：`"state_transition"` / `"span.<业务span名>"` / `"convergence_triggered"` 等 |
| `agent_type` | VARCHAR(50) | ❌ | 关联 agent（prosecutor / defender / investigator / clerk / judge）；indexed |
| `payload` | JSONB | ❌ | 业务级 span attributes（灵活字段） |
| `duration_ms` | BIGINT | ❌ | 业务级 span 耗时（毫秒） |
| `status` | VARCHAR(20) | ❌ | `"ok"` / `"error"` / 自定义；与 OpenTelemetry span semantic conventions 对齐 |
| `error_msg` | TEXT | ❌ | 失败时的错误信息（截断到 500 字符） |
| `created_at` | TIMESTAMP | ❌ | 事件发生时间；indexed |

### 9.4 查询示例

```sql
-- 单 session 全链路
SELECT * FROM decision_events
WHERE session_uuid = 'abc-123'
ORDER BY created_at ASC;

-- 单 trace
SELECT * FROM decision_events
WHERE request_id = '7f3a-bc12-...'
ORDER BY created_at ASC;

-- 仅状态机迁移
SELECT * FROM decision_events
WHERE event_type = 'state_transition'
  AND created_at > NOW() - INTERVAL '1 day';

-- 错误事件统计
SELECT event_type, COUNT(*) AS err_count
FROM decision_events
WHERE status = 'error'
  AND created_at > NOW() - INTERVAL '1 hour'
GROUP BY event_type
ORDER BY err_count DESC;
```

### 9.5 容量估算

- 每次状态机迁移 1 行（5-10 行 / 庭审）
- 每次业务级 span 关闭 1 行（预计 30-50 行 / 庭审，含 orchestrator 各 span）
- 合计约 35-60 行 / 庭审
- 单表 100 万行 ≈ 16,000-30,000 庭审，可支撑 1-2 年运营

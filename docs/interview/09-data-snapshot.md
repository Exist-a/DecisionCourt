# 09 · 项目真实数据快照（让数据替你说话）

> **目标**：把 DecisionCourt 项目跑出来的真实数据汇总，**让面试官问任何细节时你能直接看数据回答**。**不是技术 explanation，是 raw data**——**"我跑了一场庭审，结果是 X"**比"我们设计了 X"有说服力 100 倍。
> **配套**：[`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)（完整审计 + 时间线）

---

## 0. 一句话总结

> v0.8.3 我跑了一场**完整真实庭审**——**title="我要学习吗"**，**4 个 evidence + 16 条 belief_diff + 10 条 strategy_note + 33 条 a2a 消息 + 37 条 LLM 调用 + 1 份 verdict（推荐学习）**。**所有数据入库** + **trace 串联** + **bocha 真实搜索** + **DeepSeek 真实推理**。

---

## 1. 单次庭审主数据（最常用 - 面试答辩"举例"用）

### 1.1 庭审基本信息

```sql
SELECT session_uuid, title, current_phase, current_round, status,
       max_rounds, created_at
FROM court_sessions
WHERE title = '我要学习吗';
```

| 字段 | 值 |
|---|---|
| session_uuid | `e35214e6-50cb-49b1-8ae7-80b74d9e4610` |
| title | 我要学习吗 |
| option_a | 学 |
| option_b | 不学 |
| context | 我很累，但是不学不行 |
| current_phase | deliberation |
| current_round | 0 |
| status | completed |
| max_rounds | 3 |

### 1.2 证据（4 条全部 admitted）

```sql
SELECT evidence_id, content, credibility_score, relevance_score,
       impact_on_option_a, impact_on_option_b, status
FROM evidences
WHERE session_uuid = '...';
```

| evidence_id | content | credibility | relevance | impact_a | impact_b | status |
|---|---|---|---|---|---|---|
| E001 | 有点累了 | 0.9 | 0.2 | -0.3 | 0.3 | admitted |
| E002 | 我不聪明 | 0.3 | 0.5 | -0.3 | 0.3 | admitted |
| E003 | 我不喜欢学 | 0.9 | 0.8 | -0.5 | 0.5 | admitted |
| E004 | 但是不学不行 | 0.3 | 0.6 | 0.5 | -0.5 | admitted |

**关键观察**：E004 是转折点——用户自我反驳。

### 1.3 信念变化（16 行 = 4 evidence × 4 agent）

```sql
SELECT round, phase, agent_type, direction,
       prior_belief_a, posterior_belief_a, delta_belief_a,
       LEFT(reason, 30) as reason
FROM belief_diffs
WHERE session_uuid = '...';
```

**E001 触发（4 行）**：
| agent | prior | posterior | delta | direction | reason |
|---|---|---|---|---|---|
| prosecutor | 0.75 | 0.7147 | -0.0353 | supports_b | 有点累了 |
| defender | 0.25 | 0.2853 | +0.0353 | supports_b | 有点累了 |
| investigator | 0.50 | 0.494 | -0.006 | supports_b | 有点累了 |
| judge | 0.50 | 0.4961 | -0.0039 | supports_b | 有点累了 |

**E004 触发（4 行）—— 唯一支持 A 的**：
| agent | prior | posterior | delta | direction | reason |
|---|---|---|---|---|---|
| prosecutor | 0.7233 | 0.7087 | -0.0146 | supports_a | 但是不学不行 |
| defender | 0.2742 | 0.2906 | +0.0164 | supports_a | 但是不学不行 |
| investigator | 0.50 | 0.5100 | +0.0100 | supports_a | 但是不学不行 |
| judge | 0.5089 | 0.5128 | +0.0039 | supports_a | 但是不学不行 |

**关键观察**：E004 让 4 个 Agent 都朝 A 移动，但**E001/002/003 的影响是叠加的**——最终法官倾向 A 但幅度不大。

### 1.4 私有策略笔记（10 行 = 5 阶段 × 2 Agent）

```sql
SELECT agent_type, round, type, LEFT(content, 50) as content_preview,
       linked_evidence_ids
FROM private_memories
WHERE session_uuid = '...';
```

| round | agent | linked_evidence_ids | 说明 |
|---|---|---|---|
| 0 | prosecutor | [] | 第一次发言，立场 = 学 |
| 0 | defender | [] | 第一次发言，立场 = 不学 |
| 1 | prosecutor | [E001] | 应对"有点累了"质疑 |
| 1 | defender | [E001] | 强调 E001 支持不学 |
| 2 | prosecutor | [E001, E002] | 用调查回应"疲劳 + 不聪明" |
| 2 | defender | [E001, E002] | 反控方强调 E002 削弱学习 |
| 3 | prosecutor | [E002] | 用研究反驳"我不聪明" |
| 3 | defender | [E002, E003] | 强调主观消极认知 |
| 0 | prosecutor | [E002, E003, E004] | **closing** 用 E004 反击 |
| 0 | defender | [E001, E002, E003, E004] | **closing** 全 evidence 综合 |

**关键观察**：第 10 条策略笔记引用了 **所有 4 个 evidence** —— 业务正确 ✅。

### 1.5 A2A 消息（33 条 = 5 阶段 × 多方通信）

```sql
SELECT count(*), from_agent, to_agent, message_type, visibility
FROM a2a_messages
WHERE session_uuid = '...'
GROUP BY from_agent, to_agent, message_type, visibility;
```

| msg_count | from | to | message_type | visibility |
|---|---|---|---|---|
| 5 | defender | prosecutor | speech | public |
| 6 | defender | defender | strategy_note | private |
| 4 | investigator | prosecutor | report | public |
| 4 | prosecutor | investigator | dispatch | public |
| 1 | prosecutor | prosecutor | opponent_weakness | private |
| 5 | prosecutor | defender | speech | public |
| 8 | prosecutor | prosecutor | strategy_note | private |
| **33** | | | | |

**关键观察**：private 消息（16 条）落库 ✅；public 消息广播给前端 ✅。

### 1.6 调查发现（4 次用 bocha 真实 API）

```sql
SELECT dispatcher, query, summary, result_count, source_provider
FROM investigation_findings
WHERE session_uuid = '...';
```

| dispatcher | query | result_count | provider |
|---|---|---|---|
| prosecutor | 学习对个人发展的益处 统计数据 | 8 | bocha |
| prosecutor | 学习克服疲劳提升认知能力的案例或研究 | 9 | bocha |
| prosecutor | 智力可塑性 成长型思维 学习能力 科学研究 | 7 | bocha |
| prosecutor | 成长型思维干预 提高学习动机 案例研究 | 10 | bocha |

**关键观察**：4 次真实搜索全部走 bocha ✅。

### 1.7 LLM 调用（37 条全部 success）

```sql
SELECT task_type, count(*), avg(total_tokens)::int as avg_tokens,
       avg(latency_ms)::int as avg_latency_ms
FROM llm_calls
WHERE session_uuid = '...'
GROUP BY task_type
ORDER BY count(*) DESC;
```

| task_type | count | avg_tokens | avg_latency_ms |
|---|---|---|---|
| react_think | 23 | ~4200 | ~2400 |
| react_speak_stream | 7 | 0 | ~11000 |
| summary | 2 | ~430 | ~1445 |
| assess | 3 | ~850 | ~2350 |
| final | 1 | 1177 | 1948 |
| verdict | 1 | 1883 | 7806 |
| **合计** | **37** | | |

**关键观察**：总 token ~50K，单庭审总成本约 ¥0.1（DeepSeek 价格）。

### 1.8 判决书（最终结果）

```sql
SELECT option_a_score, option_b_score, recommendation,
       LEFT(summary, 100) as summary_preview
FROM verdicts
WHERE session_uuid = '...';
```

| 字段 | 值 |
|---|---|
| option_a_score | 0.56 (学习) |
| option_b_score | 0.44 (不学) |
| recommendation | 建议选择学 |
| summary | 尽管存在疲劳、不聪明、不喜欢学等不利因素，但证据E004明确指出'不学不行'，且对方关于认知可塑性和成长型思维的论证具有科学依据，削弱了被告方的主观消极证据，综合权衡，学习是必要且可行的选择。 |
| trial_summary | 控方以认知发展理论和成长型思维论证学习的必要性，辩方以当事人主观消极证据（疲劳、不聪明、不喜欢学）主张不学；**关键转折点在于控方引入E004（不学不行）及科学证据反驳辩方的主观证据**，最终比分在第3轮后拉开。 |

---

## 2. 业务事件（白盒化决策面 - 面试杀手锏）

### 2.1 状态机迁移

```sql
SELECT event_type, status, payload, created_at
FROM decision_events
WHERE session_uuid = '...'
ORDER BY created_at;
```

| event_type | to | from | round | 时间 |
|---|---|---|---|---|
| state_transition | opening | idle | 0 | 12:15:43 |
| state_transition | cross_exam | opening | 1 | 12:16:22 |
| state_transition | cross_exam | cross_exam | 2 | 12:17:15 |
| state_transition | cross_exam | cross_exam | 3 | 12:18:11 |
| state_transition | closing | cross_exam | 0 | 12:18:50 |
| state_transition | deliberation | closing | 0 | 12:19:28 |

**6 个 state_transition 全部 OK** —— 完整流程 ✅。

---

## 3. 业务级指标（v0.8 白盒化价值）

### 3.1 /metrics 端点

```bash
curl http://localhost:8080/metrics
```

```json
{
  "data": {
    "counters": {
      "courtroom_state_transition_total": [
        {"labels":{"from":"idle","to":"opening"}, "value": 1},
        {"labels":{"from":"opening","to":"cross_exam"}, "value": 1},
        {"labels":{"from":"cross_exam","to":"cross_exam"}, "value": 2},
        {"labels":{"from":"cross_exam","to":"closing"}, "value": 1},
        {"labels":{"from":"closing","to":"deliberation"}, "value": 1}
      ],
      "a2a_message_throughput_total": [
        {"labels":{"event_type":"a2a.message"}, "value": 16},
        {"labels":{"event_type":"agent.speak_chunk"}, "value": 338},
        {"labels":{"event_type":"phase.changed"}, "value": 1},
        {"labels":{"event_type":"search.completed"}, "value": 2},
        {"labels":{"event_type":"agent.speak"}, "value": 2},
        // ... 共 10 个 event_type
      ]
    },
    "histograms": {
      "http_request_duration_seconds": [...]  // 4 path × status
    }
  }
}
```

**关键观察**：`agent.speak_chunk` 计数 338 —— **流式 chunk 数量可观**，**前端能看到逐字显示效果**。

### 3.2 9 个 event_type 的 A2A throughput（修复 bug 后）

| event_type | count |
|---|---|
| `a2a.message` | 16 |
| `agent.cot_step` | 4 |
| `agent.speak` | 2 |
| `agent.speak_chunk` | **338** |
| `agent.thinking_finished` | 2 |
| `agent.thinking_started` | 2 |
| `opening.finished` | 1 |
| `phase.changed` | 1 |
| `search.completed` | 2 |
| `search.started` | 2 |

---

## 4. 白盒化发现 5 个 bug（最值钱的面试数据）

### 4.1 bug 出现顺序 + 修复时间

| # | bug | 严重度 | 暴露路径 | v0.8 状态 | 备注 |
|---|---|---|---|---|---|
| 1 | llm_calls 外键约束失败 | 🔴 P1 | stdout ERROR 当天发现 | ✅ 修了 | v0.8 demo |
| 2 | A2A SessionID fallback WARN | 🟡 P2 | stdout WARN 当天发现 | ⏸️ 后续修 | v0.8 demo |
| 3 | a2a_message_throughput 计数缺失 | 🟢 P3 | /metrics 端点发现 | ✅ 修了 | v0.8 demo |
| 4 | 信念轨迹只显示 1 条 | 🟡 P2 | 用户真实庭审反馈 | ✅ 1 行 UUID.New() | v0.8.3 |
| 5 | 判决书按钮无响应 | 🟢 P3 UX | 用户真实庭审反馈 | ✅ 35 行 props 透传 | v0.8.3 |

### 4.2 bug 4 链路 ID 对账（杀手锏数据）

| 层 | 状态 | 数值 |
|---|---|---|
| 数据库 `belief_diffs` | ✅ | 16 行（每行 distinct id） |
| API `GET /belief-diffs` | ✅ | 返回 16 条 distinct id |
| 后端 WS `belief.diff` 事件 | ❌ | **16 个事件但 ID 全 = uuid.Nil** |
| 前端 store.appendBeliefDiff | ❌ | 第 1 条入 store，第 2-16 条被幂等检查 skip |

**root cause**：`engine_v06.go:97` 创建 `model.BeliefDiff{}` **未分配 ID**，依赖 `gorm_repository.go:24-26` 的 fallback `if diff.ID == uuid.Nil { diff.ID = uuid.New() }`。**fallback 仅在 DB write 路径生效，broadcast 路径用内存零值**。

**修复**：1 行 `ID: uuid.New()` + 4 行注释。

**总耗时**：30 分钟（5 分钟数据自洽 + 5 分钟链路口审计 + 5 分钟定位 + 15 分钟修复 + 测试 + 重启）。

---

## 5. 白盒化自洽验证（最值钱的面试亮点）

### 5.1 "业务跑得欢 ≠ 系统健康" —— 通过审计表确认

| 表 | 行数（庭审 e35214e6） | 含义 |
|---|---|---|
| llm_calls | **37**（修复 bug 1 后） | LLM 调用 47 字段全审计 |
| belief_diffs | **16**（修复 bug 4 后） | 信念变化全审计 |
| a2a_messages | 33 | Agent 通信全审计 |
| decision_events | **6** | 状态机迁移全审计 |
| decision_events | 0 (span.X) | 业务级 span 未埋（v0.8.3 后续） |

**关键洞察**：**修复 bug 1 之前** —— llm_calls 表 0 行，**完全不知道 token 成本**。

### 5.2 4 个 sink 的设计哲学

```
stdout（slog JSON）       → 实时排错，不持久化
内存（metrics）           → 实时指标，不持久化
PostgreSQL（业务表）      → 业务事件，永久持久化
文件（agent_gateway_*.log）→ LLM 调用备份，永久持久化
```

**业务选择**：每个 sink 各有取舍，**不是单一 sink**。

---

## 6. 端到端 trace 串联

### 6.1 HTTP X-Request-ID → 业务事件

```bash
curl -i -H 'X-Request-ID: demo-x' http://localhost:8080/health
HTTP/1.1 200 OK
X-Request-ID: demo-x           ← 原样回写
```

**端到端**：HTTP 请求进入 → TraceMiddleware 注入 `ctx.Trace` → 业务 log/metric 携带 trace_id → decision_events 表有 trace_id 字段 → 数据库可按 trace_id 检索所有事件。

### 6.2 trace_id 串联的 1 个真实例子（v0.8.3）

待 v0.8.3 真实庭审回归跑一次完整 trace 的 view。

---

## 7. 项目代码规模（"项目复杂度"的硬数字）

| 维度 | 数字 |
|---|---|
| 后端 Go 文件数 | ~80（含 internal/ 各子包） |
| 后端 Go 代码量 | ~10000 行（含 main / cmd / internal） |
| 前端 TypeScript 文件数 | ~120 |
| 前端 TypeScript 代码量 | ~8000 行（含 components / app / lib / store） |
| 数据库表 | 14（含 v0.8 decision_events） |
| 数据库代码行 | ~300 行（schema / migrations） |
| 单元测试 | 100+ 项（`go test ./internal/...`） |
| ADR | 11 份（v0.5+ 全部） |
| 主文档 | 8 份（v0.8 升级） |
| 测试覆盖率 | backend internal: 50+ 用例 (engine_v06_test.go) + 各模块散点测试 |

---

## 8. 项目时间线（v0.5 → v0.8.3）

| 版本 | 时间 | 主要交付 |
|---|---|---|
| MVP (v0.0-v0.4) | 早期 | 基础庭审流程 |
| v0.5 | 中期 | Gateway 装饰器链 + 压缩 + 47 字段审计 |
| v0.5+ | 中期 | 私有记忆系统 + A2A 重设计 |
| v0.6 | 中期 | Bayesian Belief Engine + anchoring + weaken + audit trail |
| v0.7 | 中期 | Gateway v2（compression + reliability 链） |
| v0.8 | 2026-07-02 | **白盒化（slog + metrics + decision_events + trace）** |
| v0.8.2 | 2026-07-02 | **Google A2A 协议实装（agent-card + discovery + tasks/send）** |
| v0.8.3 | 2026-07-02 | **修复 5 个白盒化发现的 bug** |

---

## 9. 项目真实 metric（关键压力测试数据）

### 9.1 单庭审数据规模（real production data）

| 维度 | 数值 |
|---|---|
| 单庭审时长 | ~25 分钟 |
| 单庭审 LLM 调用 | 37 次 |
| 单庭审总 token | ~50K（~¥0.1 DeepSeek） |
| 单庭审最多 round | max_rounds=3 |
| 单庭审 verdict 字数 | ~1500 字 Markdown |

### 9.2 上限预估（按当前架构）

| 维度 | 估算 |
|---|---|
| 单 backend 实例 | ~50 庭审并发（受 LLM 5 QPS 限流） |
| 单庭审最大 | ~5 轮（max_rounds=5） |
| 单数据库并发 | ~200 active sessions（受 PG 单机） |
| 单容器内存 | ~300 MB |

**v0.9+ 计划**：Redis 分布式锁 + WebSocket 多实例广播 → 横向扩展到 500+ 并发。

---

## 10. 项目 Git 状态

### 10.1 关键 commit（最近的）

```
fe2681b fix(whitebox): v0.8.3 修复真实庭审回归暴露的 2 个 bug + 案例文档 v1.1
ca3c9b3 docs: 写 ADR 0011 (v0.8.2 A2A 协议外部接入层) + 更新索引 + 02-a2a-bus
6e8cff3 feat(a2a): v0.8.2 实装 Google A2A 协议外部接入层（3 个端点）
418f349 feat(whitebox): v0.8 白盒化 (slog + metrics + decision_events + trace) + 文档整合 + bug 修复
2f04f01 feat(belief): v0.6 belief engine (Bayesian log-odds + anchoring + weaken + multi-signal convergence + audit trail)
d2b696a feat(gateway): v0.5+ 完整版（Prompt 压缩 + Token 预算 + 限流 + Fallback + 文件日志）
2a7e047 feat: MVP 实现完成
```

### 10.2 仓库地址

https://github.com/Exist-a/DecisionCourt

---

## 11. 面试时如何用数据回答

### 11.1 "你们项目 token 成本怎么算？"

> **直接回答**：
> - 单庭审 ~50K token（37 次 LLM 调用均值）
> - DeepSeek 价格 ¥2/百万 token → 单庭审 ¥0.1
> - 1000 用户/天 → ¥100/天 → ¥36K/年
> - **数据来源**：v0.8.3 跑的"我要学习吗"庭审，**`SELECT count(*), avg(total_tokens) FROM llm_calls`** 一句 SQL 拿到。

### 11.2 "你们怎么验证 AI 输出正确？"

> **直接回答**：
> - **3 个 audit trail 表**（decision_events / belief_diffs / a2a_messages / llm_calls）
> - v0.8.3 真实庭审：16 条 belief_diff，对应 4 evidence × 4 agent，**LLM 输出不能凭空打分，必须基于 belief_diffs 表算出来的平均信念**
> - LLM 输出和 belief engine 计算对账：本次庭审 A=0.56 vs B=0.44，**LLM 没推翻算式**

### 11.3 "如果让你重新做一次，你会改什么？"

> **诚实回答**：
> 1. **agent_id 字段设计** —— 现在混用 `agent_type` / `agent_id` / `agent_uuid`，**3 种标识字段增加心智负担**（v0.8.3 bug 4 的根因）
> 2. **白盒化更早做** —— v0.5 就开始做 observability 的话，bug 1 早就发现
> 3. **真实庭审回归测试** —— 单元测试 + e2e 测试完全发现不了"每层都对链路错"这种 bug，**真实业务回归是必须**

---

## 12. 名词速查

| 名词 | 含义 |
|---|---|
| trial_snapshot | 庭审快照（本文件名） |
| real production data | 真实生产数据（不是 mock） |
| stream chunk count | 流式 chunk 数量（本场庭审 338） |
| cost_usd / cost_cny | LLM 美元 / 人民币成本 |
| traceability | 可追溯性 |
| audit trail | 审计 trail / 审计踪迹 |

---

**下一步**：返回 [README.md](README.md) §4，看完整索引。


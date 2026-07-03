# ADR 0011: LLM 输出概率值后端硬编码 Clamp

> **状态**：✅ Accepted (2026-07-03)
> **决策日期**：2026-07-03
> **影响范围**：`backend/internal/agent/` + `backend/internal/courtroom/service.go`

## 背景

v0.8.3 实装时跑全流程验证（前端 [http://localhost:3000](file:///d:/源码/FullStack/DecisionCourt/frontend) + 后端 + LLM），发现 verdict 页显示**异常分数**（3500 / 6500），ArgumentMap 线条**异常粗**（172.5px）。白盒日志（`/metrics` + `decision_events` 表 + `agents` 表 + `verdicts` 表 + `llm_calls` 表）联合定位后根因为：

**DeepSeek 在 JudgeAssess / JudgeFinalDecision / GenerateVerdict（clerk 角色）三个 prompt 都偶尔把 0-1 范围的小数误输出为 0-100 范围的整数（如 35.0 / 65.0）**。推测是 prompt 里"对选项 A 的支持度：40%"字符串被 LLM 误解为 0-100 范围。

旧代码存在 3 处防护缺口：

| 路径 | 旧逻辑 | 漏点 |
|---|---|---|
| [`JudgeAssess`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/orchestrator.go#L578-L592) | 信任 LLM 返回的 `belief_a` float | 无 clamp |
| [`JudgeFinalDecision`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/orchestrator.go#L630-L636) | `< 0 \|\| > 1` 时 fallback 到 `judge.BeliefA` | DB 已有脏数据时循环污染 |
| [`GenerateVerdict`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go#L1344-L1345) `verdict.OptionAScore` | 直接 `getFloat(result, ...)` | 无 clamp |

## 决策

**所有 LLM 返回的"概率值"在写入数据库前无条件 clamp 到 [0, 1] 范围**：

1. 新增 [`agent/probability.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/probability.go)`ClampProbability(v float64) float64` —— 单一入口 helper
2. `agent/orchestrator.go` 的 `JudgeAssess` 和 `JudgeFinalDecision` 返回前调一次
3. `courtroom/service.go` 的写库前**再**调一次（最后一道防线，万一未来加新 LLM 路径漏 clamp）
4. **不** fallback 到 `judge.BeliefA` 兜底 —— 那会让 DB 已有脏数据循环污染

## 约束

- **硬编码语义**：写库前无条件 clamp，不做"看起来合理就放过"的判断
- **NaN / Inf 守卫**：IEEE 754 下 NaN 的比较都是 false，会穿透 `belief.Clamp01` 直接写库。Postgres 写 NaN 列会报"invalid input syntax for type double precision"，所以 `ClampProbability` 顶部先剥 NaN/Inf
- **Clamp 范围是 [0, 1]**：而不是 v0.6 engine 内部用的 [0.05, 0.95]，因为 `verdict.option_a_score` 也走这里，允许 0 和 1 边界表达"完全倾向"

## 拒绝的方案

### A. 仅前端 cap（拒绝）

ArgumentMap strokeWidth 上限 8 + verdict 页 `Math.min(scoreA, 1)` 显示层 clamp。**问题**：DB 仍然脏，下次任何路径（导出、审计、回归测试）读到 35.0 都会再次炸。

### B. 修复 LLM prompt（拒绝）

让 prompt 显式"0-1 之间的小数，不要 0-100"。**问题**：LLM prompt 不可控，DeepSeek / Qwen / Kimi 未来版本都可能再抽风；且 prompt 改了会让"输出格式示例"变长，得不偿失。

### C. 限制 `judge.belief_a` 表的列约束（拒绝）

`ALTER TABLE agents ADD CONSTRAINT chk_belief_a CHECK (belief_a >= 0 AND belief_a <= 1)`。**问题**：只防 35.0 这种**整数**污染，**挡不住** NaN / -0.0 / 1.5 这种边缘情况；且需要在 migration 里加，部署成本比代码 clamp 高。

## 副作用

- 老庭审脏数据（`agents.belief_a = 35` / `verdicts.option_a_score = 35`）**不会**自动修复
- 配套 SQL 清理脚本 [`docs/adr/0011-llm-probability-hard-clamp.md#清理脚本`](file:///d:/源码/FullStack/DecisionCourt/docs/adr/0011-llm-probability-hard-clamp.md) 一次性 `UPDATE agents SET belief_a = LEAST(GREATEST(belief_a, 0), 1)` 即可

## 配套变更

- [`backend/internal/agent/orchestrator.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/orchestrator.go) — 3 处 clamp 调用
- [`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go) — 3 处 clamp 调用（JudgeFinalDecision fallback、agents.belief_a 写库、verdict.OptionAScore 写库）
- [`backend/internal/agent/probability.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/probability.go) — 新文件，helper
- [`backend/internal/agent/probability_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/probability_test.go) — 16 个边界 case（含 DeepSeek 实际抽风值 35.0 / 65.0）
- [`docs/refresh-and-reopen-fix.md`](file:///d:/源码/FullStack/DecisionCourt/docs/refresh-and-reopen-fix.md) — §6 附录追加

## 清理脚本

```sql
-- 一次性把 DB 里已有的脏数据 clamp 到合法范围
UPDATE agents
SET belief_a = LEAST(GREATEST(belief_a, 0.0), 1.0),
    belief_b = LEAST(GREATEST(belief_b, 0.0), 1.0)
WHERE belief_a < 0 OR belief_a > 1 OR belief_b < 0 OR belief_b > 1;

UPDATE verdicts
SET option_a_score = LEAST(GREATEST(option_a_score, 0.0), 1.0),
    option_b_score = LEAST(GREATEST(option_b_score, 0.0), 1.0)
WHERE option_a_score < 0 OR option_a_score > 1
   OR option_b_score < 0 OR option_b_score > 1;

-- 检查清理结果
SELECT id, session_id, agent_type, belief_a, belief_b
FROM agents
WHERE belief_a < 0 OR belief_a > 1 OR belief_b < 0 OR belief_b > 1;
-- 应该返回 0 行

SELECT id, session_id, option_a_score, option_b_score
FROM verdicts
WHERE option_a_score < 0 OR option_a_score > 1
   OR option_b_score < 0 OR option_b_score > 1;
-- 应该返回 0 行
```

## 已知未解决问题

1. **白盒化覆盖不全**：`decision_events` 表没记录 `judge.belief_update` / `belief.diff` / `judge.final_decision` 业务 span（v0.8 白盒化只覆盖 `state_transition`），下次定位类似问题会更难。→ 留到下个 PR 修。
2. **DeepSeek 抽风根因**：未 root cause（prompt 模板 / 模型版本 / temperature 都没动过），未来可能再触发。`ClampProbability` 是兜底，不解决根因。

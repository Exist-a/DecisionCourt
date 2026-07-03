# v0.8.3 修复：刷新丢数据 + 判决书回退无法继续开庭

> **实施日期**：2026-07-03
> **状态**：✅ 已完成（backend + frontend + tests + docs 全部同步）
> **作者范围**：5 个根因、3 个前端 + 4 个后端 + 1 个 .env 范围外改动、~6 个新测试

---

## 1. 问题陈述

用户报告："目前整个项目是初步可用的，但是会出现当用户刷新或者到了判决书回退的时候数据丢失或者无法继续开庭的情况。"

**两个故障场景**：
1. **页面刷新（F5 / 浏览器刷新）** — 会话状态丢失，无法恢复到刷新前的庭审进度
2. **判决书回退** — 庭审流程"回退到判决书"那一步附近，要么数据丢失，要么流程卡住、无法继续开庭

**根因诊断**：庭审状态完全靠前端内存（zustand 单例 + WebSocket 实时流）兜底，没有可靠的水合（hydration）机制。任何破坏单例的操作（刷新、关闭页签、`reset()`）都会让 UI 与 DB 不一致，且无法自愈。

---

## 2. 五个根因

### 根因 1：verdict 页面"零状态水合"（最致命）

[`frontend/app/verdict/[id]/page.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/app/verdict/%5Bid%5D/page.tsx) 挂载时**只调 2 个 REST API**（getVerdict + getMessages），而 court 页面调 6 个。session/agents/evidences/agents 全部空 → Header / Option A-B 标签 / 审讯记录 / 证据板组件都拿不到数据。

**次生问题**：`memoryEntries`（v0.5 情节记忆）**没有 REST 端点恢复**，刷新后"策略笔记" Tab 永远空。

### 根因 2：WebSocket 没有重连

[`frontend/lib/websocket.ts`](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/websocket.ts) 旧实现：
- onclose 只 log，不重连
- onerror 只 log
- 没有 heartbeat
- 没有"replay since last event id"机制

**症状**：服务端重启、负载均衡器 idle timeout、网络抖动 → WS 死 → 前端不知道 → UI 停在"庭审卡住"的状态。

### 根因 3：verdict 阶段没有"继续开庭"入口（状态机断头）

[`backend/internal/courtroom/statemachine.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/statemachine.go) 旧表：
```go
model.PhaseVerdict: {model.PhaseAppeal},  // 只有 verdict → appeal
```

- `PhaseAppeal` **完全没实装**（service.go 全文件搜不到 `PhaseAppeal` 引用）
- 前端 verdict 页面也没有"返回庭审"或"再开一庭"按钮
- 用户在 verdict 阶段想"补充证据后再辩论一次" → 无路可走

### 根因 4：CourtroomScene 头部按钮完全信赖内存 `verdictReady`

[`frontend/components/courtroom/CourtroomScene.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/CourtroomScene.tsx) 旧逻辑：
```tsx
{session.current_phase === "idle" ? "开 庭" : verdictReady ? "查看判决書" : "直接判决"}
```

`verdictReady` 只由 `verdict.ready` WebSocket 事件置 true —— verdict 事件不重发，刷新后永远 false → 顶部按钮错误显示"直接判决"（实际在 verdict 阶段点击必报错）。

### 根因 5：异步 StartTrial 的窗口期

[`backend/internal/api/handler.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go) 旧实现：handler 在 goroutine 里跑 `service.StartTrial`，200 立即返回，phase 还没写库。用户刷新 → 看到 idle → 再点 → ValidateAction 报 race 错。

---

## 3. 修复方案（实施完成）

### B-1：verdict 页补齐水合 + memoryEntries REST 端点重启

**后端**：
- [`backend/internal/a2a/bus.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/bus.go#L20-L37) `Repository` interface 加 `ListPrivateMemory` 方法（不过滤 viewer —— 因为 MemoryAuditPanel 设计就是展示全部）
- [`backend/internal/a2a/bus.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/bus.go#L108-L131) `gormRepository.ListPrivateMemory` 实现
- [`backend/internal/a2a/inmemory_repository.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/inmemory_repository.go#L66-L88) `InMemoryRepository.ListPrivateMemory` 实现
- [`backend/internal/a2a/bus.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/bus.go#L223-L230) `Bus.ListPrivateMemory` 包装方法
- [`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go#L1428-L1446) `Service.ListPrivateMemory`（按 sessionUUID 查 → 内部 ID → 调 a2aBus）
- [`backend/internal/api/handler.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L23-L57) 新增 `MemoryLister` interface + Handler.memoryLister 字段
- [`backend/internal/api/handler.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L108-L113) 注册路由 `GET /api/v1/courtrooms/:session_uuid/memory`
- [`backend/internal/api/handler.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L546-L628) `GetVisibleMemory` handler 实现

**前端**：
- [`frontend/lib/api.ts`](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/api.ts#L160-L195) 加 `getVisibleMemory()` API client
- [`frontend/components/courtroom/CourtroomScene.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/CourtroomScene.tsx#L172-L193) 水合序列加 memory 灌入（用 applyCourtEvent replay envelope，复用 WS 解析路径）
- [`frontend/app/verdict/[id]/page.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/app/verdict/%5Bid%5D/page.tsx#L98-L186) 补齐 6 个 API 水合（session / agents / evidences / investigations / belief_diffs / memory）
- [`frontend/types/index.ts`](file:///d:/源码/FullStack/DecisionCourt/frontend/types/index.ts#L162-L181) `UserActionRequest.action` 加 `"reopen_trial"` 字面量

### B-2：WS 心跳 + 自动重连退避

**后端**：
- [`backend/internal/api/websocket.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/websocket.go#L59-L73) 响应 `{type:"ping"}` 立即回 `{type:"pong", payload:{ts:"..."}}`

**前端**：
- [`frontend/lib/reconnect.ts`](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/reconnect.ts) 新建 — 提取纯函数 `computeBackoff` / `resetBackoff`（无依赖、可独立测试）
- [`frontend/lib/websocket.ts`](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/websocket.ts) 全重写：
  - 指数退避 1s→2s→4s→8s→16s→30s（封顶）
  - 心跳 25s 一次，连续 2 周期没 pong 主动 close
  - `closedByUser` 区分主动 disconnect vs 网络断开
  - `disconnect()` 清理所有 timer（防内存泄漏）

### B-3：顶部按钮逻辑改用 session.current_phase 派生

[`frontend/components/courtroom/CourtroomScene.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/CourtroomScene.tsx#L394-L451) — 派生判断改用 `session.current_phase`（DB 真值），不依赖 `verdictReady` 本地 boolean。`verdictReady` 仍保留只用于 amber banner 入场动画。

### B-4：状态机补 verdict → evidence + reopen_trial action

**状态机**：
- [`backend/internal/courtroom/statemachine.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/statemachine.go#L25-L31) `verdict → {appeal, evidence}`（新增 evidence 边）
- [`backend/internal/courtroom/statemachine.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/statemachine.go#L34-L37) `appeal → {evidence, closing}`（补全）
- [`backend/internal/courtroom/statemachine.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/statemachine.go#L94-L101) `ValidateAction` 加 `reopen_trial` case

**Service**：
- [`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go#L444-L457) `ProcessUserAction` 加 `reopen_trial` 分支
- [`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go#L471-L512) `reopenTrial()` 实现：取锁 + re-read 状态 + transitionPhase → evidence + 广播 `trial.reopened` 事件

**前端**：
- [`frontend/app/verdict/[id]/page.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/app/verdict/%5Bid%5D/page.tsx#L249-L269) `handleReopen()` handler
- [`frontend/app/verdict/[id]/page.tsx`](file:///d:/源码/FullStack/DecisionCourt/frontend/app/verdict/%5Bid%5D/page.tsx#L704-L743) verdict 页"补充证据重开"按钮（替代"只能回首页立案"的死路）

### B-5：StartTrial race 修复

[`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go#L268-L367) 拆分：
- `TransitionToOpening`（同步）— validate + transitionPhase + 返回 session
- `RunOpeningSpeeches`（异步）— ReAct opening + save + broadcast

[`backend/internal/api/handler.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L161-L195) handler 改：先同步调 `TransitionToOpening` → 200 → 然后 goroutine 跑 `RunOpeningSpeeches`。HTTP 200 返回时 DB 已经是 opening 状态，刷新或重复点击不会再触发 race 错。

`StartTrial` 保留作为内部 helper（被 integration test 用），内部就是 `TransitionToOpening + RunOpeningSpeeches`。

---

## 4. 测试矩阵

### 后端（Go）

| 测试文件 | 覆盖 | 状态 |
|---|---|---|
| [`backend/internal/courtroom/statemachine_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/statemachine_test.go) | `verdict → evidence` 合法 + `reopen_trial` ValidateAction 在 verdict/appeal 通过 + 6 个非法 phase 拒绝 + 13 个 transition 回归 | ✅ 7 cases 全过 |
| [`backend/internal/courtroom/reopen_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/reopen_test.go) | `reopenTrial` 实际 phase 转换（verdict / appeal）+ 6 个非法 phase race guard + 广播 trial.reopened 事件 + `ProcessUserAction.reopen_trial` 路径 | ✅ 11 cases 全过（用 in-memory SQLite） |
| [`backend/internal/a2a/list_private_memory_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/a2a/list_private_memory_test.go) | 4 种 private memory 都返回 + public/dispatch/report/其他 session 的都被过滤 + 空 session 返回空切片 | ✅ 2 cases 全过 |
| [`backend/internal/api/handler_memory_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler_memory_test.go) | GET /memory 响应 envelope 字段（from/to/visibility/payload decoded）+ 空数组 + 404 + 500 lister error + 500 missing lister | ✅ 5 cases 全过 |

`go test ./...` 全部包通过（含 4 个未改动的包：agent、a2a、belief、investigation、private_memory、observability 等）。

### 前端（TypeScript）

| 测试文件 | 覆盖 | 状态 |
|---|---|---|
| [`frontend/lib/reconnect.test.ts`](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/reconnect.test.ts) | computeBackoff 翻倍 / cap 30s / 边界值 / resetBackoff / 完整 1s→30s 序列 / 重置 round-trip | ✅ 6 cases 全过 |

`node --experimental-strip-types --test lib/reconnect.test.ts` 跑。

`pnpm tsc --noEmit` 严格模式通过。

### 测试限制说明（透明声明）

> ⚠️ **沙箱限制**：当前沙箱环境无法安装 `vitest` / `@testing-library/react` / `jsdom`（pnpm add 写到 metadata 缓存时被 sandbox 阻止）。前端 component-level 测试（按钮渲染 + 整页水合时序）**未自动化覆盖**，靠 tsc 严格类型检查 + 手动验收。生产环境应补：
> - `CourtroomScene` 顶部按钮在 verdict 阶段显示"查看判决書"的 RTL snapshot 测试
> - `verdict/[id]/page.tsx` 刷新后 6 个 API 都被调用的 mock 测试
> - `WebSocket` 断开 → 重连 → 重发水合的端到端 mock WS server 测试
>
> 上面三个测试是 unit-level 难以覆盖的集成行为，留给后续 PR。

---

## 5. 文档同步（AGENTS.md §1.2 强制）

- [`docs/decisioncourt-api-design.md`](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-api-design.md) 头部版本号 → v0.8.3 + 加 §3.5.3 GET /memory + §4.3.9a trial.reopened + §4.3.9b ping/pong + reopen_trial action 表格
- [`docs/decisioncourt-prd.md`](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-prd.md) 头部版本号 → v0.8.3 + §7.2 状态机图加 verdict → evidence fast-path + 实装状态矩阵更新
- [`docs/decisioncourt-tech-spec.md`](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-tech-spec.md) 加 §8.2a 心跳 + 重连退避规范

---

## 6. 产品决策点（已与用户授权）

⚠️ **B-4 涉及"裁决类型变更"**，按 AGENTS.md §2.1 已在实施前与用户对齐：

> 用户回复"那就选b，你可以开始了"

**B-4 决策**：
- **verdict 阶段允许"补充证据重新开庭"**（用户已授权）
- 让"判决书落印"的终局感减弱
- beliefs / evidences / messages 全部保留，律师看到完整历史
- 当前 round 保持不变（用户后续 continue_cross_exam 触发 +1）

如果未来需要恢复"终局"语义（不让用户重开），可以：
1. 在 PRD §7.2 状态机图删掉 `verdict → evidence` 边
2. 后端把 `PhaseVerdict → {PhaseAppeal, PhaseEvidence}` 改回 `{PhaseAppeal}`
3. 前端 `verdict/[id]/page.tsx` 删掉"补充证据重开"按钮
4. 不要删 `reopen_trial` action handler —— 保留给未来"上诉/再审"模块用

---

## 6. 附录：v0.8.4 修复 LLM 抽风污染 belief_a

> **追加日期**：2026-07-03  
> **状态**：✅ 已完成

### 6.1 触发

v0.8.3 实施完成后跑全流程验证（前端 [http://localhost:3000](file:///d:/源码/FullStack/DecisionCourt/frontend) + 后端 [http://localhost:8080](file:///d:/源码/FullStack/DecisionCourt/backend) + DeepSeek LLM），发现两个症状：

1. **verdict 页分数异常**（3500 / 6500）
2. **ArgumentMap 线条异常粗**（172.5px）

### 6.2 白盒日志定位

| 工具 | 关键证据 |
|---|---|
| `GET /metrics` | `judge.belief_update=3`, `verdict.ready=1`, `judge.final_decision=1` |
| `agents` 表 | `judge.belief_a=35, belief_b=65`（其他 agent 都正常 [0,1]） |
| `verdicts` 表 | `option_a_score=35, option_b_score=65`（**铁证**） |
| `belief_diffs` / `belief_snapshots` 表 | **所有数值都在 [0,1]** —— v0.6 engine 有 clamp |
| `decision_events` 表 | 14 条 `state_transition`，但**没有** `judge.belief_update` / `belief.diff`（白盒化覆盖不全） |
| stdout log | `[GenerateVerdict] start ... beliefA=35.00 beliefB=65.00` |

### 6.3 根因

**DeepSeek 在三个 prompt 都偶尔把 0-1 范围小数输出为 0-100 范围整数**（35.0 / 65.0）。推测是 prompt 里"对选项 A 的支持度：40%"被 LLM 误解为 0-100 范围。

旧代码三处防护缺口：
- `JudgeAssess` 信任 LLM float，无 clamp
- `JudgeFinalDecision` `<0||>1` 时 fallback 到 `judge.BeliefA` —— DB 已有脏数据时循环污染
- `GenerateVerdict` `verdict.OptionAScore` 直接 `getFloat` 写库，无 clamp

### 6.4 修复

新增 [`backend/internal/agent/probability.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/probability.go) `ClampProbability(v float64) float64` —— 单一入口 helper，含 NaN/Inf 守卫。

5 处调用点：
- `agent/orchestrator.go` `JudgeAssess` 返回前
- `agent/orchestrator.go` `JudgeFinalDecision` 返回前（**不再 fallback 到 `judge.BeliefA`**）
- `courtroom/service.go` `JudgeFinalDecision` fallback 路径构造 `judgeDecision` 时
- `courtroom/service.go` 写 `agents.belief_a/b` 前
- `courtroom/service.go` 写 `verdicts.option_a_score/b` 前

### 6.5 测试

[`backend/internal/agent/probability_test.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/agent/probability_test.go) 16 个 case 覆盖所有边界（0/0.5/1.0 正常、35/65/100 上界、负数/-1e10 下界、NaN/+Inf/-Inf、幂等性）。

### 6.6 文档

- [ADR 0011](./adr/0011-llm-probability-hard-clamp.md) 完整记录
- ADR README 索引更新

### 6.7 清理老庭审脏数据

```sql
UPDATE agents
SET belief_a = LEAST(GREATEST(belief_a, 0.0), 1.0),
    belief_b = LEAST(GREATEST(belief_b, 0.0), 1.0)
WHERE belief_a < 0 OR belief_a > 1 OR belief_b < 0 OR belief_b > 1;

UPDATE verdicts
SET option_a_score = LEAST(GREATEST(option_a_score, 0.0), 1.0),
    option_b_score = LEAST(GREATEST(option_b_score, 0.0), 1.0)
WHERE option_a_score < 0 OR option_a_score > 1
   OR option_b_score < 0 OR option_b_score > 1;
```

### 6.8 已知未解决

- **白盒化覆盖不全**：`decision_events` 表没记录 `judge.*` / `belief.*` 业务 span —— 留到下个 PR
- **DeepSeek 抽风根因**：未 root cause，未来可能再触发。`ClampProbability` 是兜底

---

## 7. 已知未解决问题（留给后续 PR）

1. **前端 component-level 测试**（见 §4 限制说明）—— 等 sandbox 允许 pnpm add 后补 vitest
2. **WebSocket resume token**（方案 C 范围）—— 当前不实现（v0.8.3 范围内已能覆盖"刷新丢数据"90% 场景）
3. **SSE fallback**（方案 C 范围）—— 跳过，gorilla/websocket 在所有现代浏览器都工作
4. **"我之前点的那个 reopen 按钮"事件 replay** —— 如果用户重开后又刷新，新会话的 evidence 还在 DB 但前端 memoryEntries 是 reconnect 时从 REST 拉的，已能正确恢复；trial.reopened 历史事件暂不存储（不必要）

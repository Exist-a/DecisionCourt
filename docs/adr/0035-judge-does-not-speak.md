# ADR 0035: 法官（judge Agent）在庭审中不作为独立发言人

| | |
|---|---|
| **编号** | 0035 |
| **标题** | v2.1 法官不发言 — isJudging 路径清理 + 保留未来启用能力 |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-09-01 |
| **触发** | v2.1 工作树审计发现 `isJudging` prop 是死代码；本轮 v2.1 收尾（F3） |
| **依赖** | ADR 0030 §4「法官判决书是否考虑 rebuttal 状态」已暗示当前 MVP 不" + 后端无 `judge.speak` WebSocket 事件 |
| **替代决策** | (a) 让 judge 在庭审中作为独立发言人公开发言 / (b) 保留现状不做死代码清理 / (c) **本决策**：删除 prop 路径但保留 CSS / variant 资产以备未来启用** |
| **影响** | `frontend/components/courtroom/AgentAvatar.tsx` + `frontend/components/courtroom/silhouettes/RoleSilhouette.tsx` + `frontend/components/courtroom/avatars/DotAvatar.tsx` + `frontend/components/courtroom/silhouettes/Silhouette.tsx` + `frontend/components/courtroom/animations/AvatarAnimations.tsx` |

---

## 1. 决策

### 1.1 背景

v2.1 工作树（v2.0 之后未提交）里 9 个文件改动中，**4 个组件接收 `isJudging` prop**：

| 文件 | 用法 |
|---|---|
| `AgentAvatar.tsx:254` | `isJudging={agent.agent_type === "judge" && isSpeaking}` |
| `silhouettes/RoleSilhouette.tsx` | 接收 → 透传给 DotAvatar / Silhouette |
| `avatars/DotAvatar.tsx` | `{isJudging && <span className="dot-judge-shock" />}` |
| `silhouettes/Silhouette.tsx` | `<JudgeSilhouette isJudging={isJudging} />` → 法槌加 `silhouette-gavel` class |
| `animations/AvatarAnimations.tsx` | `deriveAnimationState({ isJudging })` 返回 `"judging"` 状态 |

**核心问题**：`isJudging` 的真值条件 `agent.agent_type === "judge" && isSpeaking` 依赖 `isSpeaking === true`，而 `isSpeaking` 来自后端 `judge.speak` WebSocket 事件——但**后端从未发过此事件**：

- `backend/internal/courtroom/service.go:433-444` 创建 judge agent
- `service.go:504-535` `RunOpeningSpeeches` 仅调用 prosecutor + defender
- `service.go:1292-1308` judge 仅通过 `JudgeAssess` 评估信念
- `service.go:1555-1598` 最终判决路径调 `JudgeFinalDecision` 但不发 `judge.speak` 事件

**结论**：`isJudging` 永远是 `false`，4 个组件里所有 `isJudging` 相关代码路径**从未被执行过**。

### 1.2 选项对比

| 维度 | A. 让 judge 在庭审中作为独立发言人 | B. 保留现状（不清理死代码） | **C. 删除 prop 路径 + 保留资产（本决策）** |
|---|---|---|---|
| **触及 §2.1 裁决逻辑** | 强（法官发言直接影响庭审叙事和裁决） | 否 | 否 |
| **改动范围** | 大：后端发 `judge.speak` + ClerkPrompt 重写 + frontend state machine + UI 测试 | 0 | 小：删 5 处 prop，4 处函数体 |
| **回归风险** | 高：影响 verdict 文案生成路径（裁决类变更需用户授权） | 0（不动） | 低：CSS class / judgeVariant 保留，未来启用时 git revert |
| **可观测性** | 用户看到法官现场参与辩论 | 当前用户看到法官纯旁观（仅审判偏度仪表） | 同 B |
| **何时再做** | n/a（不做） | 持续累积技术债 | 触发条件：v3.0 端侧 TTS 启动 / 用户要求 |

### 1.3 选择 C 的具体原因**：

1. **触及裁决逻辑**（AGENTS.md §2 红线）：让法官作为发言人是"裁决类变更"，需用户先讨论后授权。本轮目标（v2.1 收尾 + 三个开关默认开启）不涉及裁决，不擅自做。
2. **保留资产路径**：本决策不是"永远不做法官发言"，而是"清理死代码 + 保留资产"——`judgeVariant` / `silhouette-gavel` / `dot-judge-shock` / `dot-judge-shock-pointer` 全部 CSS 与 variant 都**保留**，未来启用时：
   - `AvatarAnimations.tsx` 加回 `judgeVariant` import 与 `judging` 状态
   - 4 个组件加回 `isJudging?: boolean` prop（git revert 即可，diff 极小）
   - 后端发 `judge.speak` 事件（v3.0 端侧 TTS 启动时统一讨论）
3. **当前法官仍是关键角色**：judge 通过 `judge.belief_update` + `judge.final_decision` 两个事件**间接表达**立场，影响 Clerk 生成 verdict 时的 prompt（`orchestrator.go:586` `ClerkPromptWithJudgeDecision(...)`）。F3 只清理前端 prop 路径，**不动后端 belief 计算**。

---

## 2. 实施细节（F3 已落地）

### 2.1 前端改动

```diff
// AgentAvatar.tsx
- isJudging={agent.agent_type === "judge" && isSpeaking}

// RoleSilhouette.tsx — 接收 + 透传删除
- isJudging?: boolean
- <DotAvatar isJudging={isJudging} />
- <Silhouette isJudging={isJudging} />

// DotAvatar.tsx — 接收删除 + 装饰层条件删除
- isJudging?: boolean
- {isJudging && <span className="dot-judge-shock dot-judge-shock-active" data-judging="true" />}

// Silhouette.tsx — JudgeSilhouette 子组件接收删除
- function JudgeSilhouette({ isJudging })
- className={isJudging ? "silhouette-gavel" : ""}
- data-judging={isJudging ? "true" : "false"}

// AvatarAnimations.tsx — 状态机分支删除
- | "judging"
- if (opts.isJudging) return "judging"
```

### 2.2 保留的资产（未来启用路径）

| 资产 | 文件 | 状态 |
|---|---|---|
| `judgeVariant` (framer-motion 4-keyframe 敲锤) | `lib/animations/variants.ts:70-77` | ✅ 保留导出 + 测试契约保留 |
| `.silhouette-gavel` CSS keyframe | `app/globals.css:443-451` | ✅ 保留 |
| `.dot-judge-shock` / `.dot-judge-shock-active` CSS | `app/globals.css` | ✅ 保留 |
| `.dot-judge-shock-pointer` / `.dot-judge-shock-pointer-active` CSS (JudgeBiasMeter) | `app/globals.css:631-661` | ✅ 保留（F1 已修位置错位） |

### 2.3 未来启用 checklist（v3.0 端侧 TTS 启动时）

1. 后端：在 `courtroom/service.go` 选择 phase（推荐 `closing` 或新 phase `judge_summary`）发 `judge.speak` 事件，content = `JudgeFinalDecision.Reasoning`
2. 前端：`AvatarAnimations.tsx` 加回 `judgeVariant` import + `judging` 状态
3. 前端：4 个组件加回 `isJudging?: boolean` prop（git revert F3 即可）
4. ADR：新增 ADR 0036「v3.0 法官发言启用」讨论裁决语义变化

---

## 3. 不在本 ADR 范围

- **法官判决书是否考虑 rebuttal 状态**（ADR 0030 §4 显式列出）—— 属"裁决逻辑"变更，按 §2 红线需用户先讨论，本 ADR 不擅自做
- **judge belief 计算逻辑**（`belief/engine.go`）—— F3 不动
- **verdict 生成路径**（`orchestrator.go:586` `ClerkPromptWithJudgeDecision`）—— F3 不动

---

## 4. 验证

- `pnpm tsc --noEmit` PASS
- `pnpm test` 101/101 PASS（含 4 个 F3 死代码删除断言 + 1 个 judgeVariant 契约保留测试）
- 浏览器冒烟未跑（法官偏倚仪表本就不触发，新代码应视觉零回归）

---

## 5. 关联

- **前置**：v2.0 Silhouette (ADR 0034) + v1.0.4 PR-C3 Framer Motion (ADR 0033) + ADR 0030 候选 4 rebuttal
- **后续**：v3.0 端侧 TTS（V3.0-PLAN.md）— 法官发言启用 checklist 见 §2.3
- **教训**：v2.0 引入 judge 装饰层时未与后端 `judge.speak` 事件对齐，导致 6+ 个月死代码（postmortem §5 规则 3「抽象 > 模拟」教训：UI 装饰状态必须对应真实事件流）
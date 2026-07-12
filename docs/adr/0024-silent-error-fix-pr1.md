# ADR 0024: 静默错误全局修复 PR 1（后端 UserFacingError 类型 + broadcast）

| | |
|---|---|
| **状态** | ✅ Accepted（2026-07-12 决策 + 实装） |
| **决策日期** | 2026-07-12 |
| **影响范围** | `backend/internal/courtroom/errors.go`（新）+ `statemachine.go` + `service.go` + `api/handler.go` + `api/websocket.go` + `agent/react_runner.go` + `errors_test.go`（新） |
| **触发事件** | 2026-07-08 用户反馈"庭审无反应" → session `09282a8a-25c2-4019-83e0-eb1c22f49e11` opening 阶段死锁,前端无任何反馈 |
| **完整计划** | [`.trae/documents/silent-error-fix-plan.md`](../../.trae/documents/silent-error-fix-plan.md) v1.0 |
| **本 PR 范围** | PR 1: 后端 UserFacingError 类型 + 改造 4 个静默错误点 + 状态机新 action + 单元测试 |

---

## 1. 决策

把 4 个静默错误点（opening speeches 失败 / WS throttle / action 失败 / 状态机拒绝）改成结构化 UserFacingError 广播。

| 之前（裸字符串） | 之后（UFE） |
|---|---|
| `Broadcast({code:"OPENING_SPEECHES_FAILED", message:"..."})` | `BroadcastUserFacingError(ufe)` where ufe 包含 class/code/message/detail/recovery[] |
| 前端 `grep "OPENING_SPEECHES_FAILED" frontend/` = **0 匹配** | 前端按 `event.payload.class / .code` switch 渲染 Toast/Modal |

**为什么先做 PR 1**：12 个错误黑洞里 5 个 P0，PR 1 是后端基础（其他 6 个 PR 都依赖 UFE 类型）。

---

## 2. 实施内容（PR 1）

### 2.1 新增类型（courtroom/errors.go）

| 类型 | 作用 |
|---|---|
| `ErrorClass`（`user_input` / `transient` / `degraded` / `fatal`） | 前端展示策略（Toast / Modal / Banner） |
| `ErrorCode`（11 个常量） | 机器可读错误码,前端 switch |
| `RecoveryAction`（type/label/action/navigate_to） | 前端按钮文案 + 后端 action 名 |
| `UserFacingError`（class/code/message/detail/recoverable/recovery/session_uuid） | 广播 envelope |
| `StateMachineError`（typed error） | 让 `ClassifyError` 用 `errors.As` 拿到 phase |

### 2.2 关键方法

- `ClassifyError(err) UserFacingError`：自动分类 Go error → UFE
  - `agent.ErrReactMaxIterations` → `ClassFatal + CodeOpeningSpeechesFailed + 3 recovery`
  - `agent_gateway.ErrBudgetExhausted` → `ClassFatal + CodeBudgetExhausted + 1 navigate`
  - `*StateMachineError` → `ClassUserInput + CodeActionStateRejected`
  - 兜底 → `ClassTransient + CodeActionFailed + retry`
- `BroadcastUserFacingError(sessionUUID, ufe)`：包装成 `Event{Type:"error"}` 广播,自动填 sessionUUID
- 链式 API：`WithDetail / WithRecovery / MarkNonRecoverable / WithSessionUUID`

### 2.3 状态机新 action（PR 1.3）

| Action | 允许 phase | 行为 |
|---|---|---|
| `force_skip_opening` | opening | transition opening → cross_exam(round=1),广播 opening.finished |
| `restart_opening` | opening | 异步重跑 RunOpeningSpeeches,失败再广播 UFE |

`ValidateAction` 返回值从 `fmt.Errorf` 改为 `*StateMachineError`（typed error），让 ClassifyError 能拿到当前 phase 信息拼出友好中文消息。

### 2.4 4 个静默错误点改造

| 位置 | 之前 | 之后 |
|---|---|---|
| `handler.go:354-366` opening speeches 失败 | 裸字符串 `OPENING_SPEECHES_FAILED` | `BroadcastUserFacingError(ClassifyError(err))` |
| `websocket.go:217` WS throttle | 裸字符串 `WS_THROTTLED` | `NewUserFacingError(ClassUserInput, CodeActionThrottled, "操作过于频繁...")` |
| `websocket.go:261` action 失败 | 裸字符串 `ACTION_FAILED + err.Error()` | `BroadcastUserFacingError(ClassifyError(err))` |
| `handler.go:323` trial rate limit (429) | 4 个字段 envelope | 4 字段 + `user_facing_error` 子 envelope |

### 2.5 单元测试（errors_test.go, 13 个 case 全过）

- `TestClassifyError_ReactMaxIterations`：3 个 recovery action 顺序 + action 名正确
- `TestClassifyError_BudgetExhausted`：navigate recovery
- `TestClassifyError_StateMachineReject`：message 带 phase 名
- `TestClassifyError_GenericFallback`：retry recovery
- `TestClassifyError_NilError`：返回零值
- `TestUserFacingError_ChainMethods`：链式 API 完整性
- `TestUserFacingError_MarshalJSON_RecoveryNotNull`：JSON 输出 `"recovery":[]` 不是 `null`（前端 switch 关键）
- `TestUserFacingError_WithSessionUUID`：链式 setter
- `TestService_BroadcastUserFacingError`：fake broadcaster 捕获 Event,验证 payload 字段
- `TestService_BroadcastUserFacingError_AutoFillSessionUUID`：自动填 sessionUUID
- `TestStateMachineError_ErrorString`：err.Error() 格式兼容旧 slog
- `TestStateMachine_ValidateAction_ForceSkipOpening`：opening 允许,其他 7 个 phase 拒绝
- `TestStateMachine_ValidateAction_RestartOpening`：同上
- `TestStateMachine_ValidateAction_LegacyErrorMessages`：5 个旧 case 的 err.Error() substring 兼容回归
- `TestClassifyError_StateMachineReject_MessageFormat`：UFE.Message + Detail 字段带 phase + action

---

## 3. 用户决策（2026-07-12）

| Q# | 问题 | 答案 |
|----|------|------|
| Q1 | 先做哪个 P0 | **静默错误 PR 1**（推荐）|
| Q2 | Fatal 错误展示策略 | **Toast 不自动消失 + 提供按钮**（不阻塞庭审页）|
| Q3 | JWT 库选型 + 有效期 | golang-jwt/jwt/v5 + 30 天（待 P0-1 用）|
| Q4 | 容器非 root UID | 10001:10001（待 P0-3 用）|

---

## 4. 验证

- ✅ `go build ./...` 全过
- ✅ `go test ./internal/courtroom/` 全过（含新 errors_test.go 13 个 case）
- ✅ `go test ./internal/api/ ./internal/agent/` 全过（除 4 个 v0.9.4 HA-001 -skip spec，与 PR 1 无关）
- ✅ 前端兼容性：payload 是 map[string]interface{},前端 switch event.payload.class / .code 即可（PR 2-3 实装）

---

## 5. 未完成（PR 2-7 范围）

按 silent-error-fix-plan.md §3：

- [ ] **PR 2**: 前端 Toast 系统 + errorBus（`frontend/components/ui/Toast.tsx` + `frontend/lib/errorBus.ts` + `lib/api.ts::fetchJson` 401/错误处理 + `app/layout.tsx` 挂 `<ToastContainer />`）
- [ ] **PR 3**: CourtroomScene 错误反馈接入（7 个 try/catch + `event.type === "error"` 处理 + WS onConnectionStateChange）
- [ ] **PR 4**: Verdict 页 + auth.ts 错误反馈
- [ ] **PR 5**: 文档同步（`decisioncourt-api-design.md` §5 + `decisioncourt-prd.md` §4.5 + `tech-spec.md` §6.3 + `ux-refinement.md` §3）
- [ ] **PR 6 (P1)**: Breaker fallback Banner + Budget 耗尽 Modal + Investigator dispatch 失败 Banner
- [ ] **PR 7 (P2)**: ErrorBoundary 顶层兜底

**预计 PR 2-7 工作量**：10-15 天（前端 + 全栈联调）。

---

## 6. 设计权衡

### 6.1 为什么 typed error (`*StateMachineError`) 而不是 sentinel (`ErrStateMachineRejected`)

- sentinel 只能判断"是不是状态机错",无法拿到 phase / action 信息
- typed error 用 `errors.As(err, &stateErr)` 拿到 `stateErr.CurrentPhase`,前端可以显示"当前是 opening 阶段,不能做 direct_verdict"
- 旧 fmt.Errorf 调用方零修改（*StateMachineError 也实现 error 接口,err.Error() 输出格式兼容）

### 6.2 为什么 Recovery 字段不用 omitempty

- 前端 switch case 默认期待 array,如果后端偶尔给 null,前端 `event.payload.recovery.map(...)` 会 crash
- omitempty 在 slice 空（len=0）时也省略,导致前端拿不到 recovery 字段
- 强制输出 `"recovery":[]` 让前端代码 100% 一致（不需要 null check）

### 6.3 为什么 BroadcastUserFacingError 是 Service 方法

- errors.go 引用 `agent` + `agent_gateway` 做 sentinel 匹配（无 cycle）
- 如果 BroadcastUserFacingError 在 api 包写,需要 import courtroom 包 + 给 courtroom.Service 加 public method 暴露 broadcaster,反而更复杂
- 当前方案：errors.go 在 courtroom 包,BroadcastUserFacingError 写在 errors.go 末尾,直接 `s.Broadcast(...)` 调用同包 Service 方法,零 cycle 零 boilerplate

### 6.4 Detail 字段安全性

- Detail 字段是 err.Error(),可能含 Go 内部信息（如 sql table name、JSON 路径）
- 当前实装默认填充（dev 模式友好）
- 未来 PR 6 (P1 错误脱敏) 会按 `cfg.GinMode == release` 决定是否填充 Detail
- 短期风险可控：ErrReactMaxIterations / ErrBudgetExhausted / StateMachineError.Error() 都不含敏感信息

---

## 7. 关联文档

- 主计划：[`.trae/documents/silent-error-fix-plan.md`](../../.trae/documents/silent-error-fix-plan.md) v1.0
- 安全审计：[`.trae/documents/security-audit-2026-07-03.md`](../../.trae/documents/security-audit-2026-07-03.md) v1.0（P0-1 鉴权 / P0-3 容器硬化待做,Q3/Q4 答案已记录）
- ADR 0023：[`0023-github-actions-ci-pause.md`](0023-github-actions-ci-pause.md)（CI 调试结束，✅ v0.10.15 端到端跑通）
- 状态机：[`courtroom/statemachine.go`](../../backend/internal/courtroom/statemachine.go)
- 错误类型：[`courtroom/errors.go`](../../backend/internal/courtroom/errors.go)

---

## 8. 时间线

```
2026-07-08  用户反馈庭审无反应 + silent-error-fix-plan.md v1.0 写完
2026-07-12  用户授权 + 4 个设计决策 + ADR 0024 写完
            - PR 1 实装:errors.go + statemachine + service + handler + websocket + tests
            - 13 个新单元测试全过
            - go build + go test 无回归
            - 待 PR 2-7（前端 + 联调）
```
# Deferred Items（2026-08-21 新增）

| | |
|---|---|
| **生成日期** | 2026-08-21 |
| **状态** | ⏸ **Deferred**（等待下次 PR 修复） |
| **触发** | 用户在 v1.0.3 PR-B1 (Prompt Lab) 启动验证时复现 |
| **关联 PR** | v1.0.3 PR-B1 = commit `f1720f7` + 三个 dev compose 修复 (`eef932e`/`bea7289`/`1eb037f`) |

---

## D2. cross-exam 庭审记录 content 为空（silent error 黑洞）

### 现象
- 用户在前端庭审记录页面看到"质证 · 第 1 轮"+双方发言，但发言 `content`字段为空（显示「」）
- backend 日志 `promptlab loaded 1.0.3-pr1@dev` 正常，a2a bus 推送 round=1 public speech 正常
- DB 实际状态：session 仍 `current_phase=opening, current_round=0`，`messages` 表只有 opening r0 + cross-exam r1/r2 共 6 行，但**所有 cross-exam 行的 `content` 字段 length=0**

### 根因（精确路径）

`backend/internal/agent/react_runner.go` 第 391-512 行的 `case ActionSpeak` 分支：

1. `runner.Run(ctx, messages)` 走 ReAct 主循环
2. LLM 返回决策 JSON（如 `{"action":"speak","content":""}`） — 决策 JSON 里 content 通常是空字符串（这是预期：ReAct 决策不包含发言，发言走流式生成）
3. 第 408-413 行：因为传了 `OnSpeakChunk`（`chunkCb`），走**流式生成** `streamSpeakContent`
4. 第 919 行：`streamSpeakContent` 调 LLM `StreamComplete`拿 chunks，渐进提取 `"content":"..."` 字段
5. **流式解析失败**（`indexOfJSONField` 找不到 / 30s timeout / chunk Err）→ 返回 `("", false)`
6. 第 418-419 行：`streamSucceeded = false`, `out.Content = ""`
7. 第 416 行 `ValidateAgainstHallucination("")` 失败 → 触发 retry 路径（第 458-512 行）
8. **retry 也可能 silent 失败**（LLM retry 也返回空 content / parse 失败），最终返回 `speaker.Content = ""`
9. 第 451/512 行 `applySpeakerLengthLimit(speaker)` 对空串返回空串
10. `saveAgentMessage` 写入 DB（length=0），`broadcastAgentSpeak` 推送空 content → frontend 显示「」

### 这是 §4.1 silent error 黑洞典型场景

错误被吞，没 WARN/ERROR 日志，**庭审记录全空但 backend 不报错**。这正是 v0.10.17 silent-error 修复想解决但仍残留的角落。

### 与 v1.0.3 PR-B1 无关

| 维度 | 状态 |
|---|---|
| 修改的文件 | `internal/agent/react_runner.go`（v0.10.x 已稳定）+ `internal/courtroom/service.go` |
| 我的 PR-B1 | `internal/promptlab/*` + `internal/agent/prompts.go`（baseRules 来源切换）+ `cmd/server/main.go` |
| `prompts/base.yaml` 内容 | 与 v1.0.2 hardcoded 完全一致（17 条规则 + 输出格式 + stance 说明一字不差） |
| `promptlab loaded 1.0.3-pr1@dev` | 正常加载，baseRules 内容 1:1 等价 v1.0.2 |

证据 — DB 真实状态（直接 psql 验证）：

```
cross_exam | 1 | 0 | speak | 04:54:36.143617+00  -- prosecutor cross-exam r1, content 空
cross_exam | 1 | 0 | speak | 04:54:51.164196+00  -- defender cross-exam r1, content 空
cross_exam | 2 | 0 | speak | 05:01:02.183392+00  -- prosecutor cross-exam r2, content 空
cross_exam | 2 | 0 | speak | 05:01:15.494071+00  -- defender cross-exam r2, content 空
```

backend log 同步显示：

```
{"msg":"[v0.6][saveAgentMessage] session=... agentType=prosecutor phase=cross_exam round=1 len(content)=0"}
{"msg":"[v0.6][saveAgentMessage] session=... agentType=defender phase=cross_exam round=1 len(content)=0"}
```

### 触发用户场景（用户 2026-08-21 反馈）
"提交了证据后开始质证，随后每轮都无法显示"
- 提交证据本身不会触发 phase 卡 opening（SubmitEvidence 不切 phase，service.go:614 "No auto-trigger here"）
- 用户在 opening 阶段点"开始质证"按钮 → `start_cross_exam` action → `transitionPhase(opening → cross_exam, 1)` + `runCrossExamRound`
- `runCrossExamRound` 走 `speakWithReAct` → `runner.Run` → 上述 silent error 路径 → cross-exam content 空

### 建议修复（不在本次 PR-B1 范围，下次 PR）

**新 PR `fix/cross-exam-content-empty`**，工作量 2-3 天：

1. **`streamSpeakContent` 加显式 WARN 日志**：解析失败时 log "stream parse failed, falling back to retry" with chunk 长度 / raw 摘要
2. **`validateSpeak` + `applySpeakerRebuttalRetryLoop` silent error 检测**：检测到 speaker.Content="" 时返回 error 而不是继续 fallback
3. **加 3 个 regression test**：
   - 流式返回空内容 → 重试成功后产生正常 content
   - 流式连续失败 → 返回 error 而不是空 content（防止写 DB）
   - 30s timeout → 同样返回 error
4. **saveAgentMessage 入口加 sanity check**：检测到 speaker.Content=="" 时拒绝写 DB + 返回 error（最后兜底）

### 关联文档
- `docs/V1.0.3-PLAN.md` §3 PR-B1 范围（已 commit `f1720f7`）
- `docs/adr/0031-prompt-lab-architecture.md`（v1.0.3 Prompt Lab 架构 ADR）
- `docs/release-notes/v1.0.2.md`（前置版本，cross-exam 候选 4 已落地但 content 流式解析这块未单测覆盖）
- `backend/internal/agent/react_runner.go:391-512`（需要修复的代码位置）
- `backend/internal/courtroom/service.go:2046`（`saveAgentMessage` 入口日志）

### 不做什么（明确边界）

- ❌ 不在本 PR-B1 范围动 react_runner.go（已 commit `f1720f7` 仅动 promptlab + agent/prompts.go）
- ❌ 不在 silent error 路径加 panic（防止 SIGKILL 让整个 trial crash）
- ❌ 不引入新外部依赖（如 LangSmith）—— 与 v1.0.3 调研结论一致

---

## D3. direct_verdict 判决书 fallback 显示 "本场庭审共 0 轮"

### 现象
- 用户在第 3 次质证（cross-exam round=3）过程中点"直接判决"
- 最终庭审书显示"本场庭审共 0 轮。LLM 生成失败，依据法官信念度直接裁决，未生成过程纪要。"
- DB 实际状态：session `e638978e-...` 是 `current_phase=deliberation, current_round=0`，但 cross-exam r=1, r=2, r=3 都实际跑过（messages 表有 6 行 cross-exam content）

### 根因（双重，按发生顺序）

**根因 A — `cancelCall` 触发的 ctx cancel 跨 finishTrial**

`backend/internal/courtroom/service.go`：
- 第 636 行 `s.cancelCall(session.SessionUUID)` 取消 in-flight LLM（如 cross-exam r=3 还在跑）
- 第 1502 行 `s.withCancel(ctx, sessionUUID)` 创建新 ctx 并注册到 `activeCalls[sessionUUID]`
- 第 1558 行 `JudgeFinalDecision` 调 LLM 失败：`context canceled`
- 第 1615 行 `GenerateVerdict` 调 LLM 失败：`context canceled`

backend log 时间线（2026-08-21 05:21）：

```
05:21:14.04  cross-exam r=3 prosecutor 写 DB (content=749)
05:21:14.04  a2a bus.go:101 context canceled  ← direct_verdict 触发的 cancelCall
05:21:14.19  state transition cross_exam → closing round=0  ← 第1521行 transitionPhase(closing, 0)
05:21:24.39  closing prosecutor 写 DB (content=557)
05:21:24.49  closing defender content=0  ← silent error (D2 同一根因)
05:21:24.53  state transition closing → deliberation round=0
05:21:24.57  JudgeFinalDecision failed: context canceled
05:21:24.60  GenerateVerdict failed: context canceled
05:21:24.60  fallback 显示 "本场庭审共 0 轮"
```

**根因 B — `transitionPhase(PhaseClosing, 0)` 重置 round**

`service.go` 第 1521 行：

```go
if err := s.transitionPhase(&session, model.PhaseClosing, 0); err != nil {
```

`finishTrial` 进入 closing 时**显式传 round=0**，把 DB 里的 `current_round` 重置为 0（不管之前 cross-exam 跑到 round 几）。fallback 第 1625 行用 `session.CurrentRound` 显示"共 N 轮" → 始终 0。

### 与 D2 的关系

- D2 让 cross-exam r=1, r=2 的 messages content 为空
- D2 在 closing 第 2 步（defender）也命中，closing defender content=0
- D3 是 D2 的次生影响：cross-exam content 全空时，GenerateVerdict 看到 messages 几乎全空，LLM 拒答 → fallback → 但 fallback 又用了被重置的 round=0

### 建议修复（不在本次 PR-B1 范围，下次 PR）

**新 PR `fix/direct-verdict-fallback-round`**，工作量 1 天：

1. **`finishTrial` 进 closing 前保存当前 round 到 session 字段**（如 `final_round`）或不重置 round，或者 finishTrial 接受传入 round 参数
2. **fallback 文案改用 messages 表 `MAX(round)` 算真实轮数**（最简方案）：

```go
realRound := 0
for _, m := range messages {
    if m.Round > realRound && (m.Phase == "cross_exam" || m.Phase == "opening") {
        realRound = m.Round
    }
}
result["trial_summary"] = fmt.Sprintf("本场庭审共 %d 轮...", realRound)
```

3. **JudgeFinalDecision / GenerateVerdict 加 retry-on-canceled**：检测到 `context canceled` 时**用新 ctx 重试 1 次**（仅在 cancelCall 触发的取消上 — 普通用户断网不应该重试）

### 触发用户场景（用户 2026-08-21 反馈）
"最终庭审书显示本场庭审共 0 轮。LLM 生成失败，依据法官信念度直接裁决，未生成过程纪要。我在第三次质证过程中点击直接判决。"

### 关联文档
- `backend/internal/courtroom/service.go:635-641` (direct_verdict)
- `backend/internal/courtroom/service.go:1501-1638` (finishTrial + GenerateVerdict fallback)
- `backend/internal/courtroom/service.go:1521` (`transitionPhase(closing, 0)` 重置 round)
- `backend/internal/courtroom/service.go:301-333` (withCancel + activeCalls 注册)
- `backend/internal/courtroom/service.go:341-348` (cancelCall)
- D2（cross-exam silent error 黑洞）— D3 的前置依赖

### 不做什么（明确边界）

- ❌ 不在本次 PR-B1 范围动 service.go（已 commit `f1720f7` 仅动 promptlab + agent/prompts.go）
- ❌ 不重命名 `current_round` 字段（向后兼容）
- ❌ 不在 cancelCall 加白名单（与 D2 处理原则一致 — fail-soft 而非 fail-hard）
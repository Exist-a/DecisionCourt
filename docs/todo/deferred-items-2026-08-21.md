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
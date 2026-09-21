# ADR 0040 — silent error D2 + direct_verdict fallback round D3 收尾（v2.6）

| | |
|---|---|
| **状态** | ✅ Accepted |
| **日期** | 2026-09-21 |
| **作者** | Agent（用户授权"项目干净一些"启动 v2.6 silent error 收尾） |
| **关联版本** | v2.6（silent error 黑洞 D2 收尾 + direct_verdict fallback round D3 修复） |
| **触及 §2.1 裁决逻辑** | **轻**（fallback 文案 + `transitionPhase(round)` 参数；不改裁决算法） |
| **取代** | — |
| **被取代** | — |

---

## 1. 背景

`docs/todo/deferred-items-2026-08-21.md` 在 v1.0.3 PR-B1 启动验证时登记了两项 silent error 黑洞：

- **D2 cross-exam content 为空**：用户在 v1.0.3 PR-B1 启动后，cross-exam r1/r2/r3 跑出 `content length=0` 但 backend 不报错，前端庭审记录页显示空「」气泡。session e638978e 复现。
- **D3 direct_verdict fallback "共 0 轮"**：用户在 cross-exam r=3 过程中点"直接判决"，最终庭审书 fallback 文案显示"本场庭审共 0 轮"，但实际跑了 3 轮 cross-exam。session e638978e 同一会话。

`fix/cross-exam-content-empty` 分支（基于旧 base `29daff0` 的 `694a89e` commit）已实现修复代码与 9 个 sub-test，但该分支与 main 平行，修复未合入 main。V1-ROADMAP.md 行 290 / 307 已引用 `694a89e` 作为"已修"——这是过时引用，需在本次合入时一并更新。

v2.5 安全 P1 收尾后（`134531a`），用户授权"项目干净一些"，把 D2 + D3 收尾作为 v2.6 主题。

---

## 2. 决策

**A 方案（采用）：基于 main HEAD `134531a` 重写 commit**

不复用 `694a89e`（基于旧 base 平行线，merge 冲突爆炸）。代码逻辑（436 行净增）照搬，commit 重写以 base 在 main 上。配套：

- 同步发 v2.6 release notes
- 删孤儿分支 `fix/cross-exam-content-empty`（本地 + origin）
- 更新 V1-ROADMAP / deferred-items / AGENTS.md 三处过时引用

**B 方案（拒绝）：merge `694a89e`**

平行线 merge 冲突会让审查 + 调试成本 ~3 倍。**A 方案** blast radius 更小，与 v2.5 文档结构一致。

---

## 3. D2 修复：cross-exam content silent error 黑洞

### 现状（v2.5 之前）

- `backend/internal/agent/react_runner.go::streamSpeakContent` 三个 silent 失败点：
  1. `<-streamCtx.Done()` ctx 取消（line 982-985）—— silent
  2. `c.Err != nil` chunk 错误（line 992-993）—— silent
  3. `lastExtracted == ""` 流式空 content（line 1053-1068）—— 已有 `slog.Warn` 但 inline truncation
- `backend/internal/courtroom/service.go::saveAgentMessage` 已有 `strings.TrimSpace(speaker.Content)==""` 拦截 + 返 error，**但无 log line**。上游 caller 三种行为：
  - `RunOpeningSpeeches` / `runCrossExamRound`：`if err := ...; return err`（hard fail）
  - `resumeOpening` / `resumeClosing` / `finishTrial`：bare call（无错误检查）
- 结果：流式失败 → 空 content → caller 静默落入 DB + 推送给前端 → 庭审记录全空但不报错

### 风险

- §4.1 silent error 黑洞典型场景：用户看到庭审记录全空，**不知道**是 silent LLM failure 还是网络问题还是其他
- D3 是 D2 的次生：cross-exam content 全空 → `GenerateVerdict` 看到 messages 几乎全空 → LLM 拒答 → fallback → fallback 又显示 0 轮（D3 修复后）

### 修复（v2.6, 2026-09-21）

- `react_runner.go`：
  - **ctx-cancel 分支**：加 `slog.Warn("react_runner streamSpeakContent ctx canceled", "chunks", chunks, "raw_prefix", truncateForLog(collected.String(), 200))`
  - **chunk Err 分支**：加 `slog.Warn("react_runner streamSpeakContent chunk err", "err", c.Err, "chunks", chunks, "raw_prefix", truncateForLog(...))`
  - **empty lastExtracted 分支**：用 `truncateForLog(collected.String(), 500)` 替换原 inline truncation
  - **新增 `truncateForLog` helper**：字节截断 + `...(truncated, total=NB)` 注明总长，避免 LLM 长输出撑爆日志
- `service.go::saveAgentMessage`：
  - REJECT 时加 `log.Printf("[v0.6][saveAgentMessage] REJECT empty content: ... (silent LLM failure guard)", ...)`
  - sentinel 关键词从 `silent error guard` 改为 `silent LLM failure guard`（便于日志检索 + 与 v0.10.17 历史修复一致）
- caller 适配：
  - `resumeOpening` / `resumeClosing` / `finishTrial` 三个函数 6 处 bare call 包成 `if err := ... { log.Printf("[v0.6][<scope>] skip ... broadcast due to empty content: %v", err) } else { broadcast }`
  - **不动** `RunOpeningSpeeches` / `runCrossExamRound` 的 hard-fail 模式（D2 修复只针对恢复路径，正常 trial 路径硬 fail 是合理的）
- 新增 9 个 regression sub-test（见 §5）

### 关键设计决策

- **沉默拦截 vs panic**：选择沉默拦截（log + skip broadcast）。理由：silent error 黑洞一旦 panic 会让整个 trial SIGKILL；恢复路径偶发空 content 不应阻塞 trial 流程（用户提交证据不会丢，只是某条 message 缺失）。
- **不动 `transitionPhase` 签名**：D3 fix 在调用点传 `session.CurrentRound` 而非引入新参数。理由：blast radius 最小，未来 round 字段语义变化只改一处。
- **`slog.Warn` vs `log.Printf`**：react_runner.go 全文统一 `slog.Warn`（line 432, 505, 1062），本次新增三处 WARN 用 `slog.Warn` + key/value 配对，**不**引入 stdlib `log`。`saveAgentMessage` 用 `log.Printf` 匹配 courtroom/service.go 既有的 `[v0.6][scope]` 风格（line 2046, 2127）。

---

## 4. D3 修复：direct_verdict fallback "共 0 轮"

### 现状（v2.5 之前）

- `service.go::finishTrial` 第 1521 行：`transitionPhase(&session, model.PhaseClosing, 0)` —— **显式传 0** 把 round 字段重置为 0
- `service.go::finishTrial` 第 1625 行（fallback 文案）：用 `session.CurrentRound` 显示"本场庭审共 N 轮" —— **已被前一行重置为 0**

### 风险

- 用户在第 3 次质证过程点"直接判决" → 庭审书显示"共 0 轮" → 用户混淆

### 修复（v2.6, 2026-09-21）

- `finishTrial` 第 1521 行：`transitionPhase(&session, model.PhaseClosing, 0)` → `transitionPhase(&session, model.PhaseClosing, session.CurrentRound)`
- `finishTrial` 第 1625 行（fallback 文案）：用 `session.CurrentRound` → `maxRound(messages)` 算跨 phase 真实轮数
- 新增 `maxRound(messages []model.Message) int` helper：处理 nil / 全 0 / 混合边界（5 个 sub-test 覆盖）

### 关键设计决策

- **不重命名 `current_round` 字段**：向后兼容，schema 不变
- **`maxRound` vs `maxRoundByPhase`**：本次只算 max(round)，**不**按 phase 过滤。理由：fallback 文案用户在意"总共几轮"（跨 opening + cross-exam），按 phase 过滤反而信息少。D2 让 cross-exam content 全空时 round 字段仍写入 DB，maxRound 仍能反映真实轮数。
- **不在 cancelCall 加白名单**：与 D2 处理原则一致（fail-soft 而非 fail-hard）

---

## 5. 测试覆盖

### D2 (4 sub-test) + D3 (5 sub-test) — 新增 `backend/internal/courtroom/`

- `save_agent_message_d2_test.go`:
  - `TestD2_SaveAgentMessage_RejectsEmptyContent`：拦截逻辑 + sentinel 关键词稳定性
  - `TestD2_SaveAgentMessage_AcceptsNonEmptyContent`：合法路径不误伤
  - `TestD2_ErrorMessage_ContainsSentinel`：sentinel 关键词稳定性
  - `TestD2_ResumeOpening_SkipsBroadcastOnEmptyContent`：caller 行为契约
- `finish_trial_d3_test.go`:
  - `TestD3_MaxRound_EmptySlice_ReturnsZero`：nil/空 slice 边界
  - `TestD3_MaxRound_AllZeroRound`：全 0 边界
  - `TestD3_MaxRound_MixedRounds_ReturnsMax`：核心契约（opening r0 + cross-exam r1/r2/r3）
  - `TestD3_MaxRound_OnlyClosingRounds`：D3-b 边界（closing round 保留）
  - `TestD3_FinishTrial_Fallback_UsesMaxRound_NotSessionRound`：e2e 模拟 e638978e

### 验证结果

```
go build ./...     # 0 错误
go test ./...      # 23 包 100% PASS（含新增 9 sub-test）
```

---

## 6. 不做的事（明确边界）

- ❌ **不**重构 `streamSpeakContent` 流式解析逻辑（commit message 列为后续项；D2 修复仅是 silent error 检测 + 拦截，**不**修根因）
- ❌ **不**加 `JudgeFinalDecision` / `GenerateVerdict` retry-on-canceled（cancelCall 触发的 ctx cancel 链跨 finishTrial 仍可能让 fallback 路径比正常路径更常见；D3 修复的是 fallback 文案准确度，**不**修 fallback 频率）
- ❌ **不**改 `transitionPhase` 签名（D3 在调用点传 `session.CurrentRound`，blast radius 最小）
- ❌ **不**碰 `RunOpeningSpeeches` / `runCrossExamRound` 的 hard-fail 行为（D2 修复只针对恢复路径，正常路径硬 fail 是合理的）
- ❌ **不**重新命名 `current_round` 字段（向后兼容）
- ❌ **不**改 §2.1 裁决逻辑（fallback 文案 + `transitionPhase(round)` 参数是字段保留 + 文案准确度；**不**改 JudgeFinalDecision / GenerateVerdict / direct verdict 选择算法）

---

## 7. 文档同步

- `docs/todo/deferred-items-2026-08-21.md §D2 + §D3` 状态更新：⏸ → ✅
- `docs/V1-ROADMAP.md §0` 新增 v2.6 进度行；line 82 移除 D2/D3 blocker；line 290 / 307 过时 `694a89e` 引用改为新 commit hash
- `docs/release-notes/v2.6.md` 新增（套 v2.5.md 14 章节模板）
- `AGENTS.md §6.2b` 追加 v2.6 D2/D3 收尾说明
- 删孤儿分支 `fix/cross-exam-content-empty`（本地 + origin）
- `docs/adr/README.md` 索引表追加 0040 行（如适用）

---

## 8. 关联文档

- [ADR 0039 security-p1-batch-c](./0039-security-p1-batch-c.md) — 前一版（v2.5 安全 P1 收尾）
- [release-notes/v2.6.md](../release-notes/v2.6.md) — v2.6 实施细节 + 时间线
- [todo/deferred-items-2026-08-21.md](../todo/deferred-items-2026-08-21.md) — D2 + D3 原始 deferred 登记
- [V1-ROADMAP.md §0 + §6](../V1-ROADMAP.md) — v2.6 进度 + 风险/持续维护更新
- [silent-error-fix-plan.md](../../.trae/documents/silent-error-fix-plan.md) — 早期 v0.10.17 silent-error 全局修复设计稿（v0.10.25 PR 1-5/7 已修 12/12 黑洞，本次 D2 是其中 cross-exam content 子集的 v2.6 收尾）
- fix branch `694a89e`（已删，保留 commit hash 作为历史锚定）
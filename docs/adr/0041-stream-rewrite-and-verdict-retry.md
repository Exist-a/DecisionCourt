# ADR 0041 — v2.7 silent error 根因 100% 收尾 (stream parser rewrite + judge verdict retry)

| | |
|---|---|
| **状态** | ✅ Accepted |
| **日期** | 2026-09-21 |
| **作者** | ZCode Agent |
| **关联版本** | v2.7 |
| **触及 §2.1 裁决逻辑** | **否** (root-cause fix + retry-on-canceled, 不改裁决算法) |
| **取代** | ADR 0040 §6 第一条 ❌ "不重构 streamSpeakContent" + §6 第二条 ❌ "不加 JudgeFinalDecision/GenerateVerdict retry-on-canceled" |
| **被取代** | — |

---

## 1. 背景

ADR 0040 (v2.6, 2026-09-21) 把 silent error 黑洞**变可见**(slog.Warn + saveAgentMessage REJECT),留下两条**根因未修**:

- **§6 第一条 ❌**: `streamSpeakContent` 流式解析逻辑保持原状,只是 caller 拦截 + log
- **§6 第二条 ❌**: `JudgeFinalDecision` / `GenerateVerdict` 不加 retry-on-canceled

### 1.1 残余症状(v2.6 后)

虽然 v2.6 把 D2/D3 (cross-exam 空 content + verdict "0 轮") 在 user-visible 行为上修了,根因仍在:

1. **stream parser 边界 case 仍偶尔 silent fail**:
   - LLM emit \uXXXX 中文 → unquoteJSONString 跳过 backslash,前端收到 literal `\u4e2d\u6587` 而非 "中文"
   - 嵌套 JSON `{"meta":{"content":"inner"},"content":"outer"}` → indexOfJSONField 选 inner 而非 outer
   - chunk 切 `"content"` literal 边界 → scanner 没看到完整 key,跳过整个 extraction
2. **verdict 阶段仍可能 fallback**: direct_verdict 触发 cancelCall, race 条件下 JudgeFinalDecision 收到 `context.Canceled` → fallback 到 belief values 文案 (即使 detached ctx 已加,极端 race 仍可触发)

### 1.2 触发背景

v2.6 release notes "🔜 v2.X 后剩余项" 明确列出:

- `streamSpeakContent` 流式解析根因 —  中度修复
- `JudgeFinalDecision` / `GenerateVerdict` retry-on-canceled —  小修复

用户 2026-09-21 在 ExitPlanMode 后授权"进行计划,将这些问题解决",批准 v2.7 计划 (2026-09-21)。本 ADR 是 v2.7 实施交付。

---

## 2. 决策

### 2.1 方案对比

**方案 A (采用)**: 双 PR 同时交付
- PR-1: stream parser 重写 + caller 三态分流
- PR-2: detached verdict ctx + retry hook + activeCalls cleanup

理由:
- 风险/收益对称(stream + verdict 是 silent error 闭环的两个 endpoint)
- 双 PR 测试基线已经形成 (v2.6 D2/D3 测试 + `react_runner_streaming_test.go`)
- 用户期望"silent error 黑洞根因 100% 收尾" 一气呵成

**方案 B (拒绝)**: 拆 v2.7 + v2.8 两个版本
- v2.7 仅修 PR-1, v2.8 再修 PR-2
- 理由: 风险更小,失败回滚影响范围小
- 拒绝: 用户已明确要"100% 收尾",延期不必要。Plan 已经评估风险可控。

### 2.2 PR-1 设计: stream parser 重写

#### 2.2.1 新 helper `scanJSONContentField(raw string) (value string, complete bool)`

替换 `indexOfJSONField` + inline escaping parse 的 ad-hoc 链。新逻辑:

1. **跳前置非 `{`**: 支持 markdown wrap / "Sure, here's JSON:" / 中文前言
2. **brace+bracket depth tracking**: 仅 `depth==1` 时匹配顶层 `"content"` key,避免 nested object 内同名 key 误判
3. **完整 unquote `\uXXXX`**: 通过 `unquoteJSONString` 增强 (4 个 hex digit → rune)
4. **complete 语义**: 仅当 closing `"` 后是 `,` / `}` / EOF 才算 true (partial 时 false,前端 typewriter 用)

#### 2.2.2 `streamSpeakContent` 重写签名 `(value, complete bool)`

旧签名 `(string, bool)` 把"是否成功"和"是否有内容"绑在一个 bool 里。流式解析失败时只能返 `("", false)`,无法区分"成功但空"和"真正失败"两种情况。

新签名让 caller 三态分流:
- `complete && value!=""` → streamSucceeded=true
- `complete && value==""` → LLM 显式空 (走 retry,不让空 content 漏过)
- `!complete` → timeout / ctx-cancel / chunk Err (走 retry,内部 WARN)

#### 2.2.3 caller 适配 (ActionSpeak L408-471)

新增 `else if complete && streamed == ""` 分支 + WARN,告知该路径是 v2.7 收尾的边界。

### 2.3 PR-2 设计: detached verdict ctx + retry hook

#### 2.3.1 finishTrial 入口 detached verdictCtx (service.go:1517)

```go
ctx, cancel, err := s.withCancel(ctx, session.SessionUUID) // trialCtx (受 cancelCall)
...
verdictCtx, vcancel := context.WithTimeout(context.Background(), 120*time.Second) // detached
```

DB writes (closing statements, broadcasts) 仍用 trialCtx;JudgeFinalDecision + GenerateVerdict 用 verdictCtx。

借鉴 `agent/orchestrator.go:451` (`recordSideEffects`) + `:322` (`makeMemoryHook`) 的 `context.WithTimeout(context.Background(), 3s)` pattern。

#### 2.3.2 提取 `completeWithCancelRetry` helper (orchestrator.go)

retry hook 必须走 helper **function scope** (非 if-block scope),因为 Go `defer` 按 function scope:

```go
func (o *Orchestrator) completeWithCancelRetry(ctx, label, prompt, msgs, opts) (string, llm.Usage, error) {
    content, usage, err := o.llmClient.Complete(ctx, ...)
    if errors.Is(err, context.Canceled) {
        var (rContent string; rUsage llm.Usage; rErr error)
        func() {
            retryCtx, rc := context.WithTimeout(context.Background(), 90*time.Second)
            defer rc() // helper scope 内 cleanup,不会污染 caller
            rContent, rUsage, rErr = o.llmClient.Complete(retryCtx, ...)
        }()
        ...
    }
    return content, usage, err
}
```

**关键发现**: 一开始我把 `defer rc()` 写在 if block 内,Go defer 按 function scope 不是 block scope,导致 retry ctx 在 JudgeFinalDecision return 后才 cleanup,这期间 retry ctx 已被 cancel,测试拿到的 `retryCallCtx.Err() = context.Canceled`。改成 IIFE 让 defer 在 helper scope 内立刻 cleanup。

#### 2.3.3 runCrossExamRound clearCancel defer 补漏

`finishTrial` 已有 `defer s.clearCancel(session.SessionUUID)` (L1523);`runCrossExamRound` (L1225) 之前缺失,导致 cancel 后 `activeCalls[sessionUUID]` 残留 stale cancel func 直到下次 withCancel 覆盖。

#### 2.3.4 retry 行为契约

- **One-shot**: 仅 1 次 retry,不再无限 (LLM 真挂要 propagate)
- **Cancelled only**: 仅 `errors.Is(err, context.Canceled)` 触发;`context.DeadlineExceeded` / `network unreachable` / `JSON parse` 都不 retry (timeout 是真慢,parse 是格式错,retry 无效)
- **Detached**: retry 用 `context.Background()` 派生,90s timeout

---

## 3. 改动清单

### 3.1 PR-1 文件

| 文件 | 改动 |
|---|---|
| `backend/internal/agent/react_runner.go` | 重写 `streamSpeakContent` (L919-1083);增强 `unquoteJSONString` 加 `\uXXXX` 解码;新增 `scanJSONContentField` helper + `isJSONWhitespace` / `isHex` / `hexNibble`;删除 `indexOfJSONField` (死代码,只 self-caller);适配 caller L408-471 三态分流 |
| `backend/internal/agent/react_runner_stream_boundary_test.go` (NEW) | 22 个边界 case 测试 (parser unit + unquote unit + streamSpeakContent e2e + 回归 v2.6 D2) |

### 3.2 PR-2 文件

| 文件 | 改动 |
|---|---|
| `backend/internal/courtroom/service.go` | finishTrial L1517-1523: 加 `verdictCtx, vcancel := context.WithTimeout(...)` + defer vcancel();JudgeFinalDecision (L1585) + GenerateVerdict (L1642) 改用 verdictCtx;runCrossExamRound L1225 后加 `defer s.clearCancel(session.SessionUUID)` |
| `backend/internal/agent/orchestrator.go` | imports 加 `errors`;`GenerateVerdict` (L605) + `JudgeFinalDecision` (L750) 改用 `completeWithCancelRetry` helper;新增 helper 函数 (function scope 内 IIFE 保证 defer 清理) |
| `backend/internal/agent/orchestrator_verdict_retry_test.go` (NEW) | 7 个 retry hook 测试 (retry on Canceled / GenerateVerdict retry / non-cancel no retry / Deadline no retry / parent ctx cancel recovery / retry quick / one-shot limit) |

---

## 4. 测试覆盖

### 4.1 PR-1: 22 个新测试全 PASS

parser unit (12):
- T1 basic happy path
- T2 explicit empty
- T3 markdown wrap
- T4 escaped Chinese `\uXXXX`
- T5 nested 多 "content" key — top level wins
- T5b nested only, no top level → ("", false)
- T6 truncated partial
- T7 preamble prose
- T7b leading whitespace + Chinese
- T8 no JSON object
- T9 content key missing
- T11 raw value escaped quote
- T12 top-level close before content

unquoteJSONString unit (3):
- Basic escapes (`\"` `\\` `\n` `\t` `\r`)
- Unicode escape (`\uXXXX` → rune)
- Invalid unicode passes through verbatim

streamSpeakContent e2e (4):
- T7 chunk split content literal
- T5 explicit empty triggers retry
- T8 nil callback is opt-out (新增 — 修正我初版断言错误)
- T4 escaped Chinese via stream
- RegressionD2 ctx-canceled mid-stream (v2.6 baseline)

### 4.2 PR-2: 7 个新测试全 PASS

- `TestJFV2Retry_FirstCanceled_RetrySucceeds` — 第一次返 Canceled,retry detached ctx 成功
- `TestGenerateVerdictRetry_FirstCanceled_RetrySucceeds` — GenerateVerdict 同上
- `TestJFV2Retry_NonCanceledError_DoesNotRetry` — network error 不重试
- `TestJFV2Retry_DeadlineExceeded_DoesNotRetry` — DeadlineExceeded 不重试
- `TestJFV2Retry_ParentCtxCanceled_RetryRecoversCtx` — 父 ctx 已 cancel 时 retry 仍成功
- `TestJFV2Retry_RetryHappensQuickly` — retry 不消耗时间 (<1s)
- `TestJFV2Retry_OneShotLimit` — 第二次也 Canceled 时 propagate,不再 retry

### 4.3 全包回归

`go test ./...` 22 包全 OK,0 regression。

---

## 5. 关键设计决策

1. **Helper IIFE 模式**: `completeWithCancelRetry` 必须用 helper function + 内嵌 IIFE,因为 Go `defer` 按 function scope 不是 block scope。block-level defer 让 retry ctx 持续 alive 到 caller function 退出,caller 函数已 cleanup 后留下的 cancelled ctx 让测试断言无法做 (我在 debug 中才发现这一陷阱)。

2. **retryCallCtx 断言改 "派生" 而非 "alive"**: 测试最初断言 `retryCallCtx.Err() == nil`,但 helper return 后 IIFE defer 已 cleanup retryCtx。所以改成 `retryCallCtx != firstCallCtx && hasDeadline()` — 检查 retry 用了 detached ctx (Background+90s),而不是绝对 alive。

3. **partial vs final 不分大小写**: 旧实现 partial 也走 OnSpeakChunk。新实现只在 `!complete` 时推 partial,`complete=true` 时 final 推一次 (避免重复)。这样前端 typewriter 拿到的是单调前缀递增序列。

4. **`scanJSONContentField` 不匹配 nested `content`**: 律师 agent 只 emit `{"content":"..."}` 单层结构,深度 > 1 时不匹配。如果未来 LLM 输出嵌套 JSON,parser 返 ("", false),caller 走 retry。

5. **`unquoteJSONString` 非法 `\u` verbatim 保留**: `\uZZZZ` 或 `\u12` (不足 4 位) 时保留反斜杠 + 原字节,避免吞掉错误信息。让上游能定位 LLM 输出异常。

6. **不删 `indexOfJSONField`**: 它只被 `streamSpeakContent` 用过;删除避免留死代码(AGENTS.md 习惯)。

---

## 6. 不做的事 (明确边界)

- ❌ 不改 §2.1 裁决逻辑 (verdict 文案 / 证据采纳 / 法官判词算法)
- ❌ 不动 belief calc (`belief_diffs` 表 / `ClampProbabilityPair`)
- ❌ 不动 RebuttalHook (auto-overturn wiring 留给 v2.9 PR-4)
- ❌ 不改 `transitionPhase` 签名 (v2.6 D3 已用过 round 保留 pattern)
- ❌ 不删 streaming_test.go 现有 happy path 测试 (向后兼容 v0.10.1 协议)

---

## 7. 文档同步

- `docs/release-notes/v2.7.md` (NEW)
- `docs/V1-ROADMAP.md` §0 + §6 加 v2.7 进度行
- `docs/todo/deferred-items-2026-08-21.md` §D2 + §D3 翻 ✅ Done (v2.7)
- `docs/adr/0040 §6` 第一条 + 第二条 ❌ → ✅ "v2.7 落地"
- `AGENTS.md §6.2b` v2.6 + v2.7 同步

---

## 8. 关联文档

- [ADR 0040 silent error D2 + D3 收尾](./0040-silent-error-d2-d3-closeout.md) — 前一版 (v2.6)
- [ADR 0021 v0.10.1 streamSpeakContent 首次引入](release-notes/v0.10.1) — 协议原始设计
- [release-notes/v2.7.md](../release-notes/v2.7.md) — v2.7 实施细节 + 测试 + 时间线
- [todo/deferred-items-2026-08-21.md](../todo/deferred-items-2026-08-21.md) D2 + D3 — v2.7 关闭
- [V1-ROADMAP.md §0 + §6](../V1-ROADMAP.md) — v2.7 进度 + 风险/持续维护更新

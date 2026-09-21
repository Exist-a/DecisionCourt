# ADR 0042 — v2.8 PR-3 LLM Trace 全量 prompt 持久化开关 (FileLoggerPrompts tri-state)

| | |
|---|---|
| **状态** | ✅ Accepted |
| **日期** | 2026-09-21 |
| **作者** | ZCode Agent |
| **关联版本** | v2.8 |
| **触及 §2.1 裁决逻辑** | **否**（LogEntry 持久化,不影响裁决算法 / 推理 / prompt） |
| **取代** | ADR 0033 §3.2 "Input/Output 留空" 后续 PR 待办 |
| **被取代** | — |

---

## 1. 背景

ADR 0033 (v1.0.4) 设计 LLM Trace 时**显式承认**一个 trade-off:

> ⚠️ **Input/Output 留空**：plan §2 设计了但 LogEntry 没存，后续 PR 补"全量 prompt 落盘"开关

本 ADR 是补这个 PR 的设计。

### 1.1 实际症状 (v1.0.4 至今未修)

- `agent_gateway.FileLogger` 38 字段 (v2.6) 把每次 LLM 调用元数据写盘,但**不写 prompt / 不写 content**
- `internal/trace/parser.go::entryToRun` 把 LogEntry 映射成 `Run`,但 `Run.Input / Output / Tags` 永远是 nil / "" / nil — 前端 `AgentTraceNode` 渲染 input/output 时空 block 永远不出来
- 用户/ops 想看 LLM "到底被喂了什么 prompt, 回了什么" 只能 `grep + jq`,没法在 `/api/v1/courtrooms/:uuid/traces` 页面可视化

### 1.2 触发背景

ADR 0033 §3.2 已显式 defer;用户 2026-09-21 在 ExitPlanMode 后授权 v2.7+v2.8+v2.9 计划。v2.8 PR-3 实施 = 本 ADR。

---

## 2. 决策

### 2.1 方案对比

| 维度 | A. 全部 log 全开 | **B. tri-state 默认 metadata (采用)** | C. 默认 off |
|---|---|---|---|
| 默认行为 | 全量落盘 | v2.7 baseline (metadata only) | 全不开 |
| 用户需 opt-in? | 否 (默认隐私风险) | 是 (设 `full` 才落盘) | 是 (设 metadata 或 full) |
| 向后兼容 | 破坏 (旧 PR 看不到 prompt 字段但文件变了) | **不破坏** | 破坏 (Run.Input 行为变化) |
| PII 风险 | 默认暴露 | opt-in 用户承担 | 低 |
| ADR 0033 §3.2 精神 | 不符 (rollback 不友好) | **符合** (defer 标注 future opt-in toggle) | 部分符合 |

**B 方案采用**:借鉴 `AppEnv` 三态 enum (`dev | staging | prod`) 的合法 precedent (config.go §ValidateAppEnv), 用户需显式设 `full` 才能 opt-in 完整 prompt 落盘。

### 2.2 决策细节

**单字段开关 = 不足**:

- "bool 开关 = 全开" 会让默认行为变 → 文件大小增长 + Run.Input 行为变化,破坏 v2.7 baseline
- "bool 开关 = 全关" 不区分用户想要 "完全不写" (合规) vs "只写 metadata" (debug)

**三态 enum = 必须**: 借鉴 `AppEnv` (`dev | staging | prod` validated at startup) 模式:

- `off` = 完全不写 LogEntry (合规 / GDPR audit 场景)
- `metadata` = 只写 metadata (默认, v2.7 baseline 行为)
- `full` = 写 system prompt + input messages + output content (按 `FileLoggerPromptsMaxBytes` 截断)

**默认 `metadata`**: 保 backward compat — 旧 PR 部署后不需要立即升级 log retention,新 deployment 可以 opt-in `full`。

### 2.3 设计要点

#### 2.3.1 LogEntry 加 4 optional 字段 (file_logger.go L52-65)

```go
SystemPrompt    string         `json:"system_prompt,omitempty"`
InputMessages   []llm.Message  `json:"input_messages,omitempty"`
OutputContent   string         `json:"output_content,omitempty"`
OutputTruncated bool           `json:"output_truncated,omitempty"`
```

**omitempty 关键**: metadata / off 模式不填这些字段 → `json.Marshal` 自动跳过 → 旧 log 行 (v2.7 时期) 解析仍 zero value。

#### 2.3.2 writeFileLog 签名 + 分流 (gateway.go L465-530)

```go
func (g *Gateway) writeFileLog(
    ...existing args..., systemPrompt string, messages []llm.Message, content string,
) {
    if g.cfg.IsFileLoggerPromptsOff() { return }  // "off" = 整个 no-op
    ...
    if g.cfg.IsFileLoggerPromptsFull() {
        // 截断到 MaxBytes (默认 32KiB),防单 entry 撑爆文件
        entry.SystemPrompt, _ = truncateForLogBytes(systemPrompt, g.cfg.FileLoggerPromptsMaxBytes)
        entry.InputMessages = messages
        entry.OutputContent, _ = truncateForLogBytes(content, g.cfg.FileLoggerPromptsMaxBytes)
        entry.OutputTruncated = sysTrunc || contentTrunc
    }
    ...
}
```

2 个调用点 (`Complete` L310 + `StreamComplete` L451) 传 `systemPrompt` + `messages` + 流式累积的 `totalContent`。

#### 2.3.3 entryToRun 字段映射 (parser.go L183-203)

```go
var input map[string]any
if entry.SystemPrompt != "" || len(entry.InputMessages) > 0 {
    input = map[string]any{
        "system_prompt": entry.SystemPrompt,
        "messages":      entry.InputMessages,
    }
}
```

**类型兼容**: 现有 `Run.Input` 是 `map[string]any`(历史 trace 系统设计),装 `{"system_prompt": ..., "messages": [...]}` 是最简非破坏映射。

#### 2.3.4 Frontend `max-h-72` 防长 output 撑布局

`<pre className="..." overflow-x-auto max-h-72>` (替代仅 `overflow-x-auto`)。32 KiB output 滚出屏幕而不撑高 trace card。

#### 2.3.5 ValidateFileLoggerPrompts fail-fast (config/validate_file_logger_prompts.go)

跟 `ValidateAppEnv` 同级 (`cmd/server/main.go:51` 后)。拼写错误 (`"FULL"` / `"Metadata"` 大小写错误) 立即退出不让 silent miss。

### 2.4 关键理由

1. **三态 enum 模式已有 precedent**: `AppEnv` tri-state (`dev|staging|prod`) 在 config.go L319-329 已有完整 fallback pattern,直接借鉴 validator + env read + 默认值补全三件套。
2. **omitempty 后向兼容零成本**: Go stdlib `json.Marshal` 自动跳过 zero value (空字符串 / nil slice / false bool),旧 v2.7 时期 log 行不需要 reparse 即可 zero-value 填充 `Run.Input / Output`,测试覆盖 (TestParseReader_V28_OldLog_NoPromptFields)。
3. **三态默认 metadata 不破 v2.7 baseline**: 即使用户打 v2.8 tag 但忘了设 `full`, Run.Input 仍是 nil — 前端没有变化 (RebuttalTraceNode / AgentTraceNode 已有 nil-guard L121)。
4. **PII 风险 opt-in**: 用户设 `full` 前明确知道自己在做什么 (PII 进 logs); 风险在用户侧而不是默认路径。

---

## 3. PII 与磁盘风险分析

### 3.1 PII 风险

`full` 模式会暴露 evidence.content + message.content 等用户提交的自由文本到 `logs/agent_gateway_*.log`。**风险等级**: 中-高。

**缓解**:

- 默认 `metadata` — 用户需显式 opt-in
- Validate fail-fast 拼写错误不让 silent miss
- `max-h-72` (frontend) 让长 entry 滚屏而非膨胀 DOM

**没有缓解** (用户责任):

- 给 LLM 真名 / 身份证号 / 信用卡 → 落地到 `logs/`
- 与 `AGENT_GATEWAY_ENABLED=true` 的 LLM 调用无关 (PII 早就走 LLM 服务方)

### 3.2 磁盘膨胀风险

- metadata 模式: ~1KB/entry × 50 calls/trial = 50KB/trial (v2.7 baseline)
- full 模式: ~5-50KB/entry × 50 calls/trial = 250KB-2.5MB/trial

**`FileLoggerPromptsMaxBytes = 32 KiB` 默认 cap** 防 LLM 长输出撑爆日志文件,但**不防 trial 累积膨胀**。

**没有缓解** (后续 PR): 当前 logs/ 无 retention policy — full mode 每日 12.5MB,一年 4.5GB。Deferred 到 v2.9 PR-X (D4 logs retention, 新建 `docs/todo/deferred-items-2026-09-21.md` 登记)。

---

## 4. 改动清单

### 4.1 Backend

| 文件 | 改动 |
|---|---|
| `backend/internal/agent_gateway/file_logger.go` | LogEntry 加 4 optional 字段 (SystemPrompt / InputMessages / OutputContent / OutputTruncated) |
| `backend/internal/agent_gateway/gateway_config.go` | `FileLoggerPrompts` + `FileLoggerPromptsMaxBytes` 字段;`IsFileLoggerPromptsFull()` + `IsFileLoggerPromptsOff()` helpers;`Normalize()` 默认 `metadata` + 32 KiB |
| `backend/internal/agent_gateway/gateway.go` | `writeFileLog` 签名加 3 参数 (systemPrompt + messages + content); 2 个调用点 (`Complete` L310 + `StreamComplete` L451) 传递;新增 `truncateForLogBytes` helper |
| `backend/internal/config/config.go` | `AgentGatewayConfig` 加 `FileLoggerPrompts` + `FileLoggerPromptsMaxBytes` 字段;`Load()` 加 2 个 `envOrDefault{String,Int}` (默认 `"metadata"` + `32*1024`) |
| `backend/internal/config/validate_file_logger_prompts.go` (NEW) | `ValidateFileLoggerPrompts(v string) error` 三态 enum validator |
| `cmd/server/main.go` | `ValidateFileLoggerPrompts()` fail-fast 启动 (与 `ValidateAppEnv` 同级) |
| `backend/internal/trace/parser.go` | `entryToRun` 加 prompt 字段映射 (SystemPrompt+InputMessages → `Run.Input` map;OutputContent → `Run.Output` string) |
| `backend/internal/agent_gateway/file_logger_v2_8_test.go` (NEW) | 6 个边界 case 测试 (omitempty + round-trip + 4 个 truncate 边界) |
| `backend/internal/trace/parser_v2_8_test.go` (NEW) | 4 个测试 (向后兼容 + 正向映射 + entryToRun direct) |
| `backend/internal/config/validate_file_logger_prompts_test.go` (NEW) | 8 个 case (3 legal + 5 illegal) |

### 4.2 Frontend

| 文件 | 改动 |
|---|---|
| `frontend/components/trace/AgentTraceNode.tsx` | input/output `<pre>` 加 `overflow-auto max-h-72` (替代 `overflow-x-auto`),防 32 KiB 长 output 撑布局 |

---

## 5. 测试覆盖

### 5.1 Backend (18 new sub-test)

`file_logger_v2_8_test.go` (6):
- `TestFileLogger_V28_OmitsEmptyPromptFields` — omitempty 严格测试,空 entry 写盘不应出现 4 个新 key
- `TestFileLogger_V28_LogEntryCarriesPromptFields_RoundTrip` — 4 字段 marshal+unmarshal round-trip 保真
- `TestTruncateForLogBytes_BelowLimit_NoTruncate`
- `TestTruncateForLogBytes_AboveLimit_TruncatesAtMax`
- `TestTruncateForLogBytes_ZeroMax_NoTruncate`
- `TestTruncateForLogBytes_ExactLength_NoTruncate`

`parser_v2_8_test.go` (4):
- `TestParseReader_V28_OldLog_NoPromptFields` — v2.7 时期 log 行 zero-error 解析 (向后兼容契约)
- `TestParseReader_V28_NewLog_FullPrompts` — v2.8 full mode log 行 Run.Input / Output 正确填充
- `TestEntryToRun_OldLog_ZeroValues` — entryToRun direct 单元契约
- `TestEntryToRun_NewLog_PopulatedInputAndOutput`

`validate_file_logger_prompts_test.go` (8):
- `TestValidateFileLoggerPrompts_LegalValues` — `off|metadata|full` 3 sub-case
- `TestValidateFileLoggerPrompts_IllegalValues` — `OFF|OFF_METADATA|FULLLL|prompts|"  full  "` 5 sub-case

### 5.2 全包回归

`go test ./...` 22 包全 OK,0 regression。

### 5.3 Frontend

无新增 test (现有 `TrialReplay.test.ts` 已有 SSR 路径覆盖,仅 CSS utility 调整无需单测)。dev compose 业务验证时浏览器手动核对 `max-h-72` 效果。

---

## 6. 关键设计决策

1. **三态 enum 而非 bool**: 借鉴 `AppEnv` precedent,提供 `off | metadata | full` 三档而非 `bool on/off`。理由: 用户能区分"完全不写(合规)" vs "只写 metadata"。
2. **`Run.Input = map[string]any{...}` 而非新 struct**: 现有 Run.Input 是 `map[string]any`(trace 系统历史设计),装 `{system_prompt, messages}` 是最简非破坏映射,frontend 不需要改 type。
3. **`truncateForLogBytes` 用 byte limit (不是 rune)**: LLM 输出可能含 UTF-8 多字节字符,字节级 truncate 会末尾切半 → frontend 显示乱码。设计 trade-off: 接受末尾切半风险 (frontend 已 truncateForLog helper 有 watermark), 字节级一致性更易诊断。
4. **`InputMessages` 不截断数组**: 与 `SystemPrompt/OutputContent` 不同 — 每个 message 由 LLM token budget 控,整体大小仍 < `MaxBytes * 3`。truncate 整个 messages 数组会让 trial 记录不完整。
5. **`omitempty` 是 backward compat 关键**: 旧 log 行没有新字段,parser `json.Unmarshal` 自动 zero-value 填充;新 log 行有字段,Run.Input / Output 填充。即使 toggle on/off 多次,旧的 metadata log 行仍能解析。

---

## 7. 不做的事 (明确边界)

- ❌ 不动 §2.1 裁决逻辑 (LogEntry 持久化不动裁决算法)
- ❌ 不实现 logs/ retention policy (deferred 到 D4 logs retention 后续 PR,本次只 call out)
- ❌ 不动 `evidence_rebuttal_link` / `run` (auto-overturn wiring 留给 v2.9 PR-4 §2.1 范畴)
- ❌ 不改 Run 类型定义 (Input/Output 类型已是 `map[string]any` / `string`,promote 新字段用现有类型兼容)
- ❌ 不在前端 AgentTraceNode 加测试 (CSS utility 调整 + SSR-safe fixture 已覆盖)

---

## 8. 文档同步

- `docs/release-notes/v2.8.md` (NEW)
- `docs/V1-ROADMAP.md §0 + §6` 加 v2.8 进度行
- `docs/todo/deferred-items-2026-09-21.md` (NEW) §D4 logs retention 登记
- `docs/adr/0033 §3.2` 第一条 ⚠️ → ✅ "v2.8 落地"

---

## 9. 关联文档

- [ADR 0033 (v1.0.4) LLM Trace 架构](./0033-llm-trace-architecture.md) §3.2 — 本 ADR 实施其 deferred 项
- [release-notes/v2.8.md](../release-notes/v2.8.md) — v2.8 实施细节 + 测试 + 时间线
- [todo/deferred-items-2026-09-21.md §D4](../todo/deferred-items-2026-09-21.md) — logs/ retention deferred (本 PR call out)
- [V1-ROADMAP.md §0 + §6](../V1-ROADMAP.md) — v2.8 进度 + 持续维护

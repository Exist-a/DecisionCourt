# ADR 0044 — Agent Gateway Token 压缩策略审查与改进计划

| | |
|---|---|
| **状态** | ✅ Accepted (已实施) |
| **日期** | 2026-09-21 |
| **实施日期** | 2026-09-21 |
| **作者** | ZCode Agent |
| **关联版本** | v2.10 |
| **取代** | — |
| **被取代** | — |

---

## 1. 背景

ADR 0006 (2026-07-01) 设计了 Smart 评分压缩策略（评分 + 原子组 + 贪心打包 + 兜底摘要），解决了 v0.5+ Legacy keep-5 策略在庭审场景下丢失证据链的问题。ADR 0037 (v2.3) 为压缩器添加了全量 observability 埋点。

2026-09-21 对压缩策略进行代码审查，发现 7 个问题，按影响分为 P0/P1/P2/P3 四级。

---

## 2. 审查发现

### 2.1 问题清单

| # | 问题 | 优先级 | 影响 | 修复成本 |
|---|---|---|---|---|
| 1 | `IsSmartCompressionEnabled` 不继承 `isChildDefault()` | P0 | 配置 footgun | 1 行 |
| 2 | `ScoreThreshold` 是死配置 | P1 | 文档-代码不一致 | 5 行 |
| 3 | 原子组识别漏掉 evidence_id 链 | P0 | 证据链断裂 | 20 行 + 测试 |
| 4 | 评分没有 recency decay | P2 | 极端 budget 下退化 | 需调权重 |
| 5 | 按字符 ratio 而非 token ratio | P1 | 中文/代码混合偏移 | 10 行 |
| 6 | Summary 走 extractive 缺推理 | P3 | 推理链丢失 | 需 LLM 成本评估 |
| 7 | 无"压缩后判决质量"回环度量 | P2 | 长期观测缺口 | 需新指标设计 |

### 2.2 根因分析

**问题 #1：配置 footgun**

- **涉及代码**：`gateway_config.go:220-222`（IsSmartCompressionEnabled）、`config.go:205`（viper.SetDefault 设 true）
- **预期行为**：运维只设 `AGENT_GATEWAY_ENABLED=true`，期望所有子功能（包括 Smart Compression）都开启
- **实际行为**：`IsSmartCompressionEnabled()` 要求 `c.Enabled && c.SmartCompression`，不调用 `isChildDefault()`。虽然 viper.SetDefault 已经把 `SmartCompression` 默认设成 true，但设计意图是"必须显式开启"（注释原文："破坏性升级，需用户在 .env 显式开启"），与默认值矛盾
- **后果**：如果将来有人把 viper 默认改回 false，Smart Compression 会静默回落到 legacy，而其他子开关（PromptCompression、TokenBudget 等）仍然通过 `isChildDefault()` 自动开启

**问题 #2：死配置**

- **涉及代码**：`config.go:208`（viper.SetDefault 设 0.3）、`prompt_compressor.go:282`（写入 info.ScoreThreshold）
- **预期行为**：运维调 `AGENT_GATEWAY_SCORE_THRESHOLD=0.5`，期望只保留高分消息（score ≥ 0.5）
- **实际行为**：阈值被记录到 CompressionInfo（用于 metrics 上报），但**不参与筛选逻辑**。筛选完全由 `GreedyPack` 的 `keepRatio` 控制（70%/40%/20%），与分数阈值无关
- **后果**：配置存在但不起作用，违反"配置即代码"原则

**问题 #3：evidence_id 链漏识别**

- **涉及代码**：`prompt_atomic.go:27-72`（BuildAtomicGroups）、`orchestrator.go:838-841`（model.Message → llm.Message 转换）
- **预期行为**：同一证据在多轮被引用时（prosecutor 提出 → defender 反驳 → judge 评估），这些消息应该绑定成一个原子组
- **实际行为**：
  1. `BuildAtomicGroups` 只识别 `tool_call_id` 相同的消息组，**不识别 evidence_id 链**
  2. `orchestrator.speak()` 转换 `model.Message` → `llm.Message` 时，**丢弃了 `Metadata` 字段**（只取 Role 和 Content），导致评分器看到的 `m.Metadata["evidence_id"]` 永远是空的
- **后果**：证据链在压缩时可能被拆散，后续轮次引用时 LLM 看不到原始证据内容，产生幻觉

**问题 #4：无 recency decay**

- **涉及代码**：`prompt_scorer.go:14-15`（注释："分数不乘以 recency decay"）、`prompt_compressor.go:208-216`（强制保留最近 3 条）
- **预期行为**：近期消息应该比早期消息有更高权重
- **实际行为**：评分公式完全不考虑消息位置（除了首末各 +0.3）
- **后果**：极端 budget 下，早期高分消息（如开庭陈词）可能挤掉近期关键推理（如 judge 中期评估）

**问题 #5：字符 ratio ≠ token ratio**

- **涉及代码**：`prompt_greedy.go:15-18`（注释："按 ratio 截断在压缩档切换时行为更稳定"）、`prompt_greedy.go:30-31`（计算 totalBytes）
- **预期行为**：保留比例应该基于 token 数，因为 token 才是真正的预算单位
- **实际行为**：`GreedyPack` 用 `GroupLength`（字符数）计算 target
- **后果**：中文/代码混合场景，代码块被过度保留（"看起来"占空间但实际 token 少），挤掉了同等字符数但 token 更多的中文消息

**问题 #6：Summary 缺推理**

- **涉及代码**：`prompt_summary.go:23-76`（BuildEarlierSummary）、`prompt_summary.go:63-75`（summaryPreview 限 120 字）
- **预期行为**：摘要应该包含关键推理链（judge 法律依据、lawyer 论证逻辑）
- **实际行为**：只提取 anchor（evidence_id + 角色 + 120 字 preview），不含推理过程
- **后果**：LLM 看到"被告已知情"这个结论，但不知道依据是什么，可能编造其他依据

**问题 #7：无质量回环度量**

- **涉及代码**：`gateway.go:262-289`（LLM call 出口埋点）、ADR 0037（14 个 metric key）
- **预期行为**：metrics 应该能回答"压缩是否影响了判决质量"
- **实际行为**：所有 metrics 都是操作型指标（压缩比、耗时、token 用量），没有质量指标
- **后果**：无法评估压缩策略的实际效果，只能看"省了多少 token"，不能看"判决是否更准"

---

## 3. 决策

### 3.1 修复计划

**P0（下次 PR 必做）**：

1. **#1 配置 footgun**：在 `isChildDefault()` 里加 `!c.SmartCompression`，或者把 `IsSmartCompressionEnabled()` 改成也走 `isChildDefault()` 逻辑
2. **#3 evidence_id 链**：
   - 在 `orchestrator.speak()` 转换时，把 `model.Message.EvidenceRefs` 和角色信息写入 `llm.Message.Metadata`
   - 在 `BuildAtomicGroups` 第一遍加 evidence_id 分组（复用 `evidenceRef` 正则）

**P1（v2.10 候选）**：

3. **#2 死配置**：在 `GreedyPack` 里加一道筛：`if g.GroupScore < cfg.ScoreThreshold { continue }`
4. **#5 字符 ratio**：在 `GreedyPack` 入口加 token 估算（`chars / 3`），target 改用 token 算

**P2（需要独立 ADR）**：

5. **#4 recency decay**：引入 soft decay 公式 `final = (1-α) × score + α × recency_weight`，α=0.3
6. **#7 质量回环**：加 `verdict_evidence_accuracy` 指标，检查判决书引用的 evidence_id 是否存在于原 transcript

**P3（看预算）**：

7. **#6 abstractive summary**：加开关 `AGENT_GATEWAY_SUMMARY_ABSTRACTIVE=true`，开启后调轻量 LLM 生成 abstractive summary

### 3.2 关键理由

- #1 和 #3 影响正确性，修复成本低，必须优先
- #2 和 #5 是"配置即代码"和"预算单位一致性"问题，顺手改
- #4 和 #7 需要设计权重公式和新指标，各自需要独立 ADR
- #6 需要评估 LLM 成本，看预算决定

### 3.3 实施记录（2026-09-21，全部 7 项已落地）

| # | 状态 | 落地内容 |
|---|---|---|
| 1 | ✅ | `IsSmartCompressionEnabled()` 改为 `c.SmartCompression \|\| c.isChildDefault()`，与其他子开关一致（`gateway_config.go`） |
| 2 | ✅ | `GreedyPack` 新增 `scoreThreshold` 入参，排序后、贪心前预过滤低分组；`CompressScored` 传 `cfg.ScoreThreshold` |
| 3 | ✅ | (a) `orchestrator.speak()` 新增 `agentMap` 入参，把 `agent_type` + `evidence_id` 写入 `llm.Message.Metadata`；5 个 Speak 公开函数同步透传，`service.go` 构造 map。(b) `BuildAtomicGroups` 新增 evidence_id 分组 pass（已在 tool_call 组中的消息不重复成组，单条引用不成组） |
| 4 | ✅ | `ScoreMessages` 末尾加 soft decay `final = 0.7×score + 0.3×(i/(n-1))`，单条消息不施加（n>1 守卫） |
| 5 | ✅ | 预算单位由字符数改为**内容感知 token 估算**（CJK 1.5 tok/char、ASCII 0.25 tok/char），`AtomicGroup` 新增 `EstimatedTokens` 字段 |
| 6 | ✅ | 新增 `SummaryGenerator` 接口 + `NewSummaryGenerator`（LLM 实现）+ `BuildAbstractiveSummary`，由 `AGENT_GATEWAY_SMART_COMPRESSION_ABSTRACTIVE_SUMMARY` 门控（默认 false），失败自动回退 extractive；成功/空输出均记 slog（否则无法区分"没触发"和"触发了没生效"） |
| 7 | ✅ | 新增 `verdict_evidence_accuracy` gauge：`ExtractEvidenceDisplayIDs` + `ComputeVerdictEvidenceAccuracy` 纯函数，在 `finishTrial` 落库后记录 |

#### 实施中对 #5 的重要修正

原文建议"加 token 估算（`chars / 3`）"。**该方案是无效的**——`chars / N` 与旧的字符数预算是线性同构的，GreedyPack 的 `accum + gTokens > target` 比较在等比例缩放下结果完全一致，改变不了任何保留决策。

真正的根因是"字符数不能反映 token 成本"：中文单字约 1.5 token，ASCII 约 0.25 token。因此实施时改为按字符类别加权的 `EstimateTokens`：

```go
cjk*3/2 + other/4   // r >= 0x2E80 视为 CJK
```

这样代码块（ASCII 密集）不再"看起来占空间"，中文推理文本也不再被低估。

#### 实施带来的 baseline 变化

`TestCompressionEval_StrategyComparison` 的 `atomicKept` 由 10 → 9：token 精确计量后，同等 budget 下少装 1 组。经逐组核对，被丢的是 clerk 阶段总结这个低价值 singleton，judge 推理组与 tool_call 原子组全部保留——属预期行为，测试断言已从"精确 10"改为"≥ 9"的护栏语义。

### 3.4 验证结果

- `go test -count=1 ./...` 全 23 包 PASS
- `go vet ./...` 无告警
- 新增 33 个测试（含 8 个 `ExtractEvidenceDisplayIDs` 子用例 + 4 个准确率子用例 + 3 个配置门子用例 + 1 个映射护栏）
- `TestCompressionEval_StrategyComparison` baseline 按上述预期调整后通过

#### dev compose 业务验证（AGENTS.md §11，实跑真实 LLM）

开了 gateway 的临时容器（`AGENT_GATEWAY_ENABLED=true` + 小 budget 逼迫压缩触发），跑了完整庭审链路：

| 验证项 | in-situ 证据 |
|---|---|
| #1/#2/#3/#4/#5 | FileLogger: `compression_strategy=scored`、`atomic_groups=6/kept=1`、`recent_forced=1`，`compression_applied_total{strategy="scored",trigger="exhausted"}` 累计 8 次 |
| #6 | `agent_gateway: abstractive summary applied, dropped_msgs=2/4/4, summary_chars=490/752/546` |
| #7 | `verdict_evidence_accuracy = 1.0`（判决书正文引用 E001/E002，两条证据均真实存在） |

### 3.5 实施与验证中发现的额外问题（3 个）

前两个同源（字段搬运漏项），第三个是压缩器自身的截断策略缺陷。

**问题 A（本次引入，已修）**：`AGENT_GATEWAY_SMART_COMPRESSION_ABSTRACTIVE_SUMMARY` 在 `config.Load()` 里读到了，但 `cmd/server/main.go` 构造 `agent_gateway.GatewayConfig` 时**没有搬过去** → 压缩照跑、永远走 extractive，没有任何日志。该问题是 dev compose 实跑时发现的（单元测试全绿也照样漏，因为单测直接构造 GatewayConfig）。

**问题 B（存量 bug，本次一并修复）**：同一个手工映射里，**下面 9 个字段从未被搬运过**：

```
LLMTimeoutSec / CacheEnabled / CacheTTLSec / CacheMaxEntries
Breaker（整块）/ FileLoggerPrompts / FileLoggerPromptsMaxBytes
```

后果：**ADR 0013 的 Response Cache 与 Circuit Breaker 事实上从未在运行时生效**（`gateway.go` 里 `cfg.CacheEnabled` / `cfg.Breaker.Enabled` 恒为 false），ADR 0037 为它们加的 metric 恒为 0。v2.1 F5 "三个默认开关全开" 的叙述与实际不符。

之所以一直没被发现：本机 `.env` 恰好把 `CACHE_ENABLED` / `BREAKER_ENABLED` 都写成 `false`，与"永远是 false"撞了巧合；一旦有人改成 `true` 就会被静默忽略。

**防复发**：把该映射抽成 `buildGatewayConfig()`（`cmd/server/main.go`），并新增反射测试 `TestBuildGatewayConfig_MapsEveryField`（`cmd/server/gateway_config_mapping_test.go`）—— 源配置每个字段填非零值，断言目标配置对应字段非零；**已实测该测试在移除任一映射行时会失败**。新增 `GatewayConfig` 字段只需在 config 结构 + 映射函数各加一处，测试即自动覆盖。

---

**问题 C（存量 bug，严重，本次修复）**：压缩器把 **system prompt 截断成 1500 字节**，等于让 agent 失忆。

- **根因**：`react_runner.go` 的 `buildInitialMessages` 把 baseRules + 工具说明 + 庭审历史拼成**一条 system 消息**（实测 12~14 KB），整个 `messages` 数组往往就只有这一条。而 `CompressScored` / `CompressLegacy` 末尾的"单条超长截断"循环对 system 同样生效，用普通对话内容的 `compressMaxMsgLen(3000) → compressTargetLen(1500)` 处理它。
- **实测证据**（`backend/logs/agent_gateway_2026-09-21.log`，34 次压缩调用）：

  | 指标 | 值 |
  |---|---|
  | 受影响调用 | **16 / 34** |
  | 截断幅度 | `12304→1500` / `13883→1500` / `13331→1500`（**87~89% 被销毁**） |
  | 后果 | 只留下前 1482 字节（baseRules 开头），**工具说明与全部庭审历史丢失** |

- **连带观测**：同一批日志有 4 条 `streamSpeakContent hallucination validation failed ... evidence_ref_empty_with_citation pattern="证据7,证据12"`（ADR 0015/0021 反幻觉校验打回）。模型失去庭审历史后不知道庭上有哪些证据，转而编造证据编号 —— 与截断时间线吻合。（注：该告警在 gateway 关闭的对照组也出现过，故**不是**唯一成因，但截断明显加剧。）
- **代码自相矛盾**：同文件的注释写着「system 消息始终强制保留」，但它只保证不被**丢**，没保证不被**截断**。
- **附带缺陷**：截断用裸 `Content[:keep]` 按字节切，而 system prompt 几乎全中文 → 切口落在字符中间，**产出非法 UTF-8 片段**塞进 LLM 请求。`agent` 包的 `truncateForPrompt`（庭审历史逐行 240 字节）有同样问题。

**修复**：

| 改动 | 内容 |
|---|---|
| system 独立上限 | 新增 `compressMaxSystemMsgLen = 24000` / `compressSystemTargetLen = 20000`（实测最大值 13883 的 1.7 倍，正常庭审不受影响，仅对长期庭审的历史膨胀兜底）。**不做成无限**：system 含庭审历史，会随消息数增长 |
| rune 安全截断 | 新增 `cutBytesRuneSafe(s, n)` —— 字节预算语义不变，但 n 落在 rune 中间时回退到边界 |
| 消重 | 三处重复的截断循环（`CompressLegacy` / `CompressScored` 早返回 / `CompressScored` 主路径）合并为 `truncateOversizedMessages()`，保证三条路径对 system 处理一致 —— 原 bug 正是三处都无条件截断 |
| `truncateForPrompt` | 同样改为 rune 边界回退 |

**回归护栏**：`prompt_compressor_test.go` 新增 8 个测试（system 不截断 ×3 路径、病态超长兜底 + `utf8.ValidString`、普通消息行为不变、`cutBytesRuneSafe` 表驱动、limits 分支、helper 统计职责）+ `react_runner_test.go` 2 个（`truncateForPrompt` rune 安全）。**已实测**：把 `msgTruncateLimits` 的 system 分支去掉后，三个 `*_SystemNotTruncated` 测试立即失败并报出 `want 12000 bytes, got 1500 bytes` —— 与生产日志症状完全一致。

**eval baseline**：`TestCompressionEval_StrategyComparison` 的 transcript 中无任何消息超过 3000 字节，A/B 实测本修复对其数字**完全无影响**（smart `3138 chars / kept 9` 前后一致）。

### 3.6 dev compose 配置缺口（未改，仅记录）

`docker-compose.dev.yml` 的 backend 服务只显式传 `AGENT_GATEWAY_LLM_TIMEOUT_SEC`，其余 gateway 变量依赖根 `.env`；而根 `.env` **没有** `AGENT_GATEWAY_ENABLED`，其 Go 默认值为 `false`。因此**默认 `docker compose -f docker-compose.dev.yml up -d` 起来时 Agent Gateway 整体是关闭的**（`SMART_COMPRESSION=true` 等配置不起作用）。本次验证是通过临时容器注入 env 绕过的，未修改 `.env`（AGENTS.md §8 红线）。是否在 compose 里补默认值由用户决定。

### 3.7 问题 C 的 in-situ 复验状态

问题 C 是**只有实跑才看得见**的缺陷（单元测试全绿也漏），因此其修复也必须在真实链路复验。原计划的复验方式：开 gateway + 小 budget 跑一场，确认 FileLogger 里 `compression_before_count=1` 的 `react_think` 调用 `after_length` 不再等于 1500、而是接近 `before_length`。

**该复验未完成**：执行期间宿主 **D: 盘写满（652G/652G，仅剩 7.8M）**，导致 Docker Desktop 的 containerd 存储转为只读（`meta.db: read-only file system`），无法创建或启动容器。这是环境故障，与代码改动无关。磁盘腾出后按上述方式复验即可。

---

## 4. 不做的事（明确边界）

- ❌ 不重写整个压缩器（保持现有管道架构）
- ❌ 不引入外部依赖（如 Prometheus、ClickHouse）
- ❌ 不改压缩触发阈值（70%/40%/20% 保持不变）
- ❌ 不做实时质量度量（影响性能，改为 trial 结束后后台检查）

---

## 5. 验证

- `go test ./...` 全量通过
- `TestCompressionEval_StrategyComparison` baseline 变化已核对（见 §3.3）
- 新增测试用例覆盖 evidence_id 链场景
- `curl /api/v1/metrics | jq` 确认 `verdict_evidence_accuracy` 可见

---

## 6. 文档同步

- `docs/V1-ROADMAP.md` 加 v2.10 行
- `docs/todo/deferred-items-2026-09-21.md` §D6 标记 Done
- `docs/release-notes/v2.10.md` 发版说明

---

## 7. 关联文档

- [ADR 0006](./0006-smart-prompt-compression.md) — Smart 评分压缩设计
- [ADR 0007](./0007-token-budget-rejection.md) — Token Budget reject-when-exhausted
- [ADR 0037](./0037-agent-gateway-observability.md) — agent_gateway 全量 observability 埋点
- [compression_eval_test.go](../../backend/internal/agent_gateway/compression_eval_test.go) — Baseline 测试用例

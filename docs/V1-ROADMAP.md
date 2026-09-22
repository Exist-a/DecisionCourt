# DecisionCourt V1 路线图 — 个人项目长期维护

> **版本**：v1.0.0 (2026-08-20 落地) + v1.0.x 系列补丁 + v2.0 视觉升级 + v3.0 端侧 TTS
> **状态**：v1.0.0 已合并（commit `3f1be3b` / `1900813` / `f34051a`），v1.0.x 系列持续推进
> **关联**：[decisioncourt-roadmap.md](./decisioncourt-roadmap.md)（MVP 阶段路线图）+ [release-notes/v1.0.0.md](./release-notes/v1.0.0.md)（v1 发版说明）
> **2026-08-21 重要调整**：
> 1. **去掉商业化路径**（原 v1.1 M2 + M5）— 个人项目长期维护，不规划商业化前置
> 2. **v1.0.5 原方案升级为 v2.0** — 剪影小人 SVG（走路/点头/举手）原属"动画升级最后"，但用户重新评估认为工作量+调试周期都大，**升格为大版本 v2.0**
> 3. **新增 v3.0 端侧 TTS** — 离线 TTS（Piper/Coqui 等），让庭审从"文字"升级为"语音"
> 4. **v1.2 安全 P1 触发后再启动**（不变，但实际优先级低）
> **目标**：从"产品级稳定(v1.0.0)"持续打磨到"v2.0 视觉升级" + "v3.0 多模态语音"，按用户实际反馈动态调整里程碑

---

## 0. 当前进度快照（2026-09-21 v2.10 Token 压缩策略收尾）

| 维度 | 状态 | 说明 |
|---|---|---|
| v0.10.17-25 系列 | ✅ | silent-error 12 黑洞全修,4 层限流,300 字截断,新意度 / stance 检查 |
| v1.0.0 落地 | ✅ | ECS 30 天 8 问题全修 + DeepSeek v4 迁移 + 8 问题专项回归测试护栏 |
| v1.0.1 | ✅ | v1.0.0 遗留 4 类预存在失败 100% 修复 + prompt HA-001 |
| v1.0.2 | ✅ | 候选 4 已反驳证据集合跟踪 + ADR 0030 |
| **v1.0.3 PR-B1** | ✅ | Prompt Lab baseRules YAML 化 + 热加载 + 11 sub-test (`f1720f7`) |
| **v1.0.3 PR-B2** | ✅ | Eval + A/B + REST API + 14 sub-test (`4598547`) |
| **v1.0.3 PR-B3** | ✅ | release notes + V1-ROADMAP 同步（`c78182f`） |
| **v1.0.4 PR-C1** | ✅ | Trace 后端聚合 + REST + 20 sub-test (`f4a37b6`) |
| **v1.0.4 PR-C2** | ✅ | Trace 前端可视化 + 4 React 组件 + 4 sub-test (`1bcfb0b`) |
| **v1.0.4 PR-C3** | ✅ | Framer Motion 微动效 + 6 variants + 4 组件 + 6 sub-test (`1e83a45`) |
| **v1.0.4 PR-C4** | ✅ | release notes + ADR 0033 + V1-ROADMAP 同步（`726af6e`） |
| **v2.0 PR-D1+D2** | ✅ | 厕所标识剪影小人 SVG + AgentAvatar 接入 + 5 sub-test (`50e1746`) |
| **v2.0 PR-D3** | ✅ | release notes + ADR 0034 + V1-ROADMAP 同步（`c78182f` → `6e0588b`）|
| **v2.1 F1** | ✅ | JudgeBiasMeter 冲击波位置修复（视觉 bug, 跟随 targetPosition）|
| **v2.1 F2** | ✅ | TrialReplay phaseBelief 按 phase 过滤（UI 逻辑 bug, 三层 fallback）|
| **v2.1 F3** | ✅ | isJudging 死代码清理 + ADR 0035（5 组件 prop 删除, judgeVariant 保留）|
| **v2.1 F4** | ✅ | LLM_API_KEY 启动检测 + /health/llm + 前端 banner（AGENTS.md §8 红线遵守）|
| **v2.1 F5** | ✅ | 三个默认开关全开（smart_compression / cache / breaker, .env=false 回滚）|
| **v2.1 F6** | ✅ | release notes + V1-ROADMAP + SWITCH-SOAK.md + ADR 0035 README 索引 |
| **v2.1 F7** | ✅ | DotAvatar 视觉优化 + 庭审中央案卷化（scene-shell / 装订线 / 印章 / 天平）|
| **v2.1** | ✅ | v2.1 tag 推送（7 commit `a6d2d5e..d6a9fd3`）|
| **v2.2 P0-1** | ✅ | 气泡绝对定位（庭审横向滑动修复）|
| **v2.2 P0-2** | ✅ | 全局隐藏 scrollbar（庭审滑块丑修复）|
| **v2.2 P0-3** | ✅ | EvidenceBoard 改 grid wrap（跟全局隐藏 scrollbar 一致）|
| **v2.2 P0-4** | ✅ | 庭审记录文字渲染修复（MessageHistory + TrialReplay + RebuttalTraceNode）|
| **v2.2 P0-5** | ✅ | TrialHistoryList 防撑宽 + lucide Scale 图标一致化 |
| **v2.2 P0-6** | ✅ | dev compose Next.js rewrites（前端 404 修复）|
| **v2.2 P0-7** | ✅ | 整个页面 h-screen 100vh（庭审记录面板高度修复）|
| **v2.2** | ✅ | v2.2 推送（9 commit `88a3716..ec46438`）|
| **AGENTS.md §11** | ✅ | Docker 业务测试规范（DOCKER_TEST_POLICY）|
| **v2.3 observability-impl** | ✅ | agent_gateway 全量 metrics 注入骨架（Gateway / cache / breaker / compressor / throttler / budget / retryer 7 文件构造器加 metrics 参数；nil-safe）|
| **v2.3 observability-gateway** | ✅ | gateway.go 加埋点（call_total / tokens / duration / cache_put / stream / budget_rejected）|
| **v2.3 observability-cache** | ✅ | cache.go 加埋点（hit / miss / put{insert,update} / evict{lru,session} / size gauge）|
| **v2.3 observability-breaker** | ✅ | breaker.go 加埋点（state_change{from,to} / state gauge / fallback{reason}）|
| **v2.3 observability-compressor** | ✅ | prompt_compressor.go 加埋点（ratio / duration / summary_inserted + skipped_normal 路径）|
| **v2.3 observability-throttler-budget-retryer** | ✅ | throttler / token_budget / retryer 3 文件加埋点 |
| **v2.3 observability-tests** | ✅ | metrics_test.go 9 个新 sub-test（cache / breaker / compressor / throttler / retryer / gateway E2E / nil safe / budget）|
| **v2.3 docs** | ✅ | ADR 0037 + release-notes/v2.3 + SWITCH-SOAK.md §2 表格对齐 + 端口冲突文档清理 + AGENTS.md §11.7 curl 中文乱码排查 |
| **v2.4 P1-7** | ✅ | config.AppEnv + IsDev/IsProd/ValidateAppEnv + main.go enforceProdInvariants (4 条 invariant) |
| **v2.4 P1-5** | ✅ | UserFacingError.WithDetail 加 prod 守卫 (dev 保留 / prod 清空) |
| **v2.4 P1-3** | ✅ | websocket SetReadLimit(64KB) + content length check(4096 chars 与 HTTP 对齐) |
| **v2.4 tests+docs** | ✅ | 14 个新 sub-test (config / cmd / courtroom / api) + ADR 0038 + release-notes/v2.4 + deferred-items §D1 更新 |
| **v2.5 P1-6** | ✅ | frontend 22 依赖 pin 精确版本 + .npmrc save-exact + CI govulncheck/npm audit job |
| **v2.5 P1-2** | ✅ | CSRF double-submit cookie 中间件 (手写, 不引入 gorilla/csrf) + 前端 fetchJson 自动注入 header |
| **v2.5 P1-4** | ✅ | sanitize.go (21 中英文 injection pattern) + 9 个 prompt 函数适配 + orchestrator 6 处 + react_runner 2 处 |
| **v2.5 tests+docs** | ✅ | 22 个新 sub-test (csrf 10 + sanitize 12) + ADR 0039 + release-notes/v2.5 + deferred §D1 全部 P1 done |
| **v2.6 D2+D3** | ✅ | silent error 黑洞 D2 收尾（streamSpeakContent 三处 WARN + saveAgentMessage 拦截 + 6 处 caller skip broadcast）+ direct_verdict fallback round D3（transitionPhase 保留 round + maxRound helper）+ 9 个新 sub-test + ADR 0040 + release-notes/v2.6 + deferred-items-2026-08-21 §D2+§D3 ✅ + 删孤儿分支 `fix/cross-exam-content-empty` |
| **v2.7 PR-1** | ✅ | streamSpeakContent 流式解析**根因**重写（scanJSONContentField helper: brace depth + 跨 chunk 边界 reassemble + markdown wrap / 嵌套 / preamble 兼容）+ unquoteJSONString 加 \uXXXX 中文解码 + ActionSpeak caller 三态分流（complete && value!="" 视为成功；complete && value=="" 视为 LLM 显式空走 retry；!complete 视为失败/timeout 走 retry）+ 删除 indexOfJSONField 死代码 + 22 个新 sub-test（12 parser unit + 3 unquote unit + 4 e2e + 3 回归）|
| **v2.7 PR-2** | ✅ | finishTrial 加 detached verdictCtx（`context.WithTimeout(Background, 120s)`，让 JudgeFinalDecision + GenerateVerdict 不受 cancelCall / HTTP ctx 牵连）+ orchestrator.go 抽 `completeWithCancelRetry` helper（function-scope IIFE 让 defer rc() 在 retry 完成时立刻 cleanup，避免 caller-scope leak）+ JudgeFinalDecision + GenerateVerdict 走该 helper 实现 one-shot retry on `context.Canceled` 仅（network / DeadlineExceeded / parse error 都不 retry）+ runCrossExamRound 补 `defer s.clearCancel(session.SessionUUID)` 对称 finishTrial 的 activeCalls 清理 + 7 个新 sub-test |
| **v2.7 docs** | ✅ | [ADR 0041](../adr/0041-stream-rewrite-and-verdict-retry.md)（NEW，覆盖 PR-1 + PR-2 完整设计 + 5 处关键决策 + 调试历程）+ ADR 0040 §6 第一条 + 第二条 ❌ → ✅ "v2.7 落地" + release-notes/v2.7（14 章节模板）+ [deferred-items-2026-08-21.md](./todo/deferred-items-2026-08-21.md) §D2 + §D3 翻 ✅ Done (v2.7) |
| **v2.8 PR-3** | ✅ | LogEntry 加 4 optional 字段（system_prompt / input_messages / output_content / output_truncated，omitempty 后向兼容）+ AgentGatewayConfig 加 FileLoggerPrompts (off/metadata/full) + FileLoggerPromptsMaxBytes (默认 32 KiB) tri-state 开关 + Normalize 默认 metadata 保 v2.7 baseline + writeFileLog 签名 + 三态分流 + 新增 truncateForLogBytes helper + ValidateFileLoggerPrompts fail-fast（与 ValidateAppEnv 同级，cmd/server/main.go 启动校验）+ Config Load 加 2 个 envOrDefault + entryToRun 字段映射 (SystemPrompt+InputMessages → Run.Input map;OutputContent → Run.Output) + frontend AgentTraceNode `<pre>` 加 overflow-auto max-h-72 防 32 KiB 长 output 撑布局 + 18 个新 sub-test (file_logger 6 + parser 4 + Validate 8) |
| **v2.8 docs** | ✅ | [ADR 0042](../adr/0042-llm-trace-prompt-persistence.md)（NEW，覆盖 PR-3 完整设计 + 5 处关键决策 + PII / 磁盘风险分析 + 借鉴 APP_ENV tri-state precedent）+ ADR 0033 §3.2 第一条 ⚠️ → ✅ "v2.8 落地" + release-notes/v2.8（14 章节模板）+ [deferred-items-2026-09-21.md](./todo/deferred-items-2026-09-21.md) (NEW) §D4 logs retention 登记 |
| **v2.9 PR-4** | ✅ | Verdict schema 加 EvidenceAdoption jsonb + EvidenceAdoptionJSONB type (driver.Valuer + sql.Scanner) + EvidenceAdoptionEntry + belief_diff 加 BeliefSrcRebuttal 常量 + BuildAdoptionSummary 纯函数 (latest-wins semantics + adoptionTableMaxRows=200 + renderAdoptionPromptSection markdown) + JudgeFinalPrompt + ClerkPromptWithJudgeDecision 加 adoptionSummary 参数 + output schema evidence_adoption 字段 + JudgeFinalDecision + GenerateVerdict 透传 + Service.finishTrial 集成 (LLM 输出优先 + BuildAdoptionSummary 兜底) + belief_diffs Source=rebuttal rows 落库 (非 fatal) + convertToEvidenceAdoptionEntries helper + frontend Verdict.evidence_adoption?: EvidenceAdoptionEntry[] + 新 "证据采纳" 卡 (4 列 + 状态 chip 颜色 rose/stone/emerald) + 9 个 unit test (Empty / NoLinks / Standing / Overturned / Withdrawn / LatestWins / MixedStates / PromptRender / TableMaxRows) |
| **v2.9 docs** | ✅ | [ADR 0043](../adr/0043-rebuttal-aware-verdict.md)（NEW，**§2.1 范畴** — 显式标注，覆盖 PR-4 完整设计 + 8 处关键决策 + 用户 ExitPlanMode 后 §2.1 决策对齐）+ ADR 0030 §4 line 110 deferred 项 → ✅ "v2.9 落地" + release-notes/v2.9（14 章节模板，含用户决策表 + ⚠️ Breaking Changes 细列 GORM AutoMigrate 影响）+ [deferred-items-2026-09-21.md §D5](./todo/deferred-items-2026-09-21.md) 范围明确 (auto-overturn wiring 维持 deferred) |
| **v2.10** | ✅ | [ADR 0044](../adr/0044-token-compression-strategy-review.md)（Token 压缩策略审查 — 7 个问题全部落地）：#1 配置 footgun、#2 ScoreThreshold 激活、#3 evidence_id 链（Metadata 注入 + 原子组）、#4 recency decay、#5 内容感知 token 预算、#6 abstractive summary（opt-in）、#7 verdict_evidence_accuracy 质量回环。新增 43 个测试；dev compose 真实 LLM 实跑验证全 7 项。**另修复 3 个额外问题**：①abstractive 开关漏搬（本次引入）②cache/breaker/LLMTimeout/FileLoggerPrompts 共 9 字段从未搬运（存量，ADR 0013 缓存与熔断此前从未生效）→ 抽 `buildGatewayConfig()` + 反射护栏；③**压缩器把 system prompt 截断到 1500 字节**（存量，实测 16/34 调用、87~89% 指令销毁，是证据引用幻觉的直接推手）→ system 独立上限 + rune 安全截断。问题③的 in-situ 复验 2026-09-22 已就绪（D 盘腾出），按 §6 #2 跑一场确认即可 |
| ADR 累计 | 44 | 含 0031 + 0032 + 0033 + 0034 (Superseded) + 0034-supersede (Archived) + 0034-archive (✅) + 0037 agent-gateway-observability + 0038 security-p1-batch-a + 0039 security-p1-batch-c + 0040 silent-error-d2-d3-closeout + 0041 stream-rewrite-and-verdict-retry (v2.7) + 0042 llm-trace-prompt-persistence (v2.8) + 0043 rebuttal-aware-verdict (v2.9) + **0044 token-compression-strategy-review (v2.10)** |
| Go 测试 | ~485 sub-test | v1.0.3 304 + PR-C1 20 + v1.0-patch 2 + v2.1 F4+F5 6 + v2.3 observability-tests 9 + v2.4 P1-tests 14 + v2.5 P1-tests 22 + v2.6 D2+D3 9 + v2.7 PR-1 22 (stream parser) + PR-2 7 (retry hook) + v2.8 PR-3 18 + v2.9 PR-4 9 (BuildAdoptionSummary) + **v2.10 43 (ADR 0044 全 7 项 + 映射护栏 + system 截断回归)** |
| Frontend 测试 | 103 (9 .test.ts) | v1.0.4 79 + v2.0 5 + v1.0-patch 6 + **v2.1 F1+F2+F3+F4 11** + **v2.2 2** |
| 部署目标 | ⏸ 本地 dev | ECS 2026-08-05 终止,转入个人长期本地开发模式 |

**v1.0-patch 系列**（2026-08-22~23，7 commit `d72f860..f3a93e0`）：浏览器 back bug / 历史庭审回看 / 跨 session 证据污染 / hydrate 补 messages / BeliefDiffCard 渲染崩溃 / WS 断连 toast / 质证 empty content 软降级。详见 [todo/bugfix-log-2026-08-23.md](./todo/bugfix-log-2026-08-23.md)。

**v1.0-patch Round 2**（2026-08-23，5 commit `647a5dc..4c8e9f6`，合计 13 commit 含 Round 1）：
- `647a5dc` U1/U2/U3 策略笔记 hydrate 字段映射错误（Round 1 已修）
- `9353100` PR-C1 trace store 注入（v1.0.4 历史漏注）+ saveAgentMessage empty content guard + UTF-8 repro test
- `b705597` TrialReplay render-body setState 反模式修复 + TraceRun 类型补 input/output/tags
- `9e80521` CourtroomScene 返回首页按钮 + verdict 回庭审跳转显式化（用户反馈"庭审回访按钮全是 bug"）
- `ef79bda` TrialHistoryList 配色修正 + 新建庭审按钮 + 切 trial reset
- `4c8e9f6` BeliefDiffTimeline/RebuttalTraceNode mock 数据 + traceStore nil fail-fast
- (本 commit): 文档同步
详情见 [todo/bugfix-log-2026-08-23.md §F8-F12](./todo/bugfix-log-2026-08-23.md)。

**当前阻塞**：✅ Round 1/2 共 12 个 bug 全修（8 个 v1.0-patch + 4 个 Round 2 高优）。剩余 deferred：D1 安全审计 P1-P3（用户授权启动）+ M5 v3.0 端侧 TTS / v2.0 REDESIGN（用户 2026-08-23 确认不启动）。D2 + D3 已于 v2.6 收尾（ADR 0040）。

---

## 1. 总体策略

### 1.1 阶段切分原则

- **v1.0.x 是"补完 + 巩固"**:把 v0.10.x 留下的功能候选项和 ECS 沉淀修掉,不动架构
- **v1.1+ 是"前置 + 商业化"**:用户系统 / 多租户 / 订阅,任何公网部署前必须有
- **v2.0 是"产品 2.0 飞跃"**:多模态(语音庭审) / 实时协作 / 行业垂直(法律/医疗)

### 1.2 优先级原则

- **用户触发 > 自动推进**:每个里程碑必须有用户授权才能开(避免过度工程)
- **回归测试 > 新功能**:v1.0.0 起的 PR-3 回归测试框架是后续所有 PR 的强制 baseline
- **文档先行**:每个里程碑启动前先 ADR 入库 + release notes 草稿

### 1.3 不做什么

- ❌ **不引入新架构**(v1 全程沿用 v0.10 + v1.0.0 已实装的架构)
- ❌ **不重写核心组件**(ReAct runner / Agent Gateway / 信念引擎 v0.6 不动)
- ❌ **不做大规模重构**(保持向后兼容,.env / 数据库 schema 兼容)
- ❌ **不接 OTLP / Jaeger**(等 Phase D 触发条件:多实例部署)
- ❌ **不接 ClickHouse**(等 Phase E 触发条件:商业化启动)

---

## 2. 里程碑(M0 → M4)

### M0:v1.0.0 落地 ✅ (2026-08-20)

**目标**: ECS 30 天沉淀收尾 + DeepSeek v4 迁移 + 8 问题专项回归测试护栏

**已完成**:
- ✅ PR-1 P3-1 IP 脱敏(`3f1be3b`)
- ✅ PR-2 DeepSeek v3→v4 硬迁移(`1900813`)+ ADR 0029
- ✅ PR-3 ECS 8 问题专项回归测试(`f34051a`)
- ✅ PR-4 v1.0.0 release notes(本文件 [release-notes/v1.0.0.md](./release-notes/v1.0.0.md))
- ✅ PR-5 v1.0.0 tag + push(commit `6b35e92`)

### M0.5:v1.0.1 收尾 ✅ (2026-08-20)

**目标**: 修掉 v1.0.0 release notes ⏸ 已知遗留的 4 类预存在测试失败(用户授权:"在 1.0 开始之前得把错误都修了")

**已完成**:
- ✅ baseRules 第 16 条 (HA-001 调查发现 vs 用户证据混淆)
- ✅ buildInvestigationContext + ProsecutorPrompt / DefenderPrompt 接入
- ✅ FileLogger.Close() 改进(置 nil + lazy reopen)避免 Windows file lock
- ✅ 8 个 ReActRunner 测试 stale 断言同步到 v0.10.23/24 实际 call 数
- ✅ TranscriptContextInjected onCall 改为 per-call 切片数组
- ✅ v1.0.1 release notes ([release-notes/v1.0.1.md](./release-notes/v1.0.1.md))
- ✅ `go test ./...` 100% PASS

**Commit**: `ae7a464`(5 files, +91/-20)

**下次行动**: 打 v1.0.1 tag + push(待用户授权)

---

### M1:v1.0.2 候选 4 已反驳证据集合跟踪 ✅ (2026-08-20)

**目标**: 实现 PRD §4.3.3 "v0.7+ 计划" 第一项 "禁止引用被反驳且未翻盘的证据"。
roadmap §0 顶部钦定 "下一步进入候选 4 讨论"。

**已完成** (6 PR, 5 commits):
- ✅ PR-1 数据模型 + ADR 0030 (`ac8eda0`)
- ✅ PR-2 输入协议 (AgentOutput.Rebut + RebuttalHook + RebuttalSink) (`a2eddb6`)
- ✅ PR-3 后端硬拒 (applySpeakerRebuttalCheck, 与 stance/novelty 同级 guard) (`45dce74`)
- ✅ PR-4 Service 集成 + GORM RebuttalRepository + REST /rebuttal-links (`0d43e76`)
- ✅ PR-5 baseRules rebut schema + Frontend EvidenceBoard ⚔ 已反驳 X 次 chip (`88e5178`)
- ✅ PR-6 ecs_regression_v102_test + v1.0.2 release notes + tag
- ✅ `go test ./...` 100% PASS (含新增 13 sub-test)
- ✅ `pnpm test` 40/40 PASS
- ✅ `pnpm tsc --noEmit` 通过

**Commit 链**: `ac8eda0` → `a2eddb6` → `45dce74` → `0d43e76` → `88e5178` → (PR-6)

**触及 §2.1 裁决逻辑**: **中** (PRD §4.3.3 hard reject, 与 stance/novelty 同等级; 法官判决不感知 → 留 v1.0.x 后续讨论)

**回滚**: `git reset --hard v1.0.1` 回 v1.0.1 + 单独 revert 任一 PR

详见 [release-notes/v1.0.2.md](./release-notes/v1.0.2.md) + [ADR 0030](./adr/0030-evidence-rebuttal-state-machine.md)

---

### M2:v1.1 商业化前置 (⏸ 用户系统需求触发)

**目标**: 公网部署前的最小商业化能力

**子项**:

| 子项 | 工作量 | 触发 |
|---|---|---|
| 用户系统(注册 / 登录 / 个人中心) | 1 week | 用户主动要求 |
| 多租户隔离(企业版预留) | 2 weeks | 第一个企业客户 |
| 订阅 + 计费(Stripe / 国内支付) | 1 week | 商业化启动 |
| 限额分层(免费 / Pro / 企业) | 3 days | 商业化启动 |
| 用户级私有记忆(替代 session 级) | 1 week | 用户跨设备同步需求 |

**关键约束**:
- **必须跑过本地 dev compose 完整链路**(v1.0.0 已具备能力)
- **必须接 CI**(GitHub Actions CI 暂停机制需要重启,v1.0.0 不阻塞)
- **必须支持 oss 用户系统**(邮箱 + 密码 + 第三方 OAuth)

**触及 §2.1 裁决逻辑**: **0**(纯基础设施)

---

### M3:v1.2 安全 P1 阶段(⏸ 企业客户触发 deferred D1)

**目标**: D1 安全审计 14 项 P1 全部落地,为公网部署铺路

**来源**: [`docs/todo/deferred-items-2026-08-05.md` §D1](./todo/deferred-items-2026-08-05.md)

**子项**:

| 编号 | 项 | 复杂度 |
|---|---|---|
| P1-1 | WS Origin CheckOrigin 白名单(env 已支持,需补 Caddy 配置示例) | 1 day |
| P1-2 | CSRF Token 中间件(HttpOnly cookie + double-submit) | 2 days |
| P1-3 | 输入长度校验(query / evidence / verdict 长度上限) | 1 day |
| P1-4 | LLM prompt 注入防护(context 隔离 + sanitize) | 2 days |
| P1-5 | 日志脱敏(Detail 字段 prod 不填充 Go 内部信息)— v1.0.0 P3-1 部分完成 | 0.5 day |
| P1-6 | 依赖固定版本 + `npm audit` / `govulncheck` CI | 1 day |
| P1-7 | dev mode → prod 模式硬开关(GIN_MODE + frontend NODE_ENV) | 0.5 day |

**总工作量**:~8 days / 1 人

**触及 §2.1 裁决逻辑**: **0**(纯防御性配置)

**触发**: 企业客户 / 公网部署 / 安全事件

---

### M4:v2.0 产品 2.0 飞跃 (⏸ 商业化稳定 6 个月后)

**目标**: 从"辩论系统"升级到"决策操作系统"

**子项**:

| 子项 | 描述 | 触发 |
|---|---|---|
| **多模态庭审** | 语音输入 + TTS 输出 + 庭审录音回放 | 用户明确要求 |
| **实时协作** | 多人同时旁听 + 评论 + 投票 | 企业版需求 |
| **行业垂直** | 法律(合同审查) / 医疗(诊断决策) / 投资(M&A) | 行业客户 |
| **决策执行** | 判决书输出后,自动创建任务清单 + 跟进 + 复盘 | 用户主动要求 |
| **AI 法官介入** | 当双方僵持,AI 法官主动提问打破 | 复杂决策场景 |
| **OSS 化运营** | 公开判决书库 + 社区投票 + 案例检索 | 商业化稳定后 |

**触及 §2.1 裁决逻辑**: **强**(AI 法官介入 / 判决书执行)

**前置**: M1 + M2 + M3 全部落地 + 用户量 > 100 + 至少 1 个企业客户

---

## 3. 技术依赖关系（2026-08-21 重规划：v2.0 = 剪影小人，v3.0 = 端侧 TTS）

```
M0 v1.0.0 ✅ ─┐
M0.5 v1.0.1 ✅│
M1 v1.0.2 ✅ ─┴─ M2 v1.0.3 ✅ ( PR-B1 ✅ / PR-B2 ✅ / PR-B3 ✅ ) ── M3 v1.0.4 ✅ ( PR-C1 / C2 / C3 / C4 ) ── M4 v2.0 ✅ ( PR-D1+D2 / D3 ) ── M5 v3.0 ⏸ (端侧 TTS)
                                                                                                                                                                │
M6 v1.2 ⏸ (安全 P1, 触发后启动)
```

- **v2.0** ✅ 已完成（PR-D1+D2 + PR-D3，commit `50e1746` / 本 commit）
- **v2.0** → v3.0 顺序依赖（剪影小人给 v3.0 提供"角色"概念）
- **v1.0.x / v2.0 / v3.0** 与 v1.2 / v2.0 远期 无强依赖（三线独立：产品打磨 / 安全 / 远期）

---

## 4. 关键里程碑（2026-08-21 重规划）

| 里程碑 | 计划时间 | 标志 | 实际状态 |
|---|---|---|---|
| M0 v1.0.0 | 2026-08-20 | ECS 8 问题全修 + DeepSeek v4 + 263 sub-test | ✅ |
| M0.5 v1.0.1 | 2026-08-20 | v1.0.0 遗留测试 100% 修复 + prompt HA-001 | ✅ (`ae7a464`) |
| M1 v1.0.2 | 2026-08-20 | 候选 4 已反驳证据集合跟踪 + ADR 0030 | ✅ (`ac8eda0`+...) |
| **M2 v1.0.3 PR-B1** | 2026-08-21 | Prompt Lab baseRules YAML 化 + 热加载 + 11 sub-test | ✅ (`f1720f7`) |
| **M2 v1.0.3 PR-B2** | 2026-08-21 | Eval + A/B + REST API + 14 sub-test | ✅ (`4598547`) |
| **M2 v1.0.3 PR-B3** | 2026-08-21 | release notes + roadmap 同步 | ✅ (`c78182f`) |
| **M3 v1.0.4 PR-C1** | 2026-08-22 | Trace 后端聚合 + REST + 20 sub-test | ✅ (`f4a37b6`) |
| **M3 v1.0.4 PR-C2** | 2026-08-22 | Trace 前端可视化 + 4 组件 + 4 sub-test | ✅ (`1bcfb0b`) |
| **M3 v1.0.4 PR-C3** | 2026-08-22 | Framer Motion 微动效 + 6 variants + 6 sub-test | ✅ (`1e83a45`) |
| **M3 v1.0.4 PR-C4** | 2026-08-22 | release notes + ADR 0033 + V1-ROADMAP 同步 | ✅ (`726af6e`) |
| **M4 v2.0 PR-D1+D2** | 2026-08-22 | 剪影小人 SVG + AgentAvatar 接入 + 5 sub-test | ✅ (`50e1746`) |
| **M4 v2.0 PR-D3** | 2026-08-22 | release notes + ADR 0034 + V1-ROADMAP 同步 | ✅ (本 commit) |
| **M5 v3.0 端侧 TTS** | v2.0 后 | 离线 TTS（Piper/Coqui 等），庭审语音化 | ⏸ ([V3.0-PLAN.md](./V3.0-PLAN.md)) |
| M6 v1.2 | 公网部署 / 安全事件 / 个人决定 | D1 安全 P1 全部落地 | ⏸ |

**已移除**：
- ~~M5 v1.1 商业化前置~~（2026-08-21 决定：个人项目长期维护，不规划商业化）
- ~~V1.0.5 / V1.0.6 占位~~（重新规划：剪影小人升级为 v2.0，端侧 TTS 新增为 v3.0）

---

## 5. 风险与应对（2026-08-21 重规划）

| 风险 | 影响 | 应对 |
|---|---|---|
| 候选 4 触动裁决逻辑（强 §2.1） | 影响最终判决计算 | 先 ADR 详细讨论 + 用户明确授权 |
| v1.0.3 PR-B2/B3 拖延 | v1.0.3 永远"半发布" | 每 PR 用户可暂停,但完成后才 tag |
| v1.0.4 Trace 后端性能问题（1 trial ~50 runs） | API timeout | LRU 缓存 + 日期分区 + 滚动归档 |
| D1 安全审计 14 项遗漏（个人项目，未来公网部署时） | 公网部署被攻击 | v1.2 触发后再启动 |
| v2.0 多模态投入过大 | 项目方向漂移 | 远期参考，不绑定触发条件 |
| ArgumentMap 类 UI 组件"用户根本不会看" | 浪费开发资源 | 用户反馈驱动（ADR 0032 教训） |
| Dev compose 启动配置脆弱（DATABASE_URL 拼接 / NEXT_PUBLIC_* 覆盖 / Windows npipe） | Windows / Linux 切换困难 | 文档化 + 简化为hardcode；回归测试（agent_dev compose E2E）|
| Silent error 黑洞（react_runner 流式解析 silent fail） | 庭审记录全空但不报错 | D2 + D3 fix（v2.6, ADR 0040）加 WARN 日志 + saveAgentMessage 拦截 + 6 处 caller skip broadcast + maxRound helper |

---

## 6. 下一步（2026-09-21 v2.10 落地后）

### 立即可做（等用户授权）

1. ✅ **宿主 D: 盘写满已解决（2026-09-22）** — 历史快照：2026-09-21 v2.10 验证末段，D 盘 652G/652G（仅剩 7.8M）导致 Docker Desktop containerd 存储转只读（`meta.db: read-only file system`），dev 栈半损坏。**下一步**：`docker compose -f docker-compose.dev.yml up -d --force-recreate` 重启 dev 栈（如容器曾异常停止）；前端可达性不变可省
2. **问题 C 的 in-situ 复验**：dev 栈恢复后开 gateway + 小 budget 跑一场，确认 FileLogger 里 `compression_before_count=1` 的 `react_think` 调用 `after_length` 不再为 1500、而是接近 `before_length`（详见 [ADR 0044 §3.7](../adr/0044-token-compression-strategy-review.md)）
3. **确认两个字段搬运修复是否保留**（v2.10 §⚠️）：
   - abstractive 开关漏搬 → 必须保留（否则 #6 失效）
   - cache/breaker/LLMTimeout/FileLoggerPrompts 共 9 字段 → 修复后开始按 `.env` 生效。当前 `.env` 里 `CACHE_ENABLED` / `BREAKER_ENABLED` 都是 `false`，**本机行为不变**；如希望永久关闭，保持 `.env` 为 `false` 即可
4. **是否在 `docker-compose.dev.yml` 补 `AGENT_GATEWAY_ENABLED=true`** — 否则 dev 栈默认不跑 gateway（`SMART_COMPRESSION=true` 等不起作用，压缩/预算/缓存/熔断全不跑）。本次验证是用临时容器注入 env 绕过的，未改 `.env`
5. **提交工作区** — v2.10 代码 + ADR 0044 + release-notes + 文档同步（当前工作区未提交）
6. **tags** — 用户审 diff 后授权 `git tag v2.7 v2.8 v2.9 v2.10 && git push origin main --tags`（按 v2.4/v2.5/v2.6 模式）

### 显式不启动（用户已明示 2026-09-21）

- ❌ **v3.0 端侧 TTS**（Piper / Kokoro / Coqui）— 用户"目前不打算进行 3.0"，`docs/V3.0-PLAN.md` 维持 ⏸
- ❌ **v2.0 REDESIGN r3f**（3D 剪影方案）— 已三次归档（ADR 0034-archive），永不实施

### 长期（按需触发）

7. **v1.2 安全 P1 (剩 P2-1 ~ P2-5 + P3-1 ~ P3-2)** — 触发条件：公网部署 / 安全事件 / 企业客户 / 用户决定继续安全加固 (deferred-items-2026-08-05.md §D1)
8. **logs/ retention helper (D4)** — full mode 磁盘膨胀风险。v2.8 PR-3 call out，详见 deferred-items-2026-09-21.md §D4 — 后续 PR 提供 logrotate / cleanup helper。**注意**：2026-09-21 D 盘写满事件（2026-09-22 已腾出）说明磁盘治理不是纯理论风险，D4 优先级应上调
9. **verdict_evidence_accuracy 观察**（v2.10 ADR 0044 #7）— 积累几场真实庭审后回看该 gauge；持续 <1.0 说明压缩仍丢证据链或 LLM 幻觉，需回看压缩策略

### 持续维护

- **静默错误黑洞回归测试护栏**（v1.0.0 PR-3 框架 + D2/D3 在 v2.6 ADR 0040 + v2.7 PR-1 根因消除在 ADR 0041）
- **LLM Trace prompt 持久化三态开关**（v2.8 PR-3 + ADR 0042，三态 enum + 18 sub-test）
- **Token 压缩策略护栏**（v2.10 ADR 0044，43 sub-test 覆盖配置/评分/原子组/预算单位/摘要/质量指标/system 截断回归）
- **GatewayConfig 映射护栏**（v2.10，反射测试防"配置读到了但没搬进运行时"类静默失效）
- **DeepSeek API 文档变更跟进**（ADR 0029 教训）
- **Dev compose 回归测试**（host 端口冲突 + env 优先级 + Windows npipe）
- **AGENTS.md §8 敏感文件红线**（项目长期规范）
- **v2.7 三态分流 + detached verdictCtx** — silent error 黑洞根因 100% 收尾，测试护栏覆盖 22 + 7 sub-test

---

## 关联文档

- [decisioncourt-roadmap.md](./decisioncourt-roadmap.md) - MVP 阶段路线图(M0 之前)
- [release-notes/v1.0.0.md](./release-notes/v1.0.0.md) - v1.0.0 发版说明
- [todo/deferred-items-2026-08-05.md](./todo/deferred-items-2026-08-05.md) - 安全审计 P1-P3 deferred
- [roadmap/whitebox-roadmap.md](./roadmap/whitebox-roadmap.md) - 可观测性 Phase A-E
- [project-ideas.md](./project-ideas.md) - 未来灵感池
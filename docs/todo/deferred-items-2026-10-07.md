# Deferred Items（2026-10-07 新增）

| | |
|---|---|
| **生成日期** | 2026-10-07 |
| **状态** | ✅ **D7–D26 全部收口（v2.12 + v2.13，2026-10-08）**，且 **docker + 浏览器双路径实测通过**：D7–D12 + D14 已实现并实跑验证；D13 已按授权实现（ADR 0045）；D15–D19 已修复并实跑 + 浏览器验证；**v2.13 收口 D20–D24 并 docker 实跑**；复验发现的 **D25 已按"覆盖式"修复**（API + 浏览器双验证）；浏览器实测发现的 **D26 已按"GET 自愈"修复并 curl 验收通过**。详见「v2.13 收口」一节 |
| **触发** | 简历 5 条亮点逐条对照代码核对（配合 `.trae/documents/interview-answers-project-highlights.md`），发现「亮点描述成立、但支撑它的功能只做了一半」的缺口 |
| **关联 PR** | 无（本批为新增发现，D7 起编号） |
| **核对基线** | `main` @ `9db2e0a`（v2.10 之后） |

---

## 背景

为准备面试，对 5 条项目亮点做了逐条代码核对（每条都实测了调用点、配置接线、表字段和测试断言）。结论是：**亮点描述本身基本站得住**，但有 8 处「功能骨架在、闭环缺失」——表现为：方法实现了但没人调用、配置项提议了但没接线、字段定义了但不写入、路径写好了但没有消费者、能力只存在于文档化的 SQL 里。

这不是造假，是**半成品**：每一处都能说清「当时为什么停在这里」，但如果不补，面试深挖和长期维护时都会暴露。

## 缺口总表

| 编号 | 标题 | 影响 | 优先级 | 阶段 | 需用户授权 |
|---|---|---|---|---|---|
| D7 | 网关按会话状态无回收（预算表 + 响应缓存内存膨胀） | 长跑进程内存持续增长 | P1 | 阶段 3 | 否 |
| D8 | 重试器并发不安全 + 无错误分类 | 数据竞争；重试可能放大资损 | P2 | 阶段 3 | 否 |
| D9 | `llm_calls` 缺 `request_id`、`agent_id` 从不写入 | DB 层无法按链路/Agent 关联审计 | P1 | 阶段 2 | 否 |
| D10 | 限流阈值未接线配置（会话速率 / 全局并发） | 无法运维调参，只能改代码重发 | P3 | 阶段 1 | 否 |
| D11 | per-IP 层 429 缺 `Retry-After` | 被限流客户端无退避依据 | P3 | 阶段 1 | 否 |
| D12 | `decision_events` 无读取端点 | 跨域查询只有文档化 SQL，无分析能力 | P1 | 阶段 2 | 否 |
| D13 | 投影隔离两处硬化（公共消息剥离路径无消费者 + 单键黑名单） | 隔离保证有一重依赖结构性巧合 | P2 | 阶段 4 | **是**（§2.1） |
| D14 | 网关二进制默认关闭 vs 部署默认开启 | 非 compose 部署会静默不启用网关与压缩 | P3 | 阶段 1 | 否 |

## v2.11 实施状态（2026-10-08）

阶段 1–3 已实现，**逐项一个提交**（可独立回滚）。阶段 4（D13）按 AGENTS.md §2.1 停在"待授权"。

| 编号 | 状态 | commit | 遗留 |
|---|---|---|---|
| D11 | ✅ 实现 + 测试 | `a627014` | docker 验证待跑（429 响应头在真实链路） |
| D14 | ✅ 实现 + 测试 | `951504a` | docker 验证待跑（启动 WARN 需真实启动一次看到） |
| D10 | ✅ 实现 + 测试 | `5f798d7` | docker 验证待跑（config 默认值 / env 接线） |
| D12 | ✅ 实现 + 测试 | `9f03b93` | docker 验证待跑（新路由 + 真实 DB 分页）；漏斗聚合端点（可选第二步）未做 |
| D9 | ✅ 实现 + 测试 | `c322d1e` | docker 验证待跑（AutoMigrate 加列 + 真实写入两字段非空） |
| D7 | ✅ 实现 + 测试 | `6033ec2` | 现场只能观测到"预算按 session 累积"（0→17686）与终态钩子触发；**清理效果无法在默认栈观测**（cache/budget 默认关 = D16；判决后同 session 无 LLM 调用入口 = D18）。清理本身由单测 + sqlite 跑真实 `finishTrial` 覆盖 |
| D8 | ✅ 实现 + 测试 | `c1e74d4` | `-race` **本地无法执行**（Windows race runtime `exit status 0xc0000139`，连无关包 `internal/util` 也复现）→ 由 CI `go test -count=1 -race ./...` 把关。现场也观测不到重试（默认栈里 retryer 关闭 = D16） |
| D13 | 🔴 **待授权** | — | 需先讨论（AGENTS.md §2.1）。方案已出：`.trae/documents/d13-projection-isolation-hardening-proposal.md`（含核实证据 + 5 个裁决问题） |

**新增测试**：约 80 个新 top-level 测试函数（含表驱动 sub-case 更多）。要点：限流响应契约 / 配置映射反射护栏 / events 读端点 11 项 / `llm_calls` 字段写入反射护栏 / `Gateway.Release` 双端 8 项 / 重试分类 26 组 + 4xx–5xx 边界 39 例 + 32 goroutine 并发串扰 / 状态码透传 5 项。

**未做（按各节「不做什么」边界）**：D12 漏斗聚合端点（可选）、D13 全部（待授权）、D9 历史回填、D7 定时 GC / TTL 过期、D8 多级退避调参。

## docker 业务验证结果（2026-10-08，dev compose）

按 AGENTS.md §11.5 最小集实跑。**栈已 down，命名 volume 全部保留（未删数据）**。

| 项 | 结果 | 现场证据 |
|---|---|---|
| D11 | ✅ | 真实 config + 真实中间件，per-IP 洪泛 429 带 `Retry-After: 1` + `retry_after_seconds` + `code:1429` |
| D14 | ✅ | 用真二进制跑：`AGENT_GATEWAY_ENABLED` 未设 → 打出 JSON WARN + 染色 `[WARN]` banner；显式 `=false` → 0 条告警（符合"显式关不打扰"） |
| D10 | ✅ | 容器内真实 `config.Load()`：默认 2/5/5；`env RPS=0.01 BURST=1 MAX=2` 覆盖后 config 与**限流行为**同步改变（burst=1 → 第 2 个请求 1427） |
| D12 | ✅ | 真实 DB：`GET /events` 全量 3 条；`limit=2` → count=2/has_more=true；`offset=2` → count=1/has_more=false；`event_type_prefix=state_transition` → 3 条，`fe.`/`span.` → 0 条 |
| D9 | ✅ | AutoMigrate 已加 `agent_type`/`request_id` 两列 + 两个索引；一场真实庭审产生 13 行 `llm_calls`，**每行 `agent_type`（prosecutor/defender/judge/clerk）与 `request_id` 均非空**（此前 agent_id 恒 NULL） |
| D7 | 🟡 部分 | 预算按 session 累积现场可见（file log `budget_used` 0→5587→11373→17434→17686 / 20000）；判决落库（终态钩子）现场触发。**清理效果不可现场观测** —— 原因见 D16（budget/cache 默认关）与 D18（判决后无同 session LLM 入口）。清理正确性由单测 + sqlite 跑真实 `finishTrial`（断言 `Release(uuid)` 被调一次）覆盖 |
| D8 | 🟡 部分 | 分类/并发/退避由单测覆盖（含 32 goroutine 串扰 + 结构护栏）；现场观测不到重试（默认栈 retryer 关闭 = D16），且本轮未遇到可重试的瞬时错误 |

**环境适配（不是代码问题）**：本机 host:3000 被用户另一个项目（Emotion-Echo Nuxt dev server）占用，故 dev 前端临时映射到 3010（temp override 文件，未进仓库、未改 compose/.env）。host:8080 被 NI ApplicationWebServer 占用，dev 后端本就用 8180。

## v2.12 收口（2026-10-08，用户授权后）

用户在 2026-10-08 明确授权后，D13 与验证期发现的 D16/D17/D18/D19 已全部实现，**并再次实跑验证**。

| 项 | 状态 | commit | 现场证据（dev compose + 浏览器真实路径） |
|---|---|---|---|
| D19（P0）CSRF/CORS 让浏览器写路径全废 | ✅ 已修（两半） | `d3c9124` + `79683d2` | 修 cookie `Path=/` + CORS 补 `X-XSRF-TOKEN` 后：浏览器 `document.cookie` 可读 token，UI 立案成功建 session，`POST /courtrooms` 200。⚠️ 修复前已持有旧 cookie 的浏览器需清一次 cookie（见 ADR 0039 迁移注意） |
| D16（P1）网关子能力默认全关 | ✅ 已修 | `18bb55b` | 容器内真实 `config.Load()`（**无任何 env 覆盖**）：PromptCompression/TokenBudget/Throttling/Fallback/SmartCompression 全 true；Cache/Breaker 仍 false（`.env` 显式关，正确尊重） |
| D17（P1）预算默认太小导致判决被拒 | ✅ 已修 | `7c26a3b` | 默认上限 200000；一场 quick 庭审实测烧 89838（45%），**judge/clerk 不再被拒，判决为真实 LLM 生成** |
| D18（P1）reopen 不可达 + 失败静默 | ✅ 已修（两半） | `f3ea525` + `26790e9` | 判决后 DB `current_phase=verdict`、UI 按钮变「查 看 判 决 書」；判决页「补充证据重开」点击后无报错并回到 `evidence`（「· 举证阶段」）；`UserAction` 现在对阶段不允许的 action 回 400/1003 |
| D13（P2）投影隔离硬化 | ✅ 已实现 | `b10b477` | [ADR 0045](../adr/0045-projection-isolation-hardening.md)：转写出口显式化（零行为变更）+ 载荷正向白名单 + 注入式/等价性测试 12 项 |
| D15（P3→P1）prod compose 漏传子开关 | ✅ 根因已消 | `18bb55b` | D16 改成"显式 env 优先、否则继承总开关"后，**不再需要**在 compose 里逐个列出子开关（compose 已传 `AGENT_GATEWAY_ENABLED=true` 即全开）。剩余"是否在 compose 里显式列出以便阅读"属可选整洁项 |
| D20（P3）concurrency gauge 不更新 | ✅ **v2.13 已修** | — | `wrappedCancel` 在 `Release` 后调 `recordConcurrencyMetric(true)` 刷新 `global_concurrency_current`（此前只在 acquire 分支写）；并用 `sync.Once` 保证一次 trial 只释放一次 slot（`cancelCall` + `defer cancel` 可能重复 Release → 误放别的 trial 的 slot） |
| D21（P3）`/auth/anon` duplicate key 噪音 | ✅ **v2.13 已修** | — | `FirstOrCreate`（SELECT-then-INSERT）改 `clause.OnConflict{Columns: user_id, DoUpdates: last_seen/last_ip/last_ua}` 单条原子 upsert，消除并发 `SQLSTATE 23505`；`FirstSeen` 冲突时不覆盖。抽 `upsertAnonUser` 便于单测 |

**验证方式**：`go build` / `go vet` / `go test ./...` 22 包全绿；dev compose 实跑 + **浏览器真实路径**（填表立案 → 开庭 → 真实 LLM 开场陈述 → 直接判决 → 判决页 → 补充证据重开 → 回到举证阶段）。栈已 down，命名 volume 全部保留。

## v2.13 收口（2026-10-08，本轮）

v2.12 收口后剩余的 **D20 / D21 / D22 / D23 / D24 全部实现**。本轮**未 push**（用户要求）；
**docker 业务验证（AGENTS.md §11.2）按用户要求暂缓**，等用户通知后补跑。

| 项 | 状态 | 实现要点 |
|---|---|---|
| D20（P3）并发 gauge 不更新 | ✅ 已修 | `withCancel` 的 `wrappedCancel` 在 `Release` 后调 `recordConcurrencyMetric(true)`，以 `Stats()` 真值刷新 `global_concurrency_current`（此前只在 acquire 分支写 → 判决后 gauge 停在最后一次 acquire 值）。并加 `sync.Once`：`cancelCall` + `defer cancel` 可能重复触发同一个 cancel，重复 `Release` 会误放别的 trial 的 slot。 |
| D21（P3）`/auth/anon` duplicate key 噪音 | ✅ 已修 | `FirstOrCreate`（SELECT-then-INSERT）改为 `clause.OnConflict{Columns: user_id, DoUpdates: last_seen/last_ip/last_ua}` 单条原子 upsert，消除并发 `SQLSTATE 23505`（WARN + ERROR 噪音）。`FirstSeen` 冲突时不覆盖。抽 `upsertAnonUser` 便于单测（SQL 形状 + 幂等 + 并发）。 |
| D22（P2）「补充证据重开」只做一半 | ✅ 已修（三处 + D25 补第四处） | ① `reopenTrial` 把 `status` 复位 `active`（否则 `finishTrial` 幂等守卫 `Status==completed → nil` 让重开后「直接判决」静默 no-op）；② `ValidateAction` 允许 `evidence → continue_cross_exam`（状态机 transitions 本就允许该边，补齐 reopen 注释承诺）；③ 前端 `CourtroomScene` 加 `trial.reopened` 处理 + 在 `evidence` 阶段**派生渲染**「进入第 N 轮」（判决页跳转回来时事件已发过，不能只靠 WS 事件）；④ `finishTrial` 第二道 `hasVerdict` 守卫（"重开后再次判决"被静默挡住）→ 见 D25（已修） |
| D23（P3）`integration` tag 腐烂 | ✅ 已修 | `integration_helpers_test.go` 的 `CreateSession` 补必填 `ownerID`。`go vet -tags integration` + `go test -tags integration -run '^$'` 通过 → tag 恢复可编译。 |
| D24（P3）发言幻觉无度量 | ✅ 已实现 | 新增 `MetricSpeakHallucinationTotal`（label `mode`，有界枚举）；`RunnerConfig.OnHallucination` 观察者 + `Orchestrator.SetHallucinationObserver` + `Service.WithObservability` 接线；流式（`streamSpeakContent` 硬拒）与非流式（`validateSpeak`）两条路径都上报。**docker 实测**：一场 quick 庭审日志 3 条幻觉 WARN ↔ metric 3 次（`evidence_ref_empty_with_citation`=2、`..._with_stats`=1），分桶与 mode 完全对齐 |
| D25（P2）重开后再次判决被 `hasVerdict` 挡住 | ✅ **已修（覆盖式）+ docker/浏览器复验通过** | 根因 = `finishTrial` 第二道守卫 `hasVerdict()`（`count>0`）在首判后恒真，且只会在"重开后"拦到。修法：① 守卫抽成 `shouldSkipFinishTrial()`，改为"已有判决 **且** 此后 session 未被改动过 才跳过"（`session.updated_at > verdict.created_at` 即视为重开后的新一轮）；② 判决落库抽成 `upsertVerdict()`，`ON CONFLICT (session_id) DO UPDATE` 覆盖旧行（`verdicts.session_id` 是 uniqueIndex）。**复验**：API 与浏览器两条路径都通过（浏览器里新判决书变为"采纳 选项B·45/55"，旧的是"50/50 采纳 A"），`verdicts` 行数恒 **1** |
| D26（P2）CSRF token 过期后永不刷新 → 长驻浏览器写请求全 403 | ✅ **已修** | 根因 = `middleware/csrf.go` GET 分支只在 cookie **不存在**时签发新 token，而校验有 ±2h 滑动窗口 → 已存在但过期的 token 永不刷新。修法：抽 `csrfTokenTTL` 常量 + `csrfTokenValid()`（格式/签名/时效），GET 改成"缺失 **或校验不过** 就重签"。**验收**：过期 token + GET → 补发新 token；用补发的 token POST → **200**（对照：不刷新直接 POST 仍 **403 CSRF_TOKEN_EXPIRED**）；有效 token + GET → **不轮换**（无 Set-Cookie）。新增 4 条单测 |

**本轮验证**：`go build ./...` / `go vet ./...` / `go test ./...`（22 包全绿）；
`go vet -tags integration ./internal/courtroom/...` 通过；前端 `tsc --noEmit` 通过。

**docker 业务验证（2026-10-08，dev compose + 真实 LLM，已实跑）**：栈已 down，命名 volume 保留。

| 项 | 结果 | 现场证据 |
|---|---|---|
| D20 | ✅ | 庭审运行中 `/metrics` `global_concurrency_current=1`、`max=5`；判决落库后回落 **0**（修复前会停在 1） |
| D21 | ✅ | 8 个并发 `/auth/anon` 打同一 user_id → 全部 200/code 0；后端 `duplicate key` 计数 **0**、`user upsert failed` 计数 **0**；`users` 表该 user 仅 **1** 行 |
| D22（前两半） | ✅ | `reopen_trial` → `phase=evidence` 且 **`status=active`**（修复前 `completed`）；`continue_cross_exam` → **200/code 0**（修复前 400/1003）且 `phase=cross_exam`、`round` 0→**1** |
| D22（验收最后一步） | ✅（修 D25 后复验） | 再发 `direct_verdict` → phase 从 `cross_exam` 推进到 `closing → deliberation → **verdict**`（round=1），`verdicts` 行数恒 **1**，`created_at` 08:28:06→08:28:58、`summary` 变为新判决文案 → 见 **D25** |
| D25 | ✅ | 同上（同一场庭审 `74703e95…`：判决 → 重开 → 进第 2 轮 → 再判决覆盖落库）。**首轮复验时此处是静默 no-op（200s 不动）**，修 D25 后通过 |
| D24 | ✅ | 日志 3 条 `streamSpeakContent hallucination validation failed` ↔ `/metrics` `speak_hallucination_total` 3 次（按 mode 分桶一致：`evidence_ref_empty_with_citation`=2、`evidence_ref_empty_with_stats`=1） |

**浏览器 GUI 实测（2026-10-08，补做）**：此前 rAF 不触发导致 GUI 全废，根因是**浏览器面板没在前台渲染**（`about:blank` 空白页同样不触发 → 与项目无关）；用户把 ZCode 窗口切到前台后 **rAF 立即恢复（2ms）**。随后补跑真实浏览器路径，全链路通过：

| 步骤 | 结果 | 现场证据 |
|---|---|---|
| 立案（表单 → `POST /courtrooms`） | ✅ | 跳转 `/court/<uuid>`，session `8b9558d4…` 落库 |
| 开庭（`POST /start`） | ✅ | 阶段=开庭陈述，真实 LLM 生成开场陈述 |
| 直接判决 | ✅ | 判决页显示判决书（50/50、采纳 选项A） |
| 补充证据重开 | ✅ | 回到 `/court/<uuid>`，阶段=**举证阶段**、`status=active` |
| **举证阶段渲染「进 入 第 1 轮」**（D22 前端那一半） | ✅ | 底栏红色按钮真的出现（修复前不存在） |
| 点「进 入 第 1 轮」 | ✅ | 阶段=质证阶段·第 1 轮，真实 LLM 跑第 1 轮 |
| 再判决（D25 前端+后端） | ✅ | 判决页显示**新**判决书：采纳 **选项B·留在大厂**、45/55 分（旧的是 50/50 采纳 A），`verdicts` 行数恒 **1** |

**过程中发现新缺陷 D26**：第一次点「立案」被 403 挡住（CSRF token 过期未刷新），清 cookie 后才继续。**已修复**（GET 自愈重签）并 curl 验收通过。详见 D26。

### 验证中新发现（D22 / D23，未修）→ ✅ **v2.13 全部已修**

**D22（P2）「补充证据重开」只实现了一半**（v0.8.3 遗留，非本次引入）—— 重开能"回去"，但回不去"继续辩论"。 → ✅ **v2.13 已修（三处）**

**现象 1：重开后「直接判决」是静默 no-op**

| 事实 | 位置 |
|---|---|
| `reopenTrial` 只 `transitionPhase(→evidence)`，**不把 status 从 `completed` 复位** | `courtroom/service.go:803` |
| `finishTrial` 幂等守卫含 `fresh.Status == StatusCompleted → return nil` | `courtroom/service.go:1611` |

实测：重开后 DB 是 `phase=evidence` + `status=completed` → 再点「直接判决」直接 return nil，**无任何提示**（用户视角=点了没反应）。

**现象 2：举证阶段没有"继续辩论"入口，且后端注释与守卫不一致**

| 事实 | 位置 |
|---|---|
| `ValidateAction` 的 `continue_cross_exam` 只允许 `cross_exam` 阶段 | `courtroom/statemachine.go:92-95` |
| 但 `reopenTrial` 的注释写着"用户要先看到证据板…然后点 `continue_cross_exam` 进入下一轮" | `courtroom/service.go` 的 reopen 契约注释 |
| 前端「进入第 N 轮」按钮仅在 `waitingForNextRound` 为真时渲染；该状态只由 WS `round.waiting_for_user` / `opening.finished` 置位 | `frontend/components/courtroom/CourtroomScene.tsx`（`useState` 与 CTA 渲染处） |
| 前端**没有** `trial.reopened` 的处理器 | 全仓 grep 无命中 |

所以 `evidence` 阶段既没有按钮、守卫也不允许 → 用户无法恢复质证。

**修法（v2.13 已实现，三条都动了）**：

1. `reopenTrial` 里把 `status` 复位为 `active`（重开 = 庭审重新进行中），顺带消掉现象 1 的静默 no-op；
2. `ValidateAction` 允许从 `evidence` 发 `continue_cross_exam`（state machine 本来就允许 `evidence → cross_exam`），
   并把 `round` 从保留值继续递增 —— 这与 `reopenTrial` 的既有注释一致，属"补齐注释承诺"；
3. 前端加 `trial.reopened` 处理（置 `waitingForNextRound`）或在 `evidence` 阶段直接渲染「进入第 N 轮」。

**验收**：判决 → 重开 → 回举证 → 点「进入第 N 轮」→ 真的跑起来第 N+1 轮（`round` 连续、`beliefs/evidences/messages` 保留）→ 再判决能落库。

**D23（P3）`//go:build integration` 的测试文件编译不过** → ✅ **v2.13 已修**：`integration_helpers_test.go:157`
调用 `CreateSession` 少传一个参数（`have 5, want 6`）→ 该 tag 已腐烂、CI 不跑
（`go test ./...` 不带 `-tags integration`，所以从不编译这些文件）。
**修法（已实施）**：补齐 `CreateSession` 的 `ownerID` 参数。`go vet -tags integration` +
`go test -tags integration -run '^$'` 均通过 → tag 恢复可编译。
若要让它重新有效，还需在 CI 里加一个 `-tags integration` 的 job（且它需要真实 LLM/DB）。

**D24（P3，可选增强）发言级幻觉只有守卫、没有度量** → ✅ **v2.13 已实现**：ADR 0015 的幻觉守卫对
**发言**有效（实测一场 0 证据的庭审里，控方开场编造了"第3号证据/复合月增速41%"，
守卫打了 2 条 `streamSpeakContent hallucination validation failed, falling back to retry`
并重试 —— `agent/react_runner.go:447`），但**质量度量只覆盖判决书**
（`MetricVerdictEvidenceAccuracy`，`courtroom/service.go:1859`，v2.10 ADR 0044 #7）。
结果：发言阶段的幻觉率无法从 `/metrics` 观察，只能翻日志。
**触发条件**：想量化"压缩是否伤到发言质量"或做发言幻觉率告警时。

**D25（P2）「重开后再次判决」被 `finishTrial` 的 `hasVerdict` 幂等守卫挡住 —— D22 只闭环了一半**（2026-10-08 docker 复验发现）→ ✅ **已按"覆盖式"修复并复验通过**

**现象**：按 D22 的验收路径实跑（API 级，dev compose + 真实 LLM）：判决（`phase=verdict` / `status=completed`）→ `reopen_trial`（→ `evidence` / `active` ✅）→ `continue_cross_exam`（→ `cross_exam` / `round=1` ✅）→ 再发 `direct_verdict` → **HTTP 200 / code 0，但 200 秒内 phase 一直停在 `cross_exam`，`verdicts` 表仍是 1 行**（静默 no-op）。

**根因**：`finishTrial` 有两个幂等守卫（`courtroom/service.go`，`fresh` 重载后）：

```go
if fresh.CurrentPhase == model.PhaseDeliberation || fresh.CurrentPhase == model.PhaseVerdict || fresh.Status == model.StatusCompleted {
    return nil
}
if s.hasVerdict(fresh.ID) { return nil }   // ← D22 没动这一条
```

D22 只消掉了第一个（`reopenTrial` 把 status 复位 `active`）。第二个 `hasVerdict` 是 `count(verdicts where session_id) > 0`：首判已写入 1 行 → 重开后仍为真 → 直接 `return nil`。
关键观察：**它只会在"重开后"这种情况下拦到** —— 正常重复点击 `direct_verdict` 会被第一个守卫（`phase == verdict`）先拦下。所以它挡的正是 D22 想放行的路径。

**影响**：`reopen_trial` 的语义是"补充证据后重新判决"，但重新判决永远拿不到新判决书 → 该功能**仍不可用**（失败点从"重开不可达"后移到"再判决无效"），且仍是静默失败（HTTP 200 / code 0，用户视角=点了没反应）。

**关键约束**：`model.Verdict.SessionID` 带 **`uniqueIndex`**（`model/db.go:179`）→ 数据库层面**一场庭审只能有一行判决书**。所以"追加第二份判决书"必须先改 schema（去掉唯一索引），且 `GetVerdict`（`handler.go:746` 无排序 `First`）与 `ExportSession` 都要补 `ORDER BY created_at DESC`，否则多行时返回哪一份不确定。

**修法（已实施：方案 1「覆盖式」，用户 2026-10-08 拍板）**：

1. **覆盖式（推荐，最小且不改 schema）**：放松 `hasVerdict` 守卫（改成"最近一份判决之后 session 是否又被改动过"，即用 `session.updated_at > verdict.created_at` 区分"重复点击同一轮"与"重开后的新一轮"），并把 `finishTrial` 的判决落库改为 **upsert**（`ON CONFLICT (session_id) DO UPDATE`，`clause.OnConflict`）。→ 唯一索引不变、"一场一判决书"不变量不变、`/verdict` 没有 404 窗口、重开后拿到的是**新判决书覆盖旧行**。
2. **显式失败（最保守）**：保留守卫，但让 `direct_verdict` 在"已有判决且未重开"时回 400/1003（需要一个"重开标记"），至少把静默 no-op 变成可见错误；代价是"重开后再次判决"仍然不可用。
3. **追加式（改动最大，不推荐）**：去掉 `verdicts.session_id` 的唯一索引，允许同 session 多份判决书，并给读路径补排序。好处是保留判决历史，代价是 schema 变更 + 多处读路径调整。

**验收**（同 D22）：判决 → 重开 → 进第 N+1 轮 → 再判决 → `GET /verdict` 返回**新**判决书（覆盖式：`verdicts` 仍 1 行但内容/`created_at` 变化）。

**实施落点（2026-10-08）**：

- `finishTrial` 的两道守卫抽成 `Service.shouldSkipFinishTrial(fresh)`：终态 phase / `status=completed` → 跳过；否则"已有判决且 `fresh.updated_at <= verdict.created_at`" → 跳过，**`updated_at` 更新过则放行**。
- 判决落库抽成 `upsertVerdict(db, v)`：`clause.OnConflict{Columns: session_id, DoUpdates: 内容列 + user_feedback + created_at}`。`user_feedback` 复位成 `none`（新判决尚未被评分），`created_at` 更新为本次判决时间（页面/导出展示的就是它）。
- 删除不再使用的 `hasVerdict()`；新增 `latestVerdictCreatedAt()`。
- 测试：`verdict_reopen_d25_test.go` 5 个用例（无判决不跳过 / 有判决未重开跳过 / 重开后不跳过 / 终态跳过 / upsert 覆盖且恒 1 行）。另需给两个既有测试 fixture 的 `verdicts` 手工 DDL 补 `session_id ... UNIQUE`（否则 sqlite 报 `ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint`）—— 生产侧由 AutoMigrate 的 `uniqueIndex` 保证。

**D26（P2）CSRF token 过期后永不刷新 —— 长驻浏览器超过 2h 后所有写请求 403**（2026-10-08 浏览器实测发现）→ ✅ **已修并验收通过**

**现象**：浏览器里点「立 案 · 开 庭」→ 页面只弹一个笼统的 "1 error"（红色角标），历史列表不增加，用户看不到原因；后端 `POST /api/v1/courtrooms` 返回 **403**。

**证据链**（可复现）：
- `/metrics` 的 `http_request_duration_seconds`：`POST /api/v1/courtrooms` → `403 count=1`。
- 浏览器 `document.cookie` 里 `XSRF-TOKEN` 解码后时间戳 `1791436102`（≈ 3.7 小时前，正是上一次浏览器测试签发的），**超出中间件 ±2h 滑动窗口**。
- 清掉该 cookie + 刷新页面（后端补发新 token，时间戳=当前时刻）→ 同一按钮 POST **200**，session `8b9558d4…` 建成。
- 干净复现（curl，同一 `dc_session`）：**过期** token → `403 {"code":"CSRF_TOKEN_EXPIRED","message":"CSRF token expired"}`；**新鲜** token → `200`。

**根因**：`middleware/csrf.go` 的 GET 分支只在 cookie **不存在**时才签发新 token：

```go
if _, err := c.Cookie(cfg.CookieName); err != nil { /* 签发新 token */ }
```

而校验分支对已存在的 token 做 ±2h 时效检查（`abs(now-ts) > 2h → 403 CSRF_TOKEN_EXPIRED`）。两者叠加 = **"已存在但已过期"的 token 永远不会被刷新** → 写请求从此永久 403，直到用户手动清 cookie 或浏览器淘汰它。

**影响**：任何"浏览器 cookie 存活 > 2 小时"的场景（长驻标签页、每天复用同一 profile 的本地开发、真实用户长时间开着页面）都会在**所有写操作**上 403：立案 / 提交证据 / 各种 action / 判决后重开。且前端拿不到可读原因 —— 403 体用的是**字符串** `code`（`"code":"CSRF_TOKEN_EXPIRED"`），而前端按数字 `code` 判定成功/失败，只能落到笼统错误提示。

**修法（已实施：方案 1「GET 时自愈」）**：
1. **GET 时自愈（已实施）**：签发条件从"cookie 不存在"改成"cookie 不存在 **或校验不通过**"（过期 / 签名不符 / 格式错都重签）。一次 GET 即恢复，最小改动、无需前端配合。
2. 或**每次 GET 轮换** token（double-submit 语义下安全，代价是每个请求都换 cookie）——**未采用**（避免无谓的 cookie 抖动，`TestCSRF_GetKeepsValidToken` 钉住这一点）。
3. 顺带对齐错误契约：CSRF 的 403 体把 `code` 从字符串改成数字（或在 api-design 里把 CSRF 的 code 定义为字符串并让前端兼容），否则前端永远只能显示笼统错误。→ **本次未动**（属独立的契约统一工作，且改动会牵动前端 errcode 映射）。

**实施落点（2026-10-08）**：
- 新增 `csrfTokenTTL = 2 * time.Hour` 常量（把两处魔法数收敛到一处），新增 `csrfTokenValid(cfg, token) bool`（格式 + HMAC 签名 + 时效，一次判完）。
- GET/HEAD/OPTIONS 分支：`if err != nil || existing == "" || !csrfTokenValid(cfg, existing) { 重签 }`。
- 写分支的过期判定改用同一常量（行为不变）。
- 新增 4 条单测：过期 token 的 GET 补发且补发值可用（含"立刻 POST 应 200"的端到端断言）、有效 token 不轮换、格式错重签、签名不符重签。

**验收（docker 实跑，curl）**：
- 过期 token + `GET /api/v1/courtrooms` → 响应 `Set-Cookie: XSRF-TOKEN=<新值>; Path=/; Max-Age=86400` ✅
- 用**补发的** token `POST /api/v1/courtrooms` → **200** ✅
- 对照：不刷新、直接拿过期 token POST → **403 `{"code":"CSRF_TOKEN_EXPIRED"}`**（安全属性保留：过期 token 本身不能授权写操作）✅
- 有效 token + GET → **无 Set-Cookie**（不乱轮换）✅
- `go build` / `go vet` / `go test ./...` 22 包全绿（middleware 包含 4 条新测试）✅

### 设计取舍说明（**不是缺口**，避免下次误判）

- **抬预算上限后压缩不再触发**：压缩/限流的触发线是上限的**比例**（0.7 / 0.8）。
  D17 把上限从 20k 抬到 200k 后，一场 quick 庭审（实测 89.8k）全程 `compressed=false`
  —— 这是设计本意（短庭审没有可压的冗余，压缩的价值在长庭审的长历史）。
  想在本地看压缩效果，把 `AGENT_GATEWAY_BUDGET_PER_SESSION` 临时调小即可
  （ADR 0044 §3.6 当时正是这么做的）。详见 [ADR 0013 状态更新](../adr/0013-llm-gateway-engineering.md)。
- **`.env` 里 `AGENT_GATEWAY_CACHE_ENABLED=false` / `BREAKER_ENABLED=false` 是有意显式关闭**：
  D16 之后它们是"显式值优先"，所以保持关闭是正确的（不是漏配）。要开就改 `.env`。

## 本轮新发现（D15–D21）→ ✅ **D15–D19 于 v2.12 修复、D20/D21 于 v2.13 修复**

> 都是"功能骨架在、闭环/默认值断在别处"的同一族问题。**现已全部修复**（D15 随 D10 修 compose 透传 + D16 根因消除；D20/D21 见「v2.13 收口」）。

### D16（P1）Agent Gateway 子能力默认全关 —— `AGENT_GATEWAY_ENABLED=true` 只等于开了 FileLogger　→ ✅ **v2.12 已修（`18bb55b`）**

**证据**：容器内真实 `config.Load()` + 真实 `GatewayConfig` 判定：
`Enabled=true` 但 `IsPromptCompressionEnabled=false`、`IsTokenBudgetEnabled=false`、`IsThrottlingEnabled=false`、`IsFallbackEnabled(重试)=false`、`CacheEnabled=false`、`Breaker.Enabled=false`，只有 `IsFileLoggerEnabled=true`。

**根因**：`GatewayConfig.isChildDefault()` 的判据是
`!PromptCompression && !TokenBudget && !Throttling && !Fallback && !FileLogger` ——
而 `AGENT_GATEWAY_FILE_LOGGER` 的默认值在 v0.10.22 被改成 **true**，于是 `isChildDefault()` 永远为 false，
"只写 `ENABLED=true` 就子能力全开"的便捷路径成了**死代码**。dev/prod compose 都没显式设那几个子开关，
所以压缩 / 预算 / 限流 / 重试 / 缓存 / 熔断**全部静默不生效**。

**旁证**：`agent_gateway_2026-09-21.log`（v2.10 压缩验证期）63 条里 46 条 `compressed=true` ——
那轮是**临时容器注入 env** 跑的（ADR 0044 §3.6 自己记录了这个绕过）；今天的默认栈 0 条压缩。

**影响**：ADR 0013 / 0044 的压缩、预算、重试、缓存、熔断在默认部署里从未运行；简历亮点里的
"token 降 30-50% / 缓存命中 38% / 熔断降级"在默认配置下都拿不到。**这是本轮最严重的发现。**

### D17（P1）token budget 默认 20000/session + `REJECT_WHEN_EXHAUSTED=true` 会让真实庭审硬失败　→ ✅ **v2.12 已修（`7c26a3b`，默认 200000）**

**证据**：临时打开 `AGENT_GATEWAY_TOKEN_BUDGET=true` 跑一场 quick 庭审 → opening 就烧掉 17.7k/20k，
判决阶段 4 次调用全部被拒（`llm_calls.error_msg = "agent_gateway: token budget exhausted (ratio=...)"`，
含 `judge/final` 与 `clerk/verdict`）→ 判决退化成兜底文案。

**影响**：**修 D16 之前必须先修 D17**，否则"把子能力打开"会直接把庭审质量打坏。
需要决定：抬 `BUDGET_PER_SESSION` 默认值 / 把 `REJECT_WHEN_EXHAUSTED` 改回 false（超预算降级而非拒绝）/
维持 budget 默认关但**显式**关（而不是靠 D16 那个巧合）。

### D18（P1）`reopen_trial` 不可达 + `UserAction` 恒返 200/code 0 —— 用户可见的静默失败　→ ✅ **v2.12 已修（`f3ea525` + `26790e9`）；残留见 D22**

**证据**：
1. 全仓没有任何代码写 `verdict` 阶段（`transitionPhase` 的调用点只有 opening/cross_exam/evidence/closing/deliberation），
   而 `finishTrial` 明确"Stay in deliberation phase" → 判决后 session 停在 `deliberation`。
2. `ValidateAction` 的 `reopen_trial` 要求 `verdict|appeal` → 真实流程**永远拒绝**：
   实测 `POST /actions {reopen_trial}` → 日志 `state machine rejected action "reopen_trial" in phase "deliberation"`。
3. `handler.UserAction` 把 `ProcessUserAction` 丢进 detached goroutine，**立刻返回 `HTTP 200 + code 0`**，
   错误只进 `slog.Error` → 前端 `res.code === 0` 判定成功 → `router.push` 回庭审页，用户看不到任何报错。

**影响**：v0.8.3「补充证据重开」在真实流程里**从未可用**（`reopen_test.go` 手工 seed `verdict` 阶段所以一直绿）；
且这是 ADR 0024 那类"静默错误黑洞"的又一实例 —— 前端为非零 code 准备的重试/toast 分支是死代码。

### D19（P0）CSRF cookie `Path=/api/v1` → 浏览器所有写请求 403　→ ✅ **v2.12 已修（`d3c9124` cookie + `79683d2` CORS）**

**证据**：`Set-Cookie: XSRF-TOKEN=...; Path=/api/v1`（非 HttpOnly，本意是让 JS 读）；
但 `document.cookie` 只暴露"Path 是当前文档路径前缀"的 cookie，应用页面在 `/`、`/court/...` →
**前端 `readCookie("XSRF-TOKEN")` 恒为 null** → 不发 `X-XSRF-TOKEN` 头 →
后端 `AbortWithStatusJSON(403, CSRF_TOKEN_MISMATCH)`。实测：浏览器里 `POST /api/v1/courtrooms` → 403；
同一请求手工补上 header（值 = `decodeURIComponent(cookie)`，与 Go 侧读取时的 unescape 对齐）→ 200。

**影响**：**浏览器里立案 / 开庭 / 提交证据 / 所有 action 全部 403**，应用写路径整体不可用（读路径正常）。
这也是我这次无法用浏览器跑完整庭审、只能改用 curl 复现"cookie+header 双提交"契约的原因。
**修复方向（1 行）**：`DefaultCSRFConfig` 的 `CookiePath` 改 `"/"`（Path 不是 CSRF 的安全边界，攻击者本来就跨域读不到）。
ADR 0039 §7 的验证只有单测 + `tsc`，没有浏览器实跑，所以没被发现。

### D20（P3）`global_concurrency_current` gauge 只在 acquire 时更新

`recordConcurrencyMetric(true)` 仅在 `withCancel` 成功获取 slot 时调用，`Release` 后不更新 →
实测判决完成后 gauge 仍显示 1（信号量本身正常释放：若泄漏，第 2 场庭审会因为拿不到 slot 而启动失败，实际能跑通）。
影响：仪表盘读数长期不动，会掩盖真正的泄漏。D7 的触发条件正是"看 `/metrics` 的 gauge 是否回落"，这个 gauge 不可信会误事。

### D21（P3）`/auth/anon` 并发 upsert 打 duplicate key ERROR

实测浏览器一次立案触发两次 `/auth/anon`：`INSERT INTO users ... SQLSTATE 23505 duplicate key (users_pkey)` →
`user upsert failed` WARN。功能不受影响（token 照发），但日志里是 ERROR 级噪音，风控/告警会被误导。

---

### D15（P3）prod compose 漏传 gateway 子开关　→ ✅ **根因已随 D16 消除（`18bb55b`）**

实现 D10 时发现：**prod compose 用显式 `environment:` 列表而没有 `env_file`**（`docker-compose.yml`），
所以只在 `.env` 里写的变量**进不了后端容器**。这正是 D10 要修的同一类静默失效，只是换了一层。

- 已修：D10 的三个 `RATE_LIMIT_*` 已补进 `docker-compose.yml`（否则 D10 在 compose 部署下等于没做）。
- **仍然缺失（未修，超出本批范围）**：gateway 的一批子开关（`AGENT_GATEWAY_SMART_COMPRESSION` /
  `_PROMPT_COMPRESSION` / `_SCORE_THRESHOLD` / `_KEEP_RECENT_FORCED_N` / `_SUMMARY_INSERT_THRESHOLD` /
  `_SMART_COMPRESSION_ABSTRACTIVE_SUMMARY` / `_TOKEN_BUDGET` / `_THROTTLING` / `_THROTTLING_THRESHOLD` /
  `_FALLBACK` / `_BUDGET_PER_SESSION` / `_REJECT_WHEN_EXHAUSTED` / `_BUDGET_SLIDING_WINDOW_SEC` /
  `_FILE_LOGGER_PROMPTS` / `_FILE_LOGGER_PROMPTS_MAX_BYTES`）+ `IDEMPOTENCY_TTL_HOURS` /
  `PROMPTLAB_YAML_PATH` 都不在 prod compose 的显式列表里。

**影响（2026-10-08 更正）**：原先这里写"`isChildDefault()` 会让子能力全开，恰好与意图一致"——**错了**。
docker 实跑（D16）证明 `isChildDefault()` 因 `FileLogger` 默认 true 而**永远为 false**，子能力其实全关。
所以 D15 与 D16 是同一个后果的两个入口：prod compose 既没传子开关，传了 `ENABLED=true` 也不会自动全开。
**结论：D15 的严重度应从 P3 上调到 P1，并与 D16 一起修。**
**触发条件**：需要在不改 compose 的前提下用 `.env` 调 gateway 子能力（例如线上想单独关掉
`SMART_COMPRESSION`）；或将来重新部署到云环境（ECS 已于 2026-08-05 终止，故现在不紧迫）。

**修复方式**（与 D10 同构，工作量极小）：把上述变量按 `${VAR:-默认值}` 形式补进
`docker-compose.yml` 的 backend `environment:`，并加一条测试/脚本比对"config 声明的 env 清单"
与"compose 传递的 env 清单"防再次漏接。

---

## 分阶段计划

### 阶段 1 — 低风险收尾（1 个 PR，纯配置与响应头，无行为变更）

**范围**：D11、D14、D10。

**为什么放一起**：三处都不改变业务语义，只是把「本该可配的变可配、本该一致的对齐」。改动面小，适合作为一个收尾 PR 快速清掉。

**验收**：
- 新增配置项有默认值单测；沿用 v2.10 的做法，**为配置映射加反射断言测试**，防止再次出现「字段漏接」静默失效（ADR 0044 §3.5 Problem B 的同类问题）。
- 429 响应头有断言测试。
- ⚠️ 涉及 `config.go` 默认值改写 → 按 AGENTS.md §11.2 **必须起 dev compose 跑业务验证**，只跑单测不算完成。

### 阶段 2 — 可查询性（1–2 个 PR，直接加固亮点 1 与亮点 2 的 claim）

**范围**：D12（事件读取端点）、D9（审计表补链路字段）。

**为什么优先**：这两处是简历里最容易被深挖的两句话——「形成跨域查询能力」和「Trace 关联」。当前一个只存在于 SQL 文档、一个在 DB 层根本做不到。补完之后，这两条从「说法」变成「可用」。

**验收**：
- 新增只读端点带归属校验、分页、前缀过滤测试。
- 表字段变更后，断言写入记录的两个字段非空。
- 补一段可执行文档：把 db-design 里的漏斗 SQL 与端点对齐（避免两处说法漂移）。
- ⚠️ 涉及网关写入路径与 API 路由 → §11.2 要求 docker 业务验证。

### 阶段 3 — 运行时健壮性（1 个 PR）

**范围**：D7（会话终态回收）、D8（重试器并发安全 + 错误分类）。

**为什么放一起**：都属于「长时间运行/并发场景下才会暴露」的问题，单测不充分，适合一起做并配 `-race` 验证。

**验收**：
- `go test -race` 通过（D8 必查）。
- 终态回收有测试：断言清理后按会话维度的状态归零。
- 重试分类有表驱动测试。
- ⚠️ 涉及网关 → §11.2 要求 docker 业务验证（跑通一场完整庭审，确认无异常日志）。

### 阶段 4 — 隔离硬化（**需先讨论**，1 个 PR）

**范围**：D13。

**为什么单独且需授权**：改动涉及「对方提示词如何组装」，属 AGENTS.md §2.1 需要先讨论后执行的范畴（影响控辩双方可见信息与裁决上下文）。**本阶段不得直接开始实现**，需先出方案讨论、确认边界与验收标准，获得明确授权后再动。

---

## D7. ✅ 网关按会话状态无回收（预算表 + 响应缓存内存膨胀）

### 现象

- `TokenBudget.Reset(ctx, sessionUUID)`（`token_budget.go:133`，实现 `token_budget_memstore.go:210`）和 `ResponseCache.EvictSession(sessionID)`（`cache.go:241`）都已实现，但**在生产代码里没有任何调用点**——grep 全仓 `.Reset()` / `EvictSession` 只命中测试与 `internal/util/bagofwords.go` 的无关 `Reset`。
- `Gateway` 只暴露 `Complete` / `StreamComplete`（`gateway.go:124`、`:336`），预算快照与文件日志方法都是私有的，**包外根本拿不到入口去调这两个清理方法**。
- 结果：按 session 累积的预算滑动窗口与响应缓存映射随进程存活一直增长。

### 根因

- 当初实现清理方法时没有明确的「庭审结束」生命周期钩子可挂，于是只写了方法、没接。
- 缺少「会话级资源归属」的统一抽象，导致清理点分散（预算在网关、并发槽在庭审服务、缓存又在网关）。

### 建议修复

1. 在 `Gateway` 上加一个公开的会话清理方法（例如 `Release( sessionUUID )`），内部同时调 `TokenBudget.Reset` 与 `ResponseCache.EvictSession`。
2. 挂到**已有的终态钩子**上：判决落库处（`courtroom/service.go:1796` 附近，那里已经在做判决质量指标）是最自然的调用点，无需新起定时任务。
3. 补测试：断言清理后该 session 的预算记录与缓存条目均归零，且**不影响其他 session**。

### 关联文档

- ADR 0013（缓存 / 超时 / 熔断设计）、ADR 0037（可观测性）
- `courtroom/service.go:1796` — 已存在的终态钩子

### 不做什么（明确边界）

- ❌ 不引入定时任务或后台 GC 协程（保持单二进制、无 cron 依赖，与 D4 同样的边界）
- ❌ 不改成基于 TTL 的自动过期（预算表语义是「本场庭审内」，TTL 会给错语义）
- ❌ 不做跨进程共享状态（那是多副本话题，见「已决策接受」）

### 触发条件

- 单进程连续跑多场庭审后内存增长可观测（`/metrics` 的缓存 size gauge 持续升高不回落）
- 用户反馈长跑后内存占用高

---

## D8. ✅ 重试器并发不安全 + 无错误分类

### 现象

- `Retryer` 是共享单例，`lastCount` 是普通 `int` 字段（`retryer.go:22`），在 `Do`/`DoContext` 里自增（`:55`、`:79`），由 `LastCount()`（`:91`）在调用**之后**读取。并发调用下存在数据竞争，且「调完再问它」的模式会把别次调用的重试次数读成自己的。
- 重试策略是**任何非 nil 错误都重试**，没有错误分类、没有 jitter、没有退避上限（`:45-88`）。超时、鉴权失败、参数错误一视同仁。

### 根因

- 早期为了「先让失败能自动恢复」写了最简单的版本，没有做失败模式区分；重试计数用「事后查询」而非「返回值携带」，是并发缺陷的直接来源。
- 单副本、低并发阶段这个问题不会显性暴露，所以一直没有触发修复。

### 建议修复

1. `Do`/`DoContext` 改为返回 `(结果, 重试次数, error)`，删掉 `lastCount` 字段与 `LastCount()` 方法——从根上消除竞争，而不是加锁。
2. 引入错误分类：可重试（限流、服务端错误、网络超时）、不可重试（参数/鉴权、用户取消）。流式调用保持不重试（现状正确，不要改）。
3. 退避加 jitter，并设上限。
4. 补测试：`-race` 下并发调用；分类用表驱动测试覆盖各类错误。

### 关联文档

- ADR 0013 §重试决策
- `gateway.go:239-264` — 熔断包重试的层级关系（改重试器时注意不要把熔断计数语义带坏）

### 不做什么（明确边界）

- ❌ 不引入第三方重试库（当前逻辑 100 行内，引依赖不划算）
- ❌ 不实现多级退避策略调参（先做对，不做可调）
- ❌ 不改流式的「不重试」决策

### 触发条件

- `go test -race ./internal/agent_gateway/...` 报竞争
- 出现「重试了不该重试的请求」导致的资损或错误率上升

---

## D9. ✅ `llm_calls` 缺 `request_id`、`agent_id` 从不写入

### 现象

- 模型字段（`model/db.go:256-271`）为：`SessionID` / `AgentID *uuid.UUID` / `TaskType` / `Model` / tokens / `CostUSD` / `CostCNY` / `LatencyMs` / `Status` / `ErrorMsg` / `CreatedAt`。
- **没有 `request_id`**，因此数据库层无法按链路关联。
- **`AgentID` 字段存在但从不填充**——写入路径只映射了会话、任务类型、模型、token、耗时、状态、错误（`gorm_store.go:72-84`），所以该列实际恒为 NULL。
- 现状下链路关联全靠 JSON Lines 文件日志 + `decision_events` 表（按同一个 `request_id` 关联），DB 审计表只能按 session 查。

### 根因

- 审计表先于链路体系建立，`request_id` 是后来引入的，没有回头补列。
- `AgentID` 是 UUID 外键设计（指向 agents 表），但调用点手上只有 Agent 类型字符串，映射时被跳过，也没人发现——典型的「字段存在即被认为已实现」。

### 建议修复

1. 加 `request_id varchar(36)` 并建索引（GORM AutoMigrate 自动加列，无需手写迁移）。
2. 二选一并明确取舍：把 `AgentID` 真正填上（需要类型 → UUID 的解析），或者直接加 `agent_type varchar(50)` 存字符串。**建议后者**——审计需要的是可读的 Agent 类型，而不是要再 JOIN 一次。
3. 补测试：写入后断言新字段非空（这是防止「加了列但忘了写」的唯一可靠手段）。

### 关联文档

- ADR 0033（链路架构）、ADR 0037（可观测性）
- `decisioncourt-db-design.md` — 需同步补字段说明

### 不做什么（明确边界）

- ❌ 不回填历史数据（无链路信息可取）
- ❌ 不与 `decision_events` 合表（两者写入频率与保留策略不同）
- ❌ 不在此项里改审计粒度（那是另一个话题）

### 触发条件

- 需要「按某次请求查它经过了哪些 Agent / 花了多少 token」而当前只能翻日志文件时

---

## D10. ✅ 限流阈值未接线配置（会话速率 / 全局并发）

### 现象

- ADR 0027 §4.3 提议的 `RATE_LIMIT_SESSION_ACTION_RPS` / `RATE_LIMIT_SESSION_ACTION_BURST` / `RATE_LIMIT_MAX_CONCURRENT_TRIALS` **在 `config.go` 和 `.env.example` 里都不存在**（grep 无命中）。
- 全局并发上限是硬编码常量 `5`（`cmd/server/main.go:271` 处 `NewConcurrencyLimiter(5)`）。
- 会话限流用的是代码内置的 `DefaultSessionConfig`（2 rps / burst 5）。
- 真正可配的只有试用配额 `USER_TRIAL_LIMIT`。

### 根因

- 四层限流是分批落地的，提议的配置项在实现时被漏掉；由于没有配置映射的强制测试（对比 D9 同类问题），漏接无人发现。
- 单副本、无运维压力时，硬编码不会造成可见问题，所以一直没补。

### 建议修复

1. 在 config 里加上述三个字段 + viper 默认值（保持当前 5 / 2 / 5 不变，避免行为变更）+ `.env.example` 同步。
2. 加到「必须接线」的配置清单里，并**补配置映射反射断言测试**（v2.10 已为网关配置建立了这个模式，直接复用）。
3. ⚠️ 改 `config.go` 默认值 → §11.2 要求 docker 业务验证。

### 关联文档

- ADR 0027 §4.3（提议原文）、ADR 0014
- AGENTS.md §11.2 — config.go 默认值变更必须 docker 验证

### 不做什么（明确边界）

- ❌ 不做运行时热更新（改配置需重启，与现有部署模型一致）
- ❌ 不顺手改默认值（本次只做「可配」，调参是运维决策）

### 触发条件

- 需要在不重发版本的前提下调整限流阈值
- 限流阈值误伤真实用户，需要快速放宽

---

## D11. ✅ per-IP 层 429 缺 `Retry-After`

### 现象

- per-IP 层拒绝时只 `AbortWithStatusJSON(429, {code:1429, message:...})`（`middleware/ratelimit.go:131`），**没有 `Retry-After` 响应头**。
- 同项目的试用配额层是**有**的（带 `Retry-After` 头 + `retry_after_seconds` + `resets_at`），两层不一致。

### 根因

- 两层由两次不同的需求驱动实现（先做 IP 层，后做配额层时提高了标准），没有回头统一响应契约。
- 令牌桶本身能算出「还需等多久」的近似值，但当时没做。

### 建议修复

1. 在 IP 层拒绝时补 `Retry-After` 头（按当前桶余量与速率估算，向上取整秒）。
2. 补测试断言头存在且为正整数。
3. （可选）顺带统一两层的错误体字段命名，避免前端要写两套解析。

### 关联文档

- ADR 0027、ADR 0014

### 不做什么（明确边界）

- ❌ 不改拒绝码（`1429` 已被前端 `ErrorBus` 依赖）
- ❌ 不改成精确排队语义（只需给出合理退避指引，不做真排队）

### 触发条件

- 前端或客户端需要按退避重试，但拿不到等待时间
- 顺手做（工作量极小）

---

## D12. ✅ `decision_events` 无读取端点

### 现象

- 只有写路径：`POST /api/v1/courtrooms/:session_uuid/events`（`api/handler_events.go`，配合 `handler.go:207` 注册）。
- 全仓 grep 无任何对 `decision_events` 的**读取**接口（`GET` 端点不存在；`/metrics` 返回的是内存指标，不查这张表）。
- 「跨域查询能力」目前只以 SQL 形式存在于 `decisioncourt-db-design.md` §9.6（漏斗查询 + 转化率查询），需要人工连库执行。

### 根因

- 埋点（写入）是产品需求驱动的；分析（读取）当时判断「手工 SQL 够用」，优先做了写入链路。
- 没有后台/分析页面，所以从未产生读取端点的需求。

### 建议修复

分两步，先做小的一步即可形成闭环：

1. **只读列表端点**：`GET /api/v1/courtrooms/:session_uuid/events`，owner-only、分页、支持 `event_type` 前缀过滤（`fe.` 与后端事件可分开取）。这一步就能让「跨域查询」从 SQL 文档变成 API。
2. **（可选）漏斗聚合端点**：把 db-design §9.6 的两个查询固化成端点，返回阶段耗时与转化率。
3. 补测试：归属校验、分页边界、前缀过滤。
4. 同步文档：让 db-design 的 SQL 与端点输出口径一致，避免两处漂移。

### 关联文档

- ADR 0020（前端埋点决策）
- `decisioncourt-db-design.md` §9.6 — 现有查询范式
- `decisioncourt-api-design.md` — 需补新端点

### 不做什么（明确边界）

- ❌ 不引入 BI 平台 / Metabase
- ❌ 不做实时看板（端点足够，前端另议）
- ❌ 不加跨 session 的全量分析端点（越权风险，且非当前需求）

### 触发条件

- 需要回答「用户在哪个阶段流失」而当前只能人工执行 SQL
- 面试/演示需要展示跨域查询能力

---

## D13. 🔴 投影隔离两处硬化（⚠️ 需先讨论，§2.1 范畴）

> **本项涉及「对方提示词如何组装」，属 AGENTS.md §2.1「先讨论后执行」范畴。不得直接实现。**

### 现象

**A. 公共消息剥离推理的路径没有生产消费者。**
- `BuildContextView` 会为「他人发的公共消息」剥离推理字段并放进 `WorkingMemory`，排序稳定、有单测覆盖。
- 但 grep 显示 `WorkingMemory` **只在 `internal/a2a/context_view.go` 内被构造**，包外没有任何生产代码读它。
- 对方上下文实际是从 `model.Message` 拼的（渲染对方最新发言正文），而 `model.Message` **没有推理列**——推理链存在 JSONB metadata 里且不回读进提示词。
- 因此「控辩双方互不可见对方推理链」当前有两重保险，但**其中一重是结构性巧合**（因为那张表恰好没这个字段），而不是被设计保证的。

**B. 字段剥离是单键黑名单。**
- `SanitizedPayload` / 行级剥离只删 `reasoning` 一个键。
- 新增任何敏感字段（如 `planned_next_move`、内部置信度）都必须**人工记得**加进剥离逻辑，漏了就静默泄漏。ADR 0003 已记录此弱点。

### 根因

- A：v0.5 重设计时投影层先建好，但提示词组装仍走旧路径（`model.Message`），两条路没有收口，于是新路径成了「写好备用的旁路」。
- B：当时只有一个字段需要剥离，黑名单是最小实现；没有预见到字段会增长。

### 建议修复（待讨论确认）

1. **收口**：让提示词组装显式消费投影结果，或至少把「对方上下文来源」统一到一个出口，消除旁路。
2. **白名单化**：把载荷剥离改成正向投影——明确「允许进对方视图的字段」，未知字段默认不进入。这是把安全性从「记得删」变成「记得加」。
3. **回归测试**：注入一个虚构的敏感字段，断言它不出现在对方提示词里（当前测法只能断言已知字段被删，测不出新增字段泄漏）。

### 关联文档

- ADR 0003（投影决策，含已记录的弱点）、ADR 0002（私有通道）
- `docs/decisioncourt-agent-design.md` — 三道信息隔离防线
- AGENTS.md §2.1 — 需先讨论后执行

### 不做什么（明确边界）

- ❌ 不改用户/审计视角的可见性（`MemoryAuditPanel` 同时看双方私有笔记是**有意的产品设计**，不是漏洞）
- ❌ 不改 `ListVisibleTo` 的 SQL 语义（私有记忆的硬保证就在这里，不要动）
- ❌ 不在本项里新增记忆类型（那是产品需求）

### 触发条件

- 用户授权讨论并明确验收标准后
- 新增任何需要隔离的敏感字段之前（否则黑名单必然漏）

---

## D14. ✅ 网关二进制默认关闭 vs 部署默认开启

### 现象

- 代码默认：`AGENT_GATEWAY_ENABLED` 默认 **false**（`config/config.go:189`）。
- 部署默认：`docker-compose.yml` 用 `${AGENT_GATEWAY_ENABLED:-true}`、`docker-compose.dev.yml` 直接写 `"true"`、`.env.example` 写 `true`。
- 后果：通过 compose 跑一切正常；**直接跑二进制（`go run ./cmd/server`、非 compose 部署）会静默不启用网关**——也就是审计、预算、压缩、缓存、熔断全部不生效，且没有任何提示。

### 根因

- 默认值取「安全保守」（关着不会出问题），但部署侧为了功能可用又开了——两个默认值的目标不一致，中间的空档没人负责。
- 最近的 dev compose 提交只是把 dev 环境显式打开，没有解决「二进制默认」这个根。

### 建议修复

二选一（建议第 2 个，避免行为变更）：

1. 把代码默认改成 `true`，与部署默认对齐。
2. 保持默认 `false`，但**启动时检查**：若检测到「以二进制方式启动且该开关为关」，打一条显眼的 WARN 日志，说明「网关已禁用，审计/预算/压缩不生效」。让静默失效变成可见。

补测试：默认值单测 + 启动日志断言（若选方案 2）。
⚠️ 改 `config.go` 默认值 → §11.2 要求 docker 业务验证。

### 关联文档

- ADR 0044 §3.6（记录了 compose 与 .env 的开关不一致）
- AGENTS.md §11.2

### 不做什么（明确边界）

- ❌ 不强制开启（保留用户关闭网关的能力）
- ❌ 不改 compose（已正确）

### 触发条件

- 出现「功能在本地/生产不生效但代码看起来没问题」的排查
- 顺手做（工作量极小）

---

## 核对为「已实现 / 已决策接受」，不列入缺口

以下项在核对中一度被怀疑，实测后确认**不是缺口**，记录以免下次重复排查：

| 项 | 结论 |
|---|---|
| 压缩的评分档位阈值曾为死配置 | 已于 v2.10 激活（ADR 0044 #2），非缺口 |
| 压缩缺新近度衰减 | 已于 v2.10 实现（ADR 0044 #4），非缺口 |
| 压缩缺质量闭环 | 已有判决证据引用准确率指标，非缺口 |
| 文件审计无脱敏 | ADR 0042 §3 明确为**用户已接受的 opt-in 风险**（默认只记元数据），是决策不是缺口 |
| `logs/` 无保留策略 | 已有 D4（deferred-items-2026-09-21），不重复开 |
| 四层限流为进程内存态、多副本失效 | 属**架构演进**，触发条件是「多副本部署」；单副本下语义正确，且迁 Redis 会引入故障域成本。保持 deferred，触发条件见下 |
| 生成式摘要默认关闭 | ADR 0044 #6 的有意决策（opt-in），非缺口 |
| 贪心打包未按价值密度排序 | 属**实现选择**（可解释性优先），非缺口；若做，见下「可选优化」 |

### 可选优化（不在本批计划内）

- 贪心打包排序键从「组重要度」改为「组重要度 / 组 token 数」（按价值密度）——最小改动、可能提升保留质量，代价是位置加权组更容易被挤掉。需要先有评测数据支撑再动。

### 长期触发项

- **限流迁 Redis / 下沉到入口反代**：触发条件 = 改为多副本部署，或试用配额出现跨副本超额。

---

## 全局验收要求（适用于本批每个 PR）

1. **测试同步**（AGENTS.md §3）：每项都有对应的断言测试；**严禁**为通过而简化或删除既有测试。
2. **文档同步**（AGENTS.md §1.2）：涉及 API 的更新 `decisioncourt-api-design.md`，涉及字段的更新 `decisioncourt-db-design.md`。
3. **Docker 业务验证**（AGENTS.md §11.2）：触及 `config.go` 默认值、网关写入路径、路由或中间件的项，**必须**起 dev compose 跑通一场完整庭审，不能只跑单测。
4. **配置映射护栏**：新增配置字段一律补反射断言测试（复用 v2.10 建立的模式），防止再次出现静默漏接。
5. **逐项独立可回滚**：每项一个提交，避免多项交缠导致回滚困难。

## 关联文档

- [deferred-items-2026-09-21](deferred-items-2026-09-21.md) — D4 / D5 / D6，本批承接其后编号
- [deferred-items-2026-08-21](deferred-items-2026-08-21.md)
- [ADR 0044 压缩策略审查](../adr/0044-token-compression-strategy-review.md) — §3.5 记录了同类「静默失效」根因
- [ADR 0027 限流防御](../adr/0027-rate-limit-defense-in-depth.md) — D10 / D11 的提议原文
- [ADR 0020 前端埋点](../adr/0020-frontend-analytics-via-decision-events.md) — D12
- [ADR 0003 投影](../adr/0003-contextview-projection.md) / [ADR 0002 私有通道](../adr/0002-a2a-private-channel.md) — D13
- [V1-ROADMAP](../V1-ROADMAP.md) — 排期时需与此处里程碑对齐
- `.trae/documents/interview-answers-project-highlights.md` — 本批缺口的来源核对（含口径风险）

---

## v2.13 实跑中新修（2026-10-08，浏览器真实路径测试期发现，均未 push）

v2.13 收口后的浏览器实测暴露 3 个**新的运行时缺陷**，都是「功能骨架在、异常路径没兜住」同一族。已修复 + 补回归测试。

### R1（P0，卡死）ReAct 未注册 tool 是致命错误 → 整个 cross_exam round 中断

**现象**：实测一场庭审，cross_exam 第 1 轮控方发言后**辩方永远不说话**，trial 卡死在 `active/cross_exam/round 1`，用户视角「没法继续了」。

**证据**：`decision_events` 中 `span.RunCrossExamRound` 带 `error.message = react iter 2: tool "" not registered`。

**根因**：`react_runner.go` 的 tool 分支对「不在白名单 / 未注册」直接 `return error`（终止整轮）；而 tool **执行**失败却是可恢复 observation。deepseek 偶发吐出 `action="tool_call"` 但 `tool` 为空（`NormalizeAction` 只兜空 action，不管空 tool），一次畸形输出就把整轮打死。

**修复**：未授权 / 未注册的 tool 统一降级为可恢复 observation（`step.Error` 记 `tool_not_allowed` / `tool_not_registered`，把错误喂回消息流让模型自纠）。测试 `TestReActRunner_UnknownToolIsRecoverableObservation`（替换原 `..._UnknownToolRejected` —— 原测试断言的正是被本次修掉的旧契约，属契约变更而非简化测试）。

### R2（P1）埋点 transport 漏发 CSRF header → 所有 `fe.*` 事件 403

**现象**：浏览器控制台持续 `POST /api/v1/courtrooms/{uuid}/events 403` + `[analytics] frontend event flush failed: fe.evidence_submitted status=403`。

**根因**：`lib/api.ts:fetchJson` 对 POST 自动注入 `X-XSRF-TOKEN`（读 `XSRF-TOKEN` cookie），但埋点走 `lib/transport.ts` 自己的 fetch，只带 `Content-Type` + `Authorization` + cookie，**唯独没有 CSRF header** → 后端 `CSRF_TOKEN_MISMATCH`。DB 佐证：最后一条成功的 `fe.*` 埋点是 **2026-09-14 11:12**，正好在 CSRF 中间件（v2.5, 2026-09-15）上线前一天 —— **前端埋点已静默丢失约 3 周**。附带代价：失败事件回填队列后每 5s 重试 → 控制台刷屏。

**修复**：`transport.ts` 默认 fetcher 补 `X-XSRF-TOKEN`（`readCookie` 内联，避免与 api.ts 循环依赖），加 2 个测试（有 cookie 注入 / 无 cookie 不伪造）。

### R3（P1）反幻觉守卫对「带 evidence_refs 的发言」一律误拒 → 每次发言白重试一次

**现象**：每次开场/质证发言都出现 `streamSpeakContent hallucination validation failed (mode=evidence_ref_unverified)` → 2 秒后 `retry failed, restoring streamed fallback content` → 把**刚拒掉的内容原样放行**。前端表现为流式打字机**卡 2 秒**（重试是非流式调用，期间无 `agent.speak_chunk` 帧）。

**根因**：`ValidateAgainstHallucination` 的 Layer B 在 `evidence_refs` 非空但 `allowedIDs` 为空时「保守拒绝所有引用」；而 `RunnerConfig.AllowedEvidenceIDs` **从未接线**（`output_validator.go` 注释里「等 v0.11 加 AllowedEvidenceIDs」的 TODO 一直没做）。于是守卫只贡献延迟、没拦住任何东西。

**修复**：`RunnerConfig` 新增 `AllowedEvidenceIDs`，由 `lawyerSpeakReAct` 从本场 `evidences` 的 `EvidenceID` 填入；两个调用点（流式 + `validateSpeak`）都改传该列表。现在引用真实 ID 放行、编造 ID 才拒绝。测试 `TestReActRunner_ValidateSpeak_HonorsAllowedEvidenceIDs`（命中 / 编造 / 未接线三态）。

**验证**：`go build ./...` + `go test ./...` 22 包全绿；前端 `tsc --noEmit` + 99 个 lib 测试全绿（+2 新）。

**顺带发现（dev 工具链）**：`air` 未观测到 `internal/agent` 下的改动 —— 实测改完代码后端不会自动重建，需 `docker restart dc_dev_backend`。Docker Desktop Windows bind mount 上 fsnotify 事件本就不可靠，改后端代码后应显式重启容器再验证。

### R4（用户要求删除）庭审回放 / Trace 可视化整体移除（2026-10-08）

**触发**：用户在浏览器实测中点「庭审回放」报错，明确要求「将庭审回放功能删除」。

**范围确认**：该按钮打开的对话框含 3 个 Tab —— ①庭审时轴 ②信念轨迹 ③技术 trace（高级）。技术 trace 是 v1.0.4 PR-C2 / ADR 0033 的交付物且无其他入口。用户明确选择**全删（含技术 trace）**。

**已删除（仅前端；后端 trace 子系统未动）**：

| 删除项 | 说明 |
|---|---|
| `frontend/components/trace/` 整个目录 | `TrialReplay.tsx` / `AgentTraceNode.tsx` / `BeliefDiffTimeline.tsx` / `RebuttalTraceNode.tsx` / `TrialReplay.test.ts` |
| `frontend/lib/trace.ts` + `lib/trace.test.ts` | 只被 TrialReplay 使用，一并移除 |
| `CourtroomScene.tsx` | 移除 `TrialReplay` import、`replayOpen` state、「庭审回放」按钮（含 `data-testid="trial-replay-button"`）、顶层 `<TrialReplay>` JSX、`History` 图标 import、4 处 stale 注释 |

**验证**：`tsc --noEmit` 干净；lib 测试 95/95 绿（原 99，减去随功能删除的 4 个 `lib/trace.test.ts` 用例）。

**遗留（未处理，需决策）**：后端 `/api/v1/courtrooms/:uuid/traces` REST 端点 + `internal/trace` 聚合器/解析器 + trace 落盘仍在，但**前端已无消费者**，现在是可达性死代码。相关注释（`api/handler_trace.go:99`、`trace/parser.go:6`、`trace/aggregator.go:20`）仍写着「供前端 TrialReplay 渲染」，已过时。要清理需要单独一轮（涉及 `AgentGatewayTrace` 写入路径 + metrics + 测试），本次未动。

**待办清单影响**：V1-ROADMAP 里 v1.0.4 PR-C2「Trace 前端可视化」这条已交付项**现已被移除**，后续叙述不应再把它当作现有能力。

### R5 庭审现场布局重做 + 气泡改顶层浮层（2026-10-08）

**触发**：用户反馈（**反复反馈过**）：①庭审现场内部会滚动、被内容压缩；②辩方流式气泡把现场撑出横向滚动。

**R5-a 气泡横向溢出的真正根因**（此前多次"修"都没修对）：

气泡（`AgentAvatar`）是容器内 `absolute bottom-full left-1/2` + Tailwind `-translate-x-1/2` 做水平居中，**但它同时是 framer-motion 的 `motion.div`**，而 `bubbleEnterExit` variants 含 `y` + `scale`（`lib/animations/variants.ts:92`）。framer 会把整个 `transform` 写成行内样式，**直接覆盖掉 `-translate-x-1/2`** → 气泡变成 `left:50%` 但没回移，**整体右移半个气泡宽（120px）**。居中角色看不出来，最右列的辩方正好顶出面板 → 横向滚动。此前用 `overflow-x-clip` 只是把症状裁掉。

**修法**：气泡改为 **portal 到 `document.body` + `position: fixed`**（顶层浮层）。定位由不做动画的外层 div 负责（纯 CSS `-translate-x-1/2 -translate-y-full`），动画留在里层 `motion.div`，两者不再互相覆盖；气泡也不再参与任何祖先的布局/滚动。位置由 `getBoundingClientRect` 量出，并在 `resize` + `scroll`（capture）时重算 —— 左列现在是滚动容器，气泡必须跟着滚。

**R5-b 布局**：

| 容器 | 改前 | 改后 |
|---|---|---|
| 左列（庭审现场 + 证据板） | `overflow-x-clip` | `overflow-y-auto overflow-x-hidden` —— **整列作为滚动容器** |
| 庭审现场面板 | `overflow-y-auto`（内滚 + 被 flex 压缩） | `shrink-0 overflow-hidden`（**不再内滚、不再被压缩**，高度由外层决定） |

效果：庭审现场保持正常（不挤压）的渲染高度；内容多到超出视口时，滚动的是**左列整体**，而不是把现场压扁。

**验证**：`tsc --noEmit` 干净；lib 测试 95/95 绿；前端 HMR 重编译通过。**浏览器实测未做**（本机 IAB 渲染帧不可用），需用户实跑确认。

### R5-c 气泡上边界收敛到顶栏（2026-10-08，用户补充要求）

用户澄清：「在顶层」不等于可以压住顶栏 —— 气泡不许越过 `<header>`（即「直接判决」按钮所在的那条栏）。

**修法**：气泡浮层外面再包一层**固定裁剪容器**：`fixed left-0 right-0 bottom-0` + `top: <顶栏底边>` + `overflow-hidden`。气泡在容器内 `absolute` 定位，坐标减去容器 top。于是气泡**可以越出庭审现场面板、但被顶栏底边裁掉**，不会盖住功能栏。

顶栏加了 `id="courtroom-topbar"` 供 `measureAnchor` 量边界；浮层 z-index 从 `z-[60]` 降到 `z-40`（在 Radix 对话框 `z-50` 之下，避免盖住「调查员提问」等模态框）。

### R5-d 证据归档加「排队中」反馈（2026-10-08，用户要求）

**触发**：用户反馈归档时看不到任何等待提示。根因是 `SubmitEvidence` 要拿 session 互斥锁（ADR 0012 决策 1），发言轮次持同一把锁 → 发言途中提交会**排到本轮结束**才落库，而 UI 此前毫无反馈。

**修法**：乐观插入 + 待确认角标。
- `EvidenceBoard` 新增 `pendingEvidences` 入参（导出 `PendingEvidence` 类型），渲染灰态虚线卡片 + 「排队中」脉冲角标（`data-testid="pending-evidence-chip"`）。
- `CourtroomScene.handleSubmitEvidence` 提交瞬间 push 一条占位；`useEffect` 监听 `store.evidences.length` 增量，按 **FIFO** 消掉对应占位（真正的证据由后端 `evidence.added` 落库后出现）。

**已知边界（未做）**：只有"按数量增量 FIFO 消占位"，没有做超时兜底 —— 若提交最终失败（例如后端拒绝），占位会一直停在「排队中」。失败路径要精确对应需要后端回一个可关联的 request_id，属后续增强。

### R6 导出 JSON 401 + 导出 PDF 样式丢失（2026-10-08）

**R6-a 导出 JSON 401**

**根因**：`lib/api.ts:exportSession` 用的是**裸 `fetch`** —— 既没带 `Authorization`，也没带 `credentials`。auth 中间件（`internal/auth/middleware.go`）只认 `dc_session` cookie 或 `Authorization: Bearer`；而 dev 下前端(3010)与后端(8180)是**跨域**，默认 `credentials:"same-origin"` 连 cookie 都不发 → 必然 401。`fetchJson` 是对的（`ensureAuthToken()` + Bearer + `credentials:"include"`），导出这条路径当初没跟着走。

**修法**：与 `fetchJson` 对齐 —— `await ensureAuthToken()` → 带 `Authorization: Bearer` → `credentials:"include"`（GET 幂等，不需要 CSRF header）。

**实跑验证**（curl 对真实后端）：
- 不带认证 → `HTTP 401`（复现用户症状）
- 用该 session owner 的 anon token 带 Bearer → **`HTTP 200`, `application/json`, 36596 bytes**

**R6-b 导出 PDF 样式丢失（两轮：第一轮判断偏了，第二轮实拍 PDF 后重写）**

**第一轮（错）**：以为是"深底浅字没被覆盖"，于是在 print 里把 `.bg-ink*` 全刷成白底黑字。用户回传实拍 PDF 后确认**判断偏了**——用户要的不是省墨黑白，而是与屏幕一致。

**第二轮（实拍核对后）**：把用户导出的 `11.pdf` 渲染成 12 张 PNG 逐页看，确认两个系统性原因：

- **A. 配色被旧规则主动抹掉**。原 print 第 2/4/5/6 条把 `body/.bg-paper/.bg-paperDeep/.bg-white` 刷白、把 `.bg-prosecution/.bg-defense/.bg-judge` 刷成 `transparent`、把评分条刷成纯黑 → 设计里所有色条、分隔线、评分条直接消失，整份 PDF 变黑白。
- **B. 打印视口掉到 Tailwind `md` 断点以下 → 所有响应式布局失效**。旧 `@page { margin: 1.5cm 1cm }` → A4 内容宽 = 210mm−20mm = 190mm ≈ **718px < 768px(md 断点)** → `md:/lg:` 工具类全部不生效 → 多列 grid 塌成单列。**实拍证据**：判决页顶部 `grid md:grid-cols-[1fr_auto_1fr]`（控方 | ⚖ | 辩方）在 PDF 第 1 页变成控方/辩方**上下堆叠**。

**修法**（`app/globals.css` 的 `@media print` 整块重写）：
1. `print-color-adjust: exact` 全局强制 —— 背景/配色照原样打印；
2. **删掉全部"改配色"规则**（原第 2/4/5/6 条 + 第一轮加的第 10 条一并移除），只保留"隐藏交互元素 / 停动画 / 展开 max-height / section 分页"；
3. `@page { size: A4; margin: 0 }` —— 内容宽回到 A4 全宽 ≈ 794px > 768px，`md:` 断点重新生效；视觉留白交给页面自身 `container px-6`。

**验证**：`tsc --noEmit` 干净；括号配平校验通过；前端 HMR 重编译通过。**PDF 效果仍需用户实跑确认**（本机无可用浏览器渲染）。

**注意**：`@page margin:0` 依赖浏览器打印对话框的"页边距"选 `Default`（Chrome 会用 CSS 里的 `@page` 值）。若用户选 `Minimum`/自定义边距，内容宽可能又掉回 768px 以下。

### R6-c 导出 PDF 卡顿 + 分页控制（2026-10-08，用户追问后一并做）

**卡顿根因**：`window.print()` 是**同步阻塞**调用，浏览器要按 print media 整页重新布局 + 绘制（本判决页打印出来 12 页，且 transcript 容器在打印时被展开为全量内容）。加重因素两个，都在本轮去掉：
- **盒阴影**：我上一轮为保真把 `box-shadow: none` 删了，阴影是逐元素光栅化的主要开销 → 本轮加回。
- **纸张纹理**：`.paper-overlay::before` 是一整页 `repeating-linear-gradient`，打印时会整页反复光栅化 → 本轮 print 下 `display: none`。
- 另外把 `print-color-adjust: exact` 从 `*` 收窄到 `html, body`（该属性**可继承**，无需逐元素铺开）。

**分页控制**（用户问「方便做吗」→ 简单，一并做了）：判决页每个 box 本来就包在 `<section>` 里（10 处），所以主力规则是现成的；本轮补齐标准配套：
```css
section { break-inside: avoid; }              /* box 整块不劈开 */
h1..h6  { break-after: avoid; }               /* 标题不留孤行在页尾 */
p, li, blockquote { orphans: 2; widows: 2; }  /* 正文不留单行孤悬 */
img, table, pre { break-inside: avoid; }
```

**注意（预期行为）**：`break-inside: avoid` 只在「块本身装得下一整页」时生效；若某个 box 高于一整页，浏览器仍会拆开它 —— 这是规范行为，不是 bug。

**验证**：`tsc --noEmit` 干净；括号配平 142/142；前端 HMR 重编译通过。**PDF 观感与卡顿改善仍需用户实跑确认**。

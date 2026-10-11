# 阶段落地记录：2026-10-11 收口批次

| | |
|---|---|
| **文档类型** | 阶段落地记录（非 ADR；记录"这一批做了什么、为什么、验证证据、决策、遗留"） |
| **日期** | 2026-10-11 |
| **范围** | 承接 `docs/todo/deferred-items-2026-10-09.md` 的 M1–M7 + `docs/AGENT-GROUNDING-ANALYSIS.md` 的 §8/P1 + `docs/OPS-DASHBOARD-PLAN.md` 的 P1 |
| **提交** | 10 个本地 commit（**全部未 push** —— push main 会自动上线，用户决定本轮不部署） |
| **最终校验** | `go build ./... && go vet ./... && go test ./...` **22 包全绿**；dev compose 已停 |

---

## 0. TL;DR

清掉了 5 份进行中文档里登记的未完成项，**每个模块都按"单测 → dev compose 真实庭审 → IAB 浏览器 → 读真实 log"验证**。过程中还**撞出并修复了一个真 bug**（信念变更记录因非法 UTF-8 被静默丢弃），并**实测推翻了两个文档里长期成立的前提**（压缩"省 30%"、预算"够用"）——后者已按决策上调预算并登记为 §M8。

---

## 1. 落地清单（模块 → 提交 → 验证证据）

| 模块 | 提交 | 关键实测证据 |
|---|---|---|
| 基线提交（第 1–3 轮成果） | `0e324c1` `61c3016` `b32e91d` | 22 包全绿后落库；工作树清空，后续改动 diff 干净 |
| **A 发言步「全量带」** | `f9e4611` | `react_speak_stream` 的 `prompt_tokens` **~183 → 678–2321**；发言与判决引用证据具体数字（41% / 14 个月 / 82 万） |
| **hotfix `belief.truncateReason`** | `ea3217a` | 修复前 `belief_diffs` 整行被 Postgres 拒（`invalid byte sequence "UTF8"`，静默丢数据）；修复后同条件 **4 行成功落库**，reason 为合法中文 |
| **B 投影层真接线（M1+M3+M4）** | `c216545` | quick 庭审干净完成（75,267 token、零 ERROR/WARN），**判决由 LLM 正常生成（非 fallback）**，判决书页完整渲染 |
| **C 主张级接地检查 + 度量** | `680e3bf` | `/metrics?format=prometheus` 出现 `grounding_unsupported_claims_total{kind="number"} 1`；WARN 记录 `total_claims=3` |
| **D Ops 拨测** | `08c8657` | 三态实测：健康→OK/exit 0；停 backend→`BACKEND_DOWN`/exit 1；stub「200 但 configured:false」→`LLM_NOT_CONFIGURED`/exit 1 |
| E/F 文档登记 + 状态同步 | `4e733c0` | 新增 §M8；6 份文档过时状态表更正 |
| **M8-1 预算默认 200k→300k** | 见 §4 | 两处常量 + `.env.example` + 4 条测试同步；跨包一致性护栏通过 |

---

## 2. 实测撞出的两个前提反转（已登记 §M8）

### 2.1 压缩在真实庭审里省 0.0%（**确认是设计问题**）

**实测**：一场 standard 3 轮庭审里压缩被真正调用 **7 次**，每次 `before_count == after_count`
且 `before_length == after_length`（例：`msgs 5->5 chars 22815->22815`）。

**根因（读代码确认）**：
1. `CompressScored`（`agent_gateway/prompt_compressor.go:220`）**只对非 system 消息**做
   评分/原子组/贪心打包；**system 消息始终强制保留**。
2. 本项目 `react_runner.buildInitialMessages` 把 baseRules + 工具说明 + 证据 + 庭审历史
   **全部塞进单条 system 消息**，整个 messages 数组常常"只有这一条 system + 少量 ReAct 尾巴"。
3. 于是可压缩面极小 → 真实输入上 ≈0%。
4. system 被豁免**是 v2.10 的正确修复**：早先 system 共用 3000 字节上限，实测
   `12304→1500`（销毁 87% 指令），模型因此编造证据被反幻觉打回（ADR 0015/0021）。
   之后给 system 单独设 24000 字节宽松上限（`compressMaxSystemMsgLen`，仅对长期庭审兜底）。

**结论**：用户 2026-10-11 的判断——"**系统提示词的压缩应放最低优先级**"——与代码现状一致，
且是**正确的安全选择**（压 system 会破坏指令/输出契约）。代价是：**压缩器在本项目的 prompt
架构下结构性无法省 token**，PRD/tech-spec 宣称的「≥30%」不成立（fixture 值 34.1% 无生产对应物）。

**要真降 token 的正确位置不是压缩器，而是"素材组装层"**（庭审历史只带最近 N 轮、
证据按需检索），属新功能、另立项；压缩器维持"低优先级、只对对话尾巴生效"的定位。

### 2.2 默认 200k 预算对 standard 庭审不够 → 判决 fallback

**实测**：同场庭审 **209,018 token（ratio 1.05）** → `GenerateVerdict` 被
`reject-when-exhausted` 拦下 → 判决降级 fallback（判决书里用户可见"LLM 生成失败"）。
既有同类庭审 `079793a2` 已到 **197,788（98.9%）**。占比：`react_think` 77%、
`react_speak_stream` 仅 5.6%（Module A 的增量来源，是"压线最后一根稻草"而非主因）。

**处置**：默认预算 200000 → **300000**（见 §4）。

---

## 3. 用户决策记录（2026-10-11）

| 议题 | 决策 |
|---|---|
| push / 部署 | **只本地提交，不 push**（push main = 自动 Test→Deploy 上线生产） |
| 发言步全量带 | 做（Module A） |
| M1 投影层 | **接线**（非降级注释、非删除） |
| M3 `Beliefs` | **真填充**，范围限"只填本方信念"（最小行为变更） |
| M4 孤儿 API | **删除** |
| Ops P1 | **只做拨测**，不接通知渠道 |
| Phase 3 预留式预算 | **不做**（保持事后记账） |
| **M8-1 预算** | **上调到 300k** |
| **M8-2 压缩** | 不本轮修；**system prompt 压缩放最低优先级**；压缩宣称改正 |
| **P2 可见面** | **先出方案文档**（见 `docs/P2-VISIBILITY-PROPOSAL.md`） |

---

## 4. 本批代码/配置变更明细

### 4.1 预算默认值 200k → 300k（M8-1）

| 文件 | 变更 |
|---|---|
| `backend/internal/config/config.go` | `DefaultBudgetPerSession = 300000`（含依据注释） |
| `backend/internal/agent_gateway/gateway_config.go` | `DefaultBudgetPerSession = 300000`（跨包同值） |
| `.env.example` | `AGENT_GATEWAY_BUDGET_PER_SESSION=300000` + 依据注释 |
| 测试（4 处） | `config/gateway_children_default_test.go`、`agent_gateway/gateway_config_test.go`、`agent_gateway/token_budget_test.go`、`agent_gateway/token_budget_memstore_test.go`、`cmd/server/budget_default_guard_test.go`（跨包一致性护栏） |

> ⚠️ 服务器 `.env` 若显式写了 `AGENT_GATEWAY_BUDGET_PER_SESSION`，**上调默认值不会覆盖它** ——
> 上线时需手工同步（`.env` 属敏感文件，Agent 不代改，见 AGENTS.md §8）。

### 4.2 新增文件

| 文件 | 作用 |
|---|---|
| `backend/internal/agent/grounding_check.go` | 主张级接地检查（抽取 + 判定 + 素材拼装） |
| `backend/internal/agent/grounding_check_test.go` | 6 项单测（含跨边界数字不得误判） |
| `backend/internal/courtroom/grounding_metric_test.go` | 接地 metric 分桶断言 |
| `backend/internal/agent/react_runner_speak_materials_test.go` | 发言步素材注入式回归 |
| `scripts/probe-health.sh` | 存活/就绪拨测（校验 body，不接通知渠道） |
| `docs/P2-VISIBILITY-PROPOSAL.md` | P2 可见面方案（见 §5） |
| `docs/STAGE-LANDING-2026-10-11.md` | 本文件 |

---

## 5. 遗留与下一步

| 项 | 状态 | 说明 |
|---|---|---|
| **P2 可见面** | 方案待评审 | [`docs/P2-VISIBILITY-PROPOSAL.md`](P2-VISIBILITY-PROPOSAL.md) |
| **P3 措辞** | 未做 | 待 P2 之后再评估（§7.6 纪律：前三层修好再动措辞） |
| **M8-2 压缩** | 不本轮修 | 设计定位已澄清（§2.1）；若要在素材组装层真降 token，另立项 |
| **主张级检查覆盖面** | 部分 | 目前只覆盖数字/编号类主张；因果句/实体类主张尚未抽取 |
| **生产部署** | 未做 | 用户决定不 push。上线前置见 `docs/deployment/SECOND-LAUNCH-2026-10-09.md` 文末 |
| **生产 `private_memories` 表** | 未 DROP | 必须**先部署新代码再 DROP**（顺序反了线上每次发言写库报错，§M2） |

---

## 6. 关联文档

- [`docs/todo/deferred-items-2026-10-09.md`](todo/deferred-items-2026-10-09.md) —— M1–M7 + **§M8**（本批的缺口账）
- [`docs/AGENT-GROUNDING-ANALYSIS.md`](AGENT-GROUNDING-ANALYSIS.md) —— 接地三层诊断 + §8/§9 实施记录
- [`docs/AGENT-GATEWAY-BUDGET-PLAN.md`](AGENT-GATEWAY-BUDGET-PLAN.md) —— 预算维度计划（Phase 3 用户决定不做）
- [`docs/OPS-DASHBOARD-PLAN.md`](OPS-DASHBOARD-PLAN.md) —— 可观测呈现层 + 拨测
- [`docs/adr/0006-smart-prompt-compression.md`](adr/0006-smart-prompt-compression.md) —— 压缩策略（本批补"真实输入省 0%"状态更新）
- [`AGENTS.md`](../AGENTS.md) §6.2f —— 给后续 Agent 的批次纪要（含"push=上线"耦合）

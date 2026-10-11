# Agent Gateway 预算计算完善计划

| | |
|---|---|
| **文档编号** | （计划，非 ADR；落地后按需转为 ADR） |
| **标题** | Agent Gateway Token 预算计算：现状盘点 + 分阶段完善计划 |
| **状态** | ✅ **Phase 0 / 1 / 2 已落地（2026-10-10 第 3 轮）**：预算水位指标 + 成本维度接线（定价配置化）+ 流式 usage 取自末帧真实值。**Phase 3（预留式预算）用户 2026-10-11 决定不做**（保持事后记账，避免过拒）；Phase 4（多维度/持久化）触发式未做。实施记录见文末。<br>🔴 **2026-10-11 实测新增**：standard 3 轮庭审实测 **209,018 token > 200k 预算** → 判决被拒走 fallback（默认已上调 300k）；压缩在同日真实庭审里省幅 **0%～22%**（省幅主要来自 system 兜底上限）。详见 [deferred-items-2026-10-09 §M8](todo/deferred-items-2026-10-09.md) 与 [COMPRESSION-REDESIGN-PROPOSAL](COMPRESSION-REDESIGN-PROPOSAL.md)。 |
| **作者** | Exist + ZCode Agent |
| **拟定日期** | 2026-10-09 |
| **触发** | 2026-10-09 代码审查（对照代码而非文档）发现"成本维度预留但未接线"等 5 处缺口 |
| **影响范围** | `backend/internal/agent_gateway/`（记账与判定）、`backend/internal/config/`（配置/定价）、`backend/internal/observability/`（指标）、`backend/internal/model/`（llm_calls 已有 cost 列） |
| **关联** | ADR 0007（reject-when-exhausted）、ADR 0044（压缩策略）、`docs/OBSERVABILITY.md` §8 |

---

## 0. 结论（TL;DR）

**Token 维度是健全的，成本维度是空的，流式记账偏低，且没有任何预留机制。**

- ✅ 健全：输入/输出 token 分开记（来自 provider 真实 usage）、累计 + 5 分钟滑动双窗口、70/80/100 三档、预警 fire-on-upgrade。
- ❌ 不健全：`cost_usd` 字段与 `limitCostUSD` 上限存在，但网关**从不赋值**、判定逻辑**从不看它** → 生产恒为 0，成本上限永不触发；且全仓无定价表。
- ⚠️ 偏低：流式调用按"输出字符数 ÷ 4"估算，且**完全不记输入**（输入通常是 token 大头）。
- ⚠️ 事后记账：检查在调用前、累加在调用后，单次调用可越过阈值（比例可 > 1.0），无预留/预扣。
- ⚠️ 单维度单进程：仅 per-session；索引在内存、重启清零、多副本各记一份。

本计划分 4 个阶段补齐，P0（事实可观测）与 P1（成本接线）是"把已承诺但没兑现的能力补齐"，P2/P3 是增强。

---

## 1. 背景与触发

Agent Gateway 的预算能力经历了 v0.5（累计 token）→ v2（多维 + 滑动窗口 + 成本字段 + reject-when-exhausted）两轮扩展。但 2026-10-09 的逐行代码核对显示：**v2 引入的部分维度只建了"数据管道"，没建"数据来源"和"消费方"**。这与项目反复出现的"配了但没生效 / 开了但拿不到"是同一类问题（参见 `docs/OBSERVABILITY.md` §8 与 `docs/deployment/SECOND-LAUNCH-2026-10-09.md` 的三条规则）。

排查方式：不看文档，直接读 `backend/internal/agent_gateway/` 全部实现 + 装配层 + 指标常量定义。

---

## 2. 现状盘点（逐项 + 代码证据）

| 维度 | 现状 | 证据（代码位置） | 判定 |
|---|---|---|---|
| 输入 / 输出 token | 来自 provider 返回的真实 usage，分开累加 | `gateway.go` Complete 记账传 `InputTokens/OutputTokens` | ✅ |
| 成本 `cost_usd` | 字段 + 上限都存在，MemStore 也累加 `CostUSD`，但**网关从不传值** | `gateway.go` 记账处只传 Input/Output，无 CostUSD；`token_budget_store.go:31` 明写 "0 if price unknown" | ❌ 恒为 0 |
| 成本参与判定 | `classify` 只用 token ratio | `token_budget_memstore.go:169` 调 `classify(totalRatio, slidingRatio)`；`:192` 函数体不含 cost | ❌ 成本上限永不触发 |
| 定价表 | 全仓无 price / pricing 表或常量 | `grep -rn "price\|pricing" backend/internal` 仅命中一行注释 | ❌ 无成本来源 |
| 流式记账 | 按 `len(content)/4` 估，**只记输出** | `gateway.go` StreamComplete 尾部估算 + `AddUsage{OutputTokens: approx}` | ⚠️ 系统性低估 |
| 预留 / 预扣 | 无；检查在调用前、累加在调用后 | Complete 步骤 1（check）与步骤 5（AddUsage）分离 | ⚠️ 只能事后发现 |
| 维度 | 仅 per-session | `MemStore.sessions map[sessionUUID]` | ⚠️ 无 user/tenant/global |
| 存储 | 内存、单进程、重启清零 | `MemStore`；`RedisURL` 配置标注"当前 v1.x 不消费" | ⚠️ 多副本不共享 |
| 双窗口 | 累计 + 5 分钟滑动，取较严 | `token_budget_memstore.go` Check + `classify` | ✅ |
| 预警 | fire-on-upgrade，等级升高才广播 | `token_budget.go` maybeFireWarning + `warningSeverity` | ✅ |
| 缓存命中记账 | 命中在 AddUsage 之前 return，**不消耗配额** | `gateway.go` 步骤 0 vs 步骤 5 | ✅（刻意） |
| 预算指标 | `MetricBudgetRatio` 定义但**全仓无 SetGauge 调用点** | `observability/metrics.go:59` 定义；`grep` 无使用 | ❌ 死指标 |
| 降档指标 | reject / warning 已接线 | `gateway.go` MetricBudgetRejectedTotal；`token_budget.go` MetricBudgetWarningTotal | ✅ |
| 上限默认值 | 200000（v2.11 从 20000 抬高，实测驱动） | `config/config.go:83` `DefaultBudgetPerSession = 200000` | ✅（已修正） |
| 终态回收 | 判决落库时 `Release(sessionUUID)` 清预算+缓存 | `gateway.go` Release；`courtroom/service.go:1884` 调用点 | ✅（v2.11 已修） |

---

## 3. 目标与非目标

### 3.1 目标

1. **让"花了多少钱"可算**：接入模型定价，`cost_usd` 有真实值，成本维度可参与档位判定。
2. **让"预算水位"可观测**：预算 ratio 变成真正的 metric（当前是死指标），可在 `/metrics` 看到。
3. **让流式不再低估**：尽可能用真实 usage，退而求其次用合理的 token 估算，并补记输入。
4. **（增强）把"事后发现"变成"事前约束"**：可选引入预留/预扣机制。

### 3.2 非目标（明确不做，避免范围蔓延）

- 多租户计费 / 账单系统（商业化阶段再谈）。
- 跨副本强一致预算（需要 Redis，按 DAU 阈值另起决策）。
- 把预算做成"硬性成本闸门"（本次只到"可算 + 可观测 + 可预判"）。
- 改动 `llm_calls` / `decision_events` 表结构（`cost_usd` 列已存在，见 `model/db.go:291`）。

---

## 4. 分阶段实施计划

> 阶段编号不代表严格串行；P0/P1 可并行，P2 依赖 P1 的定价数据。

### Phase 0 — 补齐可观测性（让"事实"可测）

**问题**：现在无法从 `/metrics` 看到任何"预算水位"，`MetricBudgetRatio` 是死指标。

**动作**：
1. 在 `TokenBudget.Check` 或网关读取快照处，`SetGauge(MetricBudgetRatio, {session?}, snap.Ratio)`。
   - ⚠️ 注意基数：**不要**用 session_uuid 作 label（会话数无上界，会打爆指标基数）。用无 label 的 gauge（看全局水位分布）或按"最高水位档"聚合。
2. 复查 `MetricBudgetRatio` 命名语义，若确定无法低基数使用，则改为"每次 Check 上报当前 ratio 的 histogram"或直接删除该死指标（宁可删掉，不留"定义了没人写"的名）。
3. 文件日志 `BudgetCostUSD` 字段在 P1 接线前，先补注释说明当前恒 0 的语义，避免读日志者误判。

**验收**：`curl /metrics` 能看到预算水位指标（`AGENTS.md` §11.5 的 docker 业务验证最小集）。

**工作量**：0.5 天。

---

### Phase 1 — 成本维度接线（定价表 + 算 cost + 参与判定）

**问题**：`cost_usd` 恒为 0，成本上限永不触发，因为没有"单价"这个数据源。

**动作**：
1. **新增模型定价表**（数据源，三选一，倾向 a）：
   - (a) `backend/prompts/` 同级放一份 `pricing.yaml`（或 `config` 常量），按 model 名 → `{input_per_1m, output_per_1m}`；缺失时按"未知价"处理（cost 记 0 + 一次性 WARN）。
   - (b) env 变量（适合极少数模型，扩展性差）。
   - (c) 远端拉取（过度工程，不做）。
2. **网关记账时计算 `CostUSD`**：`usage.PromptTokens/1e6 * input_price + usage.CompletionTokens/1e6 * output_price`，传入 `AddUsage`。
   - 归口在 `agent_gateway` 内部（不散到业务层），保持"预算只在这一个包算"。
3. **判定逻辑纳入成本**：`classify` 增加 `costRatio`（`costUSD / limitCostUSD`，limit 为 0 时跳过），与 total/sliding 取最严。
   - 语义确认：成本档位是否与 token 档位共用 70/80/100 阈值？（默认共用；若同向则无需新增状态机）
4. **配置项**：`AGENT_GATEWAY_BUDGET_COST_LIMIT_USD`（0 = 不限），并在 `buildGatewayConfig` 映射（受 `gateway_config_mapping_test.go` 反射护栏覆盖，漏搬即测试失败）。
5. **模型价格变更的版本化**：pricing.yaml 与 `promptlab` 一样记 content hash，或至少在文件日志里记"用的哪版价格"。

**验收**：
- 单测：给定 usage + 定价 → `cost_usd` 期望值；limit 触发档位切换。
- 网关 `Release` / 多 session 隔离不退化。
- 文件日志 `budget_cost_usd` 出现非 0 值。

**工作量**：1-2 天。

---

### Phase 2 — 流式记账修正

**问题**：流式按"字符 ÷ 4"估，且**不记输入** → 低估。

**动作**：
1. **优先用真实 usage**：检查 `llm` 包流式 chunk 是否能在末尾携带 usage（OpenAI 兼容协议通常最后一个 chunk 带 `usage`）。若 provider 支持，直接采用，彻底替换估算。
2. **无法拿到时的估算改进**：
   - 复用 `prompt_greedy.go` 的 `EstimateTokens`（CJK 感知：中文约 1.5 token/字、ASCII 约 0.25），替换粗糙的 `/4`。
   - **补记输入**：流式的 system prompt + messages 也估算 input token 并记账（现在完全没记）。
3. **失败路径也要记**：流式中途断开时，已产生的输出应计入（当前 firstErr 路径仍会记，需确认估算口径一致）。

**验收**：
- 单测：流式 mock 产出 N 段内容 → 记账 token 在合理区间（与真实 usage 偏差 < 30%）。
- docker 业务验证：跑一场真实庭审，流式调用的 `budget_sliding_tokens` 与 input 记入后不再显著偏低。

**工作量**：1-2 天（取决于 provider 是否给流式 usage）。

---

### Phase 3 —（增强，可选）预留式预算

**问题**：现在是"事后记账"，单次调用可越过阈值（ratio > 1.0）。

**动作（较大改动，需先讨论）**：
1. 调用前估算 `input + max_tokens` 作为 **reservation**，预扣到 session；
2. 调用后按真实 usage **校正**（补记差额或退还多扣）；
3. 拒绝条件从"已超"变为"已用 + 预留 > limit"。

**风险**：估算不准会导致误拒（把还能跑的调用拒掉）或漏扣。**建议只在明确需要"硬成本闸门"时再做**，且要有开关。

**验收**：chaos 测试——估算偏差下不误拒正常庭审、能拦住超额调用。

**工作量**：3-5 天（含回退）。

---

### Phase 4 —（按需触发）维度扩展与持久化

**触发条件**（与既有 roadmap 一致，不主动做）：
- 出现"用户级 / 租户级预算"需求，或商业化启动 → 增加 user/tenant 维度。
- 多副本部署（DAU 阈值）→ `BudgetStore` 换 Redis 实现（接口已就绪，`token_budget_store.go` 已抽象）。

---

## 5. 每阶段验收标准（Definition of Done）

遵循 `AGENTS.md` §3（测试同步）与 §11（docker 业务验证）：

1. **单元测试**：新增/修改逻辑必须有 `*_test.go` 覆盖，不简化既有测试。
2. **配置映射**：任何新增 `GatewayConfig` 字段必须同步 `buildGatewayConfig` + `config` 层，并跑 `TestBuildGatewayConfig_MapsEveryField`（反射护栏）。
3. **指标可观测**：新增/启用指标必须在 `/metrics` 能看到非 0 / 有效值。
4. **docker 业务验证**：涉及 config 默认值、/metrics、记账路径的改动，按 §11.5 跑 dev compose 最小集（`AGENT_GATEWAY_*` 默认值改动属于 §11.2 触发条件）。
5. **文档同步**：见 §7。

---

## 6. 风险与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| 定价表配错 → cost 偏高/偏低 | 档位误判 | 未知价记 0 + WARN；pricing 版本化；上线前用已知 token 数手工核对 |
| 成本纳入判定导致误触发压缩/限流 | 输出质量下降 | 成本档位默认与 token 档位同向；先只"记录不判定"观察一轮（影子模式） |
| 流式估算仍不准 | 预算偏差 | 优先真实 usage；估算偏差写成 metric 观测 |
| 预留机制误拒 | 正常庭审被拒 | 默认关闭，opt-in；chaos 验证 |
| metric label 基数爆炸 | /metrics 内存涨 | 禁止用 session_uuid 作 label（Phase 0 明确） |

**回滚**：所有新能力走 `GatewayConfig` 子开关，默认保持现状行为（成本档位可关、预留可关），出问题一键回退到"仅 token 维度"。

---

## 7. 文档同步清单

落地任一阶段后必须同步：

- [ ] 本文件状态更新（📋 Proposed → ✅ 已落地 / 部分落地）
- [ ] `docs/decisioncourt-tech-spec.md`（Agent Gateway 章节：预算维度、成本维度）
- [ ] `docs/decisioncourt-db-design.md`（若 `llm_calls.cost_usd` 语义/来源变化）
- [ ] `docs/OBSERVABILITY.md` §8（数据可得性：新增了哪些可观测数据）
- [ ] `docs/README.md` §5（模块状态表：预算维度标 ✅）
- [ ] 视改动大小决定是否新增 ADR（Phase 1/3 建议各一份）

---

## 8. 开放问题（需用户确认）

1. **成本档位是否与 token 档位共用 70/80/100 阈值？** 还是成本独立一套阈值？
2. **定价数据放代码库（yaml）还是 env？** 涉及"价格是否需要进镜像 / 是否算敏感"。
3. **是否真的需要 Phase 3（预扣/硬闸门）？** 当前是个人项目单人使用，"事后可见"可能已足够；预扣会带来误拒风险。
4. **`MetricBudgetRatio` 是补齐还是删除？** 若无法低基数使用，删除比留死指标干净。
5. **流式 usage 支持度**：需实测 DeepSeek 流式响应是否在末尾 chunk 返回 usage（决定 Phase 2 是"取真值"还是"改估算"）。

---

## 附：排查方法备忘（供后续复用）

本次盘点的做法值得固化：**不看文档，逐行读实现 + 反查调用点 + 反查指标常量是否有写入方**。

三个专门的"半成品信号"检查：
1. **字段有声明，但 `grep` 不到赋值点** → 死维度（如 `CostUSD`）。
2. **指标常量有定义，但 `grep` 不到 `IncCounter/SetGauge` 调用** → 死指标（如 `MetricBudgetRatio`）。
3. **方法有实现，但 `grep` 不到包外调用点** → 死代码 / 未接线（历史上 `Reset` / `EvictSession` 就是这样，已由 `Release` 修复）。

---

## 实施记录（2026-10-10，第 3 轮）

### Phase 0 — 预算水位可观测 ✅

- 新增 `MemStore.MaxRatio()` / `UsedTokens()`（只读聚合，**不加 session_uuid label** —— 会话数无上界会打爆指标基数）。
- `TokenBudget.MaxRatio()/UsedTokens()` 用可选接口探测，store 不支持时返回 `false`，上层**不上报**而不是报假值。
- `Gateway.reportBudgetMetrics()` 在每次 `AddUsage` 后写 `budget_ratio`（gauge，取最高会话水位）与 `llm_budget_used_tokens`（gauge）。
- **实测**：一场庭审中 `/metrics` 出现 `budget_ratio=0.2076`、`llm_budget_used_tokens=41524`（此前两者在 `/metrics` 上完全不存在）。

### Phase 1 — 成本维度接线 ✅

- 新增 `agent_gateway/pricing.go`：`ModelPricing` + `PricingTable`（未知模型 → cost 0 + **一次性 WARN**）。
- **定价不硬编码**：走 `AGENT_GATEWAY_PRICING_JSON`（`config.AgentGatewayConfig.PricingJSON` → `GatewayConfig.PricingJSON`），在 `NewWithConfig` 解析成表；非法 JSON → ERROR 日志 + cost 记 0（不 fail-fast）。
  选 string 而非 map：`cmd/server` 的映射反射护栏（填 `"x"` 断言非零）要继续成立，且 config 层不能反向 import 本包。
- `classify(totalR, slidingR, costR)` 纳入成本占比（与 token 共用 70/80/100 阈值，取较严）；`limitCost==0` 时成本维度**完全不参与**（行为与接线前一致）。
- `AGENT_GATEWAY_BUDGET_COST_LIMIT_USD`（0 = 不限）已映射 + `.env.example` 文档化。
- **实测**：注入测试价（`input_per_1m=0.27/output_per_1m=1.10`）后 FileLogger 的 `budget_cost_usd` 从恒 0 变为递增 `0.0069 → 0.0124`；`snap.Ratio` 会取成本口径。

### Phase 2 — 流式记账 ✅（含一处**口径纠错**）

- `llm/client.go` 流式请求补 `stream_options.include_usage=true`，并在 `StreamChunk` 上新增 `Usage *Usage`，把**末帧** usage（`choices` 非空但 content 为空的那一帧，紧邻 `[DONE]`）透出。
- 网关流式出口**优先用真实 usage**（input+output 拆分）；拿不到才退回估算，且**补记输入**、并把估算尺子从 `len/4` 换成 CJK 感知的 `EstimateTokens`（消除"两把尺子"）；metric label 区分 `input`/`input_stream_estimated`；`llm_calls` 的 `prompt_tokens/completion_tokens` 不再恒 0。
- **实测（直接探针 + 真实庭审）**：DeepSeek 支持 `include_usage`，末帧形如
  `…"choices":[{"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,…}`，紧邻 `data: [DONE]`。
  新代码下的 `react_speak_stream` 行 `prompt_tokens > 0`（183/196）。
- ⚠️ **口径纠错（重要）**：R18 的原始表述「流式**不记**输入」是**误诊**。真实情况是：`react_speak_stream` 的
  prompt **本来就只有 ~180 token** —— `streamSpeakContent` 故意用一个极小的 fresh prompt
  （"你是一名资深庭审律师…**重要：忽略之前的对话历史与任何系统协议**"），只把 `论点/立场/置信度` 喂进去。
  所以"speak 的 token 远小于 think（5467）"不是记账错误，而是**发言步根本不看案件素材**（详见
  `AGENT-GROUNDING-ANALYSIS.md` §8 新增发现）。

### 仍未做

- ~~Phase 3 预留式预算（事前预扣）~~ —— **用户 2026-10-11 决定不做**：单人项目"事后可见"足够，
  预留估算不准会误拒正常庭审（Phase 3 自身风险栏已记录）。
- Phase 4 多维度 / 持久化 / Redis（触发条件式：多副本 / 商业化）。
- 定价表的**版本化归因**（至少记"用的哪版价格"）——留给后续。
- Prometheus/Grafana 侧（呈现层）见 `OPS-DASHBOARD-PLAN.md`（已落地 + 拨测）。

### 🔴 2026-10-11 真实庭审实测：预算默认值偏紧（新发现）

standard 3 轮 + 1 条证据的庭审实测 **209,018 token > 默认 200,000** → `reject-when-exhausted`
拦下 GenerateVerdict，判决降级为 fallback；**既有同类庭审已到 197,788（98.9%）**，即默认预算早已贴线。
其中 `react_think` 占 77%，流式发言仅占 5.6%。完整数据与根因（含压缩省幅 0%～22% 的复核）见
[deferred-items-2026-10-09 §M8](todo/deferred-items-2026-10-09.md) 与
[COMPRESSION-REDESIGN-PROPOSAL](COMPRESSION-REDESIGN-PROPOSAL.md)。

**✅ 已处置（2026-10-11）**：默认值 **200000 → 300000**（`config.DefaultBudgetPerSession` +
`agent_gateway.DefaultBudgetPerSession` 两处常量 + `.env.example` + 相关测试）。⚠️ 服务器 `.env`
若显式写了 `AGENT_GATEWAY_BUDGET_PER_SESSION`，**默认值变更不会覆盖它**，上线时需手工同步
（`.env` 属敏感文件，Agent 不代改，AGENTS.md §8）。

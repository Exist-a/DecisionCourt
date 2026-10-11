# 可观测性「呈现层」建设计划（Ops Dashboard）

| | |
|---|---|
| **文档编号** | （计划，非 ADR；落地后按需转为 ADR） |
| **标题** | 白盒化呈现层：从"SSH 进容器翻日志"到"打开页面看数据" |
| **状态** | ✅ **P0/P2 已落地（2026-10-10 第 3 轮）**：`/metrics?format=prometheus` 文本导出、`/metrics` 访问控制（token/public）、Prometheus + Grafana **默认启动**（dev + prod 两份 compose，带 `mem_limit`）、Grafana 数据源与仪表盘自动 provisioning —— 全部**实测验证过**。**未做**：P1 拨测告警、告警规则。实施记录见 §10。 |
| **作者** | Exist + ZCode Agent |
| **拟定日期** | 2026-10-09 |
| **触发** | 用户 2026-10-09：「白盒化的功课做了不少，但没有可视化，部署上线后拿数据还要 SSH 进去，我觉得不太对」 |
| **影响范围** | `backend/internal/api/`（新增只读聚合端点）、`frontend/app/`（新增面板页）、`deploy/`（可选：拨测 / Prometheus） |
| **关联** | ADR 0010（白盒化三支柱）、`docs/roadmap/whitebox-roadmap.md`（Phase A-E）、`docs/OBSERVABILITY.md` §8、`docs/deployment/SECOND-LAUNCH-2026-10-09.md` |

---

## 0. 结论（TL;DR）

**采集（collection）做得够，呈现（presentation）是零，且没有远程访问路径。**

- ✅ 已有：`llm_calls`（token / 延迟 / 状态 / prompt 版本 / Agent 类型 / request_id）、`decision_events`（业务 span）、slog 结构化日志、JSON 文件日志、进程内 metrics。
- ❌ 缺呈现：`/metrics` 是 **JSON 端点 + 内存快照**——重启清零、无历史、无趋势、无告警，而且**公开**（不在鉴权组内）。
- ❌ 缺访问路径：看线上数据只能 SSH 进容器翻日志或手工连库跑 SQL。
- ⚠️ 决策已下但未落地：ADR 0010 当时决定上 **Prometheus + OpenTelemetry**，实际只落地了采集侧与自研内存 metrics；`whitebox-roadmap` 的 Phase C/D 被"日均 LLM > 100""多实例部署"这类**规模**触发条件卡住。

**核心判断失误**：那两个触发条件是按**规模**设的，但"能看趋势、不用 SSH"这个收益属于**可运维性**，**在 N=1 时就已经成立**。把可运维性绑在规模上，等于永远不触发。

**建议**：不做企业全套，做 right-sized 四阶段——P0 应用内只读面板（零新基建）→ P1 外部拨测告警 → P2 Prometheus/Alertmanager → P3（可永久不做）日志聚合。

---

## 1. 背景

项目的可观测性分两轮建设：

1. **v0.8 白盒化（ADR 0010，2026-07-02）**：确立三支柱（Metrics / Logging / Tracing），落地 `internal/observability/`、`decision_events` 表、slog 结构化日志、`llm_calls` 审计、trace_id 端到端串联。
2. **v2.3（ADR 0037，2026-09）**：给 `agent_gateway` 全面补 metrics 埋点（cache / breaker / compression / throttle / budget / retry）。

两轮都强化了**采集**，但从未建设**呈现**。结果是：数据躺在 Postgres 和磁盘里，只有作者知道怎么用 SSH + SQL 把它们捞出来。这与项目反复出现的"能力建好了、最后一公里没接"是同一类问题。

---

## 2. 现状盘点（逐项 + 证据）

| 项 | 现状 | 证据 | 判定 |
|---|---|---|---|
| LLM 调用审计 | 每次调用一行 `llm_calls`，含 token / 延迟 / 状态 / 错误 / model / agent_type / request_id / prompt_version | `backend/internal/model/db.go` LLMCall；`agent_gateway/recorder.go` | ✅ 采集足 |
| 业务事件 | `decision_events` 表（span / 状态机迁移 / 前端埋点），带 payload JSONB + duration + status | `backend/internal/model/decision_event.go` | ✅ 采集足 |
| 结构化日志 | slog 统一字段（trace_id / session_uuid） | `internal/observability/logger.go`、`middleware.go` | ✅ |
| 详细调用快照 | JSON Lines 文件日志（压缩/限流/预算细节），可按 trace 还原时序 | `agent_gateway/file_logger.go`、`internal/trace/` | ✅ |
| 指标 | 进程内 `observability.Metrics`（counters / gauges / histograms） | `internal/observability/metrics.go` | ⚠️ 仅内存 |
| **指标暴露** | `GET /metrics` 返回 **JSON 快照**，**内存态**、重启清零、无历史、**公开**（不在鉴权组） | `api/handler.go` RegisterMetricsRoute；`main.go`「/health / /metrics 也公开」 | ❌ 不可用于趋势/告警 |
| **呈现/看板** | **无**。前端只有首页 / 庭审页 / 判决页 | `frontend/app/` 路由清单 | ❌ |
| **远程访问** | 只能 SSH 进容器 `docker compose logs` / 手工连库跑 SQL | `docs/deployment/SECOND-LAUNCH-2026-10-09.md`；`AGENTS.md` §9 | ❌ |
| Prometheus / Grafana | 全仓无（仅出现在 roadmap 与 ADR 的计划文字里） | `grep` prometheus/grafana/loki/jaeger 只命中 docs | ❌ 未落地 |
| 告警 | 无 | — | ❌ |

**已具备的"地基"**（这让 P0 变得很便宜）：Caddy + HTTPS 反代、Postgres 里有全部结构化数据、指标接口本就是按"可替换 adapter"设计的（`metrics.go` 注释：「未来可替换为 Prometheus client_golang adapter」）。

---

## 3. 业界标准做法（企业一般怎么做）

**一句话原则**：**不是你去 SSH 把数据捞出来，而是数据被推出去、聚起来，你在看板上看，或者被告警叫醒。**

三支柱各自"采集 → 传输 → 呈现"，再加告警与响应层：

| 支柱 | 应用侧 | 传输 | 呈现 | 保留期 |
|---|---|---|---|---|
| **Metrics** | 暴露 Prometheus 文本格式 `/metrics` | Prometheus 定时抓取 | Grafana 面板 | 数周-数月 |
| **Logs** | 结构化 JSON | Promtail / Alloy / Fluent Bit / Vector 推送 | Loki 或 ELK / OpenSearch | 按策略 |
| **Traces** | OTel SDK 埋点 | OTLP 导出 | Jaeger / Tempo（瀑布图） | 数天-数周 |

**响应层**：SLO / 错误预算、Alertmanager 规则、告警分发到 Slack / PagerDuty / 邮件 / 电话、on-call 轮值。

**"只能 SSH"为什么是反模式**（代价具体化）：

1. **没有历史**：只能回答"现在是什么样"，答不了"是不是在变坏"。
2. **感知延迟**：你得先怀疑了才会去看——问题在你看之前一直静默。
3. **系统不能主动告诉你它坏了**：没有告警。
4. **知识锁死在一个人手里**："白盒"只对作者白。
5. **可用性本身有坑**：SSH 会被限流、会不可用（本项目自己的运维文档已记录）。

---

## 4. 本项目 right-sized 方案（分阶段）

**原则**：采纳"数据走 HTTP、有鉴权、能看趋势、能告警"的**原则**，工具按单人单机**最小化**。

### P0 — 应用内只读 Ops 面板（推荐先做，零新基建）

**做什么**：在**已有**的 Next.js 前端加一个 `/ops` 页面 + 后端一个只读聚合端点，从 Postgres 与内存 metrics 聚合出面板。

**为什么这样选**：复用现有 Caddy（HTTPS）、现有 auth 中间件、现有 Postgres——**不引入任何新容器/新端口/新鉴权体系**，部署即生效，暴露面小于本就公开的 `/metrics`。

**端点设计**（只读，一个响应喂满整页，避免首屏打 N 个请求）：

- `GET /api/v1/ops/overview?hours=24`（`hours` 1-168，默认 24）
- 返回：LLM 总览（调用数 / 错误数 / 错误率 / 输入输出 token / 平均与 P50 P95 延迟 / 去重会话数）、按 model 分组、按 agent_type 分组、**按 prompt_version 分组**（顺带验证 Prompt Lab 的版本归因真的通了）、小时级时间线、`decision_events` 统计、会话统计（全局 active + 窗口内新建）、最近错误列表（LEFT JOIN 拿可读 session_uuid）、以及进程内 metrics 快照。
- 挂 `authedGroup`（需登录）；`OPS_DASHBOARD_ENABLED=false` 可关闭（不注册路由 → 404）。

**面板内容**（首屏）：

1. 顶部指标卡：窗口内调用数 / 错误率 / 总 token / P95 延迟 / 当前活跃庭审数。
2. 时间线：调用数与错误数（小时粒度）。
3. 分维度表：按模型 / 按 Agent / 按 prompt 版本（调用数、token、错误、平均延迟）。
4. 最近错误列表（时间、模型、Agent、错误信息、会话）。
5. 进程态：cache 命中率、熔断状态、预算告警 / 拒绝计数、重试次数。

**技术约束**：Postgres 方言（`percentile_cont` / `date_trunc` / `FILTER`）——本项目本就 Postgres-only，不做方言抽象；数据源走可注入接口（生产 GORM，测试内存 fake），与既有 `sessionLookup` / `DecisionEventLister` 同模式。

**工作量**：1-2 天。

### P1 — 外部拨测 + 告警（"能主动叫你"的最便宜实现）

**做什么**：一个独立的 uptime 探针（如 Uptime Kuma，或一个定时打 `/health` + `/api/v1/health/llm` 的脚本），异常推送到 Telegram / 邮件。

**为什么**：P0 解决了"能看"，但**"能被告知"**才是告警的价值。这是 80/20 的一步——不需要 Alertmanager。

**工作量**：0.5 天。

### P2 — Prometheus 文本格式 + Prometheus + Alertmanager

**做什么**：
1. 给 `/metrics` 加 `?format=prometheus` 分支（接口本就预留了这个位置），或独立一个 exporter。
2. 部署 Prometheus 抓取 + Alertmanager。
3. 配告警规则：健康检查失败、LLM 错误率超阈值、熔断 open、预算耗尽拒绝、P95 延迟异常。
4. Grafana 面板（趋势 + 保留期）。

**为什么放在 P1 之后**：P2 解决的是"趋势 + 规则化告警"，比 P0/P1 重，且引入新容器。**如果 P0+P1 已够用，P2 可以不做**。

**工作量**：2-4 天（含部署与调规则）。

### P3 — 日志聚合（Loki / Alloy）——**可永久不做**

JSON 文件日志已经在磁盘上；只有当"跨机器/跨天检索日志"成为真实痛点时才做。单人单机**建议永久跳过**。

---

## 5. 明确「可以永久不做」的清单

避免范围蔓延，以下在本项目（单人、单机、无商业化）下**默认不做**：

- ❌ Jaeger / Tempo（分布式 tracing 瀑布图）——单进程无跨服务链路，收益为零。
- ❌ ELK / OpenSearch——比 Loki 重得多，无必要。
- ❌ 数据仓库 / BI 工具（Metabase 等）——数据量级不需要。
- ❌ 多租户 / 跨用户分析面板——无租户概念。
- ❌ SLO / 错误预算 / on-call——单人项目，告警到手机即可。

> 若未来商业化或上多副本，这些按 `whitebox-roadmap` 的 Phase D/E 重新评估。

---

## 6. 验收标准（Definition of Done）

遵循 `AGENTS.md` §3（测试同步）与 §11（docker 业务验证）：

1. **P0**：`/ops` 页面能打开并显示真实数据；端点有单测（窗口边界、参数非法、数据源错误、未启用降级 404、metrics 合并）；`/metrics` 与既有端点不退化。
2. **P0 业务验证**：按 §11.5 起 dev compose，浏览器真实打开 `/ops`，确认卡片/图表/表格渲染正常（不能只 curl 端点）。
3. **P1**：断网/停容器时能收到告警（真实触发一次，不是配置了就算）。
4. **P2**：Prometheus 能抓到指标；至少一条告警规则被真实触发过。
5. **文档同步**：见 §8。

---

## 7. 风险与回滚

| 风险 | 影响 | 缓解 |
|---|---|---|
| 聚合查询扫大表 | 面板慢 / DB 压力 | 时间窗上限 168h；`created_at` 有索引；必要时加分页/缓存 |
| 面板泄露敏感数据 | 信息暴露 | 数据为聚合指标（无用户正文）；挂 auth；`OPS_DASHBOARD_ENABLED=false` 可关 |
| 无 admin 角色，任何登录用户可见 | 越权看运维数据 | 单用户项目可接受；多用户前需引入 admin role 或 IP 白名单 |
| 新容器（P2）增加运维负担 | 部署复杂化 | P2 可选；先做 P0/P1 零新基建的部分 |
| 指标基数爆炸 | 内存涨 | 禁止用 session_uuid 作 label（写入计划纪律） |

**回滚**：P0 为纯新增（新端点 + 新页面），删除即回退；`OPS_DASHBOARD_ENABLED=false` 立即关闭路由。

---

## 8. 文档同步清单

落地后必须同步：

- [ ] 本文件状态（📋 Proposed → ✅ 已落地 / 部分落地）
- [ ] `docs/OBSERVABILITY.md` §8（数据可得性：从"要 SSH"改为"面板可见"）
- [ ] `docs/README.md` §5（模块状态表）
- [ ] `docs/decisioncourt-api-design.md`（新增 `/api/v1/ops/overview` 端点）
- [ ] `docs/deployment/SECOND-LAUNCH-2026-10-09.md`（运维流程：加"打开面板"路径）
- [ ] `AGENTS.md` §9（ECS 运维：SSH 从"唯一手段"降级为"兜底手段"）

---

## 9. 开放问题（需用户确认）

1. **面板放哪**：应用内 `/ops` 页（P0，零新基建）vs 直接上 Grafana（P2 一步到位）？本计划推荐前者。
2. **访问控制**：当前无 admin 角色——面板对"任何登录用户"可见是否可接受？需要 IP 白名单或独立口令吗？
3. **告警渠道**（P1）：Telegram / 邮件 / 企业微信 / 钉钉？
4. **是否要 P2**：如果 P0+P1 已覆盖需求，Prometheus/Alertmanager 是否永久不做？
5. **是否需要历史保留**：面板默认只看窗口内（无长期留存）——需要长期趋势吗？（需要则直接跳到 P2）

---

## 附：判断这类"半成品"的通用检查法

本次盘点复用了排查预算问题时的三信号检查，同样适用于可观测性：

1. **能力有实现，但 `grep` 不到消费方** → 未接线（本例：metrics 有采集，无呈现）。
2. **决策文档写了目标，但代码里只有一半** → 决策未落地（本例：ADR 0010 的 Prometheus/OTel 只落了采集）。
3. **roadmap 的触发条件用的是"规模"而非"收益"** → 大概率永不触发（本例：Phase C/D）。

---

## 10. 实施记录（2026-10-10，第 3 轮）

### 10.1 已落地

| 项 | 内容 |
|---|---|
| Prometheus 文本导出（P2 的第一步） | `GET /metrics?format=prometheus` → `text/plain; version=0.0.4`（`internal/observability/prometheus.go::RenderPrometheus`）。默认仍返回 JSON（`?format=prometheus` 才切）。histogram 桶是 `le` 累积语义、末尾补 `+Inf`，label 按规范转义；有 3 条渲染护栏测试（形状/空快照/转义）。 |
| Prometheus + Grafana 编排 | 加进 `docker-compose.dev.yml` **与 `docker-compose.yml`（生产）**，**默认启动** + `deploy/ops/prometheus.{dev,prod}.yml`（dev 15s / prod 30s 抓取 `backend:8080/metrics?format=prometheus`）。两者都设了 `mem_limit`，并收紧 Prometheus 保留策略（15d / 512MB）。 |

启动方式：直接 `docker compose -f docker-compose.dev.yml -f docker-compose.override.yml up -d`（**无需任何 profile 参数**，观测栈默认就在里面）。
→ 面板 <http://localhost:3001>（admin / `${GF_ADMIN_PASSWORD:-admin}`）、抓取 <http://localhost:9090>。

### 10.2 实测内存（默认启动的依据，不是估计）

**生产机现状（2026-10-10 SSH 实测，2C2G 香港机）**：

| 项 | 实测 |
|---|---|
| RAM | 总 **1613 MB**（"2G"机实际可用），已用 674 MB，**可用 938 MB** |
| Swap | 2047 MB，仅用 5.8 MB（换页压力小） |
| 应用栈 5 容器 | frontend 56 + caddy 42 + postgres 39 + backend 12 + redis 5 = **≈155 MB** |
| CPU / 磁盘 | 2 vCPU；/ 40G 用 7.2G（20%） |

**Prometheus + Grafana 自身（本机 Linux 容器实测，抓取稳定后）**：

| 容器 | 实测 RSS | 设定上限 |
|---|---|---|
| `dc_dev_prometheus` | **103 MB**（35 条 series） | 384m |
| `dc_dev_grafana` | **194 MB**（峰值 222 MB） | 320m |

**结论**：观测栈 ≈ **300–400 MB**，对着生产 **938 MB 可用** → **能放得下**，但不宽裕（剩下 ~540 MB 要给应用的庭审峰值）。因此：
1. 两个容器**必须**带 `mem_limit`（已设，防观测栈把应用挤到 OOM）；
2. Grafana 是两者里更重的（~200 MB，本机曾贴近 320m 上限）→ 上线时给它 **384m** 而不是 320m；
3. 若想更省，可以**只在 ECS 上跑 Prometheus**（~103 MB），Grafana 放本地/别处按需连——P2 里 Prometheus 是数据面，Grafana 只是画图。

### 10.3 验证（可复现）

- `curl localhost:8180/metrics?format=prometheus` → `content-type: text/plain; version=0.0.4`，`# TYPE` 行 + 桶/`+Inf`/`_sum`/`_count` 齐全。
- Prometheus `/api/v1/targets` → `health=up`（历史上第一次抓取曾报
  `unsupported Content-Type "application/json"` —— 因为当时后端还是旧二进制，重启后即好；**这条值得记住：Prometheus 抓 JSON 会直接判 down**）。
- `curl localhost:9090/api/v1/query?query=up` → `1`；`…query=scrape_samples_scraped` → **35**（本项目指标是低基数，所以 Prometheus 内存才这么小）。
- Grafana `/api/health` → 200。

### 10.4 仍未做

- **P1 外部拨测/告警**（uptime 探针 + 通知渠道）。
- **告警规则**（Alertmanager / Grafana alerting 规则文件）—— 现在只有面板，没有"主动叫你"。
- **P3 日志聚合** —— 建议永久不做。
- ⏸️ **部署暂缓（用户 2026-10-10 决定）**：以上全部**已在本地跑通并实测**，但**未提交、未部署**。
  下次部署的前置（服务器 `.env` 补 `GF_ADMIN_PASSWORD` + `OPS_METRICS_TOKEN`）见
  [`deployment/SECOND-LAUNCH-2026-10-09.md`](deployment/SECOND-LAUNCH-2026-10-09.md) 文末附录。

> ✅ 已补齐（本轮后半段）：**`/metrics` 访问控制**（见 §10.5）、**生产 compose 的 ops profile**、**Grafana 仪表盘 JSON + 数据源自动 provisioning**。

---

## 10.5 `/metrics` 访问控制（2026-10-10 第 3 轮后半段）

**问题**：`/metrics` 一直注册在根引擎上（完全公开），任何人都能读到调用量/错误率/预算水位。但它又不能简单塞进 `auth` 组——消费方 Prometheus **不带用户 JWT**，塞进去 scraper 永远 401。

**做法**（`middleware.MetricsAccess`，两条独立通道）：

| 通道 | 条件 | 用途 |
|---|---|---|
| `OPS_METRICS_PUBLIC=true` | 无条件放行 | dev（`docker-compose.dev.yml` 已显式开）/ 外部拨测 |
| `OPS_METRICS_TOKEN=<t>` | `Authorization: Bearer <t>` 或 `X-Metrics-Token: <t>`（常数时间比较） | Prometheus scraper |
| 都没有 | **401** + 可操作提示 | 默认（收紧） |

默认收紧（`OPS_METRICS_PUBLIC=false`、token 空）；此时启动会打 **WARN**，避免"配完发现监控是空的、还没人告诉你"这种静默锁死。
生产 `docker-compose.yml` 里，Prometheus 用 `authorization.credentials: ${OPS_METRICS_TOKEN}`（**环境变量替换，不用文件**——文件不存在会让 Prometheus 起不来，而"观测栈起不来"不该拖死整份 compose；token 没配只是抓取失败，可见）。

**验收（实跑）**：无凭证 **401**；`Bearer testtoken123` **200**；`X-Metrics-Token: testtoken123` **200**；错 token **401**；401 响应体带可操作提示。
单测 5 项（`internal/middleware/metrics_access_test.go`）覆盖四态 + token/public 共存。AGENTS.md §11.5 的 `/metrics` 验证清单已同步。

### 10.6 仪表盘与二次核对（顺带又抓到一个死指标）

预置了 `deploy/ops/grafana/dashboards/decisioncourt-overview.json`（10 面板）+ 数据源 provisioning（uid 固定 `prometheus`，避免"数据源没选上"导致空面板）。

**实跑验收**：立案 → 开庭后，Prometheus 里出现 `llm_call_total`(4)/`llm_call_tokens_total`(4)/`budget_ratio`(1)/`llm_budget_used_tokens`(1)；仪表盘显示 **预算水位 16.2%**、**预算累计用量 32.4k**。

⚠️ **发现又一个死指标**：`courtroom_active_sessions`（`MetricActiveSessions`）**只有常量定义、没有任何 `SetGauge` 写入方** —— 与当初的 `MetricBudgetRatio` 同族。原面板"活跃庭审"挂它只会永远 `No data`。

**处置（用户 2026-10-10 选择"接线"）**：已接线 —— `Service.refreshActiveSessionsGauge(ctx)` 用 **DB 真值重算**（不是 +1/-1 增量），在 4 个生命周期点刷新：开庭（离开 idle）/ 判决落库 / 重开 / 启动恢复扫描完成。
- **口径**：`status='active' AND current_phase <> 'idle'` —— 刻意排除"立案后没开庭"的会话，否则 gauge 会随僵尸立案越涨越高；与 `global_concurrency_current`（此刻正在跑 LLM 的庭审数）**不是同一口径**，两者都保留。
- **为什么重算而不是增量**：庭审有 reopen / recovery / cancel 多条出口，增量维护迟早漂移，而"漂移的指标"比"没有指标"更坏。
- **实测**：重启后端 → gauge 报 **17**，DB 真值也是 **17**（完全一致），Prometheus 抓到 `courtroom_active_sessions=17`；单测 2 项（口径 + nil-safe）。
- 仪表盘面板已换回 `courtroom_active_sessions`。

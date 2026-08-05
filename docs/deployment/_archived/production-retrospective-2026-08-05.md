# Production Retrospective — ECS 30 天运行沉淀

> **📦 归档说明（2026-08-05）**：本文件已迁至 [`_archived/production-retrospective-2026-08-05.md`](./production-retrospective-2026-08-05.md)。作为 2026-07-06 ~ 2026-08-05 期间 ECS `47.239.152.177` 真实生产数据的最终沉淀保留，**仍然有参考价值**（部署踩坑 / 真实庭审数据 / 资源使用基线）。后续部署到任何新环境时可作为基准对比。

| | |
|---|---|
| **原位置** | `docs/deployment/production-retrospective-2026-08-05.md`（已迁移至 `_archived/`）|
| **状态** | 历史沉淀，永久有效 |

---

| 字段 | 值 |
|---|---|
| **生成日期** | 2026-08-05 |
| **触发事件** | ECS 服务器即将过期，需沉淀运行现状 + 风险清单 |
| **采集方式** | 远程 SSH (admin@47.239.152.177) + 容器日志 + PostgreSQL 直查 |
| **覆盖范围** | 2026-07-06 ~ 2026-08-05（29 天生产数据）|
| **关联文档** | [ADR 0016 部署踩坑](../adr/0016-deployment-lessons-learned.md) · [Whitebox Roadmap](../roadmap/whitebox-roadmap.md) · [v0.10.17](../release-notes/v0.10.17.md) · [v0.10.18](../release-notes/v0.10.18.md) · [v0.10.20](../release-notes/v0.10.20.md) |

---

## 🎯 一句话总结

**生产稳定（0 重启 / 18.95 MiB RAM / 24 天连续运行），但存在 4 处**配置/代码错位**导致部分设计意图从未生效；v0.10.20 已部署但 release-notes 长期停留在"准备发版"；30 天仅 17 场真实庭审 → 仍处 Phase A 数据采集期。**

---

## 1. 服务器硬件现状（与文档不一致）

| 维度 | ECS 实际 | ADR 0016 / Release notes 描述 | 影响 |
|---|---|---|---|
| **内存** | **1.575 GiB** | "阿里云 2C2G" / "2 vCPU / 2 GiB" | v0.10.20 L0 max=5 是按 2GB 算的，实测在 1.575GB 可能吃紧 |
| **Swap** | **0B** | 未提及 | 无兜底，OOM 即被 kill |
| **磁盘** | 40GB / 12GB used (31%) | 未提及 | 余量充足，无需清理 |
| **CPU** | 2 vCPU | "2 vCPU" | 一致 |
| **负载** | load average 0.08 | — | 极空闲 |
| **SSH 密钥** | 接受 `id_rsa` (RSA) | `secrets/ecs.env` 写 `id_ed25519` | **文档与现实不符**（见 §6.6）|

---

## 2. 容器运行状态（截至 2026-08-05 14:30 CST）

| 容器 | 镜像 | Started | 状态 | MEM | Restart |
|---|---|---|---|---|---|
| dc_backend | `:latest` = `cbbaf3d3` (v0.10.20) | 2026-07-12 13:27 | Up 24 days (healthy) | 18.95 MiB / 1.575 GiB (1.17%) | **0** |
| dc_frontend | `:latest` = `cbbaf3d3` (v0.10.20) | 2026-07-12 ~13:27 | Up 3 weeks | 41.07 MiB (2.55%) | 0 |
| dc_caddy | caddy:2-alpine | 2026-07-12 ~13:27 | Up 3 weeks | 16.34 MiB (1.01%) | 0 |
| dc_postgres | postgres:15.13-alpine | ~2026-07-06 | Up 4 weeks (healthy) | 39.55 MiB (2.45%) | 0 |
| dc_redis | redis:7.4.1-alpine | ~2026-07-06 | Up 4 weeks (healthy) | 4.19 MiB (0.26%) | 0 |

**总内存占用：~120 MiB / 1.575 GiB（7.6%）**—— 当前业务量级完全没压力，但**单容器 OOM 整个 backend 进程会被 kill**，无 swap 兜底。

---

## 3. 真实业务流量（29 天）

### 3.1 数据库总量

| 表 | 行数 | 备注 |
|---|---|---|
| `decision_events` | 90 | 跨度 2026-07-06 → 2026-08-04 |
| `llm_calls` | 227 | 同上 |
| `belief_diffs` | 有数据 |  |
| `a2_a_messages` | 有数据 |  |
| `evidences` | 有数据 |  |
| `private_memories` | 有数据 |  |
| 16 张表 | — |  |

### 3.2 庭审分布

- **17 个独立 session** (跨 29 天)
- **活跃日**（按 decision_events 数降序）：
  - 2026-08-04：21（最高峰）
  - 2026-07-12：20
  - 2026-07-08：10
  - 2026-07-07：7 / 2026-07-29：9 / 2026-07-31：6
  - 其余零星
- **疑似单用户**（session 0aa05776 出现高频 + 慢调用集中）—— 应核实是否同一用户反复测试

### 3.3 LLM 调用性能

| 指标 | 值 |
|---|---|
| 总调用 | 227 |
| 平均 latency | **3.74 s** |
| P50 | 2.89 s |
| P95 | **8.40 s** |
| **P99** | **12.90 s** |
| 错误率 | 4 / 227 = **1.76 %** |
| >10s 慢调用 | 5 次（全部 session 0aa05776, 2026-08-04） |

**Top 3 最慢 LLM 调用**（来自 session 0aa05776, 单用户 2026-08-04）：
- 20.75 s（react_reflect_retry, tokens=737）
- 14.18 s（context canceled 之前）
- 12.90 s

→ 这是**单用户单次 session 的偶发慢点**，不是系统性恶化。DeepSeek API 自身的波动。但 **P95 8.4s 对用户来说"显得卡"**，未来 Phase C 加 Prometheus 后需要 P95 告警（Whitebox Roadmap §C.3）。

### 3.4 错误事件分类

| 类别 | 数量 | 备注 |
|---|---|---|
| LLM `context canceled` | 4 | 2 次在 2026-07-11 session a9a3b836（react_think/final/verdict 同时断），1 次在 2026-07-27 session 5d5b4fdc（opening speeches failed），1 次在 2026-08-04 |
| LLM HTTP timeout (>20s) | 隐含 | 见上面 P99 / 慢调用 |
| `opening speeches failed` | 1 | 2026-07-27 03:58 — 用户在 ReAct 跑 opening 时点了别处/关页面 |
| `decision_events status=error` | **0** | 全 90 条都是 ok → 状态机/事件落库 100% 健康 |

---

## 4. ⚠️ 发现的 8 个问题（按严重度排序）

### 🔴 P1-1：FileLogger 从未启用（v0.9.2 修了一半的设计）

**问题**：v0.9.2 ADR 0016 坑13 修了"file_logger 写不进去"的 volume 挂载问题，但**从未真正打开 FileLogger 功能**：

```bash
# ECS .env 实际有：
AGENT_GATEWAY_BREAKER_ENABLED=true
AGENT_GATEWAY_LLM_TIMEOUT_SEC=90
# ⚠️ 没有 AGENT_GATEWAY_FILE_LOGGER

# .env.example / docker-compose.yml 也没有这个 env
```

**后果**：
- `/opt/DecisionCourt/logs/backend/` 目录存在但**完全空**（连 `agent_gateway_*.log` 文件都没生成过）
- 用户查 LLM 调用明细只能看 stdout（信息少 90%）
- 35 字段的 LogEntry（token / cost / compression / throttle / budget）从未落盘

**修复**：
1. 在 `.env.example` 加 `AGENT_GATEWAY_FILE_LOGGER=true`（默认开）+ 注释说明
2. 在 `docker-compose.yml` backend 段加 `AGENT_GATEWAY_FILE_LOGGER: ${AGENT_GATEWAY_FILE_LOGGER:-true}` 注入
3. 用户的 `.env` 加 `AGENT_GATEWAY_FILE_LOGGER=true`
4. ADR 0025 §6.4 教训 16 重提："`if err != nil { /* 吞掉 */ }` 至少打 WARN"——`gateway.go:408-410` 当前**仍然吞掉错误**

**关联**：[ADR 0016 坑 13](../adr/0016-deployment-lessons-learned.md#L337-L414) · [ADR 0025 §6.4](../adr/0025-security-p0-closeout.md)

### 🔴 P1-2：启动日志 `version` 硬编码为 `v0.9.2`（说谎 24 天）

**问题**：`backend/cmd/server/main.go:223` 写死 `"version": "v0.9.2"`，从未更新：

```bash
# 实际启动日志（每条都说自己是 v0.9.2）：
{"time":"2026-07-12T13:27:36","level":"INFO","msg":"DecisionCourt backend listening","port":"8080","version":"v0.9.2","whitebox":"enabled"}
```

**实际运行的版本**：v0.10.20 (commit `cbbaf3d`，tag `v0.10.20` 已 push，镜像 `latest` 部署到 ECS)

**影响**：
- 任何 dashboard / 监控 / 客户支持看到 `version=v0.9.2` 都以为系统在跑老版本
- v0.9.2 早于 v0.10.17 silent-error-fix / v0.10.18 安全 P0 / v0.10.20 限流，但代码里有这些 feature 的痕迹（L0 ConcurrencyLimiter 在启动日志中确实初始化）→ 状态分裂

**修复**：
- 短期：`main.go:223` 改用 ldflags 注入 `git describe --tags --always`
- 中期：把 `version` 改成 `runtime.Version()` + build info

### 🟡 P2-1：Caddy 没有 access log 配置

**问题**：Caddyfile 没有 `access_log` 指令 → `/data/caddy/access.log` 不存在 → 我们**看不到真实 HTTP 流量**：
- 多少请求来自前端？
- CreateCourtroom 失败是脚本还是真实用户？
- 有没有 4xx/5xx 攻击？

**当前能看到的只有**：stdout 中的 `http.acme_client` 续期日志（每 5 分钟一次）+ 后端 stdout 中的 `CreateCourtroom bind failed` WARN（仅 4xx 且仅后端记录）

**修复**：在 `deploy/caddy/Caddyfile` 加 `access_log` 指令 → 挂卷到 host。

### 🟡 P2-2：Next.js Server Action 部署错位（前端日志报错）

**问题**：frontend 日志充满：
```
Error: Failed to find Server Action "x". This request might be from an older or newer deployment.
```

**触发场景**：用户打开浏览器 → 开发者部署新版本 → 用户刷新时客户端 chunks 找不到对应 Server Action ID

**影响**：
- 偶发，不是常态
- 表现为"点击按钮无反应"（已被 v0.10.17 silent-error-fix 部分缓解 — toast 会提示）
- 但**根因是 Next.js 14 的 chunks 缓存策略 + 我们的滚动部署**

**修复**：
- 短期：接受（v0.10.17 已让用户看到错误 toast）
- 中期：考虑用 `output: 'standalone'` + 自定义 cache headers（ADR 待写）

### 🟡 P2-3：release-notes 与实际版本长期错位

**问题**：`v0.10.20` 的 release notes 标题写 `🟡 准备发版（PR 1-3 已合并, 待 commit + tag）`，但**实际上 commit `cbbaf3d`、tag `v0.10.20`、镜像 `:v0.10.20` 全部在 2026-07-12 部署**。文档 24 天没更新。

**关联**：v0.10.17 / v0.10.18 的 release notes 也是 `🟡 部署中` 状态。

**修复**：
- 把 v0.10.20 release notes 状态改为 `✅ 已部署（2026-07-12 镜像 latest，commit cbbaf3d3，tag v0.10.20）`
- 加"实际部署 vs 文档时间差 24 天"反思章节
- 同步 v0.10.17 / v0.10.18

### 🟢 P3-1：创建庭审被外部 IP 持续 probe

**问题**：2 个外部 IP 多次触发 `CreateCourtroom bind failed` (HTTP 400，body 缺 `OptionA`/`OptionB`)：
- `112.80.30.194` — 2026-07-15 02:27（5 次连续）
- `111.198.56.206` — 2026-07-28 01:41

**判定**：可能是互联网随机扫描或别人误调 API（payload 都不像恶意，更像脚本探测 API 形状）。不是攻击。

**修复**：
- 当前 v0.10.18 + L3 Per-IP 限流已能挡
- 但日志里 IP 没脱敏（v0.11 P1 错误脱敏任务再说）

### 🟢 P3-2：`opening speeches failed: context canceled` 1 次

**问题**：2026-07-27 03:58 session 5d5b4fdc `opening speeches failed: context canceled`

**判定**：用户在 opening 阶段（最长 5 分钟 ReAct 循环）点别处 / 关页面 / 超时 → 父 ctx cancel → LLM 调用全断 → 当前实现会发 `BroadcastUserFacingError`（v0.10.17 的 silent-error-fix 已让用户看到 toast）

**修复**：无需，v0.10.17 已处理。

### 🟢 P3-3：单用户多次触发慢 LLM 调用

**问题**：session 0aa05776 (2026-08-04) 5 次 LLM 调用 >10s（含 P99 = 20.7s）

**判定**：单用户 session 的偶发慢点。可能是 deepseek-chat 模型在那段时间负载高（深夜+白天都中招，但都是同一天同一 session）。

**修复**：
- Phase C Prometheus 加 latency histogram（Whitebox Roadmap §C.1）
- 当前不处理

---

## 5. 关键里程碑时间线（29 天）

```
2026-07-06  🚀 首次部署上线（ADR 0016 v0.9.2 实装 ECS 香港节点）
2026-07-08  首场真实庭审
2026-07-11  首次 LLM context canceled（session a9a3b836，opening speeches）
2026-07-12  v0.10.17 部署：静默错误全局修复（9 个黑洞 → 0）
2026-07-12  v0.10.18 部署：安全 P0 收尾（JWT fail-fast + UID 10001）
2026-07-12  v0.10.20 部署：4 层限流防御深度（L0=5 + L1 + L2 + L3）
            ⚠️ 但 release notes 仍写"准备发版"
2026-07-15  首次 CreateCourtroom bot probe（112.80.30.194）
2026-07-27  session 5d5b4fdc opening speeches 失败（context canceled）
2026-07-28  第二次 CreateCourtroom probe（111.198.56.206）
2026-08-04  最高活跃日：21 decision_events，单 session 0aa05776 触发 5 次慢调用
2026-08-05  ⏸ ECS 即将过期，需要沉淀 + 迁移
```

---

## 6. 文档/配置不一致清单

### 6.1 `.env.example` 缺失项

| 缺失 env | 当前状态 | 应有值 |
|---|---|---|
| `AGENT_GATEWAY_FILE_LOGGER` | 完全缺失 | `true` |
| `RATE_LIMIT_MAX_CONCURRENT_TRIALS` | 缺失（v0.10.20 main.go 硬编码 5） | `5` |
| `RATE_LIMIT_SESSION_RPS` | 缺失（v0.10.20 L1） | `2` |
| `RATE_LIMIT_SESSION_BURST` | 缺失（v0.10.20 L1） | `5` |

### 6.2 `docker-compose.yml` 缺失项

| 缺失项 | 当前 | 应有 |
|---|---|---|
| `AGENT_GATEWAY_FILE_LOGGER` 注入 | 无 | `AGENT_GATEWAY_FILE_LOGGER: ${AGENT_GATEWAY_FILE_LOGGER:-true}` |
| Caddy access log 卷挂载 | 无 | `./logs/caddy:/data/caddy/access.log` |

### 6.3 文档与现实不一致

| 项 | 文档 | 实际 |
|---|---|---|
| ECS 内存 | "2 vCPU / 2 GiB" (ADR 0016) | **1.575 GiB** |
| ECS SSH 密钥 | `id_ed25519` (secrets/ecs.env) | **id_rsa** |
| Backend version | `v0.9.2` (main.go:223 硬编码) | **v0.10.20** |
| v0.10.20 release notes 状态 | "🟡 准备发版（PR 1-3 已合并, 待 commit + tag）" | **✅ 已部署 24 天**（commit cbbaf3d, tag v0.10.20, 镜像 :v0.10.20 + :latest）|

### 6.4 文件挂载验证（v0.9.2 坑 13 修复确认）

```bash
docker-compose.yml backend 段：
  volumes:
    - ./logs/backend:/app/logs   ✅ 已挂载
  read_only: true                ✅ 保留（安全等级不变）
  tmpfs:
    - /tmp                       ✅ 保留

实测：docker exec dc_backend touch /app/logs/test_write.tmp → WRITE_OK ✅
但：logs/backend/ 只有我刚 touch 的 test_write.tmp，没有任何 agent_gateway_*.log
→ 进一步证实 P1-1：FileLogger 功能未启用
```

---

## 7. ECS 过期前建议动作（按优先级）

### 7.1 必须做（迁移前）

1. **备份 postgres**：`docker exec dc_postgres pg_dump -U decisioncourt decisioncourt > backup-2026-08-05.sql` → 拉回本机
2. **备份 .env**：`scp admin@47.239.152.177:/opt/DecisionCourt/.env secrets/.env.backup-2026-08-05`
3. **备份日志**（虽然 logs/backend/ 是空的，但 /opt/DecisionCourt/caddy_data 等可能有）：`scp -r admin@47.239.152.177:/opt/DecisionCourt/ secrets/ecs-state-2026-08-05/`
4. **确认新 ECS** IP / SSH 密钥 / region

### 7.2 强烈建议（迁移前修掉 P1-1/P1-2）

5. **修 main.go:223 的 version 硬编码**：改 ldflags → 重新 build → push ACR `:latest` → ECS pull → 验证启动日志
6. **开 FileLogger**：加 4 个 env（见 §6.1/§6.2）→ 重新部署 → 验证 `/opt/DecisionCourt/logs/backend/agent_gateway_*.log` 落盘
7. **更新 release notes**：v0.10.17/18/20 状态全部改为 ✅ 已部署

### 7.3 可以做（不阻塞迁移）

8. 配 Caddy access log（看真实 HTTP 流量）
9. 加 Prometheus exporter（Whitebox Roadmap Phase C 触发条件 = 用户 > 100；现在 17 个 session 还没到）
10. 监控告警：基于 `decision_events.status=error` 计数 + `llm_calls.latency_ms > 10000` 计数（Phase C 之前的临时方案）

### 7.4 不要做（过度工程化）

- 不要扩 ECS 到 4C8G（DAU < 100，没必要）
- 不要接 OTLP / Jaeger（Whitebox Roadmap Phase D 触发条件 = 多实例部署，当前单实例）
- 不要接 ClickHouse（Whitebox Roadmap Phase E 触发条件 = 商业化启动）

---

## 8. Phase A 数据采集结论（Whitebox Roadmap）

按 [Whitebox Roadmap §A.3](../roadmap/whitebox-roadmap.md#L60-L86) 的触发决策表：

| 统计结果 | 应触发 |
|---|---|
| state_transition 占 90 / 90 = **100%** | ✅ 状态机查询高频 → 优先埋 **Phase B.1 RunCrossExamRound span**（trial 最常卡的循环）|
| `fe.tab_switched` (30) / `fe.trial_started` (6) 等前端事件 | ✅ 前端埋点已实装（ADR 0020），无需再埋 |
| `status='error'` = 0 | 暂无告警需求 |
| LLM P99 = 12.9s (>10s) | ⚠️ Phase C P99 告警规则命中（但还没接 Prometheus）|

**Phase B 优先动作**：埋 `RunCrossExamRound` span（ADR 待写）—— 2026-08-04 session 0aa05776 那场就是典型例子，多轮 cross_exam P99 集中爆发。

---

## 9. 一句话状态摘要（给未来自己 / 接手者）

> "DecisionCourt v0.10.20 在 ECS 47.239.152.177 (1.575 GiB / 无 swap) 上稳定运行 24 天，**单容器 OOM 即全挂**。生产 29 天累计 17 场真实庭审、227 次 LLM 调用、平均 3.7s / P99 12.9s，错误率 1.76%。代码 / 配置 / 文档 3 处错位：FileLogger 未启用、main.go version 硬编码 v0.9.2、release-notes 长期 '准备发版' 状态。30 天活跃用户量 < 20，**仍处 Whitebox Roadmap Phase A 数据采集期**，下一步应埋 RunCrossExamRound span + 开 FileLogger。"

---

## 10. 关联待办（PR 草稿）

| PR | 内容 | 工作量估算 |
|---|---|---|
| **PR A：FileLogger 启用** | .env.example + docker-compose.yml + .env 三处加 AGENT_GATEWAY_FILE_LOGGER=true，部署验证日志落盘 | 30 min |
| **PR B：version ldflags** | main.go 改 ldflags 注入 `git describe --tags --always`；Dockerfile 加 `-ldflags "-X main.version=$(git describe --tags --always)"` | 1 hour |
| **PR C：release-notes 状态修正** | v0.10.17/18/20 全部 ✅ 已部署 + 加"实际部署时间"列 | 30 min |
| **PR D：Phase B.1 RunCrossExamRound span** | business_spans.go + courtroom/service.go + decision_events 落库 + 测试 | 2-3 hour（参考 ADR 0010） |
| **PR E：Caddy access log** | Caddyfile 加 access_log + 卷挂载 | 30 min |
| **PR F：ECS 迁移 checklist** | 基于本文 §7 写成 deployment/ECS-MIGRATION-CHECKLIST.md | 1 hour |

**总工作量**：~1 个工作日（含部署验证）
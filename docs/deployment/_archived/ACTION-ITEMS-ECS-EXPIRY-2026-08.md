# 紧急待办清单 — ECS 过期前（2026-08-05 起）

> **📦 归档说明（2026-08-05）**：本文件已失效。"🟡 活跃工作清单"状态改为"⏸ 暂停/归档"——ECS 不续购，过期前行动清单失去紧迫性。本文件归档至 [`_archived/ACTION-ITEMS-ECS-EXPIRY-2026-08.md`](./ACTION-ITEMS-ECS-EXPIRY-2026-08.md) 作为历史决策记录保留。

| | |
|---|---|
| **原位置** | `docs/deployment/ACTION-ITEMS-ECS-EXPIRY-2026-08.md`（已迁移至 `_archived/`）|
| **原状态** | 🟡 活跃工作清单 → **⏸ 已暂停/归档** |
| **失效原因** | ECS 不续购，无"过期前必须做"的行动项 |

---

| 字段 | 值 |
|---|---|
| **生成日期** | 2026-08-05 |
| **状态** | 🟡 **活跃工作清单**（按优先级逐项推进）|
| **触发事件** | ECS `47.239.152.177` 即将过期；30 天沉淀发现 8 处需修 |
| **范围** | 仅"ECS 过期前必须/强烈建议做的动作" |
| **完整背景** | [`production-retrospective-2026-08-05.md`](./production-retrospective-2026-08-05.md) |
| **下次评审** | 用户确认迁移日期后 |

---

## 🟥 P0：迁移前必须做（不可阻塞）

- [ ] **A1. 备份 PostgreSQL 数据库**
  ```bash
  ssh -i ~/.ssh/id_rsa admin@47.239.152.177 \
    "docker exec dc_postgres pg_dump -U decisioncourt decisioncourt \
     > /tmp/db-backup-2026-08-05.sql"
  scp admin@47.239.152.177:/tmp/db-backup-2026-08-05.sql ./secrets/
  ```
  - 验证：文件 > 1MB；用 `head -3` 看 SQL header 正常
  - 工作量：5 min

- [ ] **A2. 备份生产 .env**
  ```bash
  scp admin@47.239.152.177:/opt/DecisionCourt/.env secrets/.env.backup-2026-08-05
  # ⚠️ AGENTS.md §8 红线：只 Read 不修改生产 .env
  ```
  - 工作量：1 min

- [ ] **A3. 备份关键数据卷**
  ```bash
  ssh admin@47.239.152.177 "cd /opt && tar czf /tmp/state-backup-2026-08-05.tar.gz \
    DecisionCourt/caddy_data DecisionCourt/caddy_config \
    DecisionCourt/logs DecisionCourt/postgres_data"
  scp admin@47.239.152.177:/tmp/state-backup-2026-08-05.tar.gz ./secrets/
  ```
  - 工作量：5 min

- [ ] **A4. 确认新 ECS 信息**
  - [ ] 新 IP / hostname
  - [ ] 新 SSH 密钥（同步更新 `secrets/ecs.env` + AGENTS.md §10）
  - [ ] 新 ECS 区域 / region / 可用区
  - [ ] 新 ECS 规格（建议 ≥ 2C2G / 4GB RAM，**避免再踩 1.6GB 无 swap 的坑**）
  - [ ] 新 ECS 系统盘 ≥ 40GB
  - 工作量：等用户提供

---

## 🟧 P1：迁移前强烈建议修（30 天沉淀的代码/配置错位）

### B1. PR-A：启用 FileLogger（日志落盘）

**问题**：v0.9.2 修了 volume 挂载但从未打开功能，`/opt/DecisionCourt/logs/backend/` 完全是空的，35 字段 LogEntry 从未落盘。

**改动**：
- [ ] `.env.example` 加 `AGENT_GATEWAY_FILE_LOGGER=true` + 注释
- [ ] `docker-compose.yml` backend 段加：
  ```yaml
  AGENT_GATEWAY_FILE_LOGGER: ${AGENT_GATEWAY_FILE_LOGGER:-true}
  AGENT_GATEWAY_LOG_DIR: ${AGENT_GATEWAY_LOG_DIR:-/app/logs}
  ```
- [ ] `backend/internal/config/config.go` 同步加 `AgentGateway.LogDir` mapstructure 绑定（如缺）
- [ ] `backend/internal/agent_gateway/gateway.go:408-410` 改：err 至少打 `slog.Warn("fileLogger.Write failed", "err", err)`
- [ ] 本机 build 后端镜像 → push ACR `:latest`
- [ ] ECS pull → 重启 dc_backend → 触发一次 trial
- [ ] 验证：`ls -la /opt/DecisionCourt/logs/backend/agent_gateway_*.log` 存在且 ≥ 1 行
- 工作量：30 min

### B2. PR-B：version ldflags（修"启动日志撒谎 24 天"）

**问题**：`main.go:223` 硬编码 `"version": "v0.9.2"`，实际跑 v0.10.20。

**改动**：
- [ ] `backend/cmd/server/main.go:223` 把 `"v0.9.2"` 改成 ldflag 变量 `version`
- [ ] `backend/Dockerfile` 加：
  ```dockerfile
  ARG VERSION=dev
  RUN go build -ldflags "-X main.version=${VERSION}"
  ```
- [ ] `scripts/push-to-acr.ps1` 加 `-ldflags "-X main.version=$(git describe --tags --always)"`
- [ ] 部署后验证：`docker logs dc_backend 2>&1 | grep "backend listening"` → `version=v0.10.20`
- 工作量：1 hour

### B3. PR-C：release notes 状态修正（停止撒谎）

**问题**：v0.10.17/18/20 release notes 写 "🟡 准备发版 / 部署中"，实际全部 ✅ 已部署 24 天。

**改动**：
- [ ] `docs/release-notes/v0.10.17.md` 状态 → `✅ 已部署 ECS（2026-07-12）`，加"实际部署时间"列
- [ ] `docs/release-notes/v0.10.18.md` 同上
- [ ] `docs/release-notes/v0.10.20.md` 同上
- [ ] 在 3 个 release notes 末尾加"**反思**：文档 vs 实际部署时间差 24 天 → 引入 [production-retrospective-2026-08-05.md §5](./production-retrospective-2026-08-05.md#5-关键里程碑时间线29-天)"
- 工作量：30 min

### B4. 修 `secrets/ecs.env` SSH_KEY 错位

**问题**：文档写 `id_ed25519`，实际 ECS 接受 `id_rsa`。

**改动**：
- [ ] **AGENTS.md §10 加 2026-08-05 行**：ECS_HOST=47.239.152.177 / ECS_USER=admin / **SSH_KEY=id_rsa**
- [ ] `secrets/ecs.env` 修 `ECS_SSH_KEY_PATH=$env:USERPROFILE\.ssh\id_rsa`（用户授权后）
- 工作量：5 min

---

## 🟨 P2：迁移后立即做（不阻塞迁移）

### C1. PR-D：埋 `RunCrossExamRound` span（Whitebox Roadmap Phase B.1）

**触发依据**：Phase A 数据 — `state_transition` 占 90/90 = 100%，2026-08-04 单 session 触发 5 次慢调用集中在 cross_exam 阶段。

**改动**（参考 ADR 0010）：
- [ ] `backend/internal/observability/business_spans.go` 加 `SpanName_RunCrossExamRound = "RunCrossExamRound"`
- [ ] `backend/internal/courtroom/service.go` `runCrossExamRound` 入口加 `defer span.End()` 模式 + `SetAttr("session_uuid", ...)` + `SetAttr("round", round)`
- [ ] 单元测试 `TestRunCrossExamRound_SpanRecorded`
- [ ] 部署后跑 1 场 trial → 查 `decision_events WHERE event_type='span.RunCrossExamRound'`
- 工作量：2-3 hour

### C2. PR-E：Caddy 加 access log（看真实 HTTP 流量）

**改动**：
- [ ] `deploy/caddy/Caddyfile` 加：
  ```
  access_log {
      output file /data/caddy/access.log {
          roll_size 100mb
          roll_keep 5
      }
  }
  ```
- [ ] `docker-compose.yml` caddy 段加卷 `./logs/caddy:/data/caddy`
- 工作量：30 min

### C3. 临时监控（Phase C Prometheus 之前的兜底）

- [ ] crontab 每天跑一次 `decision_events` 错误率 + LLM P99 latency SQL
- [ ] 超阈值发邮件/通知（用户确认用什么渠道）
- 工作量：1 hour

---

## 🟩 P3：可选，不阻塞

- [ ] **D1**. 加 PR-F：ECS 迁移 checklist 文档（基于 production-retrospective §7）
- [ ] **D2**. 评估 ECS 升配到 4C4G 的成本/必要性（当前 1.575GB / 7.6% 用量，但无 swap 是隐患）
- [ ] **D3**. 清理无用旧镜像：`docker image prune --filter "until=720h"`

---

## 📋 执行顺序（建议）

```
Phase 0 ── 立即 ─────────────────────────────────────
  A1 备份 DB
  A2 备份 .env
  A3 备份数据卷
  A4 用户提供新 ECS 信息
        ↓
Phase 1 ── 修代码（1 天）───────────────────────────
  B1 PR-A: FileLogger
  B2 PR-B: version ldflags
  B3 PR-C: release notes 状态
  B4 secrets/ecs.env 修 SSH_KEY
        ↓
Phase 2 ── 部署新 ECS ──────────────────────────────
  [按 A4 提供的信息新建 ECS + 跑 deploy-on-ecs.sh]
        ↓
Phase 3 ── 验证 + 收尾 ────────────────────────────
  PR-A 验证 logs/backend 有 .log 落盘
  PR-B 验证启动日志 version=正确
  PR-D (Phase B.1) 埋 span + 跑 1 场 trial 验证
  旧 ECS 释放
```

---

## 🔗 关联文档

- [`production-retrospective-2026-08-05.md`](./production-retrospective-2026-08-05.md) — 完整背景（30 天沉淀）
- [`CHECKLIST.md`](./CHECKLIST.md) — v0.8.3 部署规划（已完成的 P0-P3）
- [`ADR 0016`](../adr/0016-deployment-lessons-learned.md) — 历史踩坑（坑 13 file_logger）
- [`Whitebox Roadmap`](../roadmap/whitebox-roadmap.md) — Phase A 统计触发 Phase B 决策

---

## 📝 更新日志

| 日期 | 更新 |
|---|---|
| 2026-08-05 | 初版（基于 production-retrospective §7 + §10） |
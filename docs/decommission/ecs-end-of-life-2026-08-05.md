# ECS 终止运营通知（2026-08-05）

| | |
|---|---|
| **生成日期** | 2026-08-05 |
| **状态** | ⛔ **Abandoned**（用户决策：不续购 ECS） |
| **触发** | 用户 2026-08-05 明确表态："不打算续购" |

---

## 决策内容

用户决策：**DecisionCourt 项目不续购 ECS 实例**。

含义：
- 旧 ECS `47.239.152.177`（推测 2C2G / 香港 / ACR 香港）到期后自然释放
- **不迁移到新 ECS**（A4 取消）
- **不再部署任何代码到 ECS**（11 个 commit 不 push 到 origin）
- 现有 ECS 上的 24 天稳定运行数据 + retrospective 是项目最终生产沉淀

---

## 现有 ECS 的命运

### 当前 ECS 状态（截至 2026-08-05）
- 5 容器运行 24 天无重启（dc_backend / dc_caddy / dc_frontend / dc_postgres / dc_redis）
- 版本：v0.10.20（commit `cbbaf3d` + tag `v0.10.20`）
- 资源使用：~120 MiB / 1.575 GiB / 8.6GB / 40GB
- 现状：稳定，无已知故障

### 到期后
- 阿里云会自动停止并释放 ECS 实例
- 数据（PostgreSQL / Redis / Caddy 证书 / 文件日志）会丢失（除非用户在阿里云控制台保留 EIP/磁盘）
- ACR 镜像仍会保留（默认 30 天免费，之后收费或删除）

### 用户决策的关键文档
- **保留**：`secrets/db-backup-2026-08-05.sql.gz` + `secrets/.env.backup-2026-08-05` + `secrets/state-backup-2026-08-05.tar.gz`
- **保留**：`docs/deployment/production-retrospective-2026-08-05.md`（30 天生产数据沉淀）

---

## 项目最终形态

DecisionCourt 项目**不再是有"在跑生产服务"的开源项目**，而是：
- 一份**完整代码 + 完整文档 + 完整 30 天生产数据**的开源 showcase
- 可在任何环境（本地 / 其他云）按 [README.md](../../README.md) + [decisioncourt-tech-spec.md](../decisioncourt-tech-spec.md) 重新部署
- 历史真实运行数据保留在 `production-retrospective-2026-08-05.md`

---

## v0.10.21 11 个 commit 的去向

| commit | 内容 | 部署？ |
|--------|------|--------|
| `cf9154b` docs(deployment): ACTION-ITEMS-ECS-EXPIRY-2026-08.md 入库 | 文档 | 不需要部署 |
| `422218f` docs(deployment): production-retrospective-2026-08-05.md 入库 | 文档 | 不需要部署 |
| `369abb8` docs(agents): §10 加 2026-08-05 ECS 连接信息 | 文档 | 不需要部署 |
| `c3da7ef` docs(release-notes): PR-C 修正 v0.10.17/18/20 状态 | 文档 | 不需要部署 |
| `13de4f9` fix(version): PR-B ldflags 注入 | 代码 | 不部署（但本地有效） |
| `b1a2e1d` feat(agent-gateway): PR-A FileLogger 启用 | 代码 + .env | 不部署 |
| `d6d0693` feat(observability): PR-D RunCrossExamRound span | 代码 | 不部署 |
| `545b8a8` feat(caddy): PR-E access log | 配置 | 不部署 |
| `21faa3f` feat(monitor): monitor-daily.sh | 脚本 | 不部署 |
| `deb2988` docs(deployment): ECS-MIGRATION-CHECKLIST.md | 文档 | **废弃**（不需要迁移了）|
| `03762a7` docs(deployment): ECS-RESOURCE-ASSESSMENT.md | 文档 | **废弃**（不需要新 ECS）|
| `d7d1a20` docs(todo): deferred-items-2026-08-05.md | 文档 | **部分废弃**（D3 A4 取消）|

**注**：代码改动（PR-A/B/D/E）在仓库内仍然有效 — 用户/其他人如果重新部署 DecisionCourt 到自己的环境，这些改进会自动生效。

---

## 影响清单

### ✅ 已完成且永久有效
- 30 天生产数据沉淀（retrospective）→ 写进文档供后人参考
- 4 个 P1 代码修复（PR-A/B/C/D）→ 代码改进保留在 git 历史
- 3 个 P2 改进（PR-D/E/F + monitor 脚本）→ 代码改进保留
- 3 个 P3 文档（checklist + assessment + deferred）→ 项目知识沉淀
- 本地 secrets/ 备份 → 不可恢复的最后生产快照

### ⛔ 取消（不再做）
- A4 迁移到新 ECS
- push 11 个 commit 到 origin（不会触发 deploy，但保留 commit 历史）
- 任何 ECS 后续运维（SSH / docker exec / crontab 安装）

### ⏸ 正式 Deferred（保留可能性）
- D1 安全审计 P1-P3（14 项）— 等任何形式的"重新部署 / 安全审计"触发
- D2 silent-error 剩余 3 项 — 同上

---

## 给后来者（项目读者）的说明

如果你是后来读到这份文档的开发者：
1. DecisionCourt 是一个**曾经真实部署 24 天、产出 17 场真实庭审**的 AI 多 Agent 辩论系统
2. 它的**代码 + 文档 + 30 天生产数据**完整保留在本仓库
3. 你可以**完全本地化运行**（[README.md](../../README.md) 有 3 种启动方式）
4. 你可以**部署到你自己的云**（按 [decisioncourt-tech-spec.md](../decisioncourt-tech-spec.md) 第 9 节）
5. 如果重新部署，建议应用 PR-A/B/D/E + 安全审计 P1-P3 的改进（[deferred-items-2026-08-05.md](../todo/deferred-items-2026-08-05.md)）

---

## 更新日志

| 日期 | 改动 |
|---|---|
| 2026-08-05 | 初版（基于用户决策"不续购 ECS"）|
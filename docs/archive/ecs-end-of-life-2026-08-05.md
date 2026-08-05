# ECS 基础设施终止记录（2026-08-05）

| | |
|---|---|
| **生成日期** | 2026-08-05 |
| **状态** | 🟡 **基础设施终止**（ECS 实例到期释放，项目继续个人长期维护） |
| **触发** | 用户 2026-08-05 明确表态："不打算续购 ECS" |
| **原始位置** | `docs/decommission/ecs-end-of-life-2026-08-05.md`（2026-08-05 同日归档改名） |

> **重要说明**：本文件记录的是 **ECS 基础设施（`47.239.152.177`）的终止**，而非项目终止。DecisionCourt 项目在 2026-08-05 后转为**个人长期本地开发模式**，代码继续维护，文档持续更新，欢迎后来者 fork / 部署到自有环境。

---

## 决策内容

用户决策：**DecisionCourt 项目不续购 ECS 实例**。

含义：
- 旧 ECS `47.239.152.177`（推测 2C2G / 香港 / ACR 香港）到期后自然释放
- **不迁移到新 ECS**（原 A4 取消）
- **不在新环境部署**（任何形式的"重新部署"由将来有需求的用户自行决定）
- 现有 ECS 上的 24 天稳定运行数据 + retrospective 是项目最后的生产沉淀

---

## 现有 ECS 的命运

### ECS 终止前状态（截至 2026-08-05）
- 5 容器运行 24 天无重启（dc_backend / dc_caddy / dc_frontend / dc_postgres / dc_redis）
- 版本：v0.10.20（commit `cbbaf3d` + tag `v0.10.20`）
- 资源使用：~120 MiB / 1.575 GiB / 8.6GB / 40GB
- 现状：稳定，无已知故障

### 到期后
- 阿里云会自动停止并释放 ECS 实例
- 数据（PostgreSQL / Redis / Caddy 证书 / 文件日志）会丢失（除非用户在阿里云控制台保留 EIP/磁盘）
- ACR 镜像仍会保留（默认 30 天免费，之后收费或删除）

### 用户决策的关键备份
- **保留**：`secrets/db-backup-2026-08-05.sql.gz` + `secrets/.env.backup-2026-08-05` + `secrets/state-backup-2026-08-05.tar.gz`
- **保留**：`docs/deployment/production-retrospective-2026-08-05.md`（30 天生产数据沉淀，详见 [`_archived/production-retrospective-2026-08-05.md`](../deployment/_archived/production-retrospective-2026-08-05.md)）

---

## 项目后续形态

DecisionCourt 项目**不再是有"持续运行生产服务"的项目**，但**仍是活跃的开源项目**，定位调整为：
- 一份**完整代码 + 完整文档 + 完整 30 天生产数据**的 AI 多 Agent 辩论系统
- 任何人可在本机按 [README.md](../../README.md) 三种启动方式本地运行
- 任何人可部署到自有云（按 [decisioncourt-tech-spec.md](../decisioncourt-tech-spec.md) 第 9 节）
- **代码 + 文档持续维护**：bug 修复、文档更新、新功能 PR 仍会接受并入 `main`
- **未来发版**：可能有 v0.10.21 / v0.11.x，但不再绑定"部署到 ECS"叙事

---

## v0.10.21 起 commit 的去向

> **2026-08-05 用户决策更新**：用户已推翻"11 commit 不 push"旧指令，所有本地 commit（包括 PR-A/B/C/D/E/F + 文档 commit）均**正常推送至 `origin/main`**。

完整 commit 表：

| commit | 内容 | 说明 |
|--------|------|------|
| `cf9154b` | docs(deployment): ACTION-ITEMS-ECS-EXPIRY-2026-08.md 入库 | 文档 |
| `422218f` | docs(deployment): production-retrospective-2026-08-05.md 入库 | 文档 |
| `369abb8` | docs(agents): §10 加 2026-08-05 ECS 连接信息 | 文档 |
| `c3da7ef` | docs(release-notes): PR-C 修正 v0.10.17/18/20 状态 | 文档 |
| `13de4f9` | fix(version): PR-B ldflags 注入 | 代码 |
| `b1a2e1d` | feat(agent-gateway): PR-A FileLogger 启用 | 代码 + .env |
| `d6d0693` | feat(observability): PR-D RunCrossExamRound span | 代码 |
| `545b8a8` | feat(caddy): PR-E access log | 配置 |
| `21faa3f` | feat(monitor): monitor-daily.sh | 脚本 |
| `deb2988` | docs(deployment): ECS-MIGRATION-CHECKLIST.md | 文档（迁移方案，已废）|
| `03762a7` | docs(deployment): ECS-RESOURCE-ASSESSMENT.md | 文档（升配方案，已废）|
| `d7d1a20` | docs(todo): deferred-items-2026-08-05.md | 文档 |
| `04fbf12` | docs(decommission): 本文件（原位置）| 文档 |
| `4a25825` | fix(todo): D3 Blocked→Cancelled | 文档 |
| `f4c33d7` | docs(agents): §10 标注 ECS 为最终状态 | 文档 |

**注**：代码改动（PR-A/B/D/E）在仓库内仍然有效 — 未来任何用户/贡献者重新部署 DecisionCourt 到自己的环境，这些改进会自动生效。

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
- 任何 ECS 后续运维（SSH / docker exec / crontab 安装 / 监控部署）

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
5. 如果重新部署，建议应用 PR-A/B/D/E + 安全审计 P1-P3 的改进（见 `docs/todo/deferred-items-2026-08-05.md`）

---

## 更新日志

| 日期 | 改动 |
|---|---|
| 2026-08-05 | 初版（基于用户决策"不续购 ECS"）|
| 2026-08-05 | 同日归档：从 `docs/decommission/` 移到 `docs/archive/`；措辞从"项目终止运营"改为"基础设施终止，项目继续维护"；移除"11 commit 不 push"段落（已由用户新指令推翻）|
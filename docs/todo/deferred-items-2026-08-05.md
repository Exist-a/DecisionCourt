# Deferred Items（正式 defer 登记，2026-08-05）

| | |
|---|---|
| **生成日期** | 2026-08-05 |
| **状态** | ⏸ **Deferred**（等待触发条件） |
| **触发** | 用户决策：等 ECS 迁移后再议 |
| **下一步评审** | ECS 迁移完成后 + 用户授权启动时 |

---

## D1. 安全审计 P1-P3（14 项未启动）

### 来源
[`.trae/documents/security-audit-2026-07-03.md`](../../.trae/documents/security-audit-2026-07-03.md) v1.0（2026-07-03）

### 背景
2026-07-03 全栈安全审计，共 **20 项**（P0 ×6 / P1 ×7 / P2 ×5 / P3 ×2 = 20 项）。

### 已完成（v0.10.17 + v0.10.18）
- ✅ **P0-1** 全栈完全无鉴权 → v0.8.3
- ✅ **P0-2** JWT_SECRET 默认值 → v0.10.18（commit `705bb12`）
- ✅ **P0-3** 容器以 root 跑 → v0.10.18（commit `705bb12`，UID 10001）
- ✅ **P0-4** 关键 env 无 fail-fast → v0.8.3
- ✅ **P0-5** SubmitEvidence 任意用户可写 → v0.8.3
- ✅ **P0-6** Export 无鉴权 + 无审计 → v0.8.3

### Deferred（**本节正式登记**）

| 等级 | # | 项 | 复杂度 |
|------|---|----|--------|
| **P1** | P1-1 | WS Origin CheckOrigin 白名单（env `ALLOWED_ORIGINS`） | 1 天 |
| **P1** | P1-2 | CSRF Token 中间件（HttpOnly cookie + double-submit） | 2 天 |
| **P1** | P1-3 | 输入长度校验（query / evidence / verdict 长度上限） | 1 天 |
| **P1** | P1-4 | LLM prompt 注入防护（context 隔离 + sanitize） | 2 天 |
| **P1** | P1-5 | 日志脱敏（Detail 字段 prod 不填充 Go 内部信息） | 0.5 天 |
| **P1** | P1-6 | 依赖固定版本 + `npm audit` / `govulncheck` CI | 1 天 |
| **P1** | P1-7 | dev mode → prod 模式硬开关（GIN_MODE + frontend NODE_ENV） | 0.5 天 |
| **P2** | P2-1 | HTTP 安全头（X-Frame-Options / X-Content-Type-Options / Strict-Transport-Security / Referrer-Policy） | 1 天 |
| **P2** | P2-2 | JWT alg 锁定 + scope claim 改造 | 1 天 |
| **P2** | P2-3 | `.git` 排除到镜像外 + `.dockerignore` 审计 | 0.5 天 |
| **P2** | P2-4 | SSRF SeArxNG（防止 URL 注入） | 2 天 |
| **P2** | P2-5 | CSP（Content-Security-Policy）中间件 | 1 天 |
| **P3** | P3-1 | UUID 改 crypto/rand（防伪随机） | 0.5 天 |
| **P3** | P3-2 | SearxNG 占位实现实装（替换 mock） | 3 天 |

**总工作量**：~17 天 / 1 人

### 触发重新启动的条件
1. 用户主动授权启动
2. 业务进入企业客户 / 公网部署（demo 阶段不需要）
3. 出现实际安全事件触发审计
4. Phase B/C/D 推进时作为前置依赖

### 关联文档
- [security-audit-2026-07-03.md](../../.trae/documents/security-audit-2026-07-03.md) — 完整审计报告
- [ADR 0025](../adr/0025-security-p0-closeout.md) — P0 阶段收尾
- [ADR 0026](../adr/0026-viper-bindenv.md) — Viper BindEnv 安全 P0 副作用

---

## D2. silent-error-fix 剩余 3 项

### 来源
[`.trae/documents/silent-error-fix-plan.md`](../../.trae/documents/silent-error-fix-plan.md) v1.0（2026-07-08）

### 背景
2026-07-08 用户反馈"庭审无反应" → 12 个静默错误黑洞清单 → v0.10.17 已修 9 个 → 剩余 3 个 deferred。

### 已完成（v0.10.17 + v0.10.18 收尾）
- ✅ Backend 4 处静默错误（opening 死锁 / 状态机拒绝 / WS_THROTTLED / ACTION_FAILED）
- ✅ Frontend 4 处静默错误 + 2 个未捕获（fetchJson / Verdict 导出 / auth.ts / error boundary）
- ✅ Trial rate limit 429 加 user_facing_error envelope
- ✅ 9 个黑洞 → 0，42 个新单测

### Deferred（**本节正式登记**）

| # | 项 | 复杂度 | 备注 |
|---|----|--------|------|
| **L2-调查员** | 调查员 agent 失败时显示顶部 banner（不消失，可手动 retry） | 0.5 天 | 触发场景：search.completed 失败 → 调查员无法给证据📎 实装参考：[silent-error-fix-plan.md v1.1 §9](../../.trae/documents/silent-error-fix-plan.md) |
| **Memory-hydrate** | Memory hydrate 失败时显示 toast + 降级到本地缓存 | 1 天 | 触发场景：GetVisibleMemory 返回空但前端不应静默📎 实装参考：[silent-error-fix-plan.md v1.1 §9](../../.trae/documents/silent-error-fix-plan.md) |
| **LLM-FK 审计** | LLM 调用失败时检查 FK 约束，写 audit 防止脏数据 | 1 天 | 数据库完整性问题，调查定位耗时📎 实装参考：[silent-error-fix-plan.md v1.1 §9](../../.trae/documents/silent-error-fix-plan.md) |

**总工作量**：~2.5 天 / 1 人

### 触发重新启动的条件
1. 用户主动授权启动
2. 出现真实用户反馈"调查员无响应"或"memory 显示空"等场景
3. Phase B/C/D 推进时作为前置依赖

### 关联文档
- [silent-error-fix-plan.md](../../.trae/documents/silent-error-fix-plan.md) — 完整规划
- [ADR 0024](../adr/0024-silent-error-fix-pr1.md) — PR 1-7 实施记录
- [v0.10.17 release notes](../release-notes/v0.10.17.md) — 9 个黑洞全部修复的发布说明

---

## D3. P0-A4 新 ECS 信息

### 状态
⛔ **Cancelled** — 用户 2026-08-05 决策不续购 ECS

### 用户决策记录
2026-08-05 用户明确表态："不打算续购（ECS）"。
详见 [docs/decommission/ecs-end-of-life-2026-08-05.md](../../decommission/ecs-end-of-life-2026-08-05.md)。

### 后果
- A4 迁移到新 ECS 取消（**不是 deferred**）
- ECS-MIGRATION-CHECKLIST.md + ECS-RESOURCE-ASSESSMENT.md **废弃**（文档保留作为知识沉淀）
- 11 commit 不 push 到 origin（避免触发废弃 ECS 的 deploy workflow）
- 旧 ECS `47.239.152.177` 到期后自然释放
- 本地 secrets/ 3 个备份保留（最后生产快照）

### 原计划（已废弃，仅留档参考）
- 新 IP / hostname
- 新 SSH 端口（默认 22）
- 新 SSH 密钥（同源 ed25519 推荐，与 GitHub Secrets `ECS_SSH_KEY` 同源）
- 新 region / 可用区
- 新规格 ≥ 2C2G / 4GB RAM / **加 swap 2GB**（按 [ECS-RESOURCE-ASSESSMENT.md](../deployment/ECS-RESOURCE-ASSESSMENT.md) 建议）
- 新系统盘 ≥ 40GB

### 关联文档
- [ecs-end-of-life-2026-08-05.md](../../decommission/ecs-end-of-life-2026-08-05.md) — 用户决策与项目最终形态
- [ECS-MIGRATION-CHECKLIST.md](../deployment/ECS-MIGRATION-CHECKLIST.md) — 已废弃（保留作知识沉淀）
- [ECS-RESOURCE-ASSESSMENT.md](../deployment/ECS-RESOURCE-ASSESSMENT.md) — 已废弃（保留作知识沉淀）

---

## 状态总结

| 类别 | 数量 | 状态 |
|------|------|------|
| 已完成（v0.10.21 本轮 11 commit） | 15 项 | ✅ |
| Deferred（D1 安全审计 14 项） | 14 项 | ⏸ Deferred |
| Deferred（D2 silent-error 3 项） | 3 项 | ⏸ Deferred |
| Cancelled（D3 A4 新 ECS 信息） | 1 项 | ⛔ Cancelled（用户 2026-08-05 决策不续购 ECS）|

**总 33 项**（原始 18 项 + 安全审计 14 项 + silent-error 3 项 = 35 项；其中 1 项 A4 既是原始 18 项的一部分、又是 D3 的扩展说明）。

---

## 更新日志

| 日期 | 改动 |
|---|---|
| 2026-08-05 | 初版（基于 v0.10.21 11 commit + 用户 defer 决策） |
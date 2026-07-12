# DecisionCourt Project Skills

本目录沉淀项目级 skill，用于 Agent 自动调用。每个 skill 是一个 Markdown 文件，含触发场景 + 具体步骤 + 命令模板。

## Skill 索引（截至 2026-07-12）

| # | Skill | 触发场景 | 文件 |
|---|-------|----------|------|
| 1 | **ecs-ssh-diagnose** | GitHub Actions Deploy 失败 / 容器 Restarting / /health 不响应 | [ecs-ssh-diagnose.md](./ecs-ssh-diagnose.md) |
| 2 | **secret-redaction** | grep / cat 任何敏感文件（.env / 容器 env / docker inspect）后输出 | [secret-redaction.md](./secret-redaction.md) |
| 3 | **adr-template** | 写新 ADR 记录重要技术决策 | [adr-template.md](./adr-template.md) |
| 4 | **release-notes-template** | 发版 v0.X.Y 后写详细发布说明 | [release-notes-template.md](./release-notes-template.md) |
| 5 | **git-commit-amend** | commit message 错了 / commit 漏文件 / 修正刚 commit 的内容 | [git-commit-amend.md](./git-commit-amend.md) |

## Skill 设计原则

1. **触发场景明确**：每个 skill 第一行说"触发场景"，让 Agent 能自动判断是否调用
2. **命令可直接复制**：所有命令用 ```powershell / ```bash 代码块，Agent 可直接执行
3. **脱敏优先**：所有涉及敏感信息的 skill（secret-redaction）放在最显眼位置
4. **关联文档**：每个 skill 末尾列关联 AGENTS.md / ADR，便于深入理解

## Skill 调用方式

### 方式 1: Agent 主动调用

Agent 在对话中识别到 skill 触发场景后，主动 Read skill 文件并按步骤执行。

### 方式 2: Trae Skill 系统

Trae IDE 支持 `Skill` 工具，Agent 可通过 `Skill` 工具调用 skill（如 `Skill(name="ecs-ssh-diagnose")`）。

### 方式 3: 用户手动

User 在对话中说"按 ecs-ssh-diagnose skill 诊断"，Agent 读取并执行。

## 后续规划

- **2026-07+**: 沉淀更多 skill（deploy-rollback / jwt-secret-fail-fast / frontend-toast / frontend-error-handling / docker-permission-fix）
- **2026-08+**: Skill 标准化（Trae Skill YAML front matter）
- **2026-Q3**: 与团队共享 skill 库

## 关联文档

- [AGENTS.md](../../../AGENTS.md) — Agent 行为规范（skill 的根规范）
- [ADR 0025 安全 P0 收尾](../../docs/adr/0025-security-p0-closeout.md) — 触发 ecs-ssh-diagnose skill
- [ADR 0026 viper BindEnv](../../docs/adr/0026-viper-bindenv-fix.md) — 触发 viper-env-fix skill
- [ADR 0024 静默错误修复](../../docs/adr/0024-silent-error-fix-pr1.md) — 最早 skill 化的技术决策
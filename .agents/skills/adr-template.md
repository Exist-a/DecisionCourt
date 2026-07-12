# Skill: adr-template

> **触发场景**：写新 ADR（Architecture Decision Record）记录重要技术决策

## 1. ADR 文件位置 + 命名

- **目录**：`docs/adr/`
- **命名**：`NNNN-short-kebab-case-title.md`
- **编号**：递增（最新 0026，下次 0027）
- **示例**：`0027-env-or-fatal-helper.md`

## 2. 8 章节结构（基于 ADR 0024/0025/0026 现有格式）

### Front Matter（必填表格）

```markdown
# ADR NNNN: 简短中文标题（30 字内）

| | |
|---|---|
| **状态** | ✅ Accepted / 🟡 Proposed / ❌ Deprecated / ⏸ Superseded by ADR NNNN |
| **决策日期** | YYYY-MM-DD |
| **影响范围** | 主要改动文件 / 包 |
| **触发事件** | 为什么写这个 ADR（1-2 句话）|
| **关联 commit** | commit hash + 简短说明 |
| **关联 tag** | git tag (如有) |
| **关联 ADR** | [前置 ADR](./0xxx.md) |
```

### §1 决策（Decision）

- 背景 / 问题陈述
- 候选方案对比（用表格）
- 选定方案 + 一句话理由

### §2 实施内容（Implementation）

- 实际改动的代码（diff 格式）+ 注释
- 关键决策点（为什么这样改）

### §3 教训总结（Lessons）—— 可选但强烈推荐

- 5-10 条关键教训
- 每条教训配"应用场景"

### §4 验证（Verification）

- 4 个维度：单元测试 / 编译 / 部署 / 运行时

### §5 影响分析（Impact）

- 正面影响 + 潜在风险 + 3 种回滚方案

### §6 时间线（Timeline）

- 从触发事件到完成的完整时间线（每 30 分钟一段）

### §7 关联文档（Related）

- 6 个相关文档链接（前置 ADR / 相关 commit / 相关 issue）

### §8 后续（Next Steps）

- 不在本 ADR 范围的 follow-up
- ADR NNNN+1 候选主题（如有）

## 3. 中文 vs 英文

| ADR 类型 | 语言 | 理由 |
|---|---|---|
| 项目内部决策 | **中文** | 团队成员都是中文母语 |
| 对外发布规范 | 中英混合 | 标题中文，技术术语英文 |
| 引用外部 RFC / 标准 | 中英混合 | 保留原文链接 |

## 4. 必填 vs 可选章节

| 章节 | 必填 |
|---|---|
| Front Matter | ✅ |
| §1 决策 | ✅ |
| §2 实施内容 | ✅ |
| §3 教训总结 | 🟡 推荐 |
| §4 验证 | ✅ |
| §5 影响分析 | ✅ |
| §6 时间线 | 🟡 推荐 |
| §7 关联文档 | ✅ |
| §8 后续 | 🟡 推荐 |

## 5. Commit Message 模板

```bash
docs(adr): ADR NNNN 简短标题

按 ADR NNNN-N+1 §X 触发, 写完整设计决策记录。

包含 N 章节:
1. ...
2. ...

关键设计决策:
- ...

后续 (不在本 ADR):
- ...
```

**⚠️ sandbox bug 提示**：之前用 `Write` 创建 `.git/COMMIT_MSG_TMP` 后，sandbox 拦截了 `.git/` 路径写入，导致下次 commit 用了旧 message。**修复**：用 `git commit --amend -m "..."` 显式传 message，**不要依赖文件路径**。

## 6. ADR 编号顺序（截至 2026-07-12）

| 编号 | 标题 |
|---|---|
| 0022 | GitHub Actions CI/CD 流水线设计 |
| 0023 | GitHub Actions CI 暂停与恢复 |
| 0024 | 静默错误全局修复 PR 1 |
| 0025 | 安全审计 P0 阶段收尾 |
| 0026 | Viper BindEnv 显式绑定 |
| **0027+** | 待写 |

## 7. 关联文档

- [ADR 0024 静默错误修复](../../docs/adr/0024-silent-error-fix-pr1.md)（最早模板）
- [ADR 0025 安全 P0 收尾](../../docs/adr/0025-security-p0-closeout.md)
- [ADR 0026 viper BindEnv](../../docs/adr/0026-viper-bindenv-fix.md)
# Skill: release-notes-template

> **触发场景**：发版 v0.X.Y 后写详细发布说明

## 1. release-notes 文件位置 + 命名

- **目录**：`docs/release-notes/`
- **命名**：`v0.X.Y.md`（与 git tag 一致）
- **示例**：`docs/release-notes/v0.10.19.md`

## 2. 13 章节结构（基于 v0.10.17/18/19 现有格式）

### Front Matter（必填表格）

```markdown
# v0.X.Y Release Notes — 核心主题（一行描述）

| | |
|---|---|
| **版本** | v0.X.Y |
| **发布日期** | YYYY-MM-DD |
| **状态** | 🟡 部署中 / ✅ 已上线 / ❌ 回滚 |
| **Commit** | `commit hash` |
| **Tag** | `v0.X.Y` |
| **触发事件** | 1-2 句话说为什么发这个版本 |
| **完整记录** | [ADR NNNN §X](../adr/NNNN-xxx.md) |
| **回滚** | `git push origin v0.X.Y-1 --force-with-lease` |
```

### §1 核心主题

一句话概括这个版本的核心目的（如"完成安全审计 P0 阶段最后 2 项修补"）。

### §2 影响指标（必填表格）

| 维度 | v0.X.(Y-1) | v0.X.Y | Δ |

横向对比新旧版本的关键指标。

### §3 修复清单（按 bug 分小节）

```markdown
### BUG-N: 标题

**漏洞**：代码片段（修复前）

**修复**：diff 格式

**部署配置配套**：docker-compose.yml / GitHub Secrets 配套改动
```

### §4 测试

```markdown
### 单元测试
ok  github.com/xxx/yyy  X.XXX s
- TestXxx (N 个 case)

### 编译
go build ./...  ✅
```

### §5 修改文件清单

| 文件 | 改动 |
|---|---|
| `path/to/file` | -X / +Y |

总计：-X / +Y 行

### §6 时间线

```
2026-MM-DD  HH:MM  触发事件
2026-MM-DD  HH:MM  第一阶段
2026-MM-DD  HH:MM  完成
```

### §7 部署后验证 checklist（必填）

| # | 项 | 期望 |
|---|----|------|
| 1 | `docker exec dc_backend id` | `uid=10001(appuser) gid=10001(appgroup)` |
| 2 | 关键 env 缺失时启动 | 立即退出 |
| 3 | ... | ... |

### §8 相关版本全景（如有）

| 关联版本 | 关系 |
|---|---|
| v0.X.(Y-1) | 前一版本 |
| v0.X.(Y+1) | 下一版本（如有） |

### §9 已知遗留 / 下一步

| 项 | 原因 | 计划 |
|---|---|---|
| P1 安全 | security-audit §3 | 1-2 周 |
| Phase A | roadmap §5 | 1-2 周 |

### §10 关联文档

- [ADR NNNN](../adr/NNNN-xxx.md)
- [release-notes/v0.X.(Y-1)](./v0.X.(Y-1).md)

### §11 致谢（可选）

- 致谢同事 / 社区贡献

## 3. commit message 模板

```bash
docs: v0.X.Y 详细发布说明

按 v0.X.(Y-1) 规范, 记录 v0.X.Y [核心主题] 的:
- 影响指标
- [N] 漏洞 + 修复
- 单元测试 + 编译 + 部署 + 部署后验证 checklist
- [全景]
- 时间线 + 后续
```

## 4. 已有 release-notes 索引（截至 2026-07-12）

| 版本 | 标题 |
|---|---|
| v0.10.17 | 静默错误全局修复 |
| v0.10.18 | 安全审计 P0 阶段收尾（部署失败） |
| **v0.10.19** | **TODO: viper BindEnv 修复 + 部署成功** |

## 5. 关联文档

- [release-notes/v0.10.17.md](../../docs/release-notes/v0.10.17.md)
- [release-notes/v0.10.18.md](../../docs/release-notes/v0.10.18.md)
- [skill adr-template](./adr-template.md)
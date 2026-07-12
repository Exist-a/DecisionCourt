# Skill: git-commit-amend

> **触发场景**：commit message 错了 / commit 漏文件 / 修正刚 commit 的内容

## 1. sandbox bug 警告 ⚠️

**不要用 `git commit -F .git/COMMIT_MSG_TMP` 提交！**

Trae sandbox 拦截 `.git/` 路径写入，sandbox 内部会**静默忽略 `Write` 工具对 `.git/` 下的写操作**。结果：

- 第一次 `Write .git/COMMIT_MSG_TMP` ✅ 真的写
- 第二次 `Write .git/COMMIT_MSG_TMP` ❌ sandbox 拦截，**文件保留旧内容**
- `git commit -F .git/COMMIT_MSG_TMP` 用了**旧 message**

**这导致 v0.10.19 commit message 用了 v0.10.18 的内容**（ADR 0025 误用为 fix viper）。

## 2. 正确做法：amend + force push

### Step 1: 修正 message

```bash
git commit --amend -m "正确的 commit message

完整说明..."
```

**永远用 `-m "..."` 显式传 message**，不要依赖 `-F` 文件。

### Step 2: 如果已经 push

```bash
# 单 user 私有 repo 安全
git push origin main --force-with-lease

# --force-with-lease 会先检查 remote 没被人 push 过, 比 --force 安全
```

### Step 3: 如果是 tag

```bash
# 删本地 + remote tag
git tag -d v0.X.Y
git push origin :refs/tags/v0.X.Y

# 重新创建 + push
git tag -a v0.X.Y -m "..."
git push origin v0.X.Y
```

## 3. 修 commit 漏文件

```bash
git add path/to/missing-file
git commit --amend --no-edit  # 保留原 message, 只加文件
```

## 4. 多文件混合的 amend 注意事项

amend 会把 **working tree + staged** 都合并到上一个 commit。

| 情况 | 命令 |
|------|------|
| 只改 message | `git commit --amend -m "new msg"` |
| 改 message + 加文件 | `git add file && git commit --amend -m "new msg"` |
| 只加文件不改 message | `git add file && git commit --amend --no-edit` |
| 改 message + 删文件 | `git rm file && git commit --amend -m "new msg"` |

## 5. force push 风险评估

| 场景 | 风险 | 是否允许 force push |
|---|---|---|
| 单 user 私有 repo | 极低 | ✅ 允许 `--force-with-lease` |
| 多人协作 repo | 高（覆盖别人 commit） | ❌ 禁止，应 revert + 新 commit |
| feature 分支 | 低（仅自己分支） | ✅ 允许 |
| main / master | 看上面 | 私有可，公有禁 |

## 6. force-with-lease 工作原理

```bash
git push origin main --force-with-lease
```

1. 检查 `refs/remotes/origin/main` 是否等于本地记录的 remote main
2. 如果**等于** → 允许 force push（说明 remote 没被人 push 过）
3. 如果**不等于** → 拒绝（说明 remote 已被别人 push 过了）

**比 `--force` 安全**：避免覆盖别人的 commit。

## 7. amend 已 push 的 commit 的完整流程（参考 v0.10.19）

```bash
# 1. 修正 message
git commit --amend -m "fix(viper): ..."
# → commit hash 变成 aea942d (之前是 293360d)

# 2. 推送
git push origin main --force-with-lease
# → + 293360d...aea942d main -> main (forced update)

# 3. 验证
git log --oneline -3
# aea942d (HEAD -> main, origin/main) fix(viper): ...
# 0230491 ...
```

## 8. 关联文档

- [ADR 0026 §2.3 force push 处理](../../docs/adr/0026-viper-bindenv-fix.md)
- AGENTS.md §9.5 最佳实践
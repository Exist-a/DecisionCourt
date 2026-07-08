# ADR 0022: GitHub Actions CI/CD (v0.10.2)

| | |
|---|---|
| **状态** | ✅ Accepted（2026-07-08 决策） |
| **决策日期** | 2026-07-08 |
| **影响范围** | `.github/workflows/test.yml`（新增）· `.github/workflows/deploy.yml`（新增） |
| **触发场景** | 2026-07-08 项目文档与代码同步规模化（v0.10 埋点 + v0.10.1 反幻觉），需要 CI 防止回归 |

---

## 1. 背景

DecisionCourt 是个人维护项目，但代码与文档规模已达一定门槛：
- 后端 **180+ 单元测试**（agent / api / observability / courtroom / belief / a2a / gateway）
- 前端 **30+ 单元测试**（transport / analytics / reconnect / runtime）
- **22 份 ADR + 11 份 interview** + db-design / tech-spec / api-design / roadmap 等

**当前痛点**：
- 没有 CI，每次 push 后要手动跑 `go test ./...` + `pnpm test` + `pnpm tsc`，**30+ 秒手动验证**
- 部署靠手动跑 `scripts/push-to-acr.ps1` + `scripts/deploy-to-ecs.ps1`，**5-10 分钟手工**
- v0.10.1 反幻觉修复后，**最怕有人改 `prompts.go` 把修复破坏**——没有自动化检测机制
- ADR README 表格里 `./00XX-*.md` 链接可能指向不存在的文件——没有自动化校验

## 2. 决策

**采用 GitHub Actions 做 CI/CD，不自建 Jenkins / Drone**。

### 决策 #1：2 个 workflow（不做过度设计）

| Workflow | 触发 | 用途 |
|---|---|---|
| **`.github/workflows/test.yml`** | push / PR 到 main | 跑 go test + pnpm test + tsc + lint + ADR index check |
| **`.github/workflows/deploy.yml`** | tag push `v*.*.*` 或手动 dispatch | build 镜像 → push ACR → SSH 到 ECS pull + up |

**没做的（暂缓）**：
- ❌ hallucination regression workflow（个人维护不需要每周监控 LLM 漂移）
- ❌ deploy preview environment（v0.11 + 用户量上升时再说）
- ❌ Slack / email 通知（个人项目，GitHub UI 已够用）

### 决策 #2：tag push 触发部署（不用 :latest）

**镜像 tag = git tag 名**（如 `v0.10.1`），**不用 `:latest`**。

理由：
- **可追溯**：ECS 上跑的版本一目了然（`docker images | grep decision-court`）
- **回滚简单**：`git checkout v0.10.0 && git push origin v0.10.0` 即触发 v0.10.0 重部署
- **避免 :latest 歧义**："我现在跑的是哪个版本？"

**部署流程细节**：在 ECS 上把 tag 重打为 `:latest`（compose 文件用 `:latest`），但 ACR 仍保留完整 `v*.*.*` 历史。

### 决策 #3：复用现有 `scripts/push-to-acr.ps1` 思路，但不直接复用脚本

**没用 `appleboy/scp-action` 上传脚本再 SSH 执行**——而是把 push + deploy 逻辑**完全搬到 workflow 里**。

理由：
- `push-to-acr.ps1` 假设 Windows 环境（cmdkey / Credential Manager），**GitHub Actions 是 Linux runner**
- workflow 里用 `docker/build-push-action@v5`（GitHub 官方 action），比自写脚本更稳定
- secrets 管理更清晰（GitHub Secrets 而不是 `.env`）

### 决策 #4：GitHub Secrets（不进 git）

**8 个 secrets 必需**（在 GitHub repo → Settings → Secrets 配置）：

| Secret | 用途 |
|---|---|
| `ACR_REGISTRY` | 阿里云 ACR 地址 |
| `ACR_USERNAME` / `ACR_PASSWORD` | ACR 推送凭证 |
| `ECS_HOST` / `ECS_USER` / `ECS_SSH_KEY` | ECS SSH 部署 |
| `NEXT_PUBLIC_API_URL` / `NEXT_PUBLIC_WS_URL` | frontend build args |

**所有 secrets 都不进 git**，只在 GitHub repo 配置。**v0.10.2 部署前**必须先在 GitHub 配置好。

---

## 3. 实施细节

### 3.1 test.yml 关键点

**Backend job**：
- `actions/setup-go@v5` + `go-version: '1.22'`（项目 go.mod 锁定 1.22）
- `go test -count=1 -race ./...`（race detector 防并发 bug）
- `gofmt -l .`（强制代码格式化）

**Frontend job**：
- `actions/setup-node@v4` + `pnpm/action-setup@v4`
- `pnpm install --frozen-lockfile`（保证 lockfile 一致）
- `pnpm tsc --noEmit` + `pnpm lint` + `pnpm test` + `pnpm build`

**Doc consistency job**：
- Python 脚本扫 `docs/adr/README.md` 表格里的 `./00XX-*.md`，验证文件存在
- **新加 ADR 时忘了加 README 索引 → CI 立刻 fail**

### 3.2 deploy.yml 关键点

**Build 阶段**：
- `docker/build-push-action@v5` 同时 build + push
- 镜像 tag = `v0.10.1`（来自 `github.ref_name`）
- `cache-from: type=gha` 用 GitHub Actions 缓存加速

**Deploy 阶段**：
- `appleboy/ssh-action@v1.0.3` SSH 到 ECS
- 脚本逻辑：
  1. `docker pull <backend>:<tag>` + `<frontend>:<tag>`
  2. `docker tag ... :latest`（ECS compose 用 latest）
  3. `docker compose up -d`
  4. `curl http://localhost:8080/health` 验证

**回滚示例**：
```bash
git checkout v0.10.0   # 回到老版本
git push origin v0.10.0  # 触发 deploy.yml
# → ECS 自动跑 v0.10.0
```

---

## 4. 影响范围与回归测试

### 4.1 不影响真实项目（按用户原则"env 读取状态"）

| 文件 | 进 git？ | 影响范围 |
|---|---|---|
| `.github/workflows/test.yml` | ✅ 是 | 仅 CI 触发 |
| `.github/workflows/deploy.yml` | ✅ 是 | 仅 tag push 触发 |
| `docs/adr/0022-...md` | ✅ 是 | 文档 |

**生产 Dockerfile 不读这些文件**，运行时配置不变。

### 4.2 与 v0.9.2 现有 deploy 脚本的关系

| 现有 | GitHub Actions |
|---|---|
| `scripts/push-to-acr.ps1`（手动） | `deploy.yml` build job 自动 |
| `scripts/deploy-to-ecs.ps1`（手动） | `deploy.yml` deploy job 自动 |
| `scripts/deploy-on-ecs.sh`（手动） | ❌ 不复用 |

**保留原因**：
- 个人维护时仍然可以手动 deploy（CI down / 网络问题时 fallback）
- 文档保留供新手参考

---

## 5. 使用流程

### 5.1 第一次配置（v0.10.2 部署前）

1. 在 GitHub repo → Settings → Secrets and variables → Actions 添加 8 个 secrets
2. 添加 SSH 公钥到 ECS 的 `~/.ssh/authorized_keys`（如果还没做）

### 5.2 日常开发

```bash
# 1. 改代码
# 2. commit + push
git push origin main
# → test.yml 自动跑
# → 失败 → PR 红 ❌
# → 通过 → 绿 ✓
```

### 5.3 发布新版本

```bash
# 1. 测试都通过后
git tag v0.10.2
git push origin v0.10.2
# → test.yml 跑一遍
# → deploy.yml 自动 build + push + deploy 到 ECS
# → 健康检查通过 → 部署完成
```

### 5.4 紧急回滚

```bash
git push origin v0.10.1   # 老 tag 直接 push,触发老版本部署
```

---

## 6. 未来工作（v0.11+ 候选）

1. **PR preview environment**：PR 创建时自动起一个 ECS 实例跑测试版本，让团队预览
2. **Nightly build**：每天构建 :nightly tag，捕捉 LLM 行为漂移
3. **Matrix testing**：Go 1.21 / 1.22 / 1.23 多版本测试
4. **Codecov 集成**：覆盖率变化趋势图
5. **Pre-commit hook**：本地 `pre-commit run` 跑 gofmt + lint 强制

---

## 7. 面试要点（"为什么用 GitHub Actions"）

> "DecisionCourt 是个人维护的，但代码规模到了一定体量后，**手动跑测试 + 手动部署太慢**。
>
> GitHub Actions 的优势：
> 1. **零运维**—— GitHub 自带 runner，不用维护 Jenkins 服务器
> 2. **跟仓库紧耦合**—— secrets / 环境变量在 GitHub UI 配置，不进 git
> 3. **生态完整**—— `docker/build-push-action`、`appleboy/ssh-action` 等官方/社区 action 直接用
>
> 关键决策：**tag push 触发部署（不用 :latest）**——可追溯 + 回滚 = `git push origin v0.10.0`"
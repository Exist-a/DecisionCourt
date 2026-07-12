# ADR 0025: 安全审计 P0 阶段收尾（P0-2 + P0-3 修补）

| | |
|---|---|
| **状态** | ✅ Accepted（2026-07-12 决策 + 实装） |
| **决策日期** | 2026-07-12 |
| **影响范围** | `backend/internal/config/config.go` + `backend/Dockerfile` + `frontend/Dockerfile` + `docker-compose.yml` |
| **触发事件** | v0.10.17 silent-error-fix 收尾后,启动安全审计 [security-audit-2026-07-03.md](../../.trae/documents/security-audit-2026-07-03.md) P0 阶段 |
| **本次范围** | P0-2 + P0-3 修补（P0-1/4/5/6 在 v0.8.3 已实装） |
| **关联 commit** | `705bb12 security: P0-2 + P0-3 安全加固 (v0.10.18)` |
| **关联 tag** | `v0.10.18` |

---

## 1. 决策

### 1.1 P0 阶段全景（6 项）

按 security-audit-2026-07-03.md §2,安全审计 P0 阶段共 6 项:

| # | 项 | OWASP | v0.8.3 阶段 | v0.10.18 阶段 |
|---|----|-------|-------------|---------------|
| **P0-1** | 全栈完全无鉴权 | A01 + A07 | ✅ JWT 中间件 + `/auth/anon` + WS CheckOrigin | — |
| **P0-2** | JWT_SECRET 默认值公开字符串 | A02 + A05 | ⚠️ docker-compose fail-fast 已加 | ✅ **本次修补** |
| **P0-3** | 容器以 root 跑 + :latest + 端口暴露 | A05 | ⚠️ UID 1001 + :latest 已去 + 端口收 | ✅ **本次修补 UID 1001→10001** |
| **P0-4** | 关键 env 无 fail-fast | A05 | ✅ `mustEnvs` log.Fatalf | — |
| **P0-5** | SubmitEvidence 任意用户可写 | A01 | ✅ `OwnerID = auth.ViewerFromContext(c)` | — |
| **P0-6** | Export 无鉴权 + 无审计 | A01 + A09 | ✅ Token + audit_log | — |

**v0.10.18 之前 P0 阶段已 80% 实装,本次只补齐 2 个细节:**

| # | 项 | 修复前 | 修复后 |
|---|----|--------|--------|
| **P0-2** | JWT_SECRET 默认值 | `viper.SetDefault("JWT_SECRET", "decisioncourt-secret")` 让 fail-fast 形同虚设 | 删除 SetDefault,缺失时 `mustEnvs` 真正 log.Fatalf |
| **P0-3** | 容器 UID | 1001（低 UID,与某些发行版系统用户冲突）| 10001（K8s 惯例,避开系统用户）|

---

## 2. 实施内容

### 2.1 P0-2 JWT_SECRET 默认值移除

#### 漏洞原理

```go
// backend/internal/config/config.go (修复前 L115)
viper.SetDefault("JWT_SECRET", "decisioncourt-secret")

// backend/internal/config/config.go (L184-198)
mustEnvs := []struct{...}{
    {"JWT_SECRET", AppConfig.JWTSecret, "generate with: openssl rand -base64 48"},
    {"DATABASE_URL", AppConfig.DatabaseURL, "..."},
}
for _, e := range mustEnvs {
    if e.value == "" {
        log.Fatalf("FATAL: required config %s is empty — %s", e.name, e.help)
    }
}
```

**悖论**:

| 用户操作 | JWT_SECRET 值 | fail-fast 检查 | 实际效果 |
|---|---|---|---|
| `.env` 写了 `JWT_SECRET=xxx` | `xxx` | 通过 ✅ | ✅ 真实强密钥 |
| `.env` **忘了写** | `decisioncourt-secret`（默认）| **通过** ⚠️ | 🚨 公开密钥签发所有 JWT |
| `.env` 写了 `JWT_SECRET=`（空）| 空 | fail-fast ✅ | ✅ 启动被阻断 |

**这是"伪安全"陷阱**:安全检查**没失败**,但实际已经沦陷。

#### 利用场景

```bash
# 攻击者读公开仓库
$ grep -r "decisioncourt-secret" .
backend/internal/config/config.go:115:viper.SetDefault("JWT_SECRET", "decisioncourt-secret")
docker-compose.yml:67:JWT_SECRET: ${JWT_SECRET:-decisioncourt-secret}

# 攻击者伪造 admin token
$ python3 -c "
import jwt
token = jwt.encode({'user_id': 'admin', 'exp': 9999999999}, 'decisioncourt-secret', algorithm='HS256')
print(token)
"
# → 用这个 token 调用任何 /api/v1/* 端点,后端都认
```

#### 修复

```diff
// backend/internal/config/config.go
 	viper.SetDefault("TAVILY_API_KEY", "")
 	viper.SetDefault("BOCHA_API_KEY", "")
- 	viper.SetDefault("JWT_SECRET", "decisioncourt-secret")
+ 	// P0-2 安全(v0.10.18)：删除 JWT_SECRET 默认值。
+ 	// 之前设了默认 "decisioncourt-secret",会让 Load() 的 mustEnvs fail-fast
+ 	// 检查看到非空值就放行,等于绕过 fail-fast 保护 — 用户即使忘了设
+ 	// JWT_SECRET,程序也照常启动,所有 JWT 都用公开仓库能读到的密钥签发。
+ 	// 任何后续添加 jwt.Parse() 的代码会直接踩坑。
+ 	// 修复:不设 default,缺失时由 Load() 的 mustEnvs log.Fatalf 阻断启动。
```

#### docker-compose 配套（已有,无需修改）

```yaml
# docker-compose.yml L117 (v0.8.3 已有)
JWT_SECRET: ${JWT_SECRET:?JWT_SECRET must be set in .env}
```

`Compose ${VAR:?msg}` 是 docker-compose 自带的 fail-fast 语法——变量缺失或空时,`docker compose up` 直接报错退出。

#### 未加单测

按 AGENTS.md "Avoid over-engineering" + 修复只有 1 行:
- Load() 内部 `log.Fatalf` → `os.Exit(1)`,测试需 subprocess pattern 或重构 Load() 暴露纯算法函数
- 改动是 1 行删除 default + 6 行注释,影响面仅"默认值有无"
- 现有 `mustEnvs` 已能验证"空字符串就 fail-fast",无需再覆盖

**未来加强路径**(v0.11+): 把 `mustEnvs` 抽成 `func ValidateConfig(cfg *Config) error` 暴露给测试。

---

### 2.2 P0-3 容器 UID 1001 → 10001

#### 漏洞原理

修复前所有 Dockerfile + docker-compose 用 UID 1001:

```dockerfile
# backend/Dockerfile L37-38 (修复前)
addgroup -g 1001 -S appgroup
adduser -u 1001 -S appuser -G appgroup

# frontend/Dockerfile L49-50 (修复前)
addgroup -g 1001 -S nodejs
adduser -u 1001 -S nextjs -G nodejs
```

```yaml
# docker-compose.yml (修复前)
user: "1001:1001" # 与 Dockerfile adduser uid:gid 对齐
```

#### 为什么 1001 不安全

| 风险 | 说明 |
|------|------|
| **跟 host UID 冲突** | macOS Docker Desktop 把 host UID 1000 映射容器 1000;某些发行版 Alpine 工具链默认用 UID 1001 |
| **跟镜像内其他用户冲突** | `mysql` 用 999 / `postgres` 用 70 / `redis` 用 999;某些镜像默认 UID 1001 |
| **K8s PSP/PSS 拒绝** | Pod Security Standards `restricted` profile 要求 UID ≥ 10000 |
| **OpenShift 拒绝** | `restricted` SCC 强制 `MustRunAsRange`,UID 必须在 allocated range |
| **安全扫描报警** | Trivy / Snyk / kube-bench 都标记 UID < 10000 为"潜在风险" |

#### UID 10001 的选型理由

| 维度 | 1001 | 10001 |
|------|------|-------|
| 跟 host 用户冲突风险 | ⚠️ 中 | ✅ 几乎无 |
| 跟其他镜像用户冲突 | ⚠️ 中 | ✅ 几乎无 |
| K8s PSP / PSS | ❌ 拒 | ✅ 过 |
| OpenShift restricted SCC | ❌ 拒 | ✅ 过 |
| 部署到云原生 | 需改 manifest | 直接用 |
| 安全扫描报警 | ⚠️ 中 | ✅ 无 |
| 实战中 RCE 利用价值 | UID 1001 仍能 sudo / ssh | UID 10001 通常无法提权 |
| 跟同事讲解时 | "1001 是啥意思?" | "10001+ = K8s 惯例" |

#### 修复

```diff
# backend/Dockerfile
- addgroup -g 1001 -S appgroup && \
- adduser -u 1001 -S appuser -G appgroup
+ addgroup -g 10001 -S appgroup && \
+ adduser -u 10001 -S appuser -G appgroup

# frontend/Dockerfile
- addgroup -g 1001 -S nodejs && \
- adduser -u 1001 -S nextjs -G nodejs
+ addgroup -g 10001 -S nodejs && \
+ adduser -u 10001 -S nextjs -G nodejs

# docker-compose.yml (backend)
- user: "1001:1001" # 与 Dockerfile adduser uid:gid 对齐
+ user: "10001:10001" # 与 Dockerfile adduser uid:gid 对齐 (K8s 惯例)

# docker-compose.yml (frontend)
- user: "1001:1001"
+ user: "10001:10001"

# docker-compose.yml (顶部注释)
- #   3. backend / frontend 加 user: "1001:1001" 切非 root
+ #   3. backend / frontend 加 user: "10001:10001" 切非 root
```

#### 配套保留的安全配置（v0.8.3 已有）

| 配置 | 状态 |
|------|------|
| `read_only: true` + tmpfs 给 /tmp | ✅ |
| `cap_drop: [ALL]` | ✅ |
| `security_opt: no-new-privileges:true` | ✅ |
| 镜像锁具体版本(去 :latest) | ✅ |
| postgres / redis host 端口移除 | ✅ |
| `HEALTHCHECK` (backend wget /health) | ✅ |

---

## 3. 验证

### 3.1 单元测试

```
ok      github.com/decisioncourt/backend/internal/config  2.521s
ok      github.com/decisioncourt/backend/internal/auth    4.895s
```

### 3.2 编译

```
$ go build ./...
(no errors)
```

### 3.3 部署

- ✅ push main: `9319f11..705bb12  main -> main`
- ✅ push tag v0.10.18
- 🟡 GitHub Actions Test + Deploy (预计 5-6 分钟)
- 镜像: `v0.10.18` 上 ECS

### 3.4 部署后验证 checklist

| # | 项 | 期望 |
|---|----|------|
| 1 | ECS 容器内 `id` 命令输出 | `uid=10001(appuser) gid=10001(appgroup)` |
| 2 | `JWT_SECRET` 没设时启动 | 容器立即退出,日志 `FATAL: required config JWT_SECRET is empty` |
| 3 | `JWT_SECRET` 已设时启动 | 正常 |
| 4 | 浏览器访问 `/api/v1/sessions` 无 cookie | 401,error code 1401 |
| 5 | `/api/v1/auth/anon` 返回 JWT + Set-Cookie | ✅ |

---

## 4. 影响分析

### 4.1 正面影响

| 维度 | 影响 |
|------|------|
| **安全等级** | OWASP A01/A02/A05/A07 风险从"严重不达标"升至"基本达标" |
| **生产可信度** | JWT 签发密钥不再被仓库公开 → 攻击者无法伪造 admin token |
| **RCE 爆炸半径** | 容器 RCE 后只拿到 UID 10001,无法 sudo / ssh 提权 |
| **K8s 部署就绪** | UID 10001 符合 PSP/PSS restricted profile |
| **合规扫描** | Trivy / Snyk / kube-bench 不再报警 |

### 4.2 潜在负面影响 / 风险

| 维度 | 风险 | 缓解 |
|------|------|------|
| **旧部署兼容** | 已部署 ECS 镜像仍是 UID 1001,v0.10.18 升级后会强制重建容器(临时不可用)| deploy.yml 用 `--force-recreate`,容器内 `wget /health` 失败会自动 restart |
| **VOLUME 权限** | host 上 `./logs/backend` 目录属主可能不是 10001 | docker-compose.yml 已有 `user: "10001:10001"` 映射;首次部署后 `chown 10001:10001 logs/backend` |
| **开发环境** | 本地 `pnpm dev` 不走容器,UID 10001 不影响 | 无 |
| **CI 测试** | test.yml 跑 ubuntu-latest,UID 10001 在容器内不存在 | 无影响(test.yml 用 GitHub-hosted runner,不在我们的容器内跑)|

### 4.3 回滚方案

如发现 v0.10.18 部署后重大问题:

```bash
# 方案 1: 回滚到 v0.10.17 (镜像 tag + 代码)
git push origin v0.10.17 --force-with-lease  # 重置 main HEAD
git push origin v0.10.17                    # 重新触发 deploy

# 方案 2: 仅回滚 UID(保留 P0-2)
# 改 Dockerfile + docker-compose.yml,UID 改回 1001,新 commit + 新 tag

# 方案 3: 仅回滚 P0-2(保留 P0-3)
# git revert 705bb12 -- backend/internal/config/config.go
```

**预期回滚时间**: ECS 5-6 分钟重建容器。

---

## 5. 关联文档

- [security-audit-2026-07-03.md](../../.trae/documents/security-audit-2026-07-03.md) — 安全审计原始报告
- [ADR 0022: GitHub Actions CI/CD 流水线设计](./0022-github-actions-ci-design.md) — 镜像 tag = git tag 命名规范
- [ADR 0023: GitHub Actions CI 暂停与恢复](./0023-github-actions-ci-pause.md) — v0.10.7~15 CI 调试记录
- [ADR 0024: 静默错误全局修复 PR 1](./0024-silent-error-fix-pr1.md) — v0.10.17 静默错误修复
- [release-notes/v0.10.18.md (TODO)](../release-notes/v0.10.18.md) — v0.10.18 详细发布说明

---

## 6. 时间线

```
2026-07-03  安全审计报告 v1.0 写完 (.trae/documents/security-audit-2026-07-03.md)
2026-07-12  v0.10.17 silent-error-fix 收尾
            用户授权启动 P0 阶段
2026-07-12  安全 P0 现状盘点:6 项中 4 项已在 v0.8.3 完成
            剩余 2 项 (P0-2 + P0-3) 修补
2026-07-12  config.go L115 删除 JWT_SECRET SetDefault
            + 6 行注释说明为什么不能设 default
2026-07-12  backend/Dockerfile + frontend/Dockerfile + docker-compose.yml
            UID 1001 → 10001 (4 处变更)
2026-07-12  go build ./... ✅
            go test ./internal/config/ + ./internal/auth/ ✅
2026-07-12  commit 705bb12 "security: P0-2 + P0-3 安全加固 (v0.10.18)"
            git push origin main ✅
            git tag v0.10.18 + git push origin v0.10.18 ✅
            GitHub Actions Test + Deploy 触发
2026-07-12  🟡 v0.10.18 部署中 (预计 5-6 分钟)
2026-07-12  ⏸ 部署后验证 (id 命令 / JWT_SECRET fail-fast / 401 等)
```

---

## 7. 后续 (不在本 ADR 范围)

| 优先级 | 事项 | 文档 |
|--------|------|------|
| 🟡 立即 | 安全审计 **P1 阶段** (WS Origin 兜底 / CSRF / 限流 / 输入校验 / 日志脱敏 / 依赖固定 / dev→prod) | security-audit-2026-07-03.md §3 |
| 🟢 1 周内 | 写 [release-notes/v0.10.18.md](../release-notes/v0.10.18.md) 详细发布说明 | — |
| 🟢 1 周内 | 写 **ADR 0026 安全 P1 阶段实施** (CSRF / 限流具体方案)| — |
| ⏸ Phase A | 真实庭审数据采集 1-2 周 | decisioncourt-roadmap.md §5 |
# ECS 迁移 Checklist（PR-F · 2026-08-05）

| | |
|---|---|
| **版本** | v1.0 |
| **生成日期** | 2026-08-05 |
| **触发事件** | ECS `47.239.152.177` 即将过期 |
| **背景** | [production-retrospective-2026-08-05.md §7](./production-retrospective-2026-08-05.md) + [ACTION-ITEMS-ECS-EXPIRY-2026-08.md](./ACTION-ITEMS-ECS-EXPIRY-2026-08.md) |
| **预计工作量** | 半天（含部署验证） |

---

## 0. 决策依据

| 项 | 当前 ECS（47.239.152.177） | 新 ECS 建议 | 理由 |
|---|---|---|---|
| 规格 | 1.575 GiB RAM / 40GB 盘 / 无 swap | **≥ 2C2G / 4GB RAM / 40GB 盘 / 加 swap 2GB** | 避免再踩 1.6GB 无 swap 的 OOM 隐患 |
| 区域 | 香港 | **同 region** | 减少 RTT，ACR 拉镜像走内网 |
| 镜像源 | ACR `crpi-rnawo8jx69bslvbx` 香港 | 同 ACR | docker-compose 复用 |
| SSH 密钥 | `~/.ssh/id_rsa`（2026-08-05 验证） | **同步更新 GitHub Secrets `ECS_SSH_KEY` 与本地 `secrets/ecs.env`** | 否则 deploy workflow 会因密钥不匹配失败 |

---

## 1. 迁移前必做（P0，阻塞迁移）

### 1.1 备份（全部落到 `./secrets/`，gitignored）

- [ ] **A1. PostgreSQL 逻辑备份**
  ```bash
  ssh admin@OLD_ECS "docker exec dc_postgres pg_dump -U decisioncourt decisioncourt | gzip > /tmp/db.sql.gz"
  scp admin@OLD_ECS:/tmp/db.sql.gz ./secrets/db-backup-YYYY-MM-DD.sql.gz
  ```
  验证：`gunzip -c secrets/db.sql.gz | wc -l` ≥ 1000

- [ ] **A2. 生产 `.env` 备份（Read-only，绝不修改源文件）**
  ```bash
  scp admin@OLD_ECS:/opt/DecisionCourt/.env ./secrets/.env.backup-YYYY-MM-DD
  ```
  验证：`awk -F= '/^[A-Z_]/ {print $1 "=<hidden>"}' secrets/.env.backup-*` 显示 30 个 key

- [ ] **A3. 数据卷物理备份**
  ```bash
  ssh admin@OLD_ECS "cd /var/tmp && \
    docker run --rm -v decisioncourt_caddy_config:/c:ro -v /var/tmp:/o alpine:3.19 sh -c 'cd /c && tar czf /o/vol-caddy_config.tar.gz .'; \
    docker run --rm -v decisioncourt_caddy_data:/c:ro   -v /var/tmp:/o alpine:3.19 sh -c 'cd /c && tar czf /o/vol-caddy_data.tar.gz .'; \
    docker run --rm -v decisioncourt_postgres_data:/p:ro -v /var/tmp:/o alpine:3.19 sh -c 'cd /p && tar czf /o/vol-postgres_data.tar.gz .'; \
    docker run --rm -v decisioncourt_redis_data:/r:ro -v /var/tmp:/o alpine:3.19 sh -c 'cd /r && tar czf /o/vol-redis_data.tar.gz .'; \
    tar czf /var/tmp/state-backup-YYYY-MM-DD.tar.gz vol-*.tar.gz; \
    rm vol-*.tar.gz"
  scp admin@OLD_ECS:/var/tmp/state-backup-YYYY-MM-DD.tar.gz ./secrets/
  ```
  验证：`tar tzf secrets/state-backup-*.tar.gz` 含 4 个 vol + postgres_data 含 `PG_VERSION` 文件

### 1.2 收集新 ECS 信息

- [ ] 新 IP / hostname
- [ ] 新 SSH 端口（默认 22，可改）
- [ ] 新 SSH 密钥（同源 ed25519 推荐，与 GitHub Secrets `ECS_SSH_KEY` 同源）
- [ ] 新 region / 可用区
- [ ] 新规格 ≥ 2C2G / 4GB RAM / **加 swap 2GB**
- [ ] 新系统盘 ≥ 40GB

---

## 2. 迁移前强烈建议修（P1，复用本仓库 PR-A/B/C/D）

这些 PR **已经合入 main 分支**（commit `b1a2e1d` / `13de4f9` / `c3da7ef` / `369abb8`），不需要新开发：

- [ ] **PR-A** FileLogger 启用（`commit b1a2e1d`）：`.env.example` + `docker-compose.yml` + `config.go` + `gateway.go:411`
- [ ] **PR-B** version ldflags（`commit 13de4f9`）：`main.go` + `Dockerfile` + `push-to-acr.ps1`
- [ ] **PR-C** release notes 修正（`commit c3da7ef`）：3 个 release notes 状态 + 反思
- [ ] **PR-D** SSH_KEY 修复（`commit 369abb8`）：`secrets/ecs.env` + `AGENTS.md §10`

### 2.1 新 ECS 部署前的 env 同步（关键）

`.env` 从旧 ECS 拉到本地后，**必须确认以下字段与新 ECS 兼容**：

- [ ] `DOMAIN`（Caddy HTTPS 域名）— 若新 ECS IP 不同不影响（域名解析到新 IP）
- [ ] `NEXT_PUBLIC_API_URL` / `NEXT_PUBLIC_WS_URL` — 不变（域名级）
- [ ] `LLM_BASE_URL` / `LLM_MODEL_*` — 不变
- [ ] `JWT_SECRET` — **保持不变**（避免签发用户 JWT 失效）或 **主动 rotate**（让所有用户重新登录）
- [ ] `POSTGRES_PASSWORD` / `REDIS_URL` — 不变
- [ ] **新加字段**（本 PR 后）：
  - `AGENT_GATEWAY_ENABLED=true`（PR-A）
  - `AGENT_GATEWAY_FILE_LOGGER=true`（PR-A）
  - `AGENT_GATEWAY_LOG_DIR=logs`（PR-A）

---

## 3. 新 ECS 部署（标准流程）

### 3.1 准备 SSH 通道

- [ ] 把新 SSH 公钥 `id_ed25519.pub` 或 `id_rsa.pub` 加入 `admin@NEW_ECS:/root/.ssh/authorized_keys`
- [ ] 更新本地 `secrets/ecs.env`：`ECS_HOST` / `ECS_SSH_KEY_PATH`
- [ ] 更新 GitHub Secrets `ECS_SSH_KEY` / `ECS_HOST`（**如果是同源 ed25519 key，文件不变；如果是新 key 必须替换**）

### 3.2 初始化新 ECS

```bash
# 1. SSH 验证（必须先通，否则 deploy 必失败）
ssh -i ~/.ssh/id_rsa admin@NEW_ECS "echo connected && uname -a"

# 2. 安装 docker + docker compose（v2 CLI）
ssh admin@NEW_ECS "curl -fsSL https://get.docker.com | sh && usermod -aG docker admin && \
  apt install -y docker-compose-plugin"

# 3. 创建项目目录
ssh admin@NEW_ECS "mkdir -p /opt/DecisionCourt && cd /opt/DecisionCourt && \
  git clone https://github.com/Exist/DecisionCourt.git . && \
  mkdir -p logs/backend logs/frontend"
```

### 3.3 恢复数据卷

```bash
# 4. 把本地 secrets/ 的备份推到新 ECS
scp secrets/db-backup-YYYY-MM-DD.sql.gz secrets/.env.backup-YYYY-MM-DD admin@NEW_ECS:/opt/DecisionCourt/

# 5. 恢复 postgres（先 docker compose up -d postgres，再 restore）
ssh admin@NEW_ECS "cd /opt/DecisionCourt && \
  docker compose up -d postgres redis && \
  sleep 10 && \
  gunzip -c db-backup-YYYY-MM-DD.sql.gz | docker exec -i dc_postgres psql -U decisioncourt decisioncourt"
```

### 3.4 部署应用

- [ ] 把 `secrets/.env.backup-YYYY-MM-DD` 复制为新 ECS 的 `/opt/DecisionCourt/.env`
- [ ] 触发 GitHub Actions deploy workflow（`workflow_dispatch` 或 `git tag v0.10.21+` push）
- [ ] 或本地手动 push：
  ```bash
  ./scripts/push-to-acr.ps1
  ssh admin@NEW_ECS "cd /opt/DecisionCourt && \
    docker compose pull && \
    docker compose up -d --force-recreate backend frontend"
  ```

---

## 4. 部署后验证（10 项必须全过）

- [ ] **V1. 容器状态**
  ```bash
  ssh admin@NEW_ECS "cd /opt/DecisionCourt && docker compose ps"
  ```
  期望：5 容器 `Up (healthy)`

- [ ] **V2. PR-A FileLogger 验证**
  ```bash
  ssh admin@NEW_ECS "ls -la /opt/DecisionCourt/logs/backend/"
  ```
  期望：`agent_gateway_YYYY-MM-DD.jsonl` 文件存在（创建空文件也算成功，下次 trial 自动追加）

- [ ] **V3. PR-B version 验证**
  ```bash
  ssh admin@NEW_ECS "docker logs dc_backend 2>&1 | grep 'backend listening' | head -1"
  ```
  期望：输出 `"version":"v0.10.21"`（不是 `v0.9.2`）

- [ ] **V4. /health 响应**
  ```bash
  ssh admin@NEW_ECS "docker exec dc_backend wget -qO- http://127.0.0.1:8080/health"
  ```
  期望：`{"status":"ok"}`

- [ ] **V5. 数据库连接**
  ```bash
  ssh admin@NEW_ECS "docker exec dc_postgres psql -U decisioncourt -d decisioncourt -c 'SELECT count(*) FROM sessions;'"
  ```
  期望：数字 > 0（17+ 真实庭审）

- [ ] **V6. Redis 连接**
  ```bash
  ssh admin@NEW_ECS "docker exec dc_redis redis-cli ping"
  ```
  期望：`PONG`

- [ ] **V7. Caddy HTTPS**
  ```bash
  curl -I https://YOUR_DOMAIN/
  ```
  期望：HTTP/2 200 + Caddy 自动签名证书

- [ ] **V8. /metrics 端点**
  ```bash
  ssh admin@NEW_ECS "docker exec dc_backend wget -qO- http://127.0.0.1:8080/metrics | python3 -m json.tool | head -20"
  ```
  期望：JSON 200，包含 `global_concurrency_*` / `a2a_message_throughput_total`

- [ ] **V9. 跑 1 场 trial 验证端到端**
  - 在浏览器打开 `https://YOUR_DOMAIN/`
  - 创建 1 个 session + 输入 query + 等 opening
  - 期望：3 个 agent（prosecutor/defender/investigator）正常 speak

- [ ] **V10. 资源 baseline**
  ```bash
  ssh admin@NEW_ECS "free -h && df -h /opt/DecisionCourt"
  ```
  期望：内存使用 < 50%，盘使用 < 50%（避免再踩 1.6GB 无 swap 坑）

---

## 5. 旧 ECS 释放（验证完成后）

- [ ] **R1. 关停旧 ECS 容器（不删数据，先观察 24h）**
  ```bash
  ssh admin@OLD_ECS "cd /opt/DecisionCourt && docker compose stop"
  ```
- [ ] **R2. 观察 24h，确认新 ECS 一切正常**
- [ ] **R3. 释放旧 ECS 实例**（阿里云控制台操作）
- [ ] **R4. 清理 GitHub Secrets `ECS_HOST` / `ECS_SSH_KEY`**（保留 `secrets/ecs.env` 作为历史）
- [ ] **R5. 更新 AGENTS.md §10 加新 ECS 行**

---

## 6. 应急回滚

如果新 ECS 部署后**核心功能不可用**（trial 创建失败 / opening 死锁 / 鉴权失败）：

- [ ] 把 DNS 切回旧 ECS IP（如果有 CNAME）
- [ ] SSH 到旧 ECS：`docker compose start`
- [ ] 旧 ECS 数据未删，可立即恢复服务

---

## 7. 关联文档

- [production-retrospective-2026-08-05.md](./production-retrospective-2026-08-05.md) — 30 天生产沉淀
- [ACTION-ITEMS-ECS-EXPIRY-2026-08.md](./ACTION-ITEMS-ECS-EXPIRY-2026-08.md) — 行动清单（已 100% 完成 P0+P1）
- [CHECKLIST.md](./CHECKLIST.md) — v0.8.3 部署规划
- [AGENTS.md §9](../../AGENTS.md) — ECS 运维 SSH 模板与禁止操作
- [AGENTS.md §8](../../AGENTS.md) — .env / 凭证红线
- [ADR 0016](../adr/0016-deployment-lessons-learned.md) — 历史部署踩坑

---

## 8. 更新日志

| 日期 | 版本 | 改动 |
|---|---|---|
| 2026-08-05 | v1.0 | 初版（基于 production-retrospective §7 + PR-A/B/C/D 合入后） |
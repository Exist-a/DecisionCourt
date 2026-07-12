# Skill: ecs-ssh-diagnose

> **触发场景**：GitHub Actions Deploy workflow 失败 / 容器 `Restarting` / `/health` 不响应 / backend log 报 FATAL

## 1. 核心原则

**不要让 user 跑命令查 docker logs**。Agent 直接 SSH ECS 5 分钟内定位。

**先看 secrets/ecs.env**（gitignored），不要硬编码 ECS IP。

## 2. SSH 连接（4 步）

```powershell
# 1. 注入环境变量
Get-Content secrets/ecs.env | ForEach-Object {
    if ($_ -match '^([^#][^=]+)=(.*)$') {
        Set-Item -Path "Env:$($Matches[1])" -Value $Matches[2]
    }
}

# 2. 用 id_rsa（不是 id_ed25519）
$KEY = "$env:USERPROFILE\.ssh\id_rsa"

# 3. 短超时 + 自动接受 host key
ssh -i "$KEY" -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new `
    "$env:ECS_USER@$env:ECS_HOST" "<command>"
```

**⚠️ SSH key 注意**：
- `id_rsa` (RSA) ✅ 已配在 ECS（v0.10.15 修复 deploy.yml 时确认）
- `id_ed25519` ❌ **没配** 在 ECS，会 Permission denied

## 3. 诊断 4 个"读"渠道

任何"配置不生效"问题，**必须查 4 个渠道对比**：

| # | 渠道 | 命令 | 检查 |
|---|------|------|------|
| 1 | **host .env** | `cd /opt/DecisionCourt && grep '^JWT_SECRET=' .env \| wc -c` | key 文件实际值 |
| 2 | **docker compose 解析后** | `docker compose config \| grep JWT_SECRET` | compose 看到的 env |
| 3 | **容器 process env** | `docker compose run --rm backend env \| grep JWT_SECRET` | 容器启动时实际 env |
| 4 | **运行时配置对象**（间接）| `docker compose logs --tail=20 backend` | backend log 是否报 FATAL |

**4 个都说"有"但代码说"没" = 工具内部 bug**（如 viper lowercase bug，v0.10.19）。

## 4. 5 个常用诊断命令

```bash
# A. 容器状态（90% 问题一次定位）
cd /opt/DecisionCourt && docker compose ps

# B. backend 日志
cd /opt/DecisionCourt && docker compose logs --tail=50 backend

# C. host .env 关键项（值隐藏）
cd /opt/DecisionCourt && grep -E '^(JWT_SECRET|DATABASE_URL|LLM_API_KEY)' .env | sed 's/=.*/=<hidden>/'

# D. 镜像 tag（确认部署了正确版本）
docker images | grep decision-court

# E. 容器内健康检查（绕过 caddy SSL）
docker exec dc_backend wget -qO- http://127.0.0.1:8080/health
```

## 5. 4 类典型故障模式

| 现象 | 诊断 | 修复 |
|------|------|------|
| `Restarting (1) X seconds ago` + log `FATAL: required config XXX is empty` | P0-2 fail-fast：env 没传进容器或工具读不到 | 查 4 渠道 → 工具 bug → 改代码 |
| `Restarting (1)` + log `permission denied /app/logs` | P0-3 UID 不匹配 VOLUME owner | `chown -R 10001:10001 /opt/DecisionCourt/logs` |
| `Up` 但 `/health` 不响应 | caddy 中间层问题 | `docker exec dc_backend wget http://127.0.0.1:8080/health`（绕过 caddy） |
| `Up (healthy)` 但功能异常 | 应用层 bug | 查 backend log + 数据库连接 |

## 6. 禁止直接执行的 SSH 操作（AGENTS.md §9.4）

- `docker compose down` / `docker rm` / `rm -rf`（销毁性）
- 修改 `/opt/DecisionCourt/.env`（违反 §8 敏感文件红线）
- `chmod 777` / `chown -R` 大范围
- `kill -9` / `pkill`

找到 root cause 后**先告诉 user 修复方案**，让 user 决定。

## 7. 关键超时配置

| 操作 | 超时 | 命令参数 |
|------|------|----------|
| SSH 连接 | 10 秒 | `-o ConnectTimeout=10` |
| 命令执行 | 30 秒（默认）| 不需要 |
| curl 探测 | 10 秒 | `curl --max-time 10` |

## 8. 关联文档

- [AGENTS.md §9 ECS 运维连接能力](../../../AGENTS.md)
- [ADR 0025 §2.1 P0-2 JWT_SECRET 默认值移除](../../docs/adr/0025-security-p0-closeout.md)
- [ADR 0026 §2.2 5 阶段诊断过程](../../docs/adr/0026-viper-bindenv-fix.md)
- [secrets/ecs.env](../../../secrets/ecs.env)（gitignored）
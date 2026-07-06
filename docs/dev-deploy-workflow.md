# DecisionCourt 日常开发与部署工作流（v0.9.2）

> **状态**: 落地（2026-07-06）
> **配套**: [CHECKLIST.md](./deployment/CHECKLIST.md)（规划）· [ADR 0016](./adr/0016-deployment-lessons-learned.md)（教训库）· [OBSERVABILITY.md](./OBSERVABILITY.md)（运维速查）
> **目标读者**: 项目 owner（自己），未来要维护 / 上线的人

---

## 0. 一图流

```
┌────────────────────────── 你的电脑 ───────────────────────────┐
│                                                                │
│   改代码 ─┐                                                    │
│           │                                                    │
│           ▼                                                    │
│   docker-compose.dev.yml  (dev 模式)                          │
│   ├─ frontend: bind mount + pnpm dev → HMR 秒级              │
│   ├─ backend:  bind mount + air → 1-3 秒热重载               │
│   └─ dozzle:   浏览器面板看所有容器实时日志                   │
│                                                                │
│   改好了 ──┐                                                   │
│           ▼                                                    │
│   .\scripts\push-to-acr.ps1   (本地脚本)                      │
│   ├─ build backend + frontend                                 │
│   ├─ inject NEXT_PUBLIC_* build args                          │
│   ├─ 走 Windows Credential Manager 自动登录                   │
│   └─ docker push → ACR                                        │
│                                                                │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                  ┌──── 阿里云 ACR ────┐
                  │  crpi-...:latest   │
                  └─────────┬──────────┘
                            │ docker pull (VPC 内网)
                            ▼
┌────────────────────────── ECS ────────────────────────────────┐
│  WorkBench (浏览器内终端)                                       │
│  ├─ docker compose pull       ←─ 拉新镜像                     │
│  ├─ docker compose up -d      ←─ 自动替换 + 健康检查           │
│  ├─ ./deploy.sh               ←─ 一键脚本(可选,带确认+日志)  │
│  └─ docker compose logs -f    ←─ 实时日志                     │
└────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                  用户访问 https://decisioncourt.cn
```

---

## 1. 日常开发（90% 时间用这套）

### 1.1 启动 dev 模式（首次约 2-3 分钟）

```powershell
# 在项目根目录(D:\源码\FullStack\DecisionCourt)下:
docker compose -f docker-compose.dev.yml up -d --build
```

**做了什么**：
- 本地构建 dev 镜像（带 Node + pnpm + air + Go 1.26）
- bind mount 源码到容器
- 容器内跑 `pnpm dev` / `air`，监听文件变化
- 起一个 Dozzle 容器（端口 9999）做日志面板

### 1.2 打开面板

| URL | 用途 |
|---|---|
| `http://localhost:3000` | 前端（你的修改立刻 HMR） |
| `http://localhost:8080/health` | 后端健康检查 |
| `http://localhost:8080/metrics` | Prometheus JSON 指标 |
| `http://localhost:9999` | Dozzle 日志面板（看所有容器实时日志） |

### 1.3 改代码 → 自动生效

| 改了什么 | 多快生效 | 备注 |
|---|---|---|
| `frontend/app/**` | < 1 秒 | Next.js HMR，浏览器自动刷新 |
| `frontend/components/**` | < 1 秒 | 同上 |
| `backend/internal/**` | 1-3 秒 | air 重新编译 Go 二进制 + 重启进程 |
| `frontend/Dockerfile.dev` | 重 build 一次 | `docker compose -f docker-compose.dev.yml up -d --build --no-deps frontend` |
| `backend/Dockerfile.dev` | 重 build 一次 | 同上 |

### 1.4 看日志（4 种方式）

```powershell
# 方式 A：Dozzle 面板（推荐，直观）
打开 http://localhost:9999，左边选容器，右边实时滚动

# 方式 B：docker logs 直接跟踪
docker logs dc_dev_backend --tail 200 -f

# 方式 C：dev compose 内置
docker compose -f docker-compose.dev.yml logs -f backend

# 方式 D：进容器内
docker exec -it dc_dev_backend sh
```

### 1.5 停止 dev 模式

```powershell
docker compose -f docker-compose.dev.yml down
# 想彻底清理（含数据卷）：
docker compose -f docker-compose.dev.yml down -v
```

---

## 2. 推到生产（一天一次或几次）

### 2.1 推送脚本用法

```powershell
# 完整流程（推 :latest 到 ACR）
.\scripts\push-to-acr.ps1

# 只构建不推送（验证 Dockerfile 没问题）
.\scripts\push-to-acr.ps1 -BuildOnly

# 跳登录（如果之前在 Windows Credential Manager 登过）
.\scripts\push-to-acr.ps1 -NoLogin

# 用命令行密码（CI 用，不留文件）
.\scripts\push-to-acr.ps1 -Password 'sk-xxx'
```

**脚本做了什么**：
1. 检查 Docker 在不在跑
2. 读 `.env` 拿 `NEXT_PUBLIC_*` 作为 frontend build args
3. **检测 Windows Credential Manager** 里有没有 ACR 凭证（没有就报错，不会强行问密码）
4. `docker build backend` （无 build args）
5. `docker build frontend` （注入 build args）
6. `docker push` 两个镜像到 ACR `:latest`

**如果 `localhost` 警告**：
```
WARN NEXT_PUBLIC_API_URL 是 localhost,生产环境浏览器连不上!
```
→ 改 `.env` 里的 `NEXT_PUBLIC_API_URL=https://你的域名` + `NEXT_PUBLIC_WS_URL=wss://你的域名` 再推。

### 2.2 部署脚本用法（ECS WorkBench 里跑）

```bash
# 第一次用：先上传脚本
# 在本地把 scripts/deploy-on-ecs.sh 的内容粘贴到 /opt/DecisionCourt/deploy.sh
# 然后:
chmod +x /opt/DecisionCourt/deploy.sh

# 之后每次部署
cd /opt/DecisionCourt && ./deploy.sh

# 跳过确认倒计时（CI 用）
./deploy.sh --yes

# 部署完看日志
./deploy.sh --logs 30

# 自定义健康检查 URL
./deploy.sh --health-url http://localhost:8080/health

# 帮 助
./deploy.sh --help
```

**脚本做了什么**：
1. 自动检测 `docker compose` v2 plugin 或 v1 docker-compose（v2 优先）
2. 前置检查（compose 文件 / Docker 在跑）
3. 5 秒倒计时确认（防误操作）
4. `docker compose pull` 拉新镜像
5. `docker compose up -d` 替换容器
6. 30 秒内轮询 `http://localhost:8080/health` 做健康检查
7. 写日志到 `deploy.log` 留档
8. 显示服务状态 + 镜像变化 diff + 常用命令

---

## 3. ECS 上的日常命令（WorkBench 里用）

### 3.1 看状态

```bash
cd /opt/DecisionCourt
docker compose ps                    # 所有容器状态
docker compose logs --tail=100 backend   # backend 最近 100 行
docker compose logs -f backend       # 实时跟踪
```

### 3.2 重启 / 重 build 某服务

```bash
# 重启单个服务（不拉新镜像，用本地缓存）
docker compose restart backend

# 强制重建（拉新镜像 + 重建容器）
docker compose up -d backend --force-recreate
```

### 3.3 应急清理

```bash
# 容器启动卡住 / 状态异常时
docker compose down --remove-orphans
docker compose up -d

# 完全清理（含数据卷，会删数据库！慎用）
docker compose down -v
docker compose up -d
```

### 3.4 进容器调试

```bash
# 进 backend 容器内
docker compose exec backend sh

# 看 postgres 数据
docker compose exec postgres psql -U decisioncourt -d decisioncourt
```

### 3.5 健康检查

```bash
# Backend
curl -i http://localhost:8080/health
# 返回 200 OK {"status":"healthy",...} 就是 OK

# 通过 Caddy（生产域名）
curl -i https://decisioncourt.cn/health
```

---

## 4. 8 个今天踩的坑 + 解决方案

> 完整教训库见 [ADR 0016](./adr/0016-deployment-lessons-learned.md)（追加了 5 个新坑）

### 坑 1：PowerShell 脚本中文乱码 + 提前退出

**症状**：PowerShell 5.1 跑 `.\push-to-acr.ps1` 报语法错误 / 中文乱码 / 跑到一半退出。

**根因**：PowerShell 5.1 默认把 UTF-8 无 BOM 文件当 ANSI 读 → 中文变成乱码字节 → tokenizer 解析失败。

**修法**：脚本文件保存为 **UTF-8 BOM**（首三字节 `EF BB BF`）。我加的 `push-to-acr.ps1` 已含 BOM。

**怎么验证**：
```powershell
[System.IO.File]::ReadAllBytes('scripts\push-to-acr.ps1')[0..2] | ForEach-Object { '0x{0:X2}' -f $_ }
# 应该输出: 0xEF 0xBB 0xBF
```

---

### 坑 2：PowerShell `&&` 报错

**症状**：`cd foo && bar` 在 PowerShell 5.1 报 `The token '&&' is not a valid statement separator`。

**根因**：`&&` 是 Bash / CMD 的语法，**PowerShell 5.1 不支持**（PowerShell 7+ 才支持）。

**修法**：全部用 `;` 替代：
```powershell
cd foo; bar   # ✓
cd foo && bar  # ✗ (5.1 报错, 7.0 才支持)
```

---

### 坑 3：PowerShell 把 Docker stderr 当错误抛出

**症状**：脚本里 `$ErrorActionPreference = "Stop"` 时，`docker build` 的正常进度输出（写 stderr 的）让脚本直接抛异常退出。

**根因**：PowerShell 把 native command 的 stderr 当 ErrorRecord，`Stop` preference 会 throw。

**修法**：
```powershell
$ErrorActionPreference = "Continue"   # 不 stop, 靠 $LASTEXITCODE 判断
# 然后在每个 docker 命令后检查:
docker build ...; if ($LASTEXITCODE -ne 0) { Write-Err "..."; exit 1 }
```

---

### 坑 4：`docker login` 在 IDE 内 PowerShell 失败

**症状**：PowerShell 跑 `docker login` 报 `cannot perform an interactive login from a non-TTY device`，即便凭证其实能用。

**根因**：`docker login` 这个特定命令会强制要求 TTY；`docker pull/push` 不需要，可以走 credential helper。

**修法**：**脚本里不要调 `docker login`**。直接调 `docker push`，凭证由 Windows Credential Manager 自动提供。

---

### 坑 5：Docker 凭证到底存在哪儿？

**症状**：`.docker/config.json` 里 `auths.aliyuncs.com: {}`（空对象），但 `docker pull` 居然成功。

**真相**：`credsStore: desktop` 表示凭证存在 **Windows Credential Manager**（控制面板 → 凭据管理器），不在 config.json。空 `{}` 是正常占位符。

**诊断命令**：
```powershell
# 看 Credential Manager 里的 Docker 凭证
cmdkey /list | Select-String "docker"

# 看 config.json 凭证存储方式
Get-Content $env:USERPROFILE\.docker\config.json
```

**如果 Credential Manager 里没凭证**：在真 PowerShell 终端（不是 IDE）里跑一次 `docker login`。

---

### 坑 6：docker-compose v1 + Docker 29 崩 `KeyError: 'ContainerConfig'`

**症状**：用 `docker-compose`（连字符，v1）跑 `up -d`，崩在：
```
File "compose/service.py", line 1579
container.image_config['ContainerConfig'].get('Volumes') or {}
KeyError: 'ContainerConfig'
```

**根因**：docker-compose v1 是 Python 写的，2021 年停更。Docker v25+ 的 API 不再返回 `ContainerConfig` 字段。

**修法**：装 docker-compose **v2 plugin**（Go 写的，还在维护）：
```bash
# 方式 A：apt（如果有 Docker CE 仓库）
sudo apt-get install -y docker-compose-plugin

# 方式 B：直接下二进制（apt 找不到时，最稳）
sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo curl -SL "https://gh-proxy.com/https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-x86_64" \
    -o /usr/local/lib/docker/cli-plugins/docker-compose
sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
docker compose version   # 应该看到 v2.x
```

之后用 `docker compose`（**空格**），不要用 `docker-compose`（连字符）。

---

### 坑 7：up -d 报 "container name already in use"

**症状**：
```
Error response from daemon: Conflict. The container name "/6a1004345ac2_dc_backend" is already in use
by container "6a1004345ac23d6878fc31b48a1493be8d6b80cf91d0844b618a9eb12300b9b2".
```

**根因**：docker-compose 没显式 `container_name:` 时，Docker 自动用 `<容器ID>_服务名` 命名。老容器还在（哪怕 Exited），新容器就起不来。

**修法**：用 `down --remove-orphans` 一键清理（脚本和孤儿都清）：
```bash
docker compose down --remove-orphans
docker compose up -d
```

**手动清特定容器**：
```bash
docker rm -f 6a1004345ac2_dc_backend
```

---

### 坑 8：deploy-on-ecs.sh 在 ECS 找不到 docker compose

**症状**：跑 `./deploy.sh` 报 `找不到 'docker compose' (v2 plugin) 或 'docker-compose' (v1 二进制)`。

**根因**：ECS 上没装 v2 plugin，也没装 v1 docker-compose。

**修法**：先装一个（推荐 v2）：
```bash
# 直接下二进制（不依赖 apt 仓库）
sudo mkdir -p /usr/local/lib/docker/cli-plugins
sudo curl -SL "https://gh-proxy.com/https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-x86_64" \
    -o /usr/local/lib/docker/cli-plugins/docker-compose
sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
```

`deploy.sh` 启动时**自动检测两个版本**，装哪个就用哪个。

---

## 5. 速查表（最常用的 10 条命令）

| 场景 | 命令 |
|---|---|
| **本地启 dev 模式** | `docker compose -f docker-compose.dev.yml up -d --build` |
| **本地关 dev 模式** | `docker compose -f docker-compose.dev.yml down` |
| **看 dev backend 日志** | `docker logs dc_dev_backend --tail 200 -f` |
| **打开日志面板** | 浏览器 `http://localhost:9999` |
| **本地推 ACR** | `.\scripts\push-to-acr.ps1` |
| **只 build 不 push** | `.\scripts\push-to-acr.ps1 -BuildOnly` |
| **ECS 部署（一行）** | `cd /opt/DecisionCourt && docker compose pull && docker compose up -d` |
| **ECS 部署（带确认+健康检查）** | `cd /opt/DecisionCourt && ./deploy.sh` |
| **ECS 看 backend 日志** | `docker compose logs --tail=100 backend` |
| **ECS 健康检查** | `curl http://localhost:8080/health` |

---

## 6. 文件清单（今天新增/修改）

| 文件 | 用途 | 跑在哪 |
|---|---|---|
| `docker-compose.dev.yml` | dev 编排（bind mount + hot reload + Dozzle） | 本地 |
| `frontend/Dockerfile.dev` | dev 镜像（带 pnpm，跑 `pnpm dev`） | 本地 |
| `backend/Dockerfile.dev` | dev 镜像（带 air，热重载 Go） | 本地 |
| `docker-compose.yml` | 生产编排（v0.9.2 加日志轮转） | ECS |
| `scripts/push-to-acr.ps1` | 本地一键 build + push 到 ACR | 本地 |
| `scripts/deploy-on-ecs.sh` | ECS 一键 pull + 重启 + 健康检查 | ECS |
| `docs/OBSERVABILITY.md` | 日志/metrics/DB 查询命令汇总 | 任意 |
| `docs/dev-deploy-workflow.md` | 本文件：日常操作手册 | 任意 |

---

## 7. 未来改进（可选）

- [ ] **自动更新 compose 文件到新 tag**（方案 B）：脚本推送成功后自动改 `docker-compose.yml` 里的 image tag，省掉 ECS 上的 sed
- [ ] **CDN 缓存层**（防流量超预期）
- [ ] **Prometheus + Grafana**（目前 JSON metrics 够用，规模化时再上）
- [ ] **GitHub Action 自动 push**：本地 build + push 改成 CI 跑，免本地配 Docker
- [ ] **dev compose 加一个 mock LLM 服务**：完全离线开发，不消耗 LLM 配额
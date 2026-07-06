# 部署规划讨论清单 (Deployment Planning Checklist)

> **状态**: ✅ **规划完成 + 2026-07-04 v0.8.3 安全全栈完成 + 本地冒烟通过**(20 项 P0-P3 + 4 项 smoke 修复)
> **性质**: 活跃工作文档,执行阶段可继续更新
> **目的**: 把"零经验者部署一个 LLM 多 Agent 全栈项目"的所有决策点铺开,逐项推进
> **下次打开条件**:用户决定购机时 + 域名解析时(Q12 域名后缀顺便定)
> **配套文档**: [`../security-audit-v0.8.3.md`](../security-audit-v0.8.3.md) — v0.8.3 安全状态详细

---

## 0. 文档元信息

| 字段     | 值                                                                                                                                                                                     |
| -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 创建日期 | 2026-07-03                                                                                                                                                                             |
| 最近更新 | 2026-07-03(初始化)                                                                                                                                                                     |
| 维护者   | 项目 owner                                                                                                                                                                             |
| 关联文档 | [`../decisioncourt-prd.md`](../decisioncourt-prd.md) · [`../decisioncourt-tech-spec.md`](../decisioncourt-tech-spec.md) · [`../decisioncourt-roadmap.md`](../decisioncourt-roadmap.md) |
| 关联 ADR | [0010-whitebox-observability](../adr/0010-whitebox-observability.md) · [0011-llm-probability-hard-clamp](../adr/0011-llm-probability-hard-clamp.md)                                    |

---

## 1. 目标设定(我们到底要部署什么?)

### 1.1 服务对象

- [x] ✅ **服务对象 = 公开作品集**(2026-07-03 决定)
- [x] ✅ **地域 = 国内大陆为主**(2026-07-03 决定)
- [ ] **规模量级?**(10-50 人 / 100-500 人 / 1000+ —— 决定监控策略)
- [ ] **使用场景?**(作品集展示 / 真实决策辅助)

### 1.2 SLA 与质量目标

- [ ] **可用性要求?**(99% / 99.9% / 99.99% —— 数量级不同,成本差 10 倍)
- [ ] **数据保留?**(无要求 / 7 天 / 30 天 / 长期)
- [ ] **故障恢复时间?**(几小时 / 30 分钟 / 5 分钟)
- [ ] **是否要 HTTPS?**(几乎是肯定的)
- [ ] **是否要备案?**(国内大陆 80/443 + 域名 = 必须)

### 1.3 预算上限

- [x] ✅ **月度成本 = ¥50/月以下**(2026-07-03 决定)
  - 主机:¥38/月 阿里云新人 2C2G(1 年优惠)
  - 域名:¥60-100/年(路径 A 新增,见 §2.6)
  - **年度总预算**:¥456(主机)+ ¥60-100(域名)+ ¥360(LLM)= **¥876-916/年**
  - ⚠️ **风险**:¥38 通常是 1 年期,续费会涨到 ¥60-100+/月
  - ⚠️ **风险**:2C2G 跑 4 Agent + DB + Redis 偏紧,需监控内存
- [x] ✅ **LLM 月度预算 = ¥30/月**(2026-07-03 决定,见 Q10)
  - 约 300 万 token / 月,够 100+ 场庭审
  - **必加监控**:DeepSeek 后台账单告警 + 自建 token 计数
  - 触发阈值:¥20 时邮件预警 / ¥30 时强制停服
- [x] ✅ **域名策略 = 路径 A 先买再备案**(2026-07-03 第六次讨论)

---

## 2. 基础设施选型(买什么机器?)

### 2.1 主机厂商

| 厂商              | 状态        | 备注                |
| ----------------- | ----------- | ------------------- |
| 阿里云            | ✅ **已选** | 用户已确认          |
| 腾讯云            | ❌          | 备选                |
| 华为云            | ❌          | 备选                |
| AWS / GCP / Azure | ❌          | 偏贵,海外场景才考虑 |

### 2.2 产品线(轻量 vs ECS)

- [ ] **阿里云轻量应用服务器** ⭐ 简单便宜,新手首选
- [ ] 阿里云 ECS 功能强但复杂

### 2.3 地域(等用户定)

| 地域         | 备案要求   | 国内延迟  | 价格 | 适合场景     |
| ------------ | ---------- | --------- | ---- | ------------ |
| **香港**     | ❌ 免      | 30-80ms   | +30% | 0 等待上线   |
| 华北 2(北京) | ✅ 7-20 天 | 5-20ms    | 标准 | 长期国内用户 |
| 华东 1(杭州) | ✅         | 5-20ms    | 标准 | —            |
| 华南 1(深圳) | ✅         | 5-20ms    | 标准 | —            |
| 新加坡       | ❌ 免      | 100-200ms | 标准 | 海外用户     |

### 2.4 规格(2026-07-03 反转:香港 2C2G)

- [x] ✅ **规格 = 香港 2C2G / 40G SSD**(2026-07-03 第九次讨论决定)
  - 香港 2C1G ¥24/月 不推荐(1G 内存跑 4 Agent 会 OOM)
  - 香港 2C2G ¥56/月 推荐(平衡点)
  - 香港 2C4G ¥150/月 略奢侈

| 档位     | CPU      | 内存   | 硬盘    | 月费(香港) | 适合              |
| -------- | -------- | ------ | ------- | ---------- | ----------------- |
| 入门     | 2 核     | 1G     | 40G     | ¥24        | ❌ 跑不动 4 Agent |
| **当前** | **2 核** | **2G** | **40G** | **¥56**    | **推荐**          |
| 舒适     | 2 核     | 4G     | 40G     | ¥150       | 流量稍大          |

> ⚠️ **风险预警**:
>
> - 2C2G 跑 4 Agent + LLM 流式 + PostgreSQL + Redis 仍偏紧
> - 建议部署后**前 7 天每天**看内存,若 OOM 升 2C4G(¥150)
> - **不跑 searxng** 自部署搜索(太重,直接用 Bocha 即可)
> - 香港轻量**不享受新人 ¥38 羊毛**,¥56 是永久同价(没有 Year 2 涨价风险)

### 2.5 操作系统镜像

- [x] ✅ **Ubuntu 22.04 LTS**(2026-07-03 决定)
  - 理由:社区最广,Docker / Node / 工具链文档最多,故障率最低
  - 阿里云镜像:`ubuntu_22_04_x64_20G_alibase_20240630.vhd` 等都行

| 选项                  | 推荐度 | 备注              |
| --------------------- | ------ | ----------------- |
| **Ubuntu 22.04 LTS**  | ⭐⭐⭐ | **当前选定**      |
| Debian 12             | ⭐⭐⭐ | 一样稳            |
| Alibaba Cloud Linux 3 | ⭐⭐   | 阿里云定制,文档少 |
| CentOS / OpenCloudOS  | ⭐     | 已 EOL,不推荐     |
| 宝塔面板应用镜像      | ⭐     | 想可视化可以考虑  |

### 2.6 域名

- [x] ✅ **首期 = 路径 A:先买域名再备案**(2026-07-03 第六次讨论决定)
  - 域名候选:Q12 待选
  - 备案政策:购域名后立即启动(Q6)
  - 灰色期:**0 天**(因为买了域名才能备案,备案期间域名 + IP 都能访问)
- [x] ✅ **风险偏好 = 低风险可接受**(2026-07-03 决定,见 Q8)
- [ ] **绑定 = 备案审核通过后,阿里云 DNS 解析**

> ⚠️ **风险预警(IP 直连 7-20 天)**:
>
> - 国内大陆主机提供 HTTP 服务,**法律上**需要 ICP 备案
> - 实际执行:小流量 IP 直连**通常不会**被自动检测
> - 浏览器会标记"不安全"(无 HTTPS,无证书),用户体验差
> - **应急方案**:若阿里云备案前发警告/断网,临时切回香港机房(可保留 IP 数据迁移)

### 2.6.1 备案 vs 域名 关键机制(2026-07-03 补)

> **核心事实**:**备案 = 域名备案**,不能给 IP 备案。**没有域名无法启动备案**。

**3 条路径**:
| 路径 | 步骤 | 灰色期 | 成本 | 推荐度 |
|---|---|---|---|---|
| **A 先买域名再备案** | 买.cn → 备案 → 解析 | 0 天 | ¥60-100/年 | ⭐⭐⭐ 零经验首选 / **当前选定** |
| B 先 IP 跑,中途买 | IP 跑 → 中途买域名 → 备案 | 7-20+ 天 | 0-100/年 | ⭐⭐ 拖延风险 |
| C 不备案,纯 IP 跑 | 永远不备案 | 永远 | 0 | ⭐ 不推荐 |

> **当前决策**:路径 A。先买域名再备案,Q12 待定具体域名后缀。

---

## 3. 架构设计(怎么布?)

### 3.1 部署架构(单机 docker-compose,推荐方案)

```
阿里云轻量(2C2G 香港)
├── 公网 IP
├── 安全组:仅放行 22/80/443
│
├── nginx(端口 80 + 443,Certbot SSL)
│   ├── /         → dc_frontend:3000
│   ├── /api/     → dc_backend:8080
│   ├── /ws/      → dc_backend:8080(WebSocket)
│   └── /metrics  → dc_backend:8080(限 127.0.0.1)
│
├── Docker(v0.8.3 容器硬化后)
│   ├── dc_frontend  (Next.js,3000,非 root read_only,镜像从 ACR 拉)
│   ├── dc_backend   (Go,8080,非 root read_only cap_drop,镜像从 ACR 拉)
│   ├── dc_postgres  (5432,非 root,官方 postgres:15-alpine)
│   └── dc_redis     (6379,非 root,官方 redis:7-alpine)
│   # ⚠️ v0.8.3 起 SearXNG 已弃用,改用 Bocha AI Search API
│
├── systemd: docker compose 自动启动
└── cron: 每日 pg_dump → OSS/本地

部署链路(本地 → 阿里云):
┌──────────────────────┐                    ┌──────────────────────────┐
│  本地开发者机器       │ docker tag + push  │  阿里云 ACR(香港)         │
│  - docker build      │ ─────────────────► │  - decisioncourt-frontend │
│  - pnpm/go 工具链    │  (公网 / VPN 推送) │  - decisioncourt-backend  │
└──────────────────────┘                    └────────────┬─────────────┘
                                                          │ docker pull(VPC 内网)
                                                          ▼
                                              ┌──────────────────────────┐
                                              │  香港 ECS(2C2G)          │
                                              │  - 仅需 docker runtime   │
                                              │  - 0 公网流量消耗         │
                                              └──────────────────────────┘
```

> 详细 ACR 工作流见 §10 阿里云 Container Registry 操作手册。

### 3.2 数据流(用户视角)

```
浏览器
  ↓ https://你的域名/
nginx(SSL 终止)
  ↓
frontend(Next.js,SSR 出 HTML)
  ↓ 用户点击"立案"
fetch POST /api/.../courtrooms
  ↓
backend(Go)
  ↓
postgres(写库) + DeepSeek API(LLM 调用) + Bocha API(搜索)
  ↓
WebSocket 推回前端(streaming 推 token)
```

### 3.3 端口规划

| 端口 | 服务     | 暴露范围     | 用途                           |
| ---- | -------- | ------------ | ------------------------------ |
| 22   | sshd     | 公网(限 IP)  | 运维登录                       |
| 80   | nginx    | 公网         | HTTP→HTTPS 跳转 + Certbot 续签 |
| 443  | nginx    | 公网         | HTTPS 主入口                   |
| 3000 | frontend | 内部         | Next.js                        |
| 8080 | backend  | 内部         | Go API + WS                    |
| 5432 | postgres | **绝不暴露** | 数据库                         |
| 6379 | redis    | **绝不暴露** | 缓存                           |

> ⚠️ **3000/8080/5432/6379 都不开放到公网**,走 Docker 内网

### 3.4 容器硬化(v0.8.3 `5938bbf` 已实装)

| 措施 | 实现位置 | 作用 |
|---|---|---|
| **非 root 运行** | 2 个 Dockerfile `USER 1001:1001` + `appuser` 用户 | 容器逃逸攻击即使成功也只能拿到低权限用户 |
| **`read_only: true`** | 4 个服务 compose 段 | 文件系统只读,只有 `tmpfs` 可写 |
| **`cap_drop: [ALL]`** | backend/frontend | 丢弃所有 Linux capabilities,只保留必需 |
| **`no-new-privileges: true`** | 4 个服务 | 禁止 suid 二进制提权 |
| **`security_opt: no-new-privileges:true`** | 4 个服务(冗余兜底) | 同上,kernel-level |
| **`tmpfs: /tmp`** | backend/frontend | 只读 fs 下的可写工作区,mem-backed |
| **`--memory=1g`** | backend/frontend(postgres/redis 用 docker 默认) | 内存硬上限,避免一个 container 把整机吃光 |
| **`:alpine` 镜像** | 全部 4 服务 | 基础镜像从 ubuntu 200MB 降到 alpine ~5MB,攻击面大幅缩小 |
| **固定版本(非 `:latest`)** | `postgres:15-alpine` / `redis:7-alpine` / `node:20-alpine` / `golang:1.26-alpine` | 防止恶意更新偷偷塞进 base image |
| **Compose 内网 alias** | `postgres:5432` / `redis:6379` 等 | 容器间走 docker 内网 DNS,而不是 IP |

> 详见 [`../security-audit-v0.8.3.md`](../security-audit-v0.8.3.md) §2.1 P0-3

### 3.5 镜像仓库架构(阿里云 Container Registry,2026-07-XX 增补)

> **触发**: v0.8.3 安全 commit 跑通后,部署从"服务器本地 `git clone + docker compose up`"升级为 **本地构建 → 推送到阿里云 ACR → 服务器从 ACR 拉取 → docker compose up** 的标准生产链路。详见 §10 ACR 操作手册。

#### 3.5.1 为什么不用"服务器本地构建"

| 维度 | 服务器本地 build | **ACR 拉镜像(当前选择)** |
|---|---|---|
| 服务器依赖 | Node 20 + pnpm + Go 1.26 + Docker | **只要 Docker**(攻击面 ↓) |
| 单次部署耗时 | 5-15 分钟(冷构建) | **30-60 秒(VPC 内网拉镜像)** |
| 公网流量 | 构建期要拉 pnpm/go module | **0**(VPC 内网全免) |
| 镜像版本管理 | 无 | **tag 化**(语义化版本,可回滚) |
| CI/CD 友好 | 否 | ✅ GitHub Action 直接 push |
| 缓存复用 | 每次冷启动 | ACR 自动分层缓存 |
| 部署原子性 | 容易半构建半跑 | 整镜像替换,失败立刻回滚 |

**结论**: 服务器**只跑 Docker runtime**,构建在本地(或未来 GitHub Action)做。

#### 3.5.2 仓库结构约定(命名空间 + 仓库名)

```
阿里云 ACR 个人版(香港地域,选 v0.8.3 香港策略)
└── <your-aliyun-username>          ← 阿里云账号全名(RAM 子账号也行)
    └── decision-court/             ← 命名空间(namespace)
        ├── decisioncourt-frontend  ← 仓库 1(Next.js 镜像)
        ├── decisioncourt-backend   ← 仓库 2(Go 镜像)
        ├── postgres                ← 可选:自托管 PG(本项目用官方 postgres:15-alpine,不入 ACR)
        └── redis                   ← 可选:同上
```

**Registry URL 模板**:

| 网络 | URL | 用途 |
|---|---|---|
| 公网 | `crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com` | 本地构建推送 / 公网拉取 |
| **VPC 内网** | `crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com` | **香港 ECS 拉镜像专用**(速度 ↑,流量免费) |

> ⚠️ **`<id>` 是阿里云分配的个人实例 ID**,登录后控制台可见。本文档示例 URL 用 `<id>` 占位,**请勿把真实 ID 写进 git**。

#### 3.5.3 镜像 tag 策略(语义化版本 + git SHA)

```bash
# 三种 tag 都打,互不冲突:
docker tag dc_backend   crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com/decision-court/decisioncourt-backend:v0.8.3
docker tag dc_backend   crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com/decision-court/decisioncourt-backend:v0.8
docker tag dc_backend   crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com/decision-court/decisioncourt-backend:v0.8.3-abc1234
                        # ↑ git short SHA,精准回滚
```

- `v0.8.3` 完整版本号 → 稳定 / 主用
- `v0.8` 浮动 minor → 自动跟 minor 内最新 patch
- `v0.8.3-abc1234` git SHA → 出问题立刻定位 commit

#### 3.5.4 docker-compose.yml 适配(从 `build:` 切到 `image:`)

**生产 `docker-compose.yml`**(服务器用)的 backend / frontend 段从 `build:` 改为 `image:`:

```yaml
services:
  backend:
    # 生产:从阿里云 ACR 拉,不构建
    image: crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com/decision-court/decisioncourt-backend:v0.8.3
    # 移除 build: 段
    # build:
    #   context: ./backend
    #   dockerfile: Dockerfile
    pull_policy: always   # 强制每次 compose up 拉最新
    ...
```

> **开发环境**继续用 `build:` 段本地构建。生产部署前用环境变量切换:`COMPOSE_FILE=docker-compose.yml:docker-compose.acr.yml`,后者覆盖 `image:` 字段。

---

## 4. 🔴 必修问题清单(部署前必须解决)

> ✅ **2026-07-04 更新**: 以下 P0 全部已在 10 个安全 commit 内修完。详细见 [`../security-audit-v0.8.3.md`](../security-audit-v0.8.3.md)。本节保留作为"过去的问题 + 修法"档案。

### 4.1 代码层(P0:代码 bug)

| #   | 问题                                                | 位置                                                                                           | 风险                                | 状态     | 修法 / Commit                                                          |
| --- | --------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ----------------------------------- | -------- | ---------------------------------------------------------------------- |
| 1   | **CORS 硬编码只允许 localhost**                     | [main.go:126](file:///d:/源码/FullStack/DecisionCourt/backend/cmd/server/main.go#L125-L131)    | 🔴 部署到任何域名浏览器都拦截       | ✅ 已修 `2572b7e` | 读 `config.AllowedOrigins` env,启动时校验                              |
| 2   | **前端 Dockerfile 用 `pnpm dev`**                   | [frontend/Dockerfile:9](file:///d:/源码/FullStack/DecisionCourt/frontend/Dockerfile#L9)        | 🔴 跑的是 dev 模式(慢 + 报错不友好) | ✅ 已修(早于本批 commit,见历史) | 改用 `pnpm build && pnpm start`                                        |
| 3   | **JWT_SECRET 默认 `decisioncourt-secret`**          | [.env.example:21](file:///d:/源码/FullStack/DecisionCourt/.env.example#L21)                    | 🔴 任何部署者用户身份可伪造         | ✅ 已修 `b759d76` | 启动时校验,默认值为空则 panic                                          |
| 4   | **NEXT_PUBLIC_API_URL 写死 localhost**              | [docker-compose.yml:85-86](file:///d:/源码/FullStack/DecisionCourt/docker-compose.yml#L85-L86) | 🟡 部署后前端找不到后端             | ✅ 已修(早于本批) | 改用 build arg 在 build 时注入(用户决定先 IP 直连,需用 IP 作为 API URL) |
| 5   | **没有数据库 migration 步骤**                       | [docker-compose.yml](file:///d:/源码/FullStack/DecisionCourt/docker-compose.yml)               | 🔴 第一次跑会报表不存在             | ✅ 已修 `b759d76` + `1277522` | GORM AutoMigrate 启动时跑;**`1277522` 修了一个真实 bug**:AutoMigrate 漏 `User` / `AuditLog`,首次部署 anon auth SQLSTATE 42P01 静默失败 |
| 6   | **postgres 默认密码 `decisioncourt:decisioncourt`** | [docker-compose.yml:8-9](file:///d:/源码/FullStack/DecisionCourt/docker-compose.yml#L8-L9)     | 🟡 上线后任何人能连库               | ✅ 已修(早于本批) | 强制从 `.env` 读,默认空则 fail,`POSTGRES_PASSWORD` 走 `${VAR:?...}`     |

> ✅ 此外,v0.8.3 还修了原本没列在 CHECKLIST.md §4.1 的 **第 7 项 P0**(容器以 root 跑,见 §3.4):`5938bbf feat(security): P0-3 容器硬化`。

### 4.2 配置层(P1:环境与密钥)

- [x] ✅ **`.env` 红线**(per AGENTS.md §8):任何时候**不写入** .env 文件,启动时用 PowerShell/env 注入(v0.8.3 起)
- [x] ✅ **密钥管理**:LLM_API_KEY / BOCHA_API_KEY / JWT_SECRET 都走环境变量,**不**写进 git(`b759d76` 后用 `${VAR:?...}` 兜底)
- [x] ✅ **`.env.example` 同步更新**:每加一个新配置项,必须同时改 .env.example(v0.8.3 SearXNG/DuckDuckGo 已清理,Bocha 字段已补)
- [ ] ⏳ **数据库连接池**:生产环境调大 MaxOpenConns / MaxIdleConns(默认 10/5 太小)— **仍未修**,部署前手动设

### 4.3 运维层(P1:基础设施)

- [ ] **nginx 反向代理配置**(frontend + API + WS 三条路径)
- [ ] **Certbot 自动续签 SSL 证书**(Let's Encrypt)
- [ ] **HTTPS 强制跳转**(HTTP→HTTPS)
- [ ] **WebSocket over WSS**(nginx 需要 `Upgrade` / `Connection` 头透传)
- [ ] **安全组最小化**(只开 22/80/443)
- [ ] **SSH key 登录**(禁用密码登录)
- [ ] **fail2ban 防护 SSH 爆破**(可选)
- [ ] **systemd 守护 docker compose**(服务器重启后自动拉起)

---

## 5. 部署流程(分阶段推进)

### 阶段 1:准备工作(预计 1 天)

- [ ] 拍板基础设施选型(已定:香港 2C2G / Ubuntu 22.04,见 §2.4 / §2.5)
- [ ] 修 §4.1 的 6 个 P0 代码 bug
- [ ] 本地用 `docker compose up -d` 端到端验证
- [ ] 跑一次完整庭审,确认 LLM 链路通
- [ ] 选 Q12 域名后缀(香港无限制,见 §2.6)

### 阶段 2:阿里云购买(预计 30 分钟)

- [ ] 注册 + 实名认证
- [ ] 买轻量应用服务器(**香港** / 2C2G / 40G SSD)
- [ ] 选镜像:Ubuntu 22.04 LTS
- [ ] 选 ¥56/月 永久同价套餐(香港无 ¥38 羊毛)
- [ ] 设置实例名 + 主机名
- [ ] 付款

### 阶段 3:服务器初始化(预计 1-2 小时)

- [ ] 重置 root 密码
- [ ] 配置安全组(只放行 22/80/443)
- [ ] SSH 登录验证
- [ ] 装 Docker + Docker Compose
- [ ] 配置时区(Asia/Shanghai)+ NTP
- [ ] 装基础工具:git / curl / vim / htop

### 阶段 4:应用部署(预计 1-2 小时,**v0.8.3+ 走 ACR 工作流**)

> **两种模式**:
> - **A. 快速验证模式**(临时):服务器本地 `git clone + docker compose up`,等同 §3.5.1 "服务器本地构建" 表格左列
> - **B. 生产推荐模式**(当前选定):本地 build → push 到 ACR → 服务器 pull → compose up
>
> 本节只列 **B 模式** 步骤,A 模式用于冒烟测试或断网应急。

#### 阶段 4-A(本地):构建并推送镜像到 ACR(预计 5-15 分钟)

> **执行人**:本地开发机(Windows + Docker Desktop 或 WSL2)

- [ ] 拉取最新代码 `git pull origin main`
- [ ] 确认 `.env` 已填好真实 key(LLM_API_KEY / BOCHA_API_KEY / JWT_SECRET / POSTGRES_PASSWORD)
- [ ] **本地构建镜像**:
  ```bash
  # backend
  cd backend && docker build -t dc_backend:v0.8.3 .
  # frontend(需要 build args:API URL 等)
  cd ../frontend
  docker build \
    --build-arg NEXT_PUBLIC_API_URL=https://<your-domain> \
    --build-arg NEXT_PUBLIC_WS_URL=wss://<your-domain> \
    --build-arg NEXT_PUBLIC_USE_MOCK=false \
    -t dc_frontend:v0.8.3 .
  ```
- [ ] **登录阿里云 ACR**(一次性,有效期 12 小时):
  ```bash
  docker login --username=<your-aliyun-username> crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com
  # 输入密码(开通服务时设置,可在「访问凭证」页面修改)
  ```
- [ ] **打 tag 并 push**(详见 §10.2):
  ```bash
  # 建议三种 tag 都打(语义版本 / minor 浮动 / git SHA)
  SHORT_SHA=$(git rev-parse --short HEAD)
  REG=crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com/decision-court
  
  docker tag dc_backend:v0.8.3  $REG/decisioncourt-backend:v0.8.3
  docker tag dc_backend:v0.8.3  $REG/decisioncourt-backend:v0.8
  docker tag dc_backend:v0.8.3  $REG/decisioncourt-backend:v0.8.3-$SHORT_SHA
  docker push $REG/decisioncourt-backend --all-tags
  
  docker tag dc_frontend:v0.8.3 $REG/decisioncourt-frontend:v0.8.3
  docker tag dc_frontend:v0.8.3 $REG/decisioncourt-frontend:v0.8
  docker tag dc_frontend:v0.8.3 $REG/decisioncourt-frontend:v0.8.3-$SHORT_SHA
  docker push $REG/decisioncourt-frontend --all-tags
  ```

#### 阶段 4-B(服务器):拉取镜像并启动(预计 5-10 分钟)

> **执行人**:香港 ECS(SSH 登录)

- [ ] 配 `.env`(**只放密钥,不放镜像 tag**;tag 在 compose 文件里):
  ```bash
  scp .env.example root@<server-ip>:/opt/decisioncourt/.env
  ssh root@<server-ip> 'cd /opt/decisioncourt && nano .env'  # 填真实 key
  ```
- [ ] 把生产版 `docker-compose.yml`(用 `image:` 替代 `build:`)上传到服务器:
  ```bash
  scp docker-compose.acr.yml root@<server-ip>:/opt/decisioncourt/
  ```
- [ ] **登录 ACR**(用 **VPC 内网地址**,香港 ECS 走内网拉镜像):
  ```bash
  ssh root@<server-ip>
  docker login --username=<your-aliyun-username> crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com
  ```
- [ ] **拉取镜像 + 启动**:
  ```bash
  cd /opt/decisioncourt
  docker compose -f docker-compose.yml -f docker-compose.acr.yml pull   # 拉 backend + frontend
  docker compose -f docker-compose.yml -f docker-compose.acr.yml up -d # 启动 4 服务
  ```
- [ ] **健康检查**:
  ```bash
  curl http://127.0.0.1:8080/health   # 期望 200 OK
  curl -I http://127.0.0.1:3000      # 期望 200,Content-Type: text/html
  docker compose ps                    # 4 个容器都是 healthy / running
  ```
- [ ] **配 nginx + Certbot**(参考 §3.1 反代配置)
- [ ] **配 SSL 证书**(Let's Encrypt,需先有域名)
- [ ] 测试 HTTPS 访问
- [ ] **首次部署后立刻观察**:`docker compose logs -f --tail=200 backend`,确认 GORM AutoMigrate 跑通(见 §6.5)

> 💡 **回滚**: `docker compose up -d --force-recreate` 配合 git SHA tag 立刻回到指定版本,无需重新构建。

### 阶段 5:上线验证(预计 1 小时)

- [ ] 浏览器访问,跑一次完整庭审
- [ ] 检查 WebSocket 流式发言正常
- [ ] 检查 DeepSeek API 调用正常
- [ ] 检查 Bocha 搜索正常
- [ ] 检查日志(`docker compose logs -f`)
- [ ] 检查 `/metrics` 端点

### ~~阶段 6:备案 + 绑域名(预计 7-20 天)~~(已废弃,2026-07-03 第九次讨论)

> ~~原计划 7-20 天备案流程~~ **已废弃**。改香港后免备案,Q6/Q11 自然失效。

### 阶段 6:买域名 + 绑 SSL(已简化,1-2 天)

> **香港免备案,域名后缀自由选择**(任意后缀都行)。SSL 证书(Let's Encrypt)可正常签发,无需备案号。

- [ ] 买 Q12 决定的域名后缀(阿里云 / Cloudflare / Namecheap 注册)
- [ ] 在阿里云 DNS 解析到公网 IP
- [ ] Certbot 用域名签发证书
- [ ] 验证 `https://你的域名.后缀` 正常
- [ ] 浏览器不再标记

---

## 6. 运维清单(上线后要做的事)

### 6.1 监控

- [ ] **基础**:docker compose 进程存活(用 `restart: unless-stopped` 拉起)
- [ ] **进阶**:v0.8 已实装 `decision_events` + `/metrics` JSON 端点
- [ ] **LLM 预算监控**(per Q10):¥30/月封顶
  - 每日 cron 跑 SQL: `SELECT SUM(total_tokens) FROM llm_calls WHERE created_at > NOW() - INTERVAL '1 month'`
  - 折算 DeepSeek 价格(chat ¥1/M,reasoner ¥4/M)→ 估算月度成本
  - 达 ¥20 → 邮件 / 钉钉预警
  - 达 ¥30 → 熔断(新庭审拒绝,老庭审继续到结束)
- [ ] **告警**:暂未实装,Phase C(roadmap/whitebox-roadmap.md)才做 Prometheus

### 6.2 备份

- [ ] **数据库**:cron 每日 `pg_dump` → OSS / 异地
- [ ] **保留期**:开发阶段 7 天,商业化前 30 天
- [ ] **恢复演练**:每月测一次"删库 → 从备份恢复"

### 6.3 日志

- [ ] **JSON 文件日志**:v0.8 `backend/logs/agent_gateway_*.log` 已实装
- [ ] **日志轮转**:logrotate 配 `/etc/logrotate.d/decisioncourt`
- [ ] **集中化**:暂未实装,Phase E 才有数据仓库

### 6.4 安全

- [ ] **每月**:升级 `go.mod` 依赖
- [ ] **每季度**:轮换 JWT_SECRET
- [ ] **异常登录**:检查 `/var/log/auth.log`

### 6.5 v0.8.3 本地冒烟测试结果(2026-07-04)

> **意义**: 这一步是"写完代码 + 静态审计 → 真实跑起来 → 暴露 bug"的循环产物。  
> 比静态审计更珍贵,因为它揭示了真实部署级故障。

#### 测试环境

- Windows 11 Pro + Docker Desktop(WSL2 / Hyper-V)
- Docker Compose v2.x + BuildKit 启用
- 4 容器:`dc_backend` / `dc_frontend` / `dc_postgres` / `dc_redis`

#### 暴露并修复的 4 个 P0/P1 真实部署 bug(`1277522`)

| Bug | 文件 | 影响 | 修复方法 |
|---|---|---|---|
| **`db.AutoMigrate` 漏 `User{}` / `AuditLog{}`** | [`backend/internal/model/db.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/model/db.go) | 首次部署任何 anon 鉴权 SQLSTATE 42P01,handler 静默吞错返回 code=0(monkey 用的 mock 测试看不见错) | 加 `&User{}` 和 `&AuditLog{}` 到迁移列表 |
| **`# syntax=docker/dockerfile:1.6` 拉不到 `auth.docker.io:443`** | 2 个 `Dockerfile` | `--no-cache` 重 build 时 BuildKit 拉 frontend parser 镜像必卡(registry-1.docker.io 通但 auth.docker.io `162.125.2.6` 被 GFW 拦) | 删该 directive,BuildKit 内置 parser 走通 |
| **`pnpm@latest` 11.x 与 `lockfileVersion: 9.0` 不兼容** | [`frontend/Dockerfile`](file:///d:/源码/FullStack/DecisionCourt/frontend/Dockerfile) | `ERR_UNKNOWN_BUILTIN_MODULE` 编译失败 | `corepack prepare pnpm@9.15.4 --activate` |
| **frontend 缺 `public/` 目录** | [`frontend/Dockerfile`](file:///d:/源码/FullStack/DecisionCourt/frontend/Dockerfile) | runtime stage `COPY --from=builder /app/public` 失败 | builder `RUN mkdir -p /app/public` |

#### 端到端验证通过

```
POST /api/v1/auth/anon                    → 200 + JWT + cookie
POST /api/v1/courtrooms                   → 200 + session_uuid
GET  /api/v1/courtrooms/{id}/messages     → 200 (空)
GET  /api/v1/courtrooms/{id}/agents       → 200 (5 个 agent)
GET  /api/v1/courtrooms/notexist/messages → 404 庭审不存在
POST /api/v1/courtrooms/{id}/start        → 200 phase=opening (同步)
```

完整鉴权链 → DB 落库 → 资源 ownership → auto-generated agents → 启动 trial,全部跑通。

#### 新增 Ops 工具

**[`tools/envcheck.ps1`](file:///d:/源码/FullStack/DecisionCourt/tools/envcheck.ps1)** — 修改 `.env` 后必跑

```powershell
powershell -ExecutionPolicy Bypass -File tools\envcheck.ps1
```

检查项:
- 重复 key(如 `PORT` / `DATABASE_URL` / `REDIS_URL` 重复会让 docker compose 默认取第一个,导致 env 覆盖失效)
- placeholder 残留(`your-key-here`、`change-me` 等)
- 误配 docker 主机名(host 端 `localhost` 不可达 `postgres:5432`,必须用服务名 `postgres`)

---

## 7. 风险与备选方案(2026-07-03 第九次讨论已反转)

> **策略反转后,以下风险已消除**:备案被驳回、¥38 羊毛到期涨价、IP 直连合规、浏览器"不安全"。

| 风险                    | 影响              | 备选方案                                            | 状态      |
| ----------------------- | ----------------- | --------------------------------------------------- | --------- |
| ~~备案被驳回~~          | ~~7-20 天延期~~   | ~~改用香港机房(免备案)~~                            | ✅ 已消除 |
| ~~¥38 羊毛到期涨价~~    | ~~续费成本 ×2-3~~ | ~~续 ¥60-100 / 升 2C4G / 换厂商~~                   | ✅ 已消除 |
| **2C2G 配置 OOM**       | 服务宕机          | 升 2C4G(¥150);前期监控 + 告警                       | ⚠️ 仍存   |
| ~~IP 直连合规风险~~     | ~~法律灰区~~      | ~~立即启动备案~~                                    | ✅ 已消除 |
| ~~浏览器标记"不安全"~~  | ~~作品集不专业~~  | ~~备案后买域名 + 签 SSL 证书~~                      | ✅ 已消除 |
| **DeepSeek API 不稳定** | 庭审卡住          | 配置 LLM provider 切换(kimi/GLM-4)                  | ⚠️ 仍存   |
| **Bocha API 限额**      | 调查员失败        | 切到 searxng 自部署 / Tavily(2C2G 不建议跑 searxng) | ⚠️ 仍存   |
| **流量超预期**          | 服务崩            | 加 v0.9 多实例 + 负载均衡                           | ⚠️ 仍存   |
| **数据丢失**            | 用户失去历史      | 备份恢复(见 §6.2)                                   | ⚠️ 仍存   |
| **被攻击**              | 服务被刷爆        | Cloudflare 代理 + 限流                              | ⚠️ 仍存   |

---

## 8. 决策记录(Discussion Log)

> 每次做出决定,记录在这里,带日期。避免"我们之前说过来着"。

### 2026-07-03 第九次讨论(策略反转:大陆→香港)

> **重大策略反转**:从大陆华东 1 → 阿里云香港。理由:免备案 + 价格更便宜 + 永久同价。

- 🔄 **Q3 地域重选 = 阿里云香港**(用户决定)
  - 影响:取消所有备案流程
  - 影响:国内延迟 5-20ms → 30-80ms(可接受)
  - 影响:年成本 ¥876-916 → **¥1092-1132**
- 🔄 **Q5 具体地域 = 阿里云香港**(随 Q3 一起重定)
- 🔄 **Q4 域名策略 = 任意后缀都行**(香港免备案,无路径 A 限制)
- 🔄 **Q6 备案等待 = N/A**(香港免备案)
- 🔄 **Q11 备案资料 = N/A**(不再需要备案)
- 🔄 **Q12 域名后缀 = "到时候再看"(2026-07-03 第九次讨论后由用户决定)**
  - 触发条件:购机时或之后任意时点
  - 候选:.cn / .com / .io / .app / 暂不买
- ✅ **Q2 预算调整** = ¥50/月以下 → ¥56/月(可接受溢价 ¥18/月)
- ✅ **配置 = 2C2G ¥56/月**(1C1G ¥24 跑不动 4 Agent)
- ⏳ **Q7 优惠期重审**:香港轻量**没有"新人羊毛"机制**,¥56 永久同价 → 风险已消除

### 2026-07-03 第八次讨论(规划关闭,后被反转)

- ✅ **下一步选择 = (a) 总结 + 停在这里**
  - 规划阶段关闭,文档封存
  - 不预先修 bug / 写脚本
  - **被第九次讨论推翻**:用户决定改香港,规划重新打开

### 2026-07-03 第六次讨论(备案路径选择,后被反转)

- ✅ **路径选择 = A(先买域名再备案)**(用户听完讲解后选)
- ✅ **Q12 域名后缀 = .cn**(用户选推荐)
  - 影响:年成本 ¥60-100
  - 影响:品牌如 `decisioncourt.cn`
  - 影响:必须通过 ICP 备案才能解析到国内主机

### 2026-07-03 第七次讨论(规划完成,待总结)

- ✅ 所有 12 个核心 Q + 1 个补充 Q 答完
- ⏳ 用户授权"先讨论完再买机",规划阶段结束,待决定下一步

### 2026-07-03 第四次讨论(执行细节)

- ✅ **Q9 操作系统 = Ubuntu 22.04 LTS**(用户选推荐)
- ✅ **Q10 LLM 月度预算 = ¥30/月**(需加监控 + 熔断)
- ⚠️ **Q11 备案资料 = 还没考虑 / 不着急** —— **与 Q6 矛盾,需用户澄清**

### 2026-07-03 第三次讨论(技术细节)

- ✅ **Q5 地域 = 华东 1(杭州)**(用户授权代选,基于通用推荐)
- ✅ **Q6 备案 = 购机后立刻启动**(不留法律真空期)
- ✅ **Q7 优惠期 = 1 年,到期待评估**(2027-07 月前重新决策)
- ✅ **Q8 IP 风险 = 低风险可接受**(明白灰色地带,接受 7-20 天过渡)

### 2026-07-03 第二次讨论(Q&A 决策)

- ✅ **Q1 服务对象 = 公开作品集** —— 决定 HTTPS / 备案 / 备份都要做
- ✅ **Q2 预算 = ¥50/月以下** —— 决定走薅 ¥38 羊毛策略,接受 2C2G 配置
- ✅ **Q3 地域 = 国内大陆** —— 决定必须走 ICP 备案流程
- ✅ **Q4 域名 = 暂不买, IP 直连** —— 短期方案,备案后再补

### 2026-07-03 第一次讨论(基础架构)

- ✅ **部署目标 = 阿里云轻量应用服务器**
- ✅ **架构 = 单机 docker-compose**(不放微服务)
- ✅ **运维 = nginx + Certbot**(不自建 K8s)
- ✅ **不在文档中修改 .env 文件**(per AGENTS.md §8 红线)
- ✅ **已识别**:6 个 P0 代码 bug(§4.1)

---

## 9. 待办与新涌现的问题

### 9.1 已回答的核心问题 ✅(2026-07-03 第九次讨论后)

> **⚠️ 标记 ⭐ 的决策已被反转**

| #   | 问题         | 答案                               | 状态      |
| --- | ------------ | ---------------------------------- | --------- |
| Q1  | 服务谁?      | 公开作品集                         | ✅ 不变   |
| Q2  | 预算上限?    | **<¥56/月(香港 2C2G 永久价)** ⭐   | 🔄 已变   |
| Q3  | 用户地域?    | **阿里云香港** ⭐                  | 🔄 已反转 |
| Q4  | 域名策略?    | **香港 + 任意后缀 / 暂不买** ⭐    | 🔄 已反转 |
| Q5  | 具体地域?    | **阿里云香港** ⭐                  | 🔄 已反转 |
| Q6  | 备案等待?    | **N/A(免备案)** ⭐                 | 🔄 已反转 |
| Q7  | 优惠期?      | **香港轻量永久同价,无羊毛机制** ⭐ | 🔄 已反转 |
| Q8  | IP 风险偏好? | 低风险可接受                       | ✅ 不变   |
| Q9  | 操作系统?    | Ubuntu 22.04 LTS                   | ✅ 不变   |
| Q10 | LLM 预算?    | ¥30/月                             | ✅ 不变   |
| Q11 | 备案资料?    | **N/A(免备案)** ⭐                 | 🔄 已反转 |
| Q12 | 域名后缀?    | **到时候再看** ⭐                  | 🔄 推迟   |
| Q13 | 开始时间?    | 先讨论 Q12,暂不购机                | ✅ 不变   |

### 9.2 已解决的矛盾 ✅

> ~~Q6 vs Q11 矛盾~~(2026-07-03 第五次讨论已解决,后被第九次讨论反转)
> 备案政策(Q6)保持有效。Q11 澄清为"资料没备齐",一旦备齐立即启动。
> **最终**:改香港后,Q6 / Q11 都 N/A,矛盾自然消失。

### 9.3 仍待解决的执行层问题

- [ ] 🔄 **Q12 域名后缀 = 待重选**(香港无限制:.com / .cn / .io / .app 都可以)
- [x] ✅ **Q13**:开始时间 = 先讨论 Q12,暂不购机

### 9.4 关键时间节点

- **2026-07-XX**:购机 + 启动备案
- **2026-08-XX**:备案审核中(同时跑 IP 访问)
- **2026-08-XX**:备案通过 + 买域名 + 签 SSL
- **2027-07-XX**:¥38 优惠到期,重新评估续费

### 9.5 下一步可选方向(已选择)

- [x] ✅ **(a) 总结 + 关闭规划阶段**(2026-07-03 决定)
- [ ] (b) 继续讨论(LLM 提供商细节 / 备份策略 / 监控告警 / Docker 优化)
- [ ] (c) 修 §4.1 的 6 个 P0 代码 bug(技术准备)
- [ ] (d) 写**部署脚本**(shell 脚本一键装 Docker + 配 nginx + 拉代码 + 启动)
- [ ] (e) 写**操作手册**(上线后运维步骤)
- [x] ✅ **(f) 引入阿里云 Container Registry**(2026-07-XX 增补,见 §10)

---

## 10. 阿里云 Container Registry 操作手册(2026-07-XX 增补)

> **本章配套**:`docs/deployment/CHECKLIST.md` §3.5 镜像仓库架构 + §5 阶段 4
> **官方参考**:[阿里云容器镜像服务 ACR · 个人版文档](https://help.aliyun.com/document_detail/60717.html)
> **触发**:v0.8.3 安全 commit 跑通后,为支持「服务器零工具链 + 版本化 + CI/CD 友好」而引入

### 10.1 一次性初始化(注册 → 创建命名空间 → 创建仓库)

#### 10.1.1 开通服务

1. 登录阿里云控制台 → **容器镜像服务 ACR**(搜「容器镜像」)
2. 首次进入提示"开通服务",选**个人版**(免费,够本项目用)
3. 地域选 **香港**(`cn-hongkong`),与 ECS 地域对齐 → 走 VPC 内网拉取

#### 10.1.2 设置访问凭证

1. 控制台 → 左侧 **访问凭证**
2. 设置/重置 Registry 登录密码(**与阿里云账号密码不同**)
3. 用户名 = 阿里云账号全名(或 RAM 子账号全名;子账号支持英文半角句号)

> ⚠️ **不要把密码写进 git / 文档 / .env 文件**。本地缓存用 `docker login`(12 小时有效)。

#### 10.1.3 创建命名空间 + 仓库

```
命名空间: decision-court       ← 项目级隔离(类似 GitHub org)
仓库名:
  decisioncourt-frontend       ← 类型:私有
  decisioncourt-backend        ← 类型:私有
仓库地域: 香港(cn-hongkong)    ← 与 ECS 对齐
```

**控制台操作**:
1. ACR 控制台 → 左侧 **个人版** → **命名空间** → 创建 `decision-court`
2. **镜像仓库** → 创建镜像仓库 → 选命名空间 `decision-court` → 仓库名 `decisioncourt-frontend` → 地域香港 → 仓库类型**私有**
3. 同样方式创建 `decisioncourt-backend`

#### 10.1.4 拿到 Registry URL

创建完后,每个仓库详情页会显示形如:

```
公网:  crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com
VPC:   crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com
```

> **`<id>` 是阿里云分配的实例 ID**(随机串)。**写进文档 / commit message 时必须用 `<id>` 占位代替**。

### 10.2 本地构建 + 推送(开发者机器)

#### 10.2.1 登录(每次有效期 12 小时)

```bash
# 公网登录(从本地推送)
docker login --username=<your-aliyun-username> crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com
# 提示输入密码:粘贴访问凭证密码(不回显)
# 登录成功显示 "Login Succeeded"
```

> **RAM 子账号登录**:`--username` 用子账号全名,**不支持企业别名带英文半角句号(.)**。详情见官方文档 §"RAM 用户登录"。

#### 10.2.2 构建镜像(注意 build args)

```bash
# 1) backend — 无 build args
cd backend
docker build -t dc_backend:v0.8.3 .
cd ..

# 2) frontend — 必须传 NEXT_PUBLIC_* build args(v0.9 修复)
cd frontend
docker build \
  --build-arg NEXT_PUBLIC_API_URL=https://<your-domain> \
  --build-arg NEXT_PUBLIC_WS_URL=wss://<your-domain> \
  --build-arg NEXT_PUBLIC_USE_MOCK=false \
  -t dc_frontend:v0.8.3 .
cd ..
```

> ⚠️ **NEXT_PUBLIC_\* 是 build-time 变量**,编译时被 inline 到 bundle.js。如果忘了传,前端所有 API 调用会走 `http://localhost:3000/...` 直接失败。

#### 10.2.3 打 tag(三种 tag 都打)

```bash
# 准备变量
REG=crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com/decision-court
VERSION=v0.8.3
SHORT_SHA=$(git rev-parse --short HEAD)

# ===== backend =====
docker tag dc_backend:$VERSION  $REG/decisioncourt-backend:$VERSION
docker tag dc_backend:$VERSION  $REG/decisioncourt-backend:${VERSION%.*}   # v0.8 浮动
docker tag dc_backend:$VERSION  $REG/decisioncourt-backend:$VERSION-$SHORT_SHA

# ===== frontend =====
docker tag dc_frontend:$VERSION $REG/decisioncourt-frontend:$VERSION
docker tag dc_frontend:$VERSION $REG/decisioncourt-frontend:${VERSION%.*}  # v0.8 浮动
docker tag dc_frontend:$VERSION $REG/decisioncourt-frontend:$VERSION-$SHORT_SHA
```

**三种 tag 各有用途**:

| Tag 模式 | 指向 | 用法 |
|---|---|---|
| `v0.8.3` | 完整语义版本 | 生产 `docker-compose.yml` 锁这个(稳定) |
| `v0.8` | minor 浮动 | 手动升级到最新 patch 用 |
| `v0.8.3-abc1234` | git short SHA | **精准回滚**("回到 7 天前的版本") |

#### 10.2.4 推送

```bash
# 推一个仓库的全部 tag(等价于 push 3 次)
docker push $REG/decisioncourt-backend --all-tags
docker push $REG/decisioncourt-frontend --all-tags

# 验证(去阿里云控制台 → ACR → 镜像仓库 → 版本列表,应该看到 3 个 tag)
```

### 10.3 服务器拉取 + 部署(香港 ECS)

#### 10.3.1 准备生产版 compose 文件

**不直接改原 `docker-compose.yml`**(本地开发还要 `build:`),而是新建 `docker-compose.acr.yml` 做 override:

```yaml
# docker-compose.acr.yml
# 用途:生产环境,从阿里云 ACR 拉镜像,不走本地 build
# 用法:docker compose -f docker-compose.yml -f docker-compose.acr.yml <command>

services:
  backend:
    # 覆盖 build:,改用 image: 从 ACR 拉
    image: crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com/decision-court/decisioncourt-backend:v0.8.3
    pull_policy: always   # 强制每次 up 前 pull 最新

  frontend:
    image: crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com/decision-court/decisioncourt-frontend:v0.8.3
    pull_policy: always
```

> **VPC 内网 URL**(`-vpc-`):香港 ECS 走阿里云内网拉镜像,速度 10-100x,且**不消耗公网流量**。公网 URL 留给本地开发者推送。

#### 10.3.2 服务器登录 + 拉取 + 启动

```bash
# 1) SSH 登录 ECS
ssh root@<server-ip>

# 2) 登录阿里云 ACR(用 VPC 内网地址)
docker login --username=<your-aliyun-username> crpi-<id>-vpc.cn-hongkong.personal.cr.aliyuncs.com

# 3) 上传生产 compose 文件(本地执行)
scp docker-compose.acr.yml root@<server-ip>:/opt/decisioncourt/

# 4) 服务器上拉镜像
cd /opt/decisioncourt
docker compose -f docker-compose.yml -f docker-compose.acr.yml pull

# 5) 启动(后台)
docker compose -f docker-compose.yml -f docker-compose.acr.yml up -d

# 6) 验证
docker compose ps                                 # 4 容器 healthy
curl http://127.0.0.1:8080/health                # 200
curl -I http://127.0.0.1:3000                    # 200 HTML
docker compose logs -f --tail=100 backend         # 看 GORM AutoMigrate 跑通
```

#### 10.3.3 升级(发新版本)

```bash
# 本地(开发者机器)
# 1) 改代码 + commit
git commit -m "feat: xxx"
# 2) 重建 + 推新 tag(假设版本升到 v0.8.4)
VERSION=v0.8.4
SHORT_SHA=$(git rev-parse --short HEAD)
REG=crpi-<id>.cn-hongkong.personal.cr.aliyuncs.com/decision-court

docker build ... -t dc_backend:$VERSION .
docker tag dc_backend:$VERSION $REG/decisioncourt-backend:$VERSION
docker push $REG/decisioncourt-backend --all-tags
# 同样处理 frontend

# 服务器(SSH)
# 1) 改 docker-compose.acr.yml 里的 tag
sed -i 's/:v0.8.3/:v0.8.4/g' /opt/decisioncourt/docker-compose.acr.yml

# 2) 滚动升级(零停机)
cd /opt/decisioncourt
docker compose -f docker-compose.yml -f docker-compose.acr.yml pull
docker compose -f docker-compose.yml -f docker-compose.acr.yml up -d
```

#### 10.3.4 回滚(出错立刻回上一版本)

```bash
# 服务器
cd /opt/decisioncourt
# 方式 A:用 git SHA tag 回到指定 commit
sed -i 's/:v0.8.4/:v0.8.3-abc1234/g' /opt/decisioncourt/docker-compose.acr.yml
docker compose -f docker-compose.yml -f docker-compose.acr.yml up -d --force-recreate

# 方式 B:回上一稳定版
sed -i 's/:v0.8.4-xyz/:v0.8.3/g' /opt/decisioncourt/docker-compose.acr.yml
docker compose -f docker-compose.yml -f docker-compose.acr.yml up -d --force-recreate
```

> 回滚的本质:**只用 ACR 已存在的镜像**,不需要重新构建,30 秒内回到任意历史版本。

### 10.4 网络选择:公网 vs VPC 内网

| 场景 | 用哪个 URL | 原因 |
|---|---|---|
| 本地开发者推送 | **公网** `crpi-<id>.cn-hongkong...` | ECS 才在 VPC,本地不在 |
| 本地开发者拉(测试镜像) | 公网 | 同上 |
| **ECS 拉镜像(生产)** | **VPC** `crpi-<id>-vpc.cn-hongkong...` | 内网 0 流量费 + 速度快 |
| CI/CD runner(在阿里云上) | VPC | 同上 |
| CI/CD runner(GitHub Action 等海外) | 公网 | 不在阿里云 VPC |

> ⚠️ **VPC 地址只能从同一地域的阿里云 VPC 内访问**。从公网或异地 VPC 拉取会失败。

### 10.5 安全最佳实践

| 措施 | 说明 | 状态 |
|---|---|---|
| **仓库类型 = 私有** | 不开公开访问(任何人可 pull = 攻击面↑) | ✅ 强制 |
| **AccessKey 用 RAM 子账号** | 主账号 AK 泄露 = 整个阿里云沦陷;RAM 子账号可限定 ACR 权限 | ⏳ 待办 |
| **登录密码定期轮换** | 控制台 → 访问凭证 → 重置 | ⏳ 每季度 |
| **`.dockerignore` 拦截敏感文件** | 见 `.dockerignore` 当前规则 | ✅ |
| **image 不带 `:latest` tag** | 防止基础镜像被供应链攻击偷偷塞进 base | ✅ 已实装(锁版本) |
| **镜像签名(可选)** | ACR EE 版支持 cosign 签名,个人版暂不支持 | ⏳ 未来 |
| **拉取日志审计** | ACR 控制台 → 访问日志 → 看谁拉过镜像 | ✅ 自带 |

### 10.6 `.dockerignore` 当前规则(2026-07-XX 验证)

确认 `backend/.dockerignore` 和 `frontend/.dockerignore` 已排除:

```
.git
.env
.env.*
backend/test-output/
frontend/node_modules/
frontend/.next/
**/*.log
**/coverage/
```

> ⚠️ **如果 `.env` 被错误 COPY 进镜像 → 立刻轮换 LLM_API_KEY / BOCHA_API_KEY / JWT_SECRET / POSTGRES_PASSWORD**(这些 key 一旦进镜像 = 公开了)。

### 10.7 故障排查

| 症状 | 可能原因 | 排查 / 修法 |
|---|---|---|
| `docker push` 报 `denied: requested access to the resource is denied` | (1) 仓库名拼错 (2) 命名空间不存在 (3) RAM 子账号无 `cr:Push` 权限 | (1) 检查拼写 (2) 控制台确认 namespace + repo (3) RAM 控制台授权 |
| `docker pull` 报 `unauthorized: authentication required` | (1) `docker login` 过期 (2) 用了错网络地址(ECS 用公网拉失败) | (1) 重新 login (2) ECS 用 `-vpc-` 地址 |
| `docker pull` 报 `pull access denied` 或 `repository does not exist` | (1) 仓库是私有的但当前账号无权限 (2) tag 不存在 | (1) 让 owner 加 RAM 权限 (2) `curl -u user:pass https://crpi-<id>.../v2/_catalog` 验证 |
| `image not found` 在 `docker compose up` | `docker-compose.acr.yml` 里 image 拼错或 tag 不存在 | `docker images \| grep decisioncourt` 验证本地有 / 阿里云控制台验证 tag 已 push |
| `docker compose up` 后 backend 启动失败 | GORM AutoMigrate 报错(§6.5 smoke 修过类似) | `docker compose logs backend` 看具体错误;常见是 .env 缺 POSTGRES_PASSWORD |
| **VPC 内网拉镜像慢/失败** | ECS 不在 ACR 同地域,或没开通 VPC 内网访问 | 控制台 → ACR → 仓库 → 设置 → 勾选"允许 VPC 内网访问" |

### 10.8 与项目其他文档的交叉引用

- **架构图**:§3.1(本文件)
- **v0.8.3 容器硬化**:§3.4(本文件)+ `security-audit-v0.8.3.md` §2.1 P0-3
- **服务器 docker-compose 改造**:生产部署前需要把 `docker-compose.yml` 拆成 base + override,见 §10.3.1
- **CI/CD 升级(未来)**:GitHub Action → `docker build + push to ACR` → ECS `docker compose pull + up`,无需 SSH
- **成本对比**:ACR 个人版免费额度 300 个仓库 / 命名空间无限,**本项目 0 额外成本**

### 10.9 决策记录

| 日期 | 决策 | 原因 |
|---|---|---|
| 2026-07-XX | 引入阿里云 ACR 个人版(香港) | 服务器零工具链 + 版本化 + CI/CD 友好 + 0 额外成本 |
| 2026-07-XX | 用 VPC 内网地址拉镜像 | 速度 + 不消耗公网流量 |
| 2026-07-XX | 三种 tag 策略(语义版/minor/SHA) | 稳定 / 自动跟新 / 精准回滚 三场景覆盖 |
| 2026-07-XX | 用 `docker-compose.acr.yml` override,不直接改原 compose | 保留本地开发 `build:` 能力 |

---

## 附录:文档变更日志

| 日期 | 章节 | 变更 | 作者 |
|---|---|---|---|
| 2026-07-03 | 全文 | 初始化:第九次讨论决定(香港 2C2G / 免备案 / 任意域名) | 项目 owner |
| 2026-07-04 | §6.5 | 新增 v0.8.3 本地冒烟测试结果 + 4 个真实 bug 修复 | Agent |
| 2026-07-XX | §3.5 / §5 / §10 | **新增阿里云 Container Registry 工作流**(本文档更新) | Agent |

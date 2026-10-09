# 第二次上线记录 · 新服务器 8.218.24.43（2026-10-09）

| | |
|---|---|
| **日期** | 2026-10-09 |
| **性质** | 第二次生产上线（第一次：2026-07-12，旧 ECS `47.239.152.177`，已释放） |
| **服务器** | `8.218.24.43` · 阿里云 ECS · 中国香港 · Ubuntu 24.04.2 LTS · Docker 29.8.2 + compose v2 · 2GB swap |
| **域名 / 证书** | `decisioncourt.cn`（解析已改指新 IP）· Let's Encrypt `2026-10-08 → 2027-01-06` |
| **上线版本** | `3fc2ae83`（首次落地）→ `a0edec6b`（Prompt Lab 接线，当前） |
| **结果** | ✅ 站点可用、跑当前 main；⚠️ **CI 的 `deploy` job 未走通**（SSH 认证失败），实际落地靠手动等价脚本 |
| **关联** | [`CHECKLIST.md`](./CHECKLIST.md)（部署规划）· [`../todo/deferred-items-2026-10-07.md`](../todo/deferred-items-2026-10-07.md) §R7–R13 · [`../OBSERVABILITY.md`](../OBSERVABILITY.md) §8 · [`_archived/production-retrospective-2026-08-05.md`](./_archived/production-retrospective-2026-08-05.md)（第一次上线的 30 天沉淀） |

> **本文档三个用途**：① 下次换机器/重装可照抄的流程；② 本次 9 条踩坑经验；③ 遗留项清单。
> 面向"3 个月后回来的自己"写，命令都是能直接粘的。

---

## 0. 一句话结论

第二次上线**成功**，但暴露了 5 个"代码已经坏/从来没通"的问题（R7–R11，全部在本次修掉），以及 2 个"配置看着对、数据其实拿不到"的问题（R12/R13，未修）。
**最值得记住的一条**：这次真正的风险不是"部署动作做错"，而是**镜像仓库里的镜像停留在 3 个月前**（ACR `:latest` 构建于 2026-07-07），所以首次部署必然把 3 个月的改动一次性暴露 —— 事实正是如此，五个问题**互相遮挡**，修掉一个才露出下一个。

---

## 1. 环境与验收结果

### 1.1 服务器画像（bootstrap 后）

| 项 | 值 |
|---|---|
| 用户 / 密钥 | `admin`（有 sudo NOPASSWD、在 `docker` 组）· 生效密钥 `~/.ssh/id_rsa`（RSA 4096 `lenvov@LAPTOP-VOOCEJD6`）；`id_ed25519` **两次实测都 Permission denied** |
| 目录 | `/opt/DecisionCourt/`（`docker-compose.yml` + `.env` + `deploy/caddy/Caddyfile` + `logs/`） |
| 镜像仓库 | 阿里云 ACR 个人版 `crpi-rnawo8jx69bsvlbx.cn-hongkong.personal.cr.aliyuncs.com`，用户名 `Exist-a`，两个仓库 `decision-court-backend` / `decision-court-frontend` |
| swap | 2GB，`swappiness=10`，写进 `/etc/fstab` 持久化（老机器复盘的第一条建议） |

### 1.2 验收证据（全部实测）

| 检查 | 结果 |
|---|---|
| `https://decisioncourt.cn/health` | 200 |
| `https://decisioncourt.cn/api/v1/health/llm` | 200 `{"configured":true,"provider":"deepseek","model":"deepseek-chat","key_preview":"sk-***"}` |
| `https://decisioncourt.cn/metrics` | 200 |
| `https://decisioncourt.cn/` | 200（23KB HTML） |
| 业务冒烟 | `POST /api/v1/auth/anon`（**必须带前端生成的 `user_id`**）→ 200 + JWT；`GET /api/v1/courtrooms` → 200 `{"count":0}`；`POST /courtrooms` 建会话 → 200；`POST/GET /courtrooms/:uuid/events` 埋点写读各 200 |
| 容器 | `dc_backend` healthy · `dc_frontend` up · `dc_postgres` healthy · `dc_redis` healthy · `dc_caddy` up |
| 启动日志三行（判断"跑的是不是当前代码"） | `promptlab loaded version=1.0.3-pr1@dev path=prompts/base.yaml` · `DecisionCourt backend listening port=8080 version=<镜像 tag>` · `recovery: scan complete active_sessions=0` |
| 前端构建期注入 | 服务的 JS chunk 里是 `https://decisioncourt.cn` / `wss://decisioncourt.cn`（说明 GitHub secrets `NEXT_PUBLIC_*` 正确） |

---

## 2. 与第一次上线的差异（为什么值得单独记）

| 维度 | 第一次（2026-07-12，旧 ECS） | 第二次（2026-10-09，新机） |
|---|---|---|
| 操作系统 | Ubuntu 22.04 | **Ubuntu 24.04.2** —— `needrestart` 会交互打断 apt，所有 apt 调用要 `sudo env NEEDRESTART_MODE=l DEBIAN_FRONTEND=noninteractive` |
| 代码跨度 | 与镜像同步 | **镜像停在 2026-07-07，代码已跑到 v2.13** —— 跨了 v0.10.20 → v1.x → v2.x 三个大版本 |
| 镜像 tag | 版本号（`v0.10.20`） | commit SHA（`deploy.yml` 的 `TAG = github.sha`），部署脚本再 retag 成 `:latest` |
| 部署触发 | push main → Test → Deploy（当时走通） | 同一条链路，但 **Deploy 的 SSH 步骤认证失败**（见 §4.7） |
| 数据 | 有 17 场真实庭审 | 全新 volume，AutoMigrate 建表，**空库** |

**核心差异**：第一次是"代码和镜像同步"，第二次是"镜像落后 3 个月"。后者会**把期间所有回归一次性压到一次部署里**，而且问题会互相遮挡（R7 挡着 R9，R9 挡着 R10……）。教训：**上线前先确认镜像是不是最新的**，`docker images` 看 `CreatedAt` 比看 tag 名字可靠。

---

## 3. 完整流程（可照抄）

### 3.1 服务器 bootstrap（一次性，本次已完成）

按顺序，每步都给"为什么"：

```bash
# ① 装 Docker —— 用官方 apt 源（download.docker.com；机器 apt 已配阿里云内网镜像，实测该源 50ms 可达）
sudo install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" \
  | sudo tee /etc/apt/sources.list.d/docker.list
# ⚠️ 24.04 的 needrestart 会交互打断 apt；`sudo VAR=x` 默认不被 sudoers 允许，必须走 `sudo env`
sudo env NEEDRESTART_MODE=l DEBIAN_FRONTEND=noninteractive apt-get update
sudo env NEEDRESTART_MODE=l DEBIAN_FRONTEND=noninteractive apt-get install -y \
  docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
sudo usermod -aG docker admin          # 之后新 SSH 会话里 admin 不用 sudo 就能跑 docker

# ② 加 swap（老机器 OOM 复盘的直接产物）
sudo fallocate -l 2G /swapfile && sudo chmod 600 /swapfile && sudo mkswap /swapfile && sudo swapon /swapfile
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/99-swap.conf

# ③ 建目录 + 上传编排文件（compose 与 Caddyfile 与仓库同源）
sudo mkdir -p /opt/DecisionCourt/deploy/caddy /opt/DecisionCourt/logs/backend /opt/DecisionCourt/logs/caddy
sudo chown -R admin:admin /opt/DecisionCourt
# 日志目录必须归容器 uid（否则 FileLogger / trace 全废，见 §6 遗留项 2 / R12）
sudo chown -R 10001:10001 /opt/DecisionCourt/logs
# 在本机仓库根目录跑（首次 bootstrap 用；之后 compose 由 CI 自动同步，Caddyfile 仍需人工）：
#   scp -i ~/.ssh/id_rsa docker-compose.yml admin@8.218.24.43:/opt/DecisionCourt/
#   scp -i ~/.ssh/id_rsa deploy/caddy/Caddyfile admin@8.218.24.43:/opt/DecisionCourt/deploy/caddy/

# ④ 上传 .env（§8 红线：Agent 不写 .env，由用户执行）
scp -i ~/.ssh/id_rsa secrets/.env.backup-2026-08-05 admin@8.218.24.43:/opt/DecisionCourt/.env

# ⑤ 服务器上登录 ACR（必须常驻！CI 的 deploy job 不做 login，见 §4.7）
ssh admin@8.218.24.43
docker login --username=Exist-a crpi-rnawo8jx69bsvlbx.cn-hongkong.personal.cr.aliyuncs.com

# ⑥ 起栈
cd /opt/DecisionCourt && docker compose up -d
```

**三个容易踩的点**：
- `.env` 里 `DATABASE_URL` 的密码必须与 `POSTGRES_PASSWORD` 一致（新 volume 按后者初始化）。
- 本机 `secrets/ecs.env` 与 GitHub Secret `ECS_HOST` 是**两处**，换机都要改；`scripts/ecs.ps1` 也曾硬编码 IP。
- **`logs/` 的属主**：Docker 会自动把 bind mount 的目标目录建成 `root:root`，而容器跑在
  uid **10001**（`docker-compose.yml` 的 `user`）→ **FileLogger 与 trace 全部写不进去**
  （本次实际踩到，见 §6 遗留项 2 / R12）。镜像现在会预建归属 appuser 的 `/app/logs`，
  但 bind mount 的属主由**宿主机**决定，所以起栈后仍要顺手修一次：
  ```bash
  sudo mkdir -p /opt/DecisionCourt/logs/backend /opt/DecisionCourt/logs/caddy
  sudo chown -R 10001:10001 /opt/DecisionCourt/logs    # 必须与 compose user / Dockerfile adduser uid 一致
  ```
  **别用 1001**：compose 与 Dockerfile 曾在这一点上不一致（R12），现在统一到 10001。
  起栈后可在启动日志里直接确认：出现 `agent_gateway: file logger log dir writable` 才算通。

### 3.2 发版（每次）

**正常路径（设计上）**：`git push origin main` → Test 工作流 → Deploy 工作流（build 镜像推 ACR → **同步 `docker-compose.yml` 到服务器** → SSH 进服务器 pull + retag `:latest` + `compose up -d --force-recreate backend frontend`）。

> **R17（2026-10-09 修）**：同步 compose 这一步**以前没有**，deploy 只对服务器上那份陈旧副本跑 `compose up` —— 于是"改了 compose"（如 R12 的 `user: 10001`）会**静默不生效**：镜像换了、容器还是按旧配置起。已在 deploy job 加 `actions/checkout` + `appleboy/scp-action`，并把 `grep -n 'user: "'` 打进 CI 日志以便肉眼确认。
> **⚠️ Caddyfile 的生效条件**：它已由 CI 自动同步，但 Caddy 只在**进程启动时**读一次配置 —— 所以 deploy 脚本会比对内容哈希，变了才 `--force-recreate caddy`（幂等，不会每次部署都重启代理）。首次运行因为还没有 baseline 哈希，会重建一次 caddy。

**本次实际路径（Deploy 的 SSH 失败，改手动）**：等 build 把镜像推到 ACR 后，在服务器上跑等价脚本。脚本内容（本次放在服务器 `/tmp/dc-deploy.sh`，未入仓，此处留档）：

```bash
#!/bin/bash
set -e
REG=crpi-rnawo8jx69bsvlbx.cn-hongkong.personal.cr.aliyuncs.com
TAG="$1"                      # 传 commit SHA（= ACR 上的镜像 tag）
cd /opt/DecisionCourt
docker pull "$REG/decision-court/decision-court-backend:$TAG"
docker pull "$REG/decision-court/decision-court-frontend:$TAG"
docker tag  "$REG/decision-court/decision-court-backend:$TAG"  "$REG/decision-court/decision-court-backend:latest"
docker tag  "$REG/decision-court/decision-court-frontend:$TAG" "$REG/decision-court/decision-court-frontend:latest"
docker compose up -d --force-recreate backend frontend
sleep 8
docker compose exec -T backend wget -qO- http://127.0.0.1:8080/health
```

**长操作要 detach**：`ssh admin@… "setsid nohup /tmp/dc-deploy.sh <SHA> > /tmp/deploy.log 2>&1 < /dev/null &"`。裸 `nohup` 不够 —— 这台机器 SSH 会偶发 reset（见 §4.8），连接断掉会把 pull 带走。

**怎么判断"镜像推上去了没有"**（CI 日志看不到时的替代观测）：

```bash
# 服务器上已登录 ACR，可以直接问仓库有没有这个 tag
docker manifest inspect "$REG/decision-court/decision-court-backend:$SHA" >/dev/null 2>&1 && echo EXISTS || echo MISSING
```

### 3.3 验收 checklist（每次发版后）

1. 四个端点：`/health` · `/api/v1/health/llm` · `/metrics` · `/`
2. 启动日志三行（§1.2 表最后一行）—— **这是判断"跑的是不是当前代码"最快的方法**：`version=` 必须等于镜像 tag；`promptlab loaded` 出现说明 YAML 进了镜像
3. 业务冒烟：`auth/anon`（带 `user_id`）→ `GET /courtrooms`
4. 容器 5 个状态
5. 埋点写读：`POST` + `GET /courtrooms/:uuid/events`（手工 curl 注意 CSRF 编码坑，见 §4.9）

---

## 4. 踩坑经验（9 条）

### 4.1 `next build` 被 ESLint 死变量拦住 —— dev 不跑 lint，CI 必挂

**现象**：本地 `pnpm run dev` 一切正常，`pnpm run build`（CI 的 `frontend-test` 与前端 Dockerfile 都跑）报 `Failed to compile`，全是 `'xxx' is assigned a value but never used`。
**根因**：删"庭审回放"和重做布局时移除了使用点、没删声明。`next dev` **不跑 ESLint**，`next build` 才跑。
**修法**：删死声明（3 个文件）。
**教训**：**本地验证要用 `build` 而不是 `dev`**。这次它是"镜像停在 7 月"的直接原因 —— build job 从这个 commit 起就不可能成功。

### 4.2 `test.yml` 缺 job key → 整段 steps 被 YAML 吞掉

**现象**：PyYAML 解析 job 列表得到 `['backend-test','frontend-test','dep-audit']`，少一个。
**根因**：`# ===== Doc cross-links =====` 注释下面直接写了 4 空格缩进的 `name:` / `runs-on:` / `steps:`，**没有 job key** → YAML 把它们当成 `dep-audit` 的**重复键**（last-wins），把 `dep-audit` 原来的 steps 整个覆盖掉。后果：v2.5 加的 `govulncheck` + `pnpm audit` **在 CI 里一次都没跑过**。
**修法**：补 `  doc-cross-links:`。
**教训**：**改 workflow 后一定要解析一遍**，别只看文件长得像：
```bash
python -c "import yaml;print(list(yaml.safe_load(open('.github/workflows/test.yml',encoding='utf-8-sig'))['jobs']))"
```
重复键在 YAML 里是**静默 last-wins**，GitHub 也不会报错。

### 4.3 阿里云 ACR 拒收 BuildKit 的 attestation index

**现象**：`buildx failed with: failed to push …: denied: unknown manifest class for application/vnd.oci.empty.v1+json`。
**根因**：`docker/build-push-action` 用 docker-container driver 时默认 `provenance: mode=min`，会额外产出 attestation manifest + 一个 media type 为 `application/vnd.oci.empty.v1+json` 的空描述符，组成 OCI index 再推 —— **ACR 个人版不认这个 media type**。
**修法**：两个 build step 各加 `provenance: false` + `sbom: false`（单平台不需要 index）。
**教训**：国内镜像仓库对 OCI 新特性的支持普遍滞后。这类错误只出现在 **push** 阶段，构建本身是成功的 —— 别误判成"代码构建失败"。

### 4.4 `prompts/base.yaml` 没进 runtime 镜像 → 线上永久降级

**现象**：每次启动都 WARN `promptlab YAML load failed, using hardcoded fallback`。
**根因**：`backend/Dockerfile` 多阶段构建，runtime 阶段只 `COPY --from=builder /app/server /app/server`，**没拷 `prompts/`**；prod compose 也没有对应 volume。
**为什么必须修**：ADR 0031 把 fallback 写成"设计路径"，但它针对的是"加载失败"的兜底；这里是**打包漏文件导致必然失败**。方向更关键：dev 栈 bind mount 源码 → 本地**一直**走 YAML，**线上跑的恰是没被测过的那条路**。
**修法**：补 `COPY --from=builder /app/prompts /app/prompts`。
**教训**：**多阶段构建要逐个检查"运行期真正需要的非二进制资产"**（配置、模板、prompt、证书）。dev 用 bind mount 会掩盖这类问题。

### 4.5 Prompt Lab 的 4 条 REST 路由从未接线 → 恒 404

**现象**：`GET /api/v1/prompts/version` 恒 404。
**根因**：`handler.promptLab` 靠 `NewPromptLabAdapter(...)` 注入，而**全仓（含测试）没有任何调用点**；`RegisterPromptLabRoutes` 开头 `if h.promptLab == nil { return }` → 路由一条都不注册。
**修法**：补 `handler.WithPromptLab()` 导出入口 + `main.go` 在 `RegisterAPIRoutes` **之前**装配 + eval/abtest 过 `LLMRateLimit` + nil 分支改成打 ERROR。
**教训（本节最有价值）**：同一个文件里 `RegisterTraceRoutes` 的同类 nil 分支**会打 ERROR**，所以那个问题早被揪出来了；Prompt Lab 的**静默 return** 让它藏了几个月。**"降级"必须出声** —— 这是这个项目反复吃到的同一个亏（AGENTS.md §2 的 silent-error 家族）。

### 4.6 Caddy 静默 exit 1 死循环（第一次上线那次，本次复用结论）

**现象**：`dc_caddy` 反复重启，`RestartCount` 一直涨，但 `docker compose logs caddy` **没有任何 error**，只有三行启动日志。
**根因**：Caddyfile 用了 `access_log { … }`，而 `caddy:2-alpine`（v2.11.7）不认识该指令。唯一线索来自 `caddy adapt`：`unrecognized directive: access_log`。Caddy v2 站点级访问日志指令是 `log`。
**修法**：`access_log` → `log`。
**教训**：**容器"读配置即退出"时，`logs` 里可能什么都没有 —— 要用该程序自己的配置校验命令**（`caddy adapt` / `nginx -t` / `sshd -t`）去照。

### 4.7 ⭐ CI 的 Deploy 失败：不是网络，是 GitHub Secret 里的私钥不对

**这是本次最有价值的一条方法论**，因为我的**第一版结论是错的**（写成"被云防火墙挡住"）。

**现象**：`build` job 成功（镜像已进 ACR），但 `deploy` job 不落地，服务器上镜像/容器长时间无变化。

**错误判断的由来**：我只查了 `Accepted publickey`，看到里面没有 GitHub runner 网段的 IP，就推断"请求没到 sshd = 被防火墙挡"。**只用了否定证据就下结论。**

**正确的排查顺序与结论**：

| 查什么 | 命令 / 位置 | 本次结果 |
|---|---|---|
| 服务器本机有没有拦 | `sudo iptables -L INPUT -n`、`sudo nft list ruleset`、`systemctl is-active ufw firewalld fail2ban` | **零规则**，全 inactive → 排除本机 |
| sshd 有没有限流 | `sudo sshd -T \| grep -iE 'maxstartups\|maxauthtries'` | `10:30:100`、`persourcemaxstartups none` → 排除限流 |
| 22 端口是否只放行特定来源 | `auth.log` 里有没有**随机公网 IP** 打到握手阶段；`sudo lastb \| head` | 有（`46.201.2.176` 等），还有爆破记录 → **22 端口对全网开放**，排除"只放行我的 IP" |
| **runner 到底有没有连上** | `sudo grep -a 'sshd' /var/log/auth.log \| grep -avE '<我的IP>\|100\.104\.'` | **连上了**：`Connection closed by authenticating user admin 172.208.126.96 [preauth]`，两次 Deploy 各一条 —— `172.208.x`/`20.168.x` 是 **Azure 网段（GitHub runner 跑在 Azure）** |
| 是密钥内容错还是 RSA 算法被拒 | `sudo sshd -T \| grep pubkeyacceptedalgorithms`；再用 GitHub action 同款库（Go `x/crypto/ssh`）拿候选私钥直连实测 | 服务器无 `ssh-rsa`(SHA-1) 但有 `rsa-sha2-*`；用本机 `id_rsa` 实测 **`DIAL OK`** → **排除算法问题** |
| 那到底是什么 | `ssh-keygen -lf /home/admin/.ssh/authorized_keys` | 清单里只有阿里云自带 ECDSA + 用户的 `id_rsa.pub` 两把 → **Secret 里的私钥不在这份清单里** |

**真因**：GitHub Secret `ECS_SSH_KEY` 装的不是新机 `authorized_keys` 里的那把钥匙。

**为什么这么难发现**：默认日志级别（INFO）下，"公钥不被接受"只记 **debug**，`auth.log` 里只剩一行 `Connection closed … [preauth]`，**没有任何 `Failed publickey`**。

**修法（只有用户能做，私钥不经 Agent 手）**：GitHub 仓库 → Settings → Secrets and variables → Actions → `ECS_SSH_KEY` → Update → 粘贴本机 `~/.ssh/id_rsa` 完整内容。

**教训**：
1. **"看不到失败记录" ≠ "没连上"**。诊断顺序必须是：先确认对端**有没有收到**（`[preauth]` 也算收到了），再分辨"认证失败 / 网络不通"。
2. **不要只用否定证据下结论**。我犯的正是这个错。
3. 想区分"密钥错"和"算法不兼容"，**用对端实际使用的那个客户端库直连实测**，比读配置猜快得多。

### 4.8 22 端口从外网时通时断（会干扰一切 SSH 排查）

**现象**：连续 3 次 TCP 探测 `8.218.24.43:22` 只有 1 次连上（83ms），另两次超时；同一时刻 HTTPS 完全正常。
**已排除**：本机防火墙（零规则）、ufw/firewalld/fail2ban（inactive）、`maxstartups`（宽松）。
**最可能的原因**：**自己的高频连接**。本次为轮询部署状态开了 200+ 个 SSH 会话，很可能触发了云侧的连接频率保护 —— 这解释"早上好、下午开始飘"。
**教训**：
- 轮询不要用"每次开一个新 SSH"，要**合并命令**、**拉开间隔**，长任务用 `setsid` detach 后读日志。
- 排查 SSH 问题时，先想想"是不是我自己把它打限流了"。
- 顺带：`46.201.2.176` / `91.196.82.14` / `186.124.x` 这些是全网扫描器，属正常背景噪音，不是事故。

### 4.9 手工 curl 验证 CSRF 保护端点的编码坑

**现象**：`POST /api/v1/prompts/eval` 恒 `CSRF_TOKEN_MISMATCH`，cookie 和 header 明明发的是同一个字符串。
**根因**：token 里含 `%3D%3D`（URL 编码的 `==`）。服务端对 **cookie 值 URL 解码后**再比较，而 `X-XSRF-TOKEN` header 是**按原样**比较的。
**修法**：
```bash
RAW=$(awk '$6=="XSRF-TOKEN"{print $7}' /tmp/cj.txt | tail -1)   # cookie 发原值（含 %3D%3D）
DEC=$(printf '%s' "$RAW" | sed 's/%3D/=/g')                     # header 发解码值（==）
curl -X POST "$B/prompts/eval" -b "dc_session=$SESS; XSRF-TOKEN=$RAW" -H "X-XSRF-TOKEN: $DEC" ...
```
**教训**：浏览器端天然正确（`readCookie` 走 `decodeURIComponent`），所以**这个坑只在手工/脚本验证时出现**。连试 4 次才定位，记下来别再重踩。

---

## 5. 方法论提炼（3 条，值得带进下一次）

### 5.1 「开关是 on」≠「数据拿到了」

R10（`prompts/base.yaml` 没进镜像）和 R12（FileLogger 目录不可写）是**同一个族**：配置项完全正确、代码路径也在跑，但**环境不具备**（文件不在镜像里 / 目录没有写权限），于是静默降级。
**验收必须落到"产物是否存在"**，而不是"开关是否为 true"：
```bash
docker compose exec -T backend ls -l /app/prompts/base.yaml     # 资产在不在
docker compose exec -T backend sh -c 'touch /app/logs/.wtest'   # 目录能不能写
```

### 5.2 「看不到失败记录」≠「没连上」

见 §4.7。默认日志级别会吞掉鉴权失败的细节。**诊断顺序**：先确认对端有没有收到 → 再分辨认证失败 / 网络不通 → 最后才怀疑防火墙。**只用否定证据下结论是我这次犯的错。**

### 5.3 沉默的 `return` 是长期隐患

`RegisterPromptLabRoutes` 静默 return 让 404 藏了几个月；`RegisterTraceRoutes` 同类分支会打 ERROR，所以早被发现。**任何"降级为不可用"的分支都要出声**（WARN/ERROR + 一句可操作提示）。

---

## 6. 遗留项

> **状态更新（2026-10-09 11:40 前后）**：第 1 项（CI Deploy 的 SSH 认证）**已由用户修复并实测生效**；
> 第 2/3/5/6/7 项已于 2026-10-09 修复；第 2 项的存量机器 chown **已执行并实测**（见 §6.3）；第 4 项 sshd 加固与第 8 项 Caddyfile 同步仍待处理。

| # | 项 | 状态 | 修法 |
|---|---|---|---|
| 1 | **GitHub Secret `ECS_SSH_KEY` 不对** → CI 自动部署走不通 | ✅ **已修（用户操作）+ 实测通过** | 见下方「§6.1 CI 部署恢复的实测证据」 |
| 2 | **R12 FileLogger 写不进去**（uid 不匹配 + 宿主机 root 属主） | ✅ **已全修（代码 + 服务器）** | 代码：compose `user` 统一 10001 + Dockerfile 预建 `/app/logs` + 启动期 `ProbeLogDir`。服务器：`chown -R 10001:10001 /opt/DecisionCourt/logs` 已执行（见 §6.3） |
| 3 | **R13 无 prompt 版本归因** | ✅ **已修并上线** | `llm_calls.prompt_version`（`semver@git_sha#内容哈希`）+ Recorder 每次现取 + git_sha 由 ldflags 注入（[`OBSERVABILITY.md §8.4`](../OBSERVABILITY.md)） |
| 4 | sshd 仍允许口令登录 + root 直登，且 22 端口对全网开放（`lastb` 已有爆破记录） | ⏳ 待授权 | `PasswordAuthentication no` + `PermitRootLogin prohibit-password`；改前先确认密钥登录可用、保留已登录会话、`sshd -t` 校验后再 reload。**2026-10-09 云盾登录告警再次印证这条的紧迫性**（见 §6.2） |
| 5 | prod compose 的 `version:` 属性已废弃（每次 compose 命令打 warning） | ✅ **已删** | 删除该行（纯噪音） |
| 6 | `prompts/base.yaml` 版本号（`semver`）需手工改 YAML 才变；`/prompts/version` 的 `git_sha` 为空 | ✅ **已修** | `git_sha` 由 Dockerfile ldflags 注入；另加 `content_hash` 覆盖"热加载改了内容但 semver/git_sha 都不动"的场景。**semver 仍保持手工维护**（它是人读的版本标签 + A/B 身份标识，有意保留） |
| 7 | **R17 `deploy.yml` 从不同步 `docker-compose.yml`** → compose 变更静默不生效 | ✅ **已修** | deploy job 补 `actions/checkout` + `appleboy/scp-action`（只同步 compose，不碰 `.env`），并把 `user:` 行打进 CI 日志（见 §3.2） |
| 8 | **Caddyfile 靠人工 scp，且改了不会 reload** | ✅ **已修** | 已纳入同一个 scp 步骤；deploy 脚本按**内容哈希**（`deploy/caddy/.caddyfile.sha256`）判断，只有在真变了的时候才 `compose up -d --force-recreate caddy` —— 避免每次部署都让边缘代理白闪一下 |

### 6.1 CI 部署恢复的实测证据（2026-10-09 10:05–10:06）

**结论**：用户修好 `ECS_SSH_KEY` 后，CI 的 Deploy job **首次成功落地**，COMMIT `f34b88d` 已上生产。

排查过程（用服务器 `auth.log` + 容器状态对齐，同一个时间窗三条证据互证）：

| 时间（+08:00） | 事件 | 来源 IP |
|---|---|---|
| 07:22:24 | `Connection closed by authenticating user admin ... [preauth]` | `172.208.126.96`（Azure，runner） |
| 07:34:44 | 同上（**认证失败**，§4.7 记录的那次） | `20.168.109.87`（Azure，runner） |
| **10:05:48** | **`Accepted publickey for admin`** | `52.161.59.0`（Azure，runner） |
| **10:06:05** | **`Accepted publickey for admin`** | `172.210.61.210`（Azure，runner） |
| 10:06:08 / 10:06:09 | `dc_backend` / `dc_frontend` 容器**被重建** | — |

两条成功记录的密钥指纹都是 `SHA256:Ny/HKBZeCc4sjvhmVBssGe4IzO+dm2FnhYAiq16Kc1w`（= 用户本机
`id_rsa.pub`，与 §4.7 诊断时的候选密钥一致）；容器重建时间正好落在两次 SSH 之后，且 `docker images`
里多了 tag = `f34b88d`（= 当时 main HEAD）的镜像。**判定：这是 CI 的正常部署，不是入侵。**

### 6.2 云盾「登录地非常用」告警 = 上述 CI 部署（2026-10-09 10:06）

同一时间收到阿里云云盾告警：`admin` 于 `2026-10-09 10:06:12` 从 `52.161.59.0`（怀俄明州）SSH 登录。
`10:06:12` 正是上表里 `52.161.59.0` 那次会话**登出**的时刻 —— **同一次 CI 部署，不是入侵**。

**但告警本身指出了一个真问题**：CI 用的部署私钥与**你个人登录用的 `id_rsa` 是同一把**，
而 GitHub Actions 的 runner 跑在 Azure（每次 IP 都不同）→ 以后每次 CI 部署都会触发"非常用登录地"告警。
建议（按性价比排序）：

1. **给 CI 单独一把部署密钥**（ed25519），公钥追加进服务器 `~/.ssh/authorized_keys`，私钥只放
   GitHub Secret `ECS_SSH_KEY`。这样 CI 私钥泄露 ≠ 你的个人登录被攻破，也便于单独吊销。
2. **做第 4 项的 sshd 加固**：关掉口令登录与 root 直登 —— 22 端口对全网开放且 `lastb` 已有爆破记录，
   这才是这次告警暴露出的真正暴露面。
3. 在云盾控制台把 GitHub Actions 的网段加入"常用登录地"白名单，或直接对该告警标注为预期行为（治标）。

### 6.3 R12 的服务器侧修复（2026-10-09，已执行并实测）

**改了什么**：

```bash
sudo mkdir -p /opt/DecisionCourt/logs/backend /opt/DecisionCourt/logs/caddy
sudo chown -R 10001:10001 /opt/DecisionCourt/logs
```

改前 `logs/`、`logs/backend`、`logs/caddy` 都是 `root:root 0755`；改后三者的属主均为 `10001:10001`。

**当场验证（不靠推测）**——用一个 uid 10001 的临时容器挂同一目录写文件：

```bash
IMG=$(docker inspect dc_backend --format '{{.Config.Image}}')
docker run --rm --user 10001:10001 -v /opt/DecisionCourt/logs/backend:/app/logs \
  --entrypoint sh "$IMG" -c 'touch /app/logs/.wtest && echo WRITABLE_AS_10001 && rm -f /app/logs/.wtest'
# → WRITABLE_AS_10001
```

**注意：光 chown 还不够，必须让容器以 uid 10001 运行** —— 这正是 **R17** 的坑：第一次部署时镜像更新了、
但服务器上的 compose 还是旧的 `user: "1001:1001"`，容器仍是 uid 1001，于是 chown 成 10001 之后**依然写不进去**，
反而由新加的启动期 probe 打出 ERROR 才暴露。补做完 compose 同步 + `--force-recreate` 后，启动日志变为：

```
{"level":"INFO","msg":"agent_gateway: file logger log dir writable","dir":"logs"}
```

**至此 R12 全链路闭合**：宿主机属主 ✅ → 容器 uid ✅ → 应用写入 ✅（有启动日志正向证据）。


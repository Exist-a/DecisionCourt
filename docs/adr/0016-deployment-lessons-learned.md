# ADR 0016: v0.9.1 部署踩坑与缓解策略

| 字段 | 值 |
|---|---|
| **状态** | 已落地 (2026-07-06) |
| **作者** | Exist-a |
| **关联 PR** | #15 (部署就绪)、#16 (阿里云 ECS) |
| **关联 ADR** | 0012、0013、0014、0015 |
| **目标读者** | 未来部署者 / 自己维护者 |

## 背景

v0.9.1 实施完成后,首次部署到阿里云 2 vCPU / 2 GiB 香港 ECS,
域名 `decisioncourt.cn` 通过阿里云 ACR 推送 + Caddy 自动 HTTPS 上线。
本次部署踩了 7 个有教育意义的坑,本 ADR 记录下来,避免重复踩。

## 决策

**v0.9.1 起,所有后端 / 前端镜像必须在开发者本机 build,推阿里云 ACR,
ECS 只 pull 镜像,不做任何 build。** 这是踩坑 1 的根因对策。

## 7 个坑与对策

### 坑 1: 2C2G ECS 上 Go build 占满资源,SSH 都进不去

**症状**:
- `Step 10/18 : RUN CGO_ENABLED=0 GOOS=linux go build` 卡住 5-10 分钟
- 阿里云监控:CPU 96% / 内存 26%(**内存其实没爆**)
- ECS WorkBench 进不去,只能控制台强重启

**根因**:
- Go 编译 1000+ 包,2 vCPU 不够并行调度
- 监控 CPU 满但内存没满,所以不是 OOM
- 是 **CPU 瓶颈**,build 慢但没死,只是 SSH 守护进程被抢占

**对策**:
- **本机 build** → **推阿里云 ACR** → **ECS pull**
- ECS 完全不做 build,省 CPU 留给运行时
- ACR 个人版免费,推送速度稳定
- 已实施:commit `2dc9ba2`

**教训**:
- 低配 ECS(2C2G)不适合做 CI/CD build runner
- "卡半天"不一定是 OOM,要先看 CPU/内存哪个瓶颈

### 坑 2: docker-compose v1 `pull` 多镜像认证缓存 bug

**症状**:
- 单 `docker pull` 镜像成功(看到 digest)
- `docker-compose pull backend frontend` 报 `pull access denied`
- 错误: `repository does not exist or may require 'docker login'`

**根因**:
- docker-compose v1 解析多 service 的 pull 时,认证缓存被覆盖
- 即使刚 `docker login` 成功,docker-compose 还是会报认证失败

**对策**:
```bash
# 不行(认证缓存冲突):
docker-compose pull backend frontend

# 行(分开 pull + 直接 up):
docker pull image-A
docker pull image-B
docker-compose up -d
```

**教训**:
- 老 docker-compose v1 跟新版 v2 plugin 行为差异很大
- ECS 上 apt 仓库没 docker-compose-plugin,只能用 v1
- 涉及认证的 docker 命令,分开跑更稳

### 坑 3: YAML 重复 top-level key 覆盖前一个

**症状**:
- 用 `cat >> docker-compose.yml << EOF` 追加 caddy service + caddy_data/caddy_config 卷
- 报错: `volumes.caddy value 'container_name', 'depends_on', ... do not match any of the regexes`
- 实质:caddy service 被错误解析成 volume.caddy

**根因**:
- YAML / docker-compose v1 解析:重复 top-level key(`volumes:`)时,后者覆盖前者
- 追加的 `volumes:` 覆盖了原 `volumes:`,postgres_data / redis_data 丢失
- caddy service 被放到了 `volumes:` 段下面,YAML 解析成 volume 名

**对策**:
- **永远只保留一个 `volumes:` 段**,新卷追加到原段下面
- 不要在末尾再追加一个 `volumes:` 段

```bash
# 错误:
cat >> docker-compose.yml << 'EOF'
  caddy:
    image: caddy:2-alpine
    ...
volumes:
  caddy_data:
EOF

# 正确(用 sed 在原 volumes 段加):
sed -i '/^  redis_data:$/a\  caddy_data:\n  caddy_config:' docker-compose.yml
```

**教训**:
- 改 YAML 不要用 `cat >> EOF` 追加新顶级 key
- 改完一定要 `grep` 验证关键字段还在
- docker-compose v1 不报错但行为静默错误,更危险

### 坑 4: SearchReplace 工具显示 diff 但实际未生效

**症状**:
- SearchReplace 改 backend 段 `build:` → `image:`,工具返回 diff 显示成功
- commit 后 git show 67b0495 里 backend 还是 `build:`
- 部署到 ECS 后 docker-compose up 又开始 build,卡死

**根因**:
- 怀疑是 SearchReplace 的 write-back 没真正写到磁盘,或者有缓存层
- 二次 commit `2dc9ba2` 才真正生效

**对策**:
- SearchReplace 改完后,**必须 Read 文件 + grep 二次确认**,不能信工具的 diff 显示
- 流水线:`SearchReplace` → `Read 验证` → `git diff 验证` → `commit` → `git show 验证`

**教训**:
- 任何工具说"改好了"都不能盲信
- Read 是金标准,grep 是兜底

### 坑 5: ECS WorkBench 2.0 抽风,SSH 进不去

**症状**:
- `login.error.publicKeyAuthenticationIsProhibited`
- `SocketTimeoutException has occurred on a socket read or accept`
- 旧的 WorkBench tab 还占着,新 tab 进不去

**根因**:
- WorkBench 2.0 不支持同时多会话,旧 tab 占着锁
- 阿里云中转服务器偶尔抽风
- 资源被 build 占满时,SSH 守护进程响应慢

**对策**:
1. **关掉所有 WorkBench 浏览器 tab** → 等 30 秒 → 重新进
2. 还不行 → **控制台 → ECS → 强制重启**(2-3 分钟)
3. 还不行 → **本地 PowerShell `ssh admin@<公网IP>`**

**教训**:
- ECS 重启是终极方案,不要纠结 WorkBench
- 重启会杀所有进程,但 docker 卷 / .env 都在磁盘,数据不丢

### 坑 6: GitHub 不再支持密码认证 git 操作

**症状**:
- `git clone https://github.com/...` 弹 `Username / Password`,输入后报
  `remote: Invalid username or token. Password authentication is not supported for Git operations.`

**根因**:
- GitHub 2021 年起强制 PAT(Personal Access Token)代替密码

**对策**:
- **本机浏览器** → GitHub → Settings → Developer settings →
  Personal access tokens → Tokens (classic) →
  Generate new token(勾选 `repo`)→ 复制 `ghp_...` → 用作 password
- 或用 **SSH key**:`ssh-keygen` + 公钥加到 GitHub → 改用 `git clone git@github.com:...`

**教训**:
- PAT 默认 30 天过期,提前 Regenerate
- SSH key 是长期方案,但配置复杂
- 项目是**私有仓库**的话,必须登录,PAT 不是可选

### 坑 7: ACR 登录用户名 ≠ GitHub 用户名

**症状**:
- `docker login --username=Exist-a crpi-...aliyuncs.com` 报 `403 Forbidden`
- 改用阿里云 UID `--username=1908713889737174` 报 `unauthorized`

**根因**:
- ACR 登录用户名是**阿里云账号全名**(不是 UID,不是 GitHub 用户名)
- 用户名格式因账号类型而异,跟 ECS WorkBench URL 里的 `UserAccount.LoginName` 字段对应

**对策**:
- ACR 控制台 → 右上角"访问凭证" → 查看正确的登录用户名
- 密码是**访问凭证里设的固定密码**,不是阿里云登录密码

**教训**:
- 跨云服务的认证字段各自定义,不能互通
- 第一次用新云服务,先看官方文档的登录示例

## 行动项（下次部署必做）

1. ☐ **部署前确认内存 >= 4 GiB**，2 GiB 无法 build Go binary
2. ☐ **.env.production 用真实域名**，DOMAIN / CADDY_EMAIL / NEXT_PUBLIC_* 都必填
3. ☐ **本机 build 镜像推 ACR**（`docker build` + `docker push`），ECS 只 `docker pull`
4. ☐ **改 docker-compose.yml 后 `grep` 验证关键字段**（`image:` / `volumes:` / `services:`）
5. ☐ **`docker pull` + `docker-compose up` 分两步**，避免 v1 认证缓存 bug
6. ☐ **GitHub 用 PAT**，密码已废
7. ☐ **ACR 用户名用阿里云账号全名**，不是 GitHub / UID
8. ☐ **WorkBench 卡死 → 控制台强重启**，不要纠结 SSH
9. ☐ **ECS 香港地域免备案**，免备案方案比大陆快 7-20 天

---

## 附录：v0.9.2 追加的 5 个新坑（2026-07-06 工作流脚本化时发现）

> **触发**: v0.9.2 实装了一键 push 脚本 + dev compose 工作流 + 部署脚本。在自动化过程中暴露了 PowerShell / Docker 版本兼容 / 凭证机制 5 类问题。
>
> **配套**: 日常命令速查见 [`docs/dev-deploy-workflow.md`](../dev-deploy-workflow.md) §4

### 坑 8: PowerShell 5.1 不识别 UTF-8 无 BOM 的中文脚本

**症状**:
- 跑 `.\scripts\push-to-acr.ps1` 报语法错误
- 中文注释 / echo 出来的中文变成乱码字节
- 脚本跑到一半神秘退出，连第一行都没执行完

**根因**:
- Windows PowerShell 5.1（不是 PowerShell 7+）默认把 UTF-8 无 BOM 文件按 ANSI（GBK）读
- 中文 UTF-8 字节（3 字节/字符）被按 ANSI 切成 2 字节/字符，乱码
- 乱码字节里可能撞上 PowerShell 关键字（如 `[`、`{`），tokenizer 直接抛 `ParserError`

**对策**:
- **脚本文件保存为 UTF-8 BOM**（首三字节 `EF BB BF`）
- 用 Write 工具时手动 BOM 头：用 `[System.IO.File]::WriteAllBytes($path, [byte[]](0xEF, 0xBB, 0xBF) + [System.IO.File]::ReadAllBytes($path))`
- 或者改用 PowerShell 7+（`pwsh` 命令），原生支持 UTF-8 无 BOM

**教训**:
- Windows 上做脚本开发必须区分 PowerShell 5.1 vs 7+
- IDE 的 PowerShell 工具链基本都按 5.1 解析
- 不要相信 IDE 显示的"脚本正确"，要真跑一次

---

### 坑 9: PowerShell 5.1 不支持 `&&` 语句分隔符

**症状**:
- `cd foo && bar` 报 `The token '&&' is not a valid statement separator in this version.`
- 在 Bash / CMD / Zsh 里都能跑

**根因**:
- `&&` 是 Bash 语法；PowerShell **7.0 才支持**（`$env:POWERSHELL_TELEMETRY_OPTOUT=1; Get-ChildItem Env:PSVersionTable` 可查版本）
- 5.1 必须用 `;` 分隔

**对策**:
- 脚本里**所有多命令串联用 `;`**
- 想短路（前一条失败就不执行后一条）用 `if ($LASTEXITCODE -eq 0) { bar }`
- 或者干脆用单条命令（多数情况 `;` 就够）

**教训**:
- 跨平台 shell 脚本是噩梦——Bash / PowerShell / CMD 语法各管各的
- Docker / GitHub Action 之类跨平台环境，**优先 Bash**，Windows 本地才用 PowerShell

---

### 坑 10: PowerShell `$ErrorActionPreference = "Stop"` + Docker stderr = 假报错

**症状**:
- 脚本里 `$ErrorActionPreference = "Stop"` 时，`docker build` 一启动就抛异常退出
- 抛的是 `NativeCommandError`，但实际 docker 还在跑，进度也照常输出
- exit code 1，误以为 docker build 失败了

**根因**:
- PowerShell 把 native command 写 stderr 的内容当 ErrorRecord
- `Stop` preference 一遇到 ErrorRecord 就 throw，把控制权从 docker 抢走
- Docker BuildKit 大量进度信息都写 stderr（`#1 [internal] load build definition` 之类），所以一定会触发

**对策**:
```powershell
# 不要用 Stop
$ErrorActionPreference = "Continue"   # ← 改这个

# 真正判断 docker 成不成功看 $LASTEXITCODE
docker build ...; if ($LASTEXITCODE -ne 0) { Write-Err "..."; exit 1 }
```

**教训**:
- `Set-StrictMode` + `$ErrorActionPreference = "Stop"` 是好习惯，但对**调 native commands 的脚本**反而是坑
- CI/CD 脚本默认 Continue + $LASTEXITCODE，是更稳的模式

---

### 坑 11: `docker login` 在 IDE 内 PowerShell / SSH 脚本里硬报错

**症状**:
- `docker login --username=... registry` 报 `cannot perform an interactive login from a non-TTY device`
- 但同台机器上 `docker pull` / `docker push` 都正常用同一个 registry
- 凭证确实存在（cmdkey /list 能查到）

**根因**:
- `docker login` 这个特定命令会**强制要求 TTY**（输密码用）
- IDE 内的 PowerShell / VS Code 的 terminal / SSH 跑的脚本，都不是 TTY
- `docker pull/push` 不需要 login 命令本身，靠 credential helper 静默用凭证

**对策**:
- **脚本里不要调 `docker login`**！让 push/pull 自己用凭证
- 如果凭证真的缺（cmdkey 查不到），在真 PowerShell 终端（Win + X → PowerShell）跑一次 login
- 确认凭证 OK 再跑 push 脚本

**教训**:
- "凭证能 pull" 不等于 "login 命令能跑"
- 自动化脚本能少调一个命令就少调一个，副作用最小

---

### 坑 12: docker-compose v1 + Docker v25+ = KeyError: 'ContainerConfig'

**症状**:
- ECS 上 `docker compose`（空格）报 `unknown command`
- 退到 `docker-compose`（连字符，v1）跑 `up -d`，崩在：
```
File "/usr/lib/python3/dist-packages/compose/service.py", line 1579
container.image_config['ContainerConfig'].get('Volumes') or {}
KeyError: 'ContainerConfig'
```

**根因**:
- `docker-compose` v1 是 Python 写的，**2021 年停更**，不维护了
- Docker Engine v25+ 的 API 不再返回 `ContainerConfig` 字段（认为调用方不需要）
- v1 还在硬访问，崩

**对策**:
- **必须装 docker-compose v2 plugin**（Go 写的，还在维护）
- 装法优先级：
  1. `sudo apt-get install -y docker-compose-plugin`（如果系统有 Docker CE apt 源）
  2. **直接下二进制**（apt 找不到时最稳，**已验证**）：
     ```bash
     sudo mkdir -p /usr/local/lib/docker/cli-plugins
     sudo curl -SL "https://gh-proxy.com/https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-x86_64" \
         -o /usr/local/lib/docker/cli-plugins/docker-compose
     sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
     ```
  3. 用 `gh-proxy.com` 走 GitHub 加速，国内 ECS 也能拉

- 装完用 `docker compose version`（**空格**）验证，不再用 `docker-compose`（连字符）

**教训**:
- v1 docker-compose 已经死了，别再用了
- ECS 默认装的"轻量" Ubuntu 镜像往往没有 Docker CE apt 源，直接下二进制最稳
- `gh-proxy.com` 是国内 ECS 拉 GitHub 镜像的救命稻草

### 坑 13: backend `read_only: true` + file_logger 默认路径 `/app/logs` = 日志写不进去（沉默 bug，潜伏 1 个月）

**症状**:
- 后端跑了 37 分钟 stable，stdout 日志全正常
- LLM Gateway 的 file_logger 设计写 JSON Lines 到 `/app/logs/agent_gateway_YYYY-MM-DD.log`，带 35 个字段（token / cost / compression / throttle / budget）
- 实际上**文件根本没生成**，`/app/logs/` 目录都不存在
- 用户调 LLM 看日志只能用 stdout（信息少 90%）

**根因链**:

```
v0.6: 实现 FileLogger
   写入路径 = "logs" (相对 WORKDIR /app/)
        ↓
v0.8.3: 安全加固，backend 容器加 `read_only: true`
        /app/ 整个文件系统只读
        只允许 /tmp 写(tmpfs)
        ↓
v0.8.3: 没改 FileLogger 写入路径
        ↓
FileLogger.Write() 报 EACCES（只读文件系统）
        ↓
backend/internal/agent_gateway/gateway.go:408-410
   if err := g.fileLogger.Write(entry); err != nil {
       // 仅吞掉错误;避免日志失败拖死主流程
       // ← 注释说吞掉,代码也吞掉了,连 error log 都没记
   }
        ↓
没人发现（LLM 调用照常工作，主流程不报错）
        ↓
2026-07-06: v0.9.2 上线后用户查"日志怎么看"，才发现
```

**修复**（v0.9.2 方案 B）:

```yaml
# docker-compose.yml backend 段
volumes:
  - ./logs/backend:/app/logs    # 宿主机 /opt/DecisionCourt/logs/backend/ 挂载到容器 /app/logs/
read_only: true                  # 保留,安全等级不变
tmpfs:
  - /tmp                         # 保留
```

- 容器能写（挂载点覆盖 read_only）
- 持久化（容器重启日志不丢）
- 安全等级不变（其他路径还是只读）
- 宿主机直接 `tail -f logs/backend/agent_gateway_*.log` 即可

**为什么不简单点——关掉 read_only**：
- 关掉 read_only 安全等级从 A 降 B（v0.8.3 20 项加固之一）
- 挂载 volume 是精准放行，只对 logs/ 目录可写

**为什么不改 gateway.go 错误吞掉逻辑**：
- 那是设计选择（避免日志失败拖死 LLM 主流程）
- 但**应该至少在 stdout 打个 warning**，让运维发现

**教训（给未来自己）**:
- **架构变更要跨层验证**：v0.8.3 加 read_only 是架构变更，应该触发"全代码搜索需要写文件的路径"检查
- **静默失败是危险的**：`if err != nil { /* 吞掉 */ }` 比 panic 还可怕，应该至少打 WARN 日志
- **写文件的代码必须有 e2e 测试**：写日志 → 看文件存在 → 内容正确

**部署后验证命令**:

```bash
# 1) 在 ECS 创建宿主机目录(权限 755,uid 1001 = backend 容器用户)
sudo mkdir -p /opt/DecisionCourt/logs/backend
sudo chown -R 1001:1001 /opt/DecisionCourt/logs/backend
sudo chmod 755 /opt/DecisionCourt/logs/backend

# 2) 触发一次 LLM 调用(浏览器开庭一个 trial)
# 3) 看日志生成了
ls -la /opt/DecisionCourt/logs/backend/
# 应该看到:agent_gateway_2026-07-06.log

# 4) 看内容(JSON Lines,每行一条 LLM 调用)
cat /opt/DecisionCourt/logs/backend/agent_gateway_2026-07-06.log | head -5
```

---

## v0.9.2 追加的行动项

10. ☐ **Windows 写 PowerShell 脚本必须 UTF-8 BOM**，否则 5.1 跑中文乱码
11. ☐ **脚本里多命令串联用 `;`**（不要 `&&`，5.1 不支持）
12. ☐ **`$ErrorActionPreference = "Continue"`**，靠 `$LASTEXITCODE` 判断成功
13. ☐ **自动化脚本不要调 `docker login`**，让 push/pull 自动用凭证
14. ☐ **ECS 上必须装 docker-compose v2 plugin**，v1 已死
15. ☐ **架构变更触发"全代码搜索需要写文件的路径"检查**（v0.9.2 坑 13 教训）
16. ☐ **`if err != nil { /* 吞掉 */ }` 至少打 WARN**，不能让错误完全沉默

## 关联资料

- [docs/deployment/CHECKLIST.md](../deployment/CHECKLIST.md) — 总体部署清单
- [docs/deploy-guide.md](../deploy-guide.md) — ECS 8 步部署流程(待写)
- [ADR 0012](0012-...md) — 单机部署高可用
- [ADR 0013](0013-...md) — LLM Gateway 三件套
- [ADR 0014](0014-...md) — 用户级 Trial 限流
- [ADR 0015](0015-...md) — 防 LLM 幻觉
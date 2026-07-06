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

## 行动项(下次部署必做)

1. ☐ **部署前确认内存 >= 4 GiB**,2 GiB 无法 build Go binary
2. ☐ **.env.production 用真实域名**,DOMAIN / CADDY_EMAIL / NEXT_PUBLIC_* 都必填
3. ☐ **本机 build 镜像推 ACR**(`docker build` + `docker push`),ECS 只 `docker pull`
4. ☐ **改 docker-compose.yml 后 `grep` 验证关键字段**(`image:` / `volumes:` / `services:`)
5. ☐ **`docker pull` + `docker-compose up` 分两步**,避免 v1 认证缓存 bug
6. ☐ **GitHub 用 PAT**,密码已废
7. ☐ **ACR 用户名用阿里云账号全名**,不是 GitHub / UID
8. ☐ **WorkBench 卡死 → 控制台强重启**,不要纠结 SSH
9. ☐ **ECS 香港地域免备案**,免备案方案比大陆快 7-20 天

## 关联资料

- [docs/deployment/CHECKLIST.md](../deployment/CHECKLIST.md) — 总体部署清单
- [docs/deploy-guide.md](../deploy-guide.md) — ECS 8 步部署流程(待写)
- [ADR 0012](0012-...md) — 单机部署高可用
- [ADR 0013](0013-...md) — LLM Gateway 三件套
- [ADR 0014](0014-...md) — 用户级 Trial 限流
- [ADR 0015](0015-...md) — 防 LLM 幻觉
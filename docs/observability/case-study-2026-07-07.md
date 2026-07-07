# 真实案例：WS 403 两段式 Bug 链(v0.9.3 真域名回归,2026-07-07)

> **目的**：继 [case-study-2026-07-02.md](./case-study-2026-07-02.md)("白盒化让 v0.5 隐藏 bug 显形")之后,**用同一个"白盒化诊断 + 真域名回归"的方法学**,记录一次**两段式隐藏 bug 链**的完整复盘 —— v0.9.3 修复的不是 1 个 bug,而是**两个串联的 bug,只修前一个看不到任何效果**。
>
> **配套**: [ADR 0017](../adr/0017-websocket-uuid-credential.md) · [ADR 0018](../adr/0018-websocket-origincheck-init-timing.md) · [ADR 0016](../adr/0016-deployment-lessons-learned.md) · [OBSERVABILITY.md](../OBSERVABILITY.md) · [dev-deploy-workflow.md §3](../dev-deploy-workflow.md)
>
> **完成于**: 2026-07-07

---

## 0. 一句话总结

> **v0.9.3 修复部署后,WS 403 仍然存在。SSH 远程白盒化排查定位为"两段式 bug 链":第一段 `viper.Unmarshal` 不 split `[]string`(已知)+ 第二段 `buildCheckOrigin` 在 package init 阶段锁死白名单(隐藏)。**任何一段不修,403 都不会消失。**整个排查 + 修复 32 分钟(16:00 报告 → 16:32 修复上线)**,**6 次 SSH 远程诊断命令 + 2 次 Git commit + 1 次无缓存 Docker rebuild**。

---

## 1. 背景

### 1.1 事件起点(2026-07-07 16:00 +0800)

用户报告:浏览器控制台连续报 403,无法接入庭审:

```
[Frontend] page-27da1b690b4665ca.js:1
  WebSocket connection to 'wss://decisioncourt.cn/ws/courtrooms/c54eee66-1537-4939-a4cc-01346b1f03e1?token=...' failed:
    Error during WebSocket handshake: Unexpected response code: 403

[WebSocket] error: Event { isTrusted: true, type: 'error' ... }
```

### 1.2 已知的"半修复"——ADR 0017(v0.9.2 owner 软校验)

[ADR 0017](../adr/0017-websocket-uuid-credential.md) 在 2026-07-06 上线,把 v0.8.3 的"硬 owner 校验"改成"软校验"(UUID 即凭证)。**理论应该能解决 403**。但实际生产复现,403 依然存在,只是 403 的来源从 owner 阶段换成了 upgrader 阶段。

### 1.3 "已知的已知"的修复——v0.9.3 第一段 commit `2c876f0`

[docs/security-audit-v0.8.3.md §8.1](../security-audit-v0.8.3.md) 修了一个已知问题:`viper.Unmarshal` 对 `[]string` 字段 + 单值 env var 不会自动 split,导致 `AppConfig.AllowedOrigins` 一直是 `nil`。修复方式是在 [config.go Load()](../backend/internal/config/config.go) 里手动 `strings.Split` + `strings.TrimSpace`。

**部署后 403 仍然存在**。这是个危险的时刻 —— "commit 看起来逻辑正确,测试都过,为什么 prod 还是 403?"

---

## 2. 排查路径(SSH 远程白盒化)

### 2.1 第 1 步:确认 ECS 容器在跑且 backend 是新版本

```powershell
.\scripts\ecs.ps1 status
```

```
dc_backend    Up 29 hours (healthy)  decision-court-backend:latest
dc_caddy      Up 29 hours (healthy)  caddy:2-alpine
dc_frontend   Up 29 hours (healthy)  decision-court-frontend:latest
dc_postgres   Up 29 hours (healthy)  postgres:16-alpine
dc_redis      Up 29 hours (healthy)  redis:7-alpine
```

→ backend 跑 29h,版本 v0.9.2。**等等,v0.9.3 fix 应该在 7h 前 commit,但 backend 跑 29h 一直没重启**?这就是个红旗。

### 2.2 第 2 步:确认 backend 进程里是否真的有 v0.9.3 fix

```powershell
docker --context ecs exec dc_backend sh -c "strings /app/server | grep -E 'UUID-as-credential|ws.connect.foreign_owner' | head -3"
```

→ `ws.connect.foreign_owner` 存在 → **v0.9.2 (ADR 0017) 修复在 binary 里**。

```powershell
docker --context ecs exec dc_backend sh -c "ls -la --full-time /app/server"
```

→ `2026-07-06 03:51:15 +0000`(老 binary)。**但这不是说 binary 是 v0.9.3 之前的版本** —— 因为 v0.9.3 的 config fix 是同一个 binary 里改一行 Go 代码,strings grep 不会区分。

**这步的结论**:确认 v0.9.2 (owner 软校验) 在 binary 里。但 v0.9.3 config fix 的 strings marker 不明显,需要别的办法验证。

### 2.3 第 3 步:从 caddy 容器内复现 403

```powershell
docker --context ecs exec dc_caddy sh -c "wget -S -O- 'http://backend:8080/ws/courtrooms/c54eee66-1537-4939-a4cc-01346b1f03e1' 2>&1 | head -5"
```

→ `HTTP/1.1 403 Forbidden`(没带 cookie → 应该 401 而不是 403!)

**关键观察**:没带 cookie 就返 403,**说明请求根本没走到 handler**。handler 失败应该是 401(没 token),upgrader 阶段才会 403。

**这就锁定了 403 的来源 = upgrader.CheckOrigin**。

### 2.4 第 4 步:带 Origin + Cookie 复现 403,排除 token 干扰

```powershell
docker --context ecs exec dc_caddy sh -c "wget -S -O- --header='Origin: https://decisioncourt.cn' --header='Cookie: dc_session=...' 'http://backend:8080/ws/courtrooms/...' 2>&1 | head -5"
```

→ 依然 `HTTP/1.1 403 Forbidden`。

**关键观察**:带 token + Origin 后,正常情况应该 101 Switching Protocols,实际 403 → **Origin check 在拒**。

### 2.5 第 5 步:检查 ALLOWED_ORIGINS 实际值

```powershell
docker --context ecs exec dc_backend sh -c "env | grep -i allowed"
```

→ `ALLOWED_ORIGINS=https://decisioncourt.cn` ← **env var 正确**

### 2.6 第 6 步:检查 DB 确认 session 存在 + owner 匹配

```powershell
docker --context ecs exec dc_postgres psql -U decisioncourt -d decisioncourt \
  -c "SELECT session_uuid, owner_id, current_phase FROM court_sessions WHERE session_uuid = 'c54eee66-...'"
```

→ `owner_id=anon_a256c89f...`(跟 JWT user_id 一致)**排除 owner 软校验 403 的可能**。

### 2.7 第 7 步:对比原代码逻辑,定位 init-timing

[backend/internal/api/websocket.go](../backend/internal/api/websocket.go) 的 package init:

```go
var upgrader = websocket.Upgrader{
    CheckOrigin: buildCheckOrigin(),
}
```

```go
func buildCheckOrigin() func(r *http.Request) bool {
    allowed := config.AppConfig.AllowedOrigins  // ← init 时刻 = nil
    if len(allowed) == 0 {
        allowed = []string{"http://localhost:3000", "http://127.0.0.1:3000"}  // ← 锁死 fallback
    }
    allowedSet := make(map[string]bool, len(allowed))  // ← 闭包捕获
    ...
    return func(r *http.Request) bool {
        ...
        return allowedSet[strings.TrimRight(origin, "/")]  // ← 永远用 init 时刻的 allowedSet
    }
}
```

**根因清楚**:init 阶段 `config.AppConfig.AllowedOrigins` 是零值 → 走 localhost:3000 fallback → 闭包把 `{localhost:3000, 127.0.0.1:3000}` 锁死 → 之后 main() 跑 `config.Load()` 让 AllowedOrigins 填好,upgrader.CheckOrigin 已经定型 → 看不到新值 → 生产 Origin 不在白名单 → 403。

### 2.8 第 8 步:定位根因的时间

| 阶段 | 累计耗时 |
|---|---|
| 报告 + 第 1-2 步(确认后端跑旧版) | ~3 min |
| 第 3 步(没 cookie 也 403) | ~1 min |
| 第 4 步(带 cookie + Origin 还是 403) | ~1 min |
| 第 5-6 步(确认 env + DB 都没问题) | ~1 min |
| 第 7 步(回到代码读 buildCheckOrigin) | ~3 min |
| 定位 init-timing 是根因 | ~1 min |
| **总计** | **~10 min** |

---

## 3. 修复(2 个 commit + 1 次 rebuild)

### 3.1 commit `e097d7c`:buildCheckOrigin 改运行时读 config

[backend/internal/api/websocket.go](../backend/internal/api/websocket.go) 的 buildCheckOrigin 改为:

```go
func buildCheckOrigin() func(r *http.Request) bool {
    return func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        if origin == "" { return true }

        // 关键:每次调用都读 config(不闭包捕获),保证 main() Load() 之后生效
        allowed := config.AppConfig.AllowedOrigins
        if len(allowed) == 0 {
            allowed = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
        }
        origin = strings.TrimRight(origin, "/")
        for _, o := range allowed {
            if strings.TrimRight(o, "/") == origin {
                return true
            }
        }
        return false
    }
}
```

### 3.2 新增 4 个回归测试

[backend/internal/api/websocket_origin_test.go](../backend/internal/api/websocket_origin_test.go):

| 测试 | 覆盖 |
|---|---|
| `TestBuildCheckOrigin_ReReadsConfigPerCall` | **核心**:同一闭包在 AllowedOrigins=nil 时拒、设置后必须接受 |
| `TestBuildCheckOrigin_EmptyOriginAlwaysAllowed` | 浏览器外调用(curl)无 Origin → 始终通过 |
| `TestBuildCheckOrigin_TrimsTrailingSlash` | 白名单带 `/` / Origin 带 `/` 都能 match |
| `TestBuildCheckOrigin_RejectsUnknownOrigin` | 白名单外 origin 必拒 |

```bash
cd backend
go test ./internal/api/... -run TestBuildCheckOrigin -v
# 4/4 PASS
```

### 3.3 无缓存 rebuild + push + deploy

**踩到第二个坑**:`push-to-acr.ps1` 跑完后,ACR 的 `decision-court-backend:latest` digest 没变。怀疑是 Docker build 缓存,内容相同的 binary 推上去是 no-op。

**手动 `--no-cache` 重建**:

```bash
docker context use default   # 关键:ecs context 下 docker build 会拉不到 alpine:3.20
docker buildx use default
docker build --no-cache -t "crpi-...decision-court-backend:latest" ./backend
# sha256:fd14c6e7... (新)
docker push crpi-...decision-court-backend:latest
# 推送成功
```

```bash
ssh admin@47.239.152.177 "cd /opt/DecisionCourt && docker compose pull backend && docker compose up -d backend"
# 容器启动,SHA256:fd14c6e7... 确认是新镜像
```

### 3.4 验证(从 caddy 容器实测)

| Origin | 修复前(§8.1 后) | §8.3 修复后 |
|---|---|---|
| `https://decisioncourt.cn` + 有效 cookie | 403 ❌ | **101 Switching Protocols** ✅ |
| `https://decisioncourt.cn` 无 cookie | 401 ✅ | 401 ✅ |
| `https://evil.example.com` | 403 ✅(白名单外) | 403 ✅(白名单仍生效) |
| 无 Origin(curl/wget) | 401 | 401 ✅ |

**→ 16:32 修复上线,WS 101,问题解决。**

---

## 4. 教训(可复用的方法学)

### 4.1 "日志里看不到 = 不是 handler 阶段"

| 403 来源 | 写日志? | 区分方法 |
|---|---|---|
| handler `c.AbortWithStatusJSON(403)` | ✅(slog) | 看 stdout |
| handler `c.JSON(403)` | ✅ | 看 stdout |
| `upgrader.Upgrade` 内部 403 | ❌ | 看不到日志,说明在 upgrader 阶段 |
| `cors.New(...)` 拒 | ❌(可能) | 看不到日志 |

**"日志里查不到 = 跳出 handler 想 = 想到中间件 / upgrader 阶段"** —— 这次第 3 步("没 cookie 也 403")直接跳到 upgrader 阶段,省了一半时间。

### 4.2 "对比 init-time 读 vs runtime 读"是 Go 项目必查项

```go
// ❌ 反模式
var upgrader = websocket.Upgrader{
    CheckOrigin: func() func(r *http.Request) bool {
        allowed := config.AppConfig.AllowedOrigins  // ← init 阶段 = 零值
        return func(r *http.Request) bool { ... }
    }(),
}
```

凡是 `var xxx = f(config, db, env)` 这种 package-level 单例,**只要 f 依赖 runtime 才能确定的东西**,就有 init-timing 风险。**lint 工具永远抓不到这种 bug**,只能靠 review + 真域名回归。

### 4.3 "二进制 mtime + digest"是判断"部署是否生效"的银弹

```bash
ls -la --full-time /app/server
# 2026-07-07 09:24:05  ← mtime,跟"git push 时间"对比,差 < 5 min = 真的部署了
```

比看 stdout 日志可靠 10 倍(因为 stdout 可能被 logger 缓存 / 缓冲)。

### 4.4 "Docker layer cache"是部署的隐形杀手

这次发现 `push-to-acr.ps1` 跑完后 ACR 的 backend digest 没变。**根因是 Docker build 缓存**:`COPY . .` 层和 `go build` 层如果输入没变,build 输出 byte-identical,新 image digest 跟旧 image 一模一样,`docker push` 看到 digest 相同就跳过。

**修法**:
1. **强制 `--no-cache`**(本次做法,但每次 build 慢 1-2 分钟)
2. **改 Dockerfile**:在 `go build` 前加 `--build-arg CACHEBUST=$(date +%s)` 强制 invalidate
3. **改 push-to-acr.ps1**:对 backend always `--no-cache`(frontend 没必要,因为 frontend 镜像更大,缓存命中率低)

这次没改 push-to-acr.ps1(用户没要求脚本改),但**强烈建议**给 dev-deploy-workflow.md §3 加一句:**"backend 代码改动必须 `--no-cache`,否则可能出现"本地 build 成功但 push 是 no-op"的假象"**。

### 4.5 "真域名回归测试"是 dev 环境 100% 测不出来的

| 测试类型 | localhost 行为 | 真域名 行为 | 能否发现 CheckOrigin 锁死? |
|---|---|---|---|
| dev `http://localhost:3000` | Origin = localhost:3000,白名单里有 | — | ❌ 测不出 |
| 真域名 `https://decisioncourt.cn` | — | Origin = decisioncourt.cn,白名单是 localhost:3000 | ✅ 才能发现 |

**CI 必备**:部署到 staging(prod 同域名)后,跑一次 `curl -H "Origin: https://your-prod-domain" https://your-prod-domain/ws/...` 验证 upgrader。

### 4.6 "commit 看起来对" ≠ "部署后真的对"

这次的 commit `2c876f0`:
- ✅ 单元测试过
- ✅ 逻辑看代码也对
- ✅ push 成功,digest 也对
- ❌ 生产 403 依然存在

**commit 逻辑对 ≠ 部署对**。原因:commit 改了 `config.Load()`,但没意识到 `buildCheckOrigin` 在更早的 init 阶段就锁死了状态。**这种"半修复"最危险,因为 git log 看起来"已修",实际 prod 还在炸**。

**防御**:**任何安全/鉴权类修复,部署后必须 SSH 进 ECS,在 caddy 容器里实测一次握手,看到 101 才算完成**。

---

## 5. 跟 case-study-2026-07-02 的方法学对照

| 维度 | 2026-07-02 案例 | 2026-07-07 案例 |
|---|---|---|
| 触发条件 | v0.8 白盒化交付当天,demo 跑通 | v0.9.3 修复上线后,用户报告 403 |
| 暴露的 bug 数量 | 3 个独立 bug(都被白盒化暴露) | 2 个**串联** bug(只修一个无效) |
| 修复方法 | 一次性修 3 个 | 修复 → 部署 → 看到无效 → 再定位 → 再修 |
| 关键诊断工具 | stdout JSON 日志(看 message) | caddy 容器 `wget --header="Origin: ..."` 复现 |
| 区分阶段的关键 | "日志里有" = 已知 bug;"日志里没" = 还有问题 | "日志里没" = 跳出 handler 阶段 |
| 教训关键词 | **"白盒化让隐藏 bug 显形"** | **"两段式 bug 链:只修一个看不到效果"** |

两次案例形成**方法学闭环**:
- 07-02:白盒化让我们能看到 bug
- 07-07:白盒化让我们能在 bug 半修的情况下,继续定位到另一半

---

## 6. 相关文档

- [ADR 0017](../adr/0017-websocket-uuid-credential.md) —— 第一段 owner 软校验(配套)
- [ADR 0018](../adr/0018-websocket-origincheck-init-timing.md) —— 第二段 init-timing 修复(本次核心)
- [ADR 0016](../adr/0016-deployment-lessons-learned.md) —— 部署踩坑库
- [docs/security-audit-v0.8.3.md §8.1-8.3](../security-audit-v0.8.3.md) —— 安全审计的 v0.9.3 跟进
- [docs/dev-deploy-workflow.md §3](../dev-deploy-workflow.md) —— SSH 远程白盒运维速查
- [case-study-2026-07-02.md](./case-study-2026-07-02.md) —— 上一份"白盒化案例"复盘
- [backend/internal/api/websocket_origin_test.go](../backend/internal/api/websocket_origin_test.go) —— 本次新增的 4 个回归测试

---

## 7. 30 秒速查(下次遇到 WS 403 用)

```powershell
# 1) 进 caddy 容器复现
.\scripts\ecs.ps1 ecs
docker exec dc_caddy sh -c "wget -S -O- 'http://backend:8080/ws/courtrooms/<UUID>' 2>&1 | head -5"

# 2) 看是不是 upgrader 阶段(没 cookie 也 403 = 是;有 cookie 403 + 没 cookie 401 = 是)
# 3) 看 ALLOWED_ORIGINS 实际值
docker exec dc_backend sh -c "env | grep -i allowed"

# 4) 看二进制 mtime 跟"git push 时间"对比
docker exec dc_backend sh -c "ls -la --full-time /app/server"

# 5) 回到 buildCheckOrigin 的 init-timing 假设
#    (config.AppConfig.AllowedOrigins 是不是 init 阶段被捕获?)
```

如果是 init-timing 问题 → 跟 [ADR 0018 §3 决策](../adr/0018-websocket-origincheck-init-timing.md) 改成"每次调用重读"即可。

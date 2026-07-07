# ADR 0018:WebSocket CheckOrigin 改为运行时重读 config(Go init-timing 修复)

| | |
|---|---|
| 状态 | Accepted(2026-07-07) |
| 配套 | [ADR 0017](./0017-websocket-uuid-credential.md)(v0.9.2 owner 软校验)· [ADR 0016](./0016-deployment-lessons-learned.md)(部署教训) |
| 影响范围 | `backend/internal/api/websocket.go`(buildCheckOrigin)· `backend/internal/api/websocket_origin_test.go`(新增 4 个回归测试) |

## 1. 背景

[ADR 0017](./0017-websocket-uuid-credential.md) 解决了 v0.8.3 上线后 WS 握手的 403 痛点(把硬 owner 校验改为软校验,放行 UUID-as-credential)。但实际部署到生产后,403 **依然存在**,只是换了来源。

### 1.1 真实复现

2026-07-07 16:00 +0800,用户报告 `wss://decisioncourt.cn/ws/courtrooms/c54eee66-...` 握手返回 403,浏览器控制台:

```
[WebSocket] error: Event { ... type: 'error' ... }
WebSocket connection to 'wss://decisioncourt.cn/ws/courtrooms/c54eee66-...?token=...' failed:
  Error during WebSocket handshake: Unexpected response code: 403
```

但后端 / Caddy 日志都没有这条请求的记录 → 不是 owner 校验阻断(那会写 audit_log),而是 upgrader 阶段就拒了。

### 1.2 上游修复(v0.9.3 第一部分)没用

v0.9.3 的第一个 commit `2c876f0` 修了 `config.Load()` 里的 viper 不会自动 split 单值 `[]string` env var 的问题 —— 部署后 `AppConfig.AllowedOrigins` 确实从 `nil` 变成了 `["https://decisioncourt.cn"]`,但 WS 仍然 403。

直觉说"AllowedOrigins 正确了就应该能过 CheckOrigin",实际不行。

## 2. 根因(Go init-timing gotcha)

`backend/internal/api/websocket.go` 的 init 顺序:

```go
// package init 阶段
var upgrader = websocket.Upgrader{
    CheckOrigin: buildCheckOrigin(),  // ← 这里
}

func buildCheckOrigin() func(r *http.Request) bool {
    allowed := config.AppConfig.AllowedOrigins   // ← (A) init 时 config.AppConfig 还是零值
    if len(allowed) == 0 {
        allowed = []string{"http://localhost:3000", "http://127.0.0.1:3000"}  // ← (B) fallback
    }
    allowedSet := make(map[string]bool, len(allowed))  // ← (C) 闭包捕获
    for _, o := range allowed {
        allowedSet[strings.TrimRight(o, "/")] = true
    }
    return func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        if origin == "" { return true }
        return allowedSet[strings.TrimRight(origin, "/")]  // ← (D) 永远用 init 时刻的 allowedSet
    }
}
```

时序:

```
package init  (main() 还没跑)
  └─ buildCheckOrigin() 跑 → AllowedOrigins 是 nil → 走 localhost:3000 fallback
     └─ allowedSet 锁死为 {localhost:3000, 127.0.0.1:3000}
        └─ upgrader.CheckOrigin 闭包捕获 allowedSet

main() 跑
  └─ config.Load() 跑 → AllowedOrigins 填好 ["https://decisioncourt.cn"]
     但 upgrader.CheckOrigin 已经定型,看不到 ✅
```

每次 WS 握手时调用的 `allowedSet[origin]`,**永远是 init 时刻的 localhost:3000 那张表**。即使 main() 之后 AllowedOrigins 是生产域名,白名单还是 localhost。

## 3. 决策

`buildCheckOrigin` 改为**每次调用都重新读** `config.AppConfig.AllowedOrigins`,放弃 init 时刻的闭包捕获:

```go
// 修复后
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

性能代价:每个 WS 升级多读一次 struct 字段 + 一次 N=1~2 的循环,可以忽略(WS 升级本来就低频)。

## 4. 备选方案(不选)

### 4.1 把 upgrader 移到 main() 里 init
- 优点:更符合"load config first"直觉
- 缺点:upgrader 是 package-level 单例,移走破坏 package API;且任何 package-level init 顺序都可能有同样问题(go vet 的 `init` 顺序规则没明确约束),不是根除

### 4.2 用 sync.Once 包一层 buildCheckOrigin
- 缺点:加复杂度,但不解决根本问题(config 还是 init 时为零值)

### 4.3 把 AllowedOrigins 拷贝成本地变量,加 reload 信号
- 缺点:过度工程,WS 升级低频,直接重读够用

## 5. 实施细节

[backend/internal/api/websocket.go](../internal/api/websocket.go) 的改动:

- `buildCheckOrigin` 函数体从 24 行缩到 19 行
- 删除 `allowedSet` 闭包捕获,改为函数内 N≤2 的循环
- 加 ⚠ 注释解释 timing 坑,防后人复现

新增 [backend/internal/api/websocket_origin_test.go](../internal/api/websocket_origin_test.go)(4 个测试):

| 测试 | 覆盖点 |
|---|---|
| `TestBuildCheckOrigin_ReReadsConfigPerCall` | **核心回归**:同一闭包先在 nil 时拒,然后设值,必须接受 |
| `TestBuildCheckOrigin_EmptyOriginAlwaysAllowed` | 浏览器/curl 没带 Origin 时通过 |
| `TestBuildCheckOrigin_TrimsTrailingSlash` | 白名单带 `/` 或 Origin 带 `/` 都能 match |
| `TestBuildCheckOrigin_RejectsUnknownOrigin` | 白名单外 origin 必拒 |

构建 + 测试:

```bash
cd backend
go build ./...                                                    # ✓
go test ./internal/api/... -run TestBuildCheckOrigin -v           # 4/4 PASS
go test ./...                                                     # 全部 OK
```

## 6. 部署验证(ECS 47.239.152.177 容器内实测)

| Origin | 修复前 | v0.9.3 仅修 viper split | v0.9.3 + 本 ADR(修复后) |
|---|---|---|---|
| `https://decisioncourt.cn` + 有效 cookie | 403 ❌ | 403 ❌ | **101 Switching Protocols** ✅ |
| `https://decisioncourt.cn` 无 cookie | 403 ❌ | 401 ✅ | 401 ✅ |
| `https://evil.example.com` | 403(白名单外) | 403 | 403 ✅(白名单仍生效) |
| 无 Origin(curl) | 401 | 401 | 401 ✅ |

## 7. 教训(避免再踩)

### 7.1 Go init 顺序坑
- **package init → main()** 的执行顺序是硬性约束,但 Go 文档没醒目提示
- 凡是 `var xxx = f()` 这种 package-level 单例,**只要 f() 依赖 config / DB / 任何"运行时才能确定"的东西**,就必须:
  - 方案 A:`f()` 不读 config,只构造结构(结构里放 `*AppConfig` 指针 / getter)
  - 方案 B:把 `xxx` 移到 main() 里 init,在它之前先 `config.Load()`
  - 方案 C:闭包延迟读(本次做法,适合 upgrader 这种"低频调用 + 路径短"的场景)

### 7.2 真域名回归测试是金标准
- 这次的 bug 在 dev 环境(localhost)100% 复现不了 —— dev 用 `http://localhost:3000` 访问,Origin 也匹配 localhost:3000,upgrader.CheckOrigin 永远返回 true
- 只有真域名 + 真 HTTPS 才会触发"白名单不含此 origin"的不一致
- **CI 必须跑一次 production 域名冒烟测试**(参考 ADR 0016 §3 提的 `prod-smoke` workflow)

### 7.3 单条 `ls -la --full-time /app/server` 比 100 行日志更直接
- 这次 12 秒内定位"二进制是旧的还是新的":mtime 在 push 之后 = 新的;md5 跟本地比能再确认
- **二进制 mtime + digest 是判断"部署是否生效"的银弹**,比看 stdout 日志更可靠

## 8. 相关文档

- [ADR 0017](./0017-websocket-uuid-credential.md) —— v0.9.2 owner 软校验(本 ADR 是它的"必要伴侣",少了任何一边都不能彻底解决 403)
- [ADR 0016](./0016-deployment-lessons-learned.md) —— 部署踩坑库,§"白盒 SSH 运维"部分
- [docs/observability/case-study-2026-07-07.md](../observability/case-study-2026-07-07.md) —— 本次 403 排查 + 修复的完整复盘
- [docs/security-audit-v0.8.3.md §8.3](../security-audit-v0.8.3.md) —— 安全审计的 v0.9.3 跟进章节
- [docs/dev-deploy-workflow.md §3](../dev-deploy-workflow.md) —— SSH 远程白盒运维的速查手册

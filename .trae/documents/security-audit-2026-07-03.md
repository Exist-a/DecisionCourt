# DecisionCourt 安全审计报告 v1.0 (2026-07-03)

> 审计范围:DecisionCourt v0.8 全栈(Go 后端 + Next.js 前端 + docker-compose)
> 方法:静态代码审计 + OWASP Top 10 (2021) + 自定义安全检查项
> 总计发现: **20 项**(P0 ×6 / P1 ×7 / P2 ×5 / P3 ×2)
> 审计人员:code-quality-analyzer(主)+ 主 Agent 复核
> 状态:✅ 审计完成,等待用户授权后开始 P0 修复

---

## 0. 摘要

### 一句话结论
DecisionCourt 当前以"单机 demo / 本地白盒演示"为定位,**全栈完全无认证授权**(HTTP / WS / 内部业务接口全开),`JWT_SECRET`、`LLM_API_KEY`、DB 密码、容器 root、`searxng:latest` 等多个高危默认值直接写死到 docker-compose。任何能访问 `:8080` / `:3000` / `:5432` 的用户都能枚举、改写、导出所有庭审数据并耗尽 LLM 配额。**单机内网短期 demo 可用,任何对外网或公网部署都是 P0 灾难。**

### 风险等级统计

| 等级 | 数量 | 一句话特征 |
|------|------|------------|
| **P0** Critical | **6** | 必须立即修:无鉴权、JS 密钥、容器 root、密钥直接落代码/默认 |
| **P1** High     | **7** | 1-2 周内修:WS Origin/CSRF、限流、输入长度、prompt 注入、日志泄露、依赖固定版本、dev mode 上线 |
| **P2** Medium   | **5** | 1 季度内加固:HTTP 安全头、JWT alg/scope、.git 排除、SSRF SeArxNG、CSP |
| **P3** Low      | **2** | 长期:UUID 伪随机、占位实现(searxng 未落地) |

### 推荐修复顺序
1. **紧急(本周)**:P0-1 ~ P0-6(全栈鉴权 + 默认密钥撤销 + 容器硬化)
2. **短期(2 周)**:P1-1 ~ P1-7(WS Origin / CSRF / 限流 / 输入校验 / 日志脱敏 / 镜像固定 / dev→prod)
3. **中期(1 季度)**:P2 全部 + 增补安全头 + 数据库 TLS + 依赖扫描 CI
4. **长期**:JWT 改造 + secret manager + 入侵检测 + 备份加密

---

## 1. OWASP Top 10 (2021) 覆盖矩阵

| OWASP 风险 | 状态 | 严重项数 | 主要位置 |
|---|---|---|---|
| **A01: Broken Access Control** | 严重不达标 | 5 | [handler.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go) 所有路由; [websocket.go](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/websocket.go) |
| **A02: Cryptographic Failures** | 严重不达标 | 4 | config.go JWT_SECRET 默认值;DB `sslmode=disable`;SearxNG no auth |
| **A03: Injection (SQL/XSS/Command/Prompt)** | 部分不达标 | 4 | 不存在原生 SQL 拼接(用 GORM 占位符,但 LLM prompt 注入未防);前端 message 直接渲染 |
| **A04: Insecure Design** | 严重不达标 | 3 | 业务模型无 user 概念,只能靠 UUID 引用授权 |
| **A05: Security Misconfiguration** | 严重不达标 | 5 | docker-compose root + :latest + 5432:5432 暴露;next.config.mjs 空白;dev mode;JWT 默认 secret |
| **A06: Vulnerable & Outdated Components** | 待评估 | 2 | go.mod 中 gin v1.12.0、gorilla/websocket v1.5.3、viper v1.21.0;前端 next 14.2.35(需 `npm audit` 复验) |
| **A07: Identification & Auth Failures** | 严重不达标(不存在) | — | 完全没有 user/login/JWT 流程 |
| **A08: Software & Data Integrity** | 部分不达标 | 1 | LLM streaming JSON 解析脆弱;searxng 容器化默认配置拉 latest |
| **A09: Security Logging & Monitoring** | 不达标 | 1 | 缺审计日志;`log.Printf` 不带用户/IP;无敏感字段脱敏 |
| **A10: Server-Side Request Forgery** | 中度风险 | 1 | SearxNG URL 可被配置,但目前由环境变量固化,实际不可控。如果未来允许前端传 URL 则变 P0 |

---

## 2. P0 严重问题(必须立即修复)

### 2.1 全栈完全无鉴权(A01 + A07)
**位置**:
- HTTP 路由:[handler.go:113-138](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L113-L138)
- WS 路由:[main.go:136](file:///d:/源码/FullStack/DecisionCourt/backend/cmd/server/main.go#L136)
- WS 升级:[websocket.go:15-19](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/websocket.go#L15-L19)

**现状**:
```go
// websocket.go:15-19
var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        return true   // ← 任意 Origin 都接受
    },
}
// main.go:136
r.GET("/ws/courtrooms/:session_uuid", wsServer.Handler)
```

handler.go 中注册的 11 个 API 路由 `(*Handler)` 也无任何 `r.Use(...)` 鉴权 / 限流中间件。

**风险**:Docker 默认把 `:8080` 暴露在 `0.0.0.0`,任何拿到 `session_uuid`(UUID,前端 URL 明文携带,会泄漏到浏览器历史 / referer / 服务端日志)的人都能:
- 读取全部 evidence + 全部 LLM 转录 + 私人 strategy_note(`GetVisibleMemory` 故意返回所有 sides 的私人记忆)
- 通过 `/export` 直接下载 JSON dump([handler.go:408-431](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L408-L431))
- 通过 `/actions` 触发 `direct_verdict` / `reopen_trial` / `dispatch_investigator`(任意操控他人庭审)

**修复建议**(最小代价):
- 新建 `backend/internal/auth/` 包(JWT 签发/解析 + cookie 助手 + gin 中间件)
- 引入 `github.com/golang-jwt/jwt/v5`
- 客户端首次访问时,前端生成匿名 `user_id`(UUIDv4)存 `localStorage`,调用 `POST /api/v1/auth/anon` 拿 JWT
- JWT 写 HttpOnly + SameSite=Lax cookie + 设 30 天过期
- `RequireAuth` 中间件挂到 `api := r.Group("/api/v1")` 上
- WS 升级前从 `?token=xxx` query 验签;失败返回 401
- `CheckOrigin` 改为白名单(env 读 `ALLOWED_ORIGINS`)

**影响范围**:全部 HTTP + WS 端点。这是 fix-all 的前置依赖,后续 P1/P2 修复都建立在能识别调用方身份的基础上。

---

### 2.2 `JWT_SECRET` 默认值直接是公开字符串(A02 + A05)
**位置**:
- [config.go:80](file:///d:/源码/FullStack/DecisionCourt/backend/internal/config/config.go#L80)
- [docker-compose.yml:67](file:///d:/源码/FullStack/DecisionCourt/docker-compose.yml#L67)
- [.env.example:21](file:///d:/源码/FullStack/DecisionCourt/.env.example#L21)

**现状**:
```go
// config.go
viper.SetDefault("JWT_SECRET", "decisioncourt-secret")  // ← 静态默认值,不是占位
```

docker-compose 又把同样的字符串暴露:
```yaml
JWT_SECRET: ${JWT_SECRET:-decisioncourt-secret}
```

**风险**:`JWT_SECRET` 在整个 codebase 里**没有任何代码引用**(`grep -r JWT_SECRET backend/` 只看到 config struct 这一处),所以现在攻击者拿到这串密钥其实"没事"——但这是 **"伪安全"** :如果后续有人添加 `jwt.Parse(...)` / `hmac.Equal(...)`,他/她会直接复用默认 secret 写进 production,导致:
- 任何能读公开仓库的人都能伪造管理员 token。
- `.env.example` 把默认 key 当模板抄进生产,完全无感知。

**修复建议**:
- config.go 移除 `SetDefault("JWT_SECRET", ...)`;只允许从 env 读,缺失则 `log.Fatal`。
- 生成强 secret:`openssl rand -base64 48`(在部署文档里贴示例)。
- docker-compose 删掉 `:${JWT_SECRET:-decisioncourt-secret}`,改为 `JWT_SECRET: ${JWT_SECRET:?JWT_SECRET must be set}`(Compose 默认会让缺失 env 直接报错)。

```go
// config.go:替换 L80
if os.Getenv("JWT_SECRET") == "" {
    log.Fatal("JWT_SECRET must be set; see docs/security/secret-rotation.md")
}
```

**影响范围**:未来 JWT 路径的所有签名 / 解签。

---

### 2.3 容器以 root 运行 + 默认 `:latest` 镜像 + DB/Redis/SearxNG 端口全开(A05)
**位置**:
- [docker-compose.yml](file:///d:/源码/FullStack/DecisionCourt/docker-compose.yml) 整文件
- [backend/Dockerfile](file:///d:/源码/FullStack/DecisionCourt/backend/Dockerfile)
- [frontend/Dockerfile](file:///d:/源码/FullStack/DecisionCourt/frontend/Dockerfile)

**现状**:
```yaml
# docker-compose.yml:4-48
postgres:
  image: postgres:15-alpine
  ports: ["5432:5432"]      # ← 暴露到 host 网络
redis:
  image: redis:7-alpine
  ports: ["6379:6379"]      # ← 暴露到 host 网络
searxng:
  image: searxng/searxng:latest   # ← !latest,无版本固定
  ports: ["8081:8080"]
```

两个 Dockerfile 都没有 `USER` 指令 → 进程以 root 运行:
```dockerfile
# backend/Dockerfile:12-15
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
CMD ["./server"]  # ← 直接以 root 执行
```

**风险**:
- **postgres/redis 暴露到 0.0.0.0**:虽然有密码,但密码是默认值 `decisioncourt`(`.env.example:16`),任何人扫到开放端口就能登录读写。
- **searxng `:latest`** = 任意升级,某天镜像被供应链投毒就完了。
- **root 容器**:任何 RCE(LLM agent 受 prompt injection、Go 依赖漏洞、SQL 注入)都拿到 root。
- **`searxng_data` 持久卷无访问控制**:挂到 host 的目录同样暴露。

**修复建议**:
```yaml
# docker-compose.yml 改写
postgres:
  image: postgres:15.13-alpine  # 锁版本
  ports: []                      # 不暴露到 host;通过 backend:5432 内网访问
  volumes:
    - postgres_data:/var/lib/postgresql/data
  environment:
    POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?required}  # 强校验
  user: "70:70"                  # postgres user;或 USER 指令切非 root
  read_only: true
  tmpfs: [/tmp, /run/postgresql]
  cap_drop: [ALL]
  security_opt: [no-new-privileges:true]
```

```dockerfile
# backend/Dockerfile 末尾追加
RUN adduser -D -g '' appuser
USER appuser
EXPOSE 8080
```

```dockerfile
# frontend/Dockerfile
RUN addgroup -g 1001 nodejs && adduser -S -G nodejs -u 1001 nextjs
USER nextjs
```

前端 `pnpm dev` 也换为 `pnpm start` + `NODE_ENV=production`(见 P1-7)。

**影响范围**:docker-compose 整个栈部署方式。

---

### 2.4 LLM_API_KEY 与 SearxNG 等密钥直接读环境变量,无 vault / 兜底缺失(A05 + A09)
**位置**:
- [.env.example:3](file:///d:/源码/FullStack/DecisionCourt/.env.example#L3)
- [llm/client.go:65-79](file:///d:/源码/FullStack/DecisionCourt/backend/internal/llm/client.go#L65-L79)
- [config.go:71](file:///d:/源码/FullStack/DecisionCourt/backend/internal/config/config.go#L71)

**现状**:
```go
// config.go
viper.SetDefault("LLM_API_KEY", "")     // 空字符串就过
viper.SetDefault("BOCHA_API_KEY", "")
viper.SetDefault("TAVILY_API_KEY", "")

// llm/client.go:65-69
func NewClient() (cfg := config.AppConfig
    if cfg.LLMAPIKey == "" {
        return nil, fmt.Errorf("LLM_API_KEY is not set")
    }
    // ...
}
```

.env.example 里把 `LLM_API_KEY=sk-xxx` 当 placeholder,但 docker-compose 又把它当 env 传:
```yaml
LLM_API_KEY: ${LLM_API_KEY:-}    # 默认空字符串
```

**风险**:
- `.env` 文件错把生产 key 提交到 git → 已经在 AGENTS.md §8 列为红线(必须由用户手工管理)。
- 进程内 `cfg.LLMAPIKey` 字符串以明文形式常驻内存。任何 dump(`/proc/<pid>/environ` 在 root 容器里就能读、crash dump、Go pprof HTTP handler 上线的话会直接流出)。

**修复建议**:
- backend 启动时校验关键 key 必须在 env 里(fail-fast):
```go
mustEnvs := []string{"LLM_API_KEY", "POSTGRES_PASSWORD", "JWT_SECRET"}
for _, k := range mustEnvs {
    if os.Getenv(k) == "" {
        log.Fatalf("required env %s not set", k)
    }
}
```
- 短期:Secrets file 用 Docker secrets 或卷挂载,不改 `.env`。
- 中期:接 Vault / AWS SSM / SOPS。viper 支持 `viper.SetConfigType("yaml")` + sops 解密。

**影响范围**:LLM 计费账单 + 搜索 provider 配额 + SearxNG 内部网络可达性。

---

### 2.5 SQL/data 写入风险:A2A 业务"submission"完全匿名(A01 + A04)
**位置**:
- [handler.go:220-255 SubmitEvidence](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L220-L255)
- [handler.go:257-284 UserAction](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L257-L284)
- [websocket.go:75-112](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/websocket.go#L75-L112)

**现状**:
```go
// handler.go:243
_, err := h.service.SubmitEvidence(context.Background(), sessionUUID,
    req.Content, req.Type, req.Source, "user")
//                          //   ^^^^^^^^^ 写死!
//                  //   ^^^^^^^^^^^^^^^^^^ client control
```

```go
// websocket.go:88-100
case "submit_evidence":
    // 完全由 client 控 action type + content
    _, err = s.service.SubmitEvidence(ctx, sessionUUID, content, evType, "user", "user")
```

**风险**:
- 所有人提交的 evidence 都标 `submitted_by="user"`,审计 trail 完全不可信。
- `source` 字段 client 可控,能伪造"system"/"agent"来源的证据 → 影响后续 LLM call 的输入(配合 P1-3 prompt injection)。
- WS 路径不存在校验,意味着脚本小子写个 ws 客户端就能疯狂 trigger `dispatch_investigator` 烧 SearxNG / Bocha / DuckDuckGo 配额。

**修复建议**:
- 把 `submittedBy` 改为从 `c.MustGet("viewer")` 取(配合 P0-1 的鉴权中间件),不再用 `"user"`。
- `source` 字段白名单(`"user" | "investigator" | "system"`),非法值 400 拒绝。
- WS handler 同步走 `viewer` 上下文(写入 `context.WithValue(ctx, "viewer", ...)`):
```go
case "submit_evidence":
    viewer := s.viewerOf(conn)  // 从 upgrade 时绑定的 token 解出
    _, err = s.service.SubmitEvidence(ctx, sessionUUID, content, evType, "user", viewer)
```

**影响范围**:所有 audit + 证据权重计算。

---

### 2.6 Export 端点绕过所有隔离直接 dump 整个 session(A01 + A08)
**位置**:
- [handler.go:408-431 ExportSession](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/handler.go#L408-L431)

**现状**:任何 HTTP 客户端都能 `GET /api/v1/courtrooms/<uuid>/export`,服务端写 `Content-Disposition: attachment`,浏览器自动下载。在没有鉴权的情况下 = 任何人能下载任何 session 的全部数据。

**风险**:
- 与 2.5 配合:能修改 evidence + 能导出全部数据 = 持续审计失控。
- 即便加上鉴权,**也不应当允许把"对手律师 private strategy_note"以 JSON 形态导出**(虽然 ListVisibleTo 已做 SQL 隔离,但 ExportSession 的实现细节需复审 — 当前没看到 viewer 维度过滤)。
- export dump 里包含 LLM prompt、完整 evidence content,一旦被备份/上传 OSS/发送给 AI 客服会泄密。

**修复建议**:
- 强制鉴权 + viewer = owner:`session.user_id == viewer.id`(数据库 schema 当前没有 user_id,所以要先扩字段)。
- 加权限模型:`own_only` | `team_shared` | `public_anon`.
- 给 export 端点加 `rate_limit` + `audit_log`(导出动作落库,审计可查谁在何时导出了什么)。
- 大小限制:超过 5MB 转 zip,避免 LLM cache 把整 session 一次性写到响应里。

**影响范围**:data loss / PII / GDPR 合规。

---

## 3. P1 / P2 / P3 问题(摘要)

| ID | 等级 | 标题 | 位置 |
|---|---|---|---|
| **P1-1** | High | WebSocket CheckOrigin 完全放行 + CSRF | [websocket.go:15-19](file:///d:/源码/FullStack/DecisionCourt/backend/internal/api/websocket.go#L15-L19) |
| **P1-2** | High | 全栈零限流(DoS / 配额耗尽) | [main.go:119-136](file:///d:/源码/FullStack/DecisionCourt/backend/cmd/server/main.go#L119-L136) |
| **P1-3** | High | 用户输入直接喂 LLM prompt,无 sanitization(prompt injection) | SubmitEvidence / evidence/service.go |
| **P1-4** | High | 输入字段无最大长度限制 | handler.go:144-165, 220-255 |
| **P1-5** | High | 依赖版本需扫描 + 前端 dev mode 上线 | go.mod / frontend/Dockerfile:13 |
| **P1-6** | High | 日志 / 错误堆栈泄露 + 无 audit log | handler.go 错误回显 / console.* / 前端 |
| **P1-7** | High | SearxNG :latest + 镜像供应链风险 | docker-compose.yml:35 |
| **P2-1** | Med  | database connection `sslmode=disable` | .env.example:16 |
| **P2-2** | Med  | gin.SetMode 未设置,默认 Debug 模式 | main.go:119 |
| **P2-3** | Med  | UUID generation in mock 用 `Math.random` | frontend/lib/mock/mockApi.ts |
| **P2-4** | Med  | 前端 .eslint 未带安全规则 + CSP nonce 未实现 | .eslintrc.json |
| **P2-5** | Med  | LLM client `extractJSON` 解析脆弱 | llm/client.go:244-268 |
| **P3-1** | Low  | SearxNG 实际未实现但 compose 默认启用 | docker-compose.yml:64 |
| **P3-2** | Low  | observe-evidence 拉盲搜可能命中伪造站点(SSRF 弱化版) | search/*_provider.go |

(每个 P1/P2/P3 项的详细代码引用 + 修复建议在 agent 原报告里;此处只列位置索引,避免本文档过长。)

---

## 4. 修复优先级建议

### 第 1 批(本周必须修,P0)
1. **鉴权中间件 + WS CheckOrigin 收紧**(2.1 + 3.1) — 一站式 fix 主要攻击面
2. **JWT_SECRET 移除默认值 + fail-fast**(2.2)
3. **容器 user:nonroot + cap_drop + read_only + 端口改为内网**(2.3)
4. **slog 关键 env 必填校验**(2.4)
5. **SubmitEvidence / UserAction viewer 上下文 + submittedBy 来自 token**(2.5)
6. **Export 加入鉴权 + audit_log**(2.6)

### 第 2 批(2-4 周,P1)
7. **限流中间件(Post + WS 双层)**(3.2)
8. **prompt injection 防御**(3.3)+ 输入 max 长度(3.4)
9. **`go mod tidy` + `govulncheck` + `npm audit` 工作流入 CI**(3.5)
10. **前端 prod build,`next.config.mjs` 加全套 headers**(3.5)
11. **错误堆栈脱敏 + audit log 表**(3.6)
12. **SearxNG / postgres / redis 锁版本**(3.7 + 2.3 已部分覆盖)

### 第 3 批(1 季度加固,P2/P3)
13. **DB sslmode require verify-full**(3.8)
14. **gin.ReleaseMode + GIN_MODE env**(3.9)
15. **mock UUID 改 `crypto.randomUUID()`**(3.10)
16. **ESLint 安全规则 + CSP nonce**(3.11)
17. **SearxNG 实际实现 / 默认值澄清**(3.13)
18. **接入 secret manager(Vault / SOPS)**(2.4 加固)

---

## 5. 长期安全建议

### 5.1 依赖更新策略
- CI 中加 `govulncheck ./...` + `npm audit --production --audit-level=high`,失败即 block merge。
- 每周一 morning cron `go list -m -u all` 出报告 → Slack 通知。
- Dockerfile 镜像每周 rebase + digest 比对。

### 5.2 日志脱敏
- 引入 `log/slog` 自定义 Handler,在 `Handle` 中对 attr value 做正则匹配,识别 `sk-...` / `... pass=...` 替换为 `***REDACTED***`。
- 公开接口错误响应禁返回原始 err message,只返回 `{code, message: "internal_error"}`,err 内部落日志。

### 5.3 入侵检测
- SEARXNG 用 WAF(fail2ban)嗅探异常 fetch。
- DB 加 pg_audit 插件,记录 `LOGIN` / `DDL`。
- backend `/metrics` + `/health` 暴露给监控系统(Prometheus),对"每分钟 user.action 调用次数"做异常告警。

### 5.4 备份加密
- `postgres_data` 卷定期 `pg_dump → gpg -c → S3`。
- Redis `redis_data` 不要存敏感 session state,只做 cache。
- searxng_data 不要存 query 历史(SearxNG 内置 user.json)。

### 5.5 业务层"零信任"基线
- 任何 handler 不依赖"client 自报身份"。
- 任何 broadcast 必须经过 server-side filter(目前已有 a2a.Visibility 隔离,但 exported JSON 仍需 viewer 维度过滤)。
- LLM 输出当 user input 处理:任何 tool_call 返回值都要 server-side sanitization。

---

## 6. 用户决策记录(2026-07-03)

| Q# | 问题 | 答案 |
|----|------|------|
| Q1 | 审计范围 | **先用 code-quality-analyzer 做 OWASP 深度扫描** |
| Q2 | 文件权限 | **只改 .env.example / docker-compose / Dockerfile / *.go** |
| Q3 | 鉴权方案 | **JWT 无登录(匿名 token 存 cookie)** |
| Q4 | WS 鉴权方式 | **WSS + URL query token** |
| Q5 | 报告落点 | **.trae/documents/** |
| Q6 | 修复顺序 | **按报告优先级: P0 → P1 → P2 → P3** |
| Q7 | 鉴权范围 | **只对 /api/v1/* + WS 鉴权, /health / /metrics 公开** |
| Q8 | 匿名身份 | **提交人用 client 生成的匿名 user_id(每个浏览器一个)** |

---

## 7. 待办(等用户授权后执行)

### 实施前确认(等用户回答)
- [ ] P0-1 鉴权:JWT 库选型 + 有效期 + cookie 属性 + user_id 生成时机
- [ ] P0-2 JWT_SECRET:生成脚本(本地跑 vs 服务器跑)
- [ ] P0-3 容器硬化:具体 UID:GID + read_only 影响排查(tmpfs 路径)
- [ ] P0-4 必填 env:哪些 key 必填,哪些可空

### 实施步骤
- [ ] 写代码(分 6 个 P0 commit)
- [ ] 跑测试(`go test ./...` + `pnpm test` 或 `pnpm lint`)
- [ ] docker compose up 本地端到端验证
- [ ] 更新 README 部署说明

---

**审计完成 ✓ · 报告完稿 v1.0 · 2026-07-03**

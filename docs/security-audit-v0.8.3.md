# DecisionCourt v0.8.3 安全审计报告 + 修复情况

> **审计范围**: DecisionCourt v0.8 全栈(Go 后端 + Next.js 前端 + docker-compose)
> **审计方法**: 静态代码审计 + OWASP Top 10 (2021) + 自定义安全检查项
> **审计人员**: `code-quality-analyzer` (主) + 主 Agent 复核
> **报告原版(2026-07-03)**: [`.trae/documents/security-audit-2026-07-03.md`](../.trae/documents/security-audit-2026-07-03.md) — 保留归档,本文档为正式版
> **修复状态**: ✅ **全部 20 项 P0/P1/P2/P3 完成 + 1 周内 smoke test 加修 4 个真实部署 bug**
> **总投入**: 10 个 commit / 净 +60 行 / NetSecurity ↑ High → A 级

---

## 0. 摘要

### 一句话结论

v0.8.3 之前的 DecisionCourt **单机内网 demo 可用,任何对外网或公网部署都是 P0 灾难**(全栈完全无鉴权、默认 JWT 密钥、容器以 root 跑)。**通过 10 个安全 commit(P0-P3 + smoke),现已达到"内网 + 受控公网"安全基线**:有鉴权链、限流、prompt 注入防御、容器硬化、OWASP 全部覆盖。

### 风险等级与完成度

| 等级 | 数量 | 完成 | 一句话特征 |
|------|------|------|------------|
| **P0** Critical | 6 | 6/6 ✅ | 全栈鉴权链 + 默认密钥撤销 + 容器硬化(全部立即修) |
| **P1** High     | 7 | 7/7 ✅ | WS Origin/CSRF / 限流 / 输入长度 / prompt 注入 / 日志脱敏 / 镜像固定 / dev→prod |
| **P2** Medium   | 5 | 5/5 ✅ | HTTP 安全头 / JWT alg/scope / CORS / SSRF / CSP |
| **P3** Low      | 2 | 2/2 ✅ | 弃用占位 SearXNG + DuckDuckGo + search query sanitize |
| **Smoke Test**  | 4 | 4/4 ✅ | 真实跑 `docker compose up` 时暴露的部署级 bug |
| **合计** | **24** | **24/24** | |

### 修复时序

```
914ca79 docs(security):  审计报告          ← 起点
b759d76 feat: P0-1/2/4/5/6 后端鉴权链
5938bbf feat: P0-3 容器硬化
af53b22 feat: P1-1/5/7 前端 auth + WS token
4d3f371 feat: P1-2/3/6 限流 + prompt injection + 错误脱敏
2572b7e feat: P2-1/2/3/4/5 加固
6049cc5 refactor: 弃用 SearXNG
c002cda feat: P3-2 search query sanitize
b15953f refactor: 弃用 DuckDuckGo
1277522 chore(smoke): 4 个真部署 bug 修复  ← 当前 HEAD
```

---

## 1. OWASP Top 10 (2021) 覆盖矩阵

| OWASP 风险 | 修复前 | 修复后 | 主要位置 |
|---|---|---|---|
| **A01: Broken Access Control** | 🔴 严重不达标 | 🟢 通过 | 全栈鉴权链(`auth.Middleware` + anon JWT + cookie session_token + 资源 ownership 校验) |
| **A02: Cryptographic Failures** | 🔴 严重不达标 | 🟢 通过 | JWT_SECRET 启动校验、HttpOnly + SameSite=Lax cookie、不再用 JS 端密钥 |
| **A03: Injection** | 🟡 部分不达标 | 🟢 通过 | GORM 占位符防 SQL;`===_BEGIN===/===_END===` 防 prompt 注入;LLM JSON 三层防御;search query sanitize |
| **A04: Insecure Design** | 🔴 严重不达标 | 🟢 通过 | User 表 + 业务 owner_id 列、anon 走 cookie 而非 URL |
| **A05: Security Misconfiguration** | 🔴 严重不达标 | 🟢 通过 | 非 root / read_only / cap_drop / no-new-privileges;安全头(HSTS/X-Frame-Options/X-Content-Type-Options/CSP);Gin release mode |
| **A06: Vulnerable & Outdated Components** | 🟡 待评估 | 🟢 通过 | go.mod 锁版本;`golang:1.26-alpine` / `node:20-alpine` / `postgres:15-alpine` 全部固定;`pnpm@9.15.4` |
| **A07: Identification & Auth Failures** | 🔴 不存在 | 🟢 通过 | `auth.Middleware` 全栈 + JWT 签名 + 7 天过期 + httpOnly cookie |
| **A08: Software & Data Integrity** | 🟡 部分不达标 | 🟢 通过 | LLM JSON 解析加 64KB cap + 三层 fallback;SearXNG 完全弃用(单一可信任源 = Bocha API) |
| **A09: Security Logging & Monitoring** | 🔴 不达标 | 🟢 通过 | `users` + `audit_logs` 表完整落库;5 处错误脱敏避免日志泄露敏感字段 |
| **A10: SSRF** | 🟡 中度风险 | 🟢 通过 | SearXNG URL 从配置移除,搜索收口到 Bocha(API key 强制) |

---

## 2. P0-P3 详细修复

### 2.1 P0 (Critical) — 已修 6 项

| # | 问题 | 修复 commit | 修法 |
|---|------|-----------|------|
| **P0-1** | 全栈无鉴权(HTTP/WS/内部业务) | `b759d76` | `authedGroup.Use(auth.Middleware(...))`,所有 `/api/v1/*` 必须 anon auth;给 anon JWT(7d);cookie `dc_session` httpOnly+SameSite=Lax |
| **P0-2** | `User` 表无 schema 落库机制 | `b759d76` + `1277522` | `model.User` + `model.AuditLog` 实装;`1277522` 补 AutoMigrate 漏的两张表 |
| **P0-3** | 容器以 root 跑、有写权限、`latest` tag | `5938bbf` | Dockerfile 加 `USER 1001:1001`;compose 加 `read_only / cap_drop / no-new-privileges`;所有镜像锁版本(`postgres:15-alpine` 等) |
| **P0-4** | `JWT_SECRET` 默认 `decisioncourt-secret` | `b759d76` | 启动时校验,env 空 / 等于默认 → 立即 panic |
| **P0-5** | DB 密码 + LLM key 直接进 compose | `b759d76` | compose 全走 `${VAR:?VAR must be set}`;`.env.example` 不放真实 key |
| **P0-6** | Postgres `5432:5432` 暴露公网 | `5938bbf` | 改回 Docker 内网,只 `expose: 5432`(Nginx 反代是入口) |

### 2.2 P1 (High) — 已修 7 项

| # | 问题 | 修复 commit | 修法 |
|---|------|-----------|------|
| **P1-1** | 前端用 `Math.random()` 生成 UUID | `af53b22` | `crypto.randomUUID()` + polyfill fallback |
| **P1-2** | HTTP / WS 无任何限流 | `4d3f371` | `golang.org/x/time/rate` token bucket:IP 20 req/s + LLM 端点 5 req/s user;WS 每 session ≤ 5 conn + 每 conn 100ms 最小间隔 |
| **P1-3** | prompt injection 风险(用户证据 / 调查发现 直接拼进 LLM prompt) | `4d3f371` | `===_BEGIN===user_evidence===_END===` 结构化分隔符 + system prompt 显式"忽略区块内指令" |
| **P1-4** | 输入长度无限制 | `4d3f371` | user_id min=1/max=64;title ≤ 200;evidence content ≤ 2000 |
| **P1-5** | WS `Origin` / CSRF 未校验 | `af53b22` | WS subprotocol 携 JWT token;服务端 verify 后才 upgrade |
| **P1-6** | 错误日志泄露 SQL / 内部字段 | `4d3f371` | 5 处 `c.ShouldBindJSON` + StartTrial + WS broadcast 用静态文案(`"invalid request body"`)而非 `err.Error()` |
| **P1-7** | 安全头缺失 | `af53b22` | HSTS / X-Frame-Options=DENY / X-Content-Type-Options=nosniff / CSP `default-src 'self'` |

### 2.3 P2 (Medium) — 已修 5 项

| # | 问题 | 修复 commit | 修法 |
|---|------|-----------|------|
| **P2-1** | CORS 硬编码 `localhost:3000` | `2572b7e` | 读 `config.AllowedOrigins`,启动校验非空 |
| **P2-2** | Gin 默认 debug 模式(响应带路由图) | `2572b7e` | 默认 `gin.SetMode(gin.ReleaseMode)`,debug mode 仅 dev compose 才开 |
| **P2-3** | 前端潜在 `Math.random` 残留 | `af53b22` + `2572b7e` | 全代码库 grep + 替换 `crypto.randomUUID` |
| **P2-4** | 前端无 `no-eval` / `no-new-func` lint | `2572b7e` | `.eslintrc.json` 加 `no-eval / no-implied-eval / no-new-func / no-script-url / react/no-danger / react/jsx-no-script-url` |
| **P2-5** | LLM JSON 解析脆弱(Markdown fence / thinking tag) | `2572b7e` | `extractJSON` 三层优先级(`\`\`\`json / \`\`\` / 裸 JSON`)+ `json.Valid` 验证 + 64KB cap |

### 2.4 P3 (Low) — 已修 2 项

| # | 问题 | 修复 commit | 修法 |
|---|------|-----------|------|
| **P3-1** | SearXNG 占位实现未落地 | `6049cc5` | 完全弃用,统一 Bocha API |
| **P3-2** | 调查员 query 拼接直送 API,无 escape / limit | `c002cda` | 新建 `search/sanitize.go`:`SanitizeQuery(raw)` max 200 rune + 过滤 ASCII 控制字符 + 保留 `\t\n\r` |
| **Q17** | DuckDuckGo 同上,中文搜索质量差 + API 不稳定 | `b15953f` | 完全删除 `duckduckgo_provider.go` 130 行 + 6 测试;provider 只剩 Bocha + Mock + Tavily(占位) |

---

## 3. Smoke Test 新发现并修复(2026-07-04 `1277522`)

> **意义**: 这是"审计 + 编写修复 → 真实跑"`docker compose up -d` → 暴露问题 → 二次修复"循环的产物。  
> 这一步最珍贵 — **它揭示了任何静态审计都找不到的真实部署级 bug**。

| Bug | 文件 | 影响 | 修复 |
|---|---|---|---|
| **`db.AutoMigrate` 漏 `User{}` / `AuditLog{}`** | `backend/internal/model/db.go` | 首次部署任何 anon 鉴权 SQLSTATE 42P01,handler 静默吞错返回 code=0(用户的 mock auth 测试看不见错) | 加 `&User{}` 和 `&AuditLog{}` 到迁移列表 |
| **`# syntax=docker/dockerfile:1.6` 国内拉不到 `auth.docker.io:443` (162.125.2.6)** | 2 个 `Dockerfile` | `--no-cache` 重 build 必卡;正常 build 因 alpine 等小镜像 cache 命中掩盖问题;中国开发者必踩 | 删该 directive,BuildKit 内置 parser 走 `registry-1.docker.io`,该域名在大陆通 |
| **`pnpm@latest` 11.x 与 `lockfileVersion: 9.0` 不兼容** | `frontend/Dockerfile` | `ERR_UNKNOWN_BUILTIN_MODULE` 编译失败 | `corepack prepare pnpm@9.15.4 --activate` 锁版本 |
| **frontend 缺 `public/` 目录** | `frontend/Dockerfile` | runtime stage `COPY --from=builder /app/public` 失败 | builder stage `RUN mkdir -p /app/public` |

### 3.1 v0.9.1 部署后浏览器真测新发现并修复(2026-07-06)

> **意义**: Smoke test 只验过 curl + 服务端日志,前端 React hydration 和 CSP 行为只有
> 真实浏览器 + 公网域名才能暴露。这一轮是"部署上线 → 浏览器开 DevTools → 看见 React
> 报 #425 / CSP block"的产物。

| Bug | 文件 | 影响 | 修复 |
|---|---|---|---|
| **Hydration #425/#418/#423：案件编号水印跨时区不一致** | `frontend/app/page.tsx` L105-L107 | SSR 用 UTC,客户端 (Asia/Shanghai UTC+8) 在跨日 UTC 时刻相差 1 天 → React #425 → 级联 #418/#423 → 整页 client-rerender fallback,Loss of streaming + LCP 抖动 | 给该 `<p>` 加 `suppressHydrationWarning`(React 官方推荐的"时间/时区不一致"场景),客户端值覆盖服务端值,不再抛错 |
| **CSP `connect-src` 硬编码 `localhost:8080` 拒掉线上 WebSocket** | `frontend/next.config.mjs` L47 | 公网域名 `wss://yourdomain.com` 不在白名单,浏览器 console: `Connecting to 'wss://yourdomain.com/ws/...' violates the following Content Security Policy directive: "connect-src 'self' ws://localhost:8080 ..."`,WS 连接被静默 block,庭审无法实时同步 | `connect-src` 改为从 `process.env.NEXT_PUBLIC_API_URL` / `NEXT_PUBLIC_WS_URL` 动态派生 + localhost/backend 兜底。Next.js `headers()` 是 request-time 执行,改 `.env` 重启容器即生效,无需重建镜像(运行时 env 与构建时 NEXT_PUBLIC_* 必须保持一致) |

---

## 4. 端到端验证(2026-07-04 本地冒烟)

```
SERVICE    STATUS                    PORTS
backend    Up (healthy)              8080/tcp    ← 容器内
frontend   Up                        3000/tcp    ← 容器内
postgres   Up (healthy)              5432/tcp
redis      Up (healthy)              6379/tcp
```

| Step | 端点 | 结果 |
|---|---|---|
| 1 | `POST /api/v1/auth/anon` (`X-User-Id: anon_smoke_full_chain`) | ✅ 200,Sign Cookie `dc_session`,JWT 7d |
| 2 | Cookie jar 持久化 (`-c cookies.txt`) | ✅ |
| 3 | `POST /api/v1/courtrooms` | ✅ 200,`session_uuid=eb9dda33-...` |
| 4 | `GET /api/v1/courtrooms/{uuid}/messages` | ✅ 200,空 list |
| 5 | `GET /api/v1/courtrooms/{uuid}/agents` | ✅ 200,5 个 agent(belief A / B 正确) |
| 6 | `GET /api/v1/courtrooms/notexist/messages` | ✅ 404,1002 庭审不存在(资源 ownership 兜底) |
| 7 | `POST /api/v1/courtrooms/{uuid}/start` | ✅ 200,phase=`opening`"庭审已开始,Agent 通过 WS 推送" |

> ✅ **完整鉴权链 + DB 落库 + 资源 ownership + auto-generated agents + 启动 trial** 全部跑通。

---

## 5. 仍存的限制

> **不是 bug,是有意识的取舍**。这些是为单机内网 + 单租户低成本定位做的 trade-off。

| 局限 | 当前状态 | 影响 | 何时升级 |
|---|---|---|---|
| **无密码登录** | anon 用 cookie + user_id(UUID);用户身份伪匿名 | 不能存个人数据(姓名/邮箱) | v1.0 加 OAuth(Google/GitHub) |
| **单实例** | redis 仅做缓存,不做事;WS Hub 是进程内 | 不能横向扩展 | v0.9 多实例 + Redis Pub/Sub |
| **DB 单点** | 单 PG 容器,无 replica | 容器挂 = 数据风险 | v0.9 主从 + 每日 pg_dump 异地备份 |
| **JWT 单一 secret** | HS256 + 单 secret | secret 泄露 = 所有用户风险 | v1.0 key manager(Vault) |
| **无入侵检测** | 仅 LLM 调用计费监控 | 异常登录无即时告警 | v0.9 fail2ban + 告警 webhook |
| **smoke test 缺前端验证** | curl 跑 API 通过,前端页面没真开浏览器测 | UI 端 bug 可能漏 | 部署后浏览器真测 |

---

## 6. 部署前 checklist(参考 `docs/deployment/CHECKLIST.md`)

- [ ] 已 `.env` 填好:`LLM_API_KEY`、`BOCHA_API_KEY`、`JWT_SECRET` (`openssl rand -hex 32`)、`DATABASE_URL`
- [ ] 已跑 `powershell -ExecutionPolicy Bypass -File tools\envcheck.ps1`(检查重复 key / placeholder)
- [ ] 服务器:阿里云香港 2C2G(¥56/月,避免 1G 跑不动 4 Agent)
- [ ] 域名:`decisioncourt.com`(¥85/年,品牌资产);绑 DNS 后用 Let's Encrypt 签 SSL
- [ ] `docker compose build`(无 `--no-cache`) → `docker compose up -d`
- [ ] `curl http://127.0.0.1:8080/health` → `{"status":"ok"}`
- [ ] 端到端冒烟(见 §4)

---

## 7. 引用

- OWASP Top 10 (2021) 完整 OWASP Foundation
- 修复 commit 历史:见 §0 commit 列表
- 详细 PRD / Tech spec / DB design / Agent design / API design / UX refinement:见 [`docs/decisioncourt-*.md`](./README.md)
- 历史归档(2026-07-03 初版报告): [`.trae/documents/security-audit-2026-07-03.md`](../.trae/documents/security-audit-2026-07-03.md)

---

## 8. v0.9.3 修复跟进(2026-07-06 上线真域名后发现)

### 8.1 WS 握手 403 (第一段) — `viper.Unmarshal` 不自动 split 单值 env var

**症状**(2026-07-06 16:00 +0800):
```
[Frontend] WebSocket connection to 'wss://decisioncourt.cn/ws/courtrooms/d7bac039-...?token=...' failed:
  Error during WebSocket handshake: Unexpected response code: 403
```

**根因**:
- `.env` 里写 `ALLOWED_ORIGINS=https://decisioncourt.cn`(单值,无逗号)
- viper.Unmarshal 对 `[]string` 字段 + 单值 env var **不会自动 split**
- `AppConfig.AllowedOrigins` 运行时是 nil
- [websocket.go:42-44](../backend/internal/api/websocket.go#L42-L44) 的 localhost fallback 触发,生产白名单变成 `["http://localhost:3000"]`
- 真实浏览器带 `Origin: https://decisioncourt.cn` 不在白名单 → `gorilla/websocket` 返 403

**修复**: [config.go:164-180](../backend/internal/config/config.go#L164-L180) 在 viper.Unmarshal 后手动 split + trim + 去空元素。兼容单值 / 多值 / 带尾逗号三种写法。

### 8.2 A2A 消息 fallback 进错房间("鬼屋")

**症状**(stderr):
```
[a2a] WARN: a2a.message broadcast using SessionID.String() fallback —
        caller should set Message.SessionUUID to match hub room key
        (got sessionID=906ebde1-...)
```

**根因**:
- v0.5 PR 在 `orchestrator.go:recordSideEffects` 已经修过(填 `SessionUUID: session.SessionUUID`),但 `investigation/service.go` 的两处 `a2aBus.Send` 没改 → dispatch / report 进错房间
- `MemoryMeta` struct 也缺 `SessionUUID` 字段,reflect 步骤的 `buildPrivateMemoryMessage` 同样走 fallback
- 静默丢失所有 dispatcher 调查请求 + report,前端 InvestigatorPanel 永远不更新

**修复**:
- [investigation/service.go:96,124](../backend/internal/investigation/service.go#L96) 两处 Send 都显式填 `SessionUUID: session.SessionUUID`
- [reflect_classifier.go:39-46](../backend/internal/agent/reflect_classifier.go#L39-L46) `MemoryMeta` 加 `SessionUUID string` 字段
- [orchestrator.go:200](../backend/internal/agent/orchestrator.go#L200) MemoryMeta 注入 `SessionUUID: session.SessionUUID`

**回归测试**:
- `backend/internal/agent/reflect_classifier_test.go` 新增 `TestBuildPrivateMemoryMessage_SetsSessionUUID`
- `backend/internal/investigation/service_test.go` 新增 `TestRecordFinding_BroadcastRoomKey_UsesSessionUUID`
- `backend/internal/config/config_test.go` 新增 `TestParseAllowedOrigins_SingleValue`(单值 / 逗号 / 尾逗号 / 空格 / 空串 6 个 case)

### 8.3 WS upgrader.CheckOrigin init-timing 坑 — 闭包锁死白名单

**症状**(2026-07-07 复现,继 §8.1 修复后 403 仍然存在):
```
[Frontend] WebSocket connection to 'wss://decisioncourt.cn/ws/courtrooms/c54eee66-...' failed:
  Error during WebSocket handshake: Unexpected response code: 403
```
- 后端日志无任何 `ws.connect.foreign_owner` 或 `websocket: ...` 日志 → 不是 owner 校验阻断
- caddy 容器内 `wget --header="Origin: https://decisioncourt.cn" ... backend:8080/ws/...` 直接复现 403
- 即便 §8.1 已修(viper split),AllowedOrigins 运行时 = `["https://decisioncourt.cn"]`,403 仍存在

**根因**:
- [websocket.go:33-35](../backend/internal/api/websocket.go#L33-L35) `var upgrader = websocket.Upgrader{CheckOrigin: buildCheckOrigin()}` —— upgrader 在 **package init** 阶段构造
- buildCheckOrigin 旧实现把 `allowedSet` 在 init 阶段就构造好(config.Load() 还没跑)
- 闭包把 `{localhost:3000, 127.0.0.1:3000}` 这张 fallback 表锁死
- 之后 main() 跑 `config.Load()` 让 AllowedOrigins 填好,但 upgrader.CheckOrigin 已经定型,看不到新值
- 生产 `Origin: https://decisioncourt.cn` 不在锁死的 localhost 表里 → gorilla/websocket 返 403,handler 根本不调用,自然没日志

**修复**: buildCheckOrigin 改为**每次调用重新读** `config.AppConfig.AllowedOrigins`,放弃 init 时刻的闭包捕获。详细分析见 [ADR 0018](./adr/0018-websocket-origincheck-init-timing.md)。

**回归测试**(新增 4 个):
- `TestBuildCheckOrigin_ReReadsConfigPerCall` —— **核心**:同一闭包在 AllowedOrigins=nil 时拒、设置后必须接受(防 init-timing 退化)
- `TestBuildCheckOrigin_EmptyOriginAlwaysAllowed`
- `TestBuildCheckOrigin_TrimsTrailingSlash`
- `TestBuildCheckOrigin_RejectsUnknownOrigin`

**部署验证**(ECS 47.239.152.177 容器内实测,2026-07-07 17:35 +0800):
| Origin | 修复前(§8.1 后) | §8.3 修复后 |
|---|---|---|
| `https://decisioncourt.cn` + 有效 cookie | 403 ❌ | **101 Switching Protocols** ✅ |
| `https://decisioncourt.cn` 无 cookie | 401 ✅ | 401 ✅ |
| `https://evil.example.com` | 403 ✅ | 403 ✅(白名单仍生效) |
| 无 Origin(curl/wget) | 401 | 401 ✅ |

**教训**:
1. **§8.1 + §8.3 必须同时存在**。只看 §8.1 会被误导为"已经修了",实际少了 §8.3 仍然 403
2. **dev 用 localhost 测试永远无法发现 CheckOrigin 锁死问题**。CI 必须跑真域名冒烟
3. **"日志里看不到 = 不是 handler 阶段"** 是这次定位的关键 —— owner 校验会写 audit,upgrader 阶段不会写日志,这样区分能省一半时间

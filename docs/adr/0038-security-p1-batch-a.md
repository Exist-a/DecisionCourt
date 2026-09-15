# ADR 0038 — 安全 P1 批量修复（A 选项：P1-3 + P1-5 + P1-7）

| | |
|---|---|
| **状态** | ✅ Accepted |
| **日期** | 2026-09-15 |
| **作者** | Agent（用户 /goal 启动）|
| **关联版本** | v2.4（安全 P1 批量 A）|
| **取代** | — |
| **被取代** | — |

---

## 1. 背景

2026-07-03 安全审计列出 20 项 P0-P3，v0.8.3 + v0.10.18 已修 P0（6 项）+ D2 silent-error 后续修完 12 项。剩余 P1 7 项 deferred 至 `docs/todo/deferred-items-2026-08-05.md §D1`，无明确修复时间。

2026-09-15 用户授权 "尝试修复"，本 ADR 记录**A 选项**实施：低风险批量（3 项，约 1.5 天）。未涵盖 P1-2 CSRF / P1-4 sanitize / P1-6 依赖审计 — 留待下轮 PR。

调研结论（2026-09-15）：
- **P1-1** WS Origin 白名单：✅ v0.8.3 + v0.9.3 已完成（`buildCheckOrigin()` 每次重读 config，4 个回归测试）。**从 deferred 清单移除**。
- **P1-2** CSRF Token：未做（2 天，高风险，引入 gorilla/csrf 依赖 + 改前端 fetch wrapper）
- **P1-3** WS 输入校验：HTTP 端点已实装（gin binding max），WS 端点未实装（0.5 天，低风险）
- **P1-4** LLM prompt 注入 sanitize：未做（2 天，高风险，动 prompts.go 所有拼接点 + 可能影响输出格式）
- **P1-5** 日志脱敏 Detail：注释承诺 vs 代码未做（0.5 天，低风险）
- **P1-6** 依赖固定 + audit CI：CI 零 audit step（1 天，中风险，依赖 pin 一次性 review）
- **P1-7** dev/prod 硬开关：后端软开关 / 前端零硬化（0.5 天，低风险）

---

## 2. 决策

**A 选项**：本轮实施 P1-3 + P1-5 + P1-7（共 ~1.5 天工作量，3 项低风险），其余 P1 留待下轮。

---

## 3. P1-3：WebSocket 输入长度校验

### 现状

- `backend/internal/api/handler.go:534-536` HTTP `SubmitEvidence`：content `min=1, max=4096`（gin binding）
- `backend/internal/api/websocket.go:185-271` WS `user.action` handler：**无 content 长度校验**，无 `conn.SetReadLimit`

### 风险

恶意 client 通过 WS 发送 `{action: "submit_evidence", content: <10MB string}` —— gin binding 只在 HTTP 路径生效，WS 路径绕过 → 后端内存暴涨 / DB 写满。

### 修复

- `wsMaxMessageBytes = 64 * 1024`（64 KiB，足够 4KB content + JSON envelope）
- `wsMaxContentChars = 4096`（与 HTTP max 对齐）
- `wsMaxInterruptChars = 4096`（与 HTTP max 对齐）
- `conn.SetReadLimit(wsMaxMessageBytes)` 在升级后立即调用
- `submit_evidence` / `interrupt` handler 加 `len(content) > maxChars` 检查，超长时广播 `ClassUserInput + CodeActionFailed` UFE，前端 Toast 提示"内容超长（最大 4096 字符）"

### 单测

- `TestWSConstants_ReasonableValues`：验证 64KB / 4096 / 4096 数值
- `TestWS_PayloadLimitProducesUFE`：验证 UFE 文案正确
- `TestWS_InterruptLimitProducesUFE`：interrupt 同上

---

## 4. P1-5：UserFacingError.WithDetail prod 守卫

### 现状

- `backend/internal/courtroom/errors.go:84` 注释承诺 "Detail 字段仅 dev 模式填充;prod 留空"
- `errors.go:194-197` `WithDetail` 实现：**无条件赋值** —— 注释与代码脱节（"注释说一套做另一套"）
- 全仓 0 处探测 `APP_ENV` / `IsDevMode`

### 风险

prod 模式下 Go 内部字符串（gorm stack / 文件路径 / panic message）原样进 UFE JSON → 前端 → 公网用户。攻击者拿到内部错误后能 reverse-engineer 数据库 schema / 路由细节。

### 修复

- `backend/internal/config/config.go`：新增 `Config.AppEnv` 字段 + `IsDev() / IsProd() / IsProdLike() / ValidateAppEnv()` 方法
- `backend/internal/courtroom/errors.go`：`WithDetail` 加 prod 守卫

```go
func (e UserFacingError) WithDetail(detail string) UserFacingError {
    if config.AppConfig.IsDev() {
        e.Detail = detail
    }
    // prod|staging → 静默丢弃（不进 wire）
    return e
}
```

### 单测（4 个）

- `TestWithDetail_DevKeepsDetail`：dev 保留
- `TestWithDetail_ProdStripsDetail`：prod / staging / PROD 都清空
- `TestWithDetail_EmptySafe`：空 detail 无副作用
- `TestClassifyError_ProdStripsAllDetails`：5 个 err 分支（concurrency / react_max_iter / budget / state_machine / unknown）prod 模式下 Detail 全清空

---

## 5. P1-7：APP_ENV + prod 启动 fail-fast

### 现状

- `backend/cmd/server/main.go:248` 后端有 `GIN_MODE` 软开关（dev = debug / prod = release），但无 `APP_ENV` 概念
- 前端 `docker-compose.dev.yml:158` 有 `NODE_ENV: development`，prod compose 无 `NODE_ENV=production` 显式

### 风险

dev compose 的 fallback（`ALLOWED_ORIGINS` 含 localhost / `COOKIE_SECURE=false`）被误部署到公网：
- session cookie 不加密传输（中间人攻击）
- CORS 接受任意 localhost 反射（CSRF 类风险）

### 修复

- `backend/internal/config/config.go`：`Config.AppEnv string` + `envOrDefaultString("APP_ENV", "dev")`
- `backend/internal/config/config.go`：5 个方法（`IsDev` / `IsProd` / `IsStaging` / `IsProdLike` / `ValidateAppEnv`）
- `backend/cmd/server/main.go`：启动时 `config.ValidateAppEnv()` + `enforceProdInvariants()`

```go
func enforceProdInvariants() error {
    // 1. ALLOWED_ORIGINS 不含 dev-style host（localhost / 127.0.0.1）
    // 2. COOKIE_SECURE = true
    // 3. JWT_SECRET 长度 ≥ 32
    // 4. LLM_API_KEY 非空
}
```

触发：仅 `APP_ENV=prod|staging` 时跑 `enforceProdInvariants()`，dev 模式跳过（保持本地开发体验）。

### 单测（7 个）

- `TestEnforceProdInvariants_OK`：完整 prod 配置通过
- `TestEnforceProdInvariants_LocalhostOrigin`：localhost 拒绝
- `TestEnforceProdInvariants_LoopbackIP`：127.0.0.1 拒绝
- `TestEnforceProdInvariants_EmptyOrigins`：空 AllowedOrigins 拒绝
- `TestEnforceProdInvariants_CookieInsecure`：COOKIE_SECURE=false 拒绝
- `TestEnforceProdInvariants_ShortJWTSecret`：JWT_SECRET < 32 拒绝
- `TestEnforceProdInvariants_EmptyLLMKey`：LLM_API_KEY 空拒绝

---

## 6. 配置示例

`backend/.env` 增加（dev 默认值，无需设置）：

```bash
# 默认值 dev (本地开发模式 fall-back)
# APP_ENV=dev

# 生产部署 .env 必须显式设 prod，并配套：
# APP_ENV=prod
# ALLOWED_ORIGINS=https://yourdomain.com   # 不含 localhost / 127.0.0.1
# COOKIE_SECURE=true
# JWT_SECRET=<至少 32 字符的强 secret>
# LLM_API_KEY=sk-...
```

---

## 7. 不做的事（明确边界）

- ❌ **P1-2 CSRF Token** —— 引入 gorilla/csrf 依赖 + 改前端 fetch wrapper，留 v2.5 PR
- ❌ **P1-4 prompt sanitize** —— 动 prompts.go 所有拼接点 + 影响输出格式，留 v2.5 PR
- ❌ **P1-6 依赖固定 + audit CI** —— govulncheck / npm audit step，留 v2.5 PR
- ❌ **P1-1**（已 ✅ 完成，无需改）
- ❌ 不动 §2.1 裁决逻辑
- ❌ 不碰 backend/.env（§8 红线）
- ❌ 不做 prod compose 部署验证（仅本地 dev compose）

---

## 8. 验证

- `go test ./...` **24 包 100% PASS**（新增 14 个 P1 测试 sub-test）
- dev mode：保持本地开发体验（无 invariant 检查）
- prod mode：fail-fast 拦截 dev-style 配置

---

## 9. 文档同步

- `docs/V1-ROADMAP.md §0` 加 v2.4 进度 3 行（P1-3 / P1-5 / P1-7）
- `docs/release-notes/v2.4.md` 新增
- `docs/todo/deferred-items-2026-08-05.md §D1` 表格更新（P1-1 ✅ done by v0.8.3 + v0.9.3 / P1-3 ✅ done by v2.4 / P1-5 ✅ done by v2.4 / P1-7 ✅ done by v2.4；剩余 P1-2 / P1-4 / P1-6）

---

## 10. 关联文档

- `.trae/documents/security-audit-2026-07-03.md` — 完整审计报告
- ADR 0025 — v0.10.18 安全 P0 收尾
- ADR 0026 — viper BindEnv 安全 P0 副作用
- ADR 0024 — silent-error-fix PR 1
- `docs/todo/deferred-items-2026-08-05.md` — D1 deferred items 清单
- v2.4 release notes — 实施细节 + 时间线
- v2.3 release notes — 前置版本（agent_gateway observability）
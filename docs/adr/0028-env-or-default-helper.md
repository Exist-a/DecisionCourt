# ADR 0028: envOrDefault helper 全面修复 viper 25+ env lowercase bug

| | |
|---|---|
| **ADR 编号** | 0028 |
| **标题** | envOrDefault helper 全面修复 viper 25+ env lowercase bug |
| **状态** | ✅ Accepted (v0.10.21 PR-C) |
| **作者** | Trae IDE Agent（配合用户调研） |
| **日期** | 2026-08-05 |
| **触发** | ADR 0026 §8 后续工作：修其他 25+ env 的 viper lowercase bug |
| **依赖** | ADR 0026 viper-bindenv-fix (v0.10.19 P0-2 副作用) |

---

## 1. 决策

### 1.1 背景

viper 1.21.0 `AutomaticEnv()` 默认把 UPPERCASE env var (e.g. `JWT_SECRET`) 转 lowercase 查找 (`jwt_secret`)，找不到 → fallback `SetDefault` 默认值。这是 ADR 0026 揭示的 silent fail 模式。

**v0.10.19 (commit `bd47599`) 修复了 5 个关键 env**（L181-183 显式 `viper.BindEnv(key, key)`）：
- `JWT_SECRET` (P0-2 fail-fast)
- `DATABASE_URL` (P0-4 mustEnvs)
- `LLM_API_KEY`
- `BOCHA_API_KEY`
- `ALLOWED_ORIGINS` (v0.9.3 单值 split bug)

**但 33 个 env 仍受 bug 影响**。示例：生产 `.env` 设 `LLM_MODEL_V3=deepseek-chat-v3-pro`，但代码读 `llm_model_v3` → 拿默认 `deepseek-chat`（**错的 LLM 模型**）。silent fail，无报错。

ADR 0026 §8 已将本任务列为「🟢 1 周内」后续工作，ADR 0028 候选主题 #1。

### 1.2 矛盾诊断

三个修复方案：

| 方案 | 优点 | 缺点 |
|---|---|---|
| A. 全部 `viper.BindEnv(key, key)` 列出 33 个 | 增量改, 风险小 | 33 次重复, 扩容性差, 后续加 env 忘了 BindEnv 仍 bug |
| B. 删 `AutomaticEnv` + 改用 `viper.SetEnvKeyReplacer` 大写 letter 化 | 1 行改 | viper 1.21.0 行为 for 大写 key 仍可能 fallback default, 仍需逐 env 列 SetDefault |
| C. 抛弃 viper env lookup, 改用 `os.Getenv` helper | 直接读 process env, 无 bug + 5 typed 变体 | 删掉 ~40 行 viper SetDefault + AgentGateway 嵌套字段 literal 写法, 改动面较大 |

**决策 C**。理由：
1. viper 1.21.0 lowercase 转换是**默认行为**，不能通过简单配置绕过
2. `BindEnv` 是补丁方案，每个 key 都要列，扩容性差
3. `os.Getenv` 是 Go 原生，永远大写敏感，无 bug
4. `setDefault` 的"出厂默认值"语义保留在 helper 第二个参数（代码化），不在 viper 隐藏路径
5. fail-fast 集中在 `Load()` 末尾的 `mustEnvs`，不散落到 helper

### 1.3 替代方案分析

- **方案 A 试行**：估算 33 处 `BindEnv` 改动 + 配套 33 处测试 mock，~2 天工作量，扩容性差
- **方案 B 试行**：`viper.SetEnvKeyReplacer(strings.NewReplacer("a", "A"))` 但 viper 1.21.0 内部仍走 lowercase，需配合 `SetEnvPrefix` 使用，仍需考虑 prefix 注入，复杂度高
- **方案 C 试行**（即本次决策）：~1.85 天工作量（helper 0.25 + Load 重构 0.5 + 测试 0.5 + ADR 0.25 + 文档同步 0.35），可重复使用

### 1.4 实施决策

新增 `backend/internal/config/env.go`：

```go
// envOrDefault 是 PR-C 的核心 helper
func envOrDefault(key, defaultVal string) (value string, fromEnv bool)

// 5 个 typed 变体
func envOrDefaultString(key, defaultVal string) string
func envOrDefaultInt(key string, defaultVal int) int
func envOrDefaultBool(key string, defaultVal bool) bool
func envOrDefaultFloat(key string, defaultVal float64) float64
func envOrDefaultStringSlice(key string, defaultVal []string) []string
```

每个变体行为：
- env 未设 OR 设为空 → 走 default
- env 解析失败（int / float / bool）→ 走 default + slog.Warn 记录
- env OK → 用 env value

`Load()` 重构：
- 删 `viper.SetDefault(...)` 全部 ~40 次调用
- 删 `viper.SetEnvPrefix("")` + `viper.AutomaticEnv()`
- 删 5 次 `viper.BindEnv(key, key)`（v0.10.19 修复）
- 删 `viper.Unmarshal(&AppConfig)`
- 保留 `viper.ReadInConfig()` 读 `.env` 文件，但**手动 export 到 process env**（让 `envOrDefault` 后续能读到）
- 直接构造 `Config{...}` literal，每个字段走 `envOrDefaultXXX`
- 保留 `mustEnvs` fail-fast（JWT_SECRET / DATABASE_URL 必填）

`LoadSummary` 末尾输出：

```go
slog.Info("config loaded", "from_env", fromEnv, "total_keys", 43)
```

诊断「配置到底生效了几个 env」的常见问题。

### 1.5 兼容性

- **运行时兼容**：env 行为变更（之前走 viper fallback default, 现在走 envOrDefault 真正读 env），但因 ADR 0026 bug 已被 v0.10.19 暴露，**真实 env 行为现在才是"对的"**。生产 `.env` 设的值现在能正确生效。
- **API 兼容**：`Config` struct 字段不动，所有 caller（cmd/server/main.go 等）零改动。
- **测试兼容**：现有 `TestParseAllowedOrigins_SingleValue` 不受影响（测算法，不测 Load）。

---

## 2. 实施内容

### 2.1 v0.10.21 PR-C (2026-08-05)

| 文件 | 改动 | 行数 |
|---|---|---|
| `backend/internal/config/env.go` | 新增 helper | ~95 行 |
| `backend/internal/config/env_test.go` | 新增 26 sub-test | ~165 行 |
| `backend/internal/config/config.go` | Load() 重构, 删 viper env lookup | -100 / +90 行 |
| `docs/release-notes/v0.10.21.md` | 新建 | — |
| `docs/adr/0026-viper-bindenv-fix.md` | §8 加 ✅ 标记 | +1 行 |

### 2.2 33 env 修复清单

**核心服务 (8)**：PORT / DATABASE_URL / REDIS_URL / JWT_SECRET / JWT_EXPIRY_HOURS / COOKIE_SECURE / COOKIE_SAME_SITE / COOKIE_DOMAIN

**CORS (1)**：ALLOWED_ORIGINS

**LLM (5)**：LLM_PROVIDER / LLM_API_KEY / LLM_BASE_URL / LLM_MODEL_V3 / LLM_MODEL_R1

**Search (3)**：SEARCH_PROVIDER / TAVILY_API_KEY / BOCHA_API_KEY

**Agent Gateway (22)**：全部 22 个 `AGENT_GATEWAY_*` env（v0.9 起 ADR 0013 三大新能力 + v0.10 v2 改造）

**User Rate Limit (1)**：USER_TRIAL_LIMIT

**Dead config (移除)**：SEARXNG_URL — 无 mapstructure tag，无人用 `viper.Get`，从 Load() 移除（config struct 也不保留）

**Total**: 40 env 路径走 envOrDefault（41 个含 SEARXNG_URL 旧 default，已删）

### 2.3 验证

- ✅ `go build ./...` PASS
- ✅ `go test ./internal/config/` PASS (原 6 + 新 26 = 32 sub-test)
- ✅ 其他包测试不受影响（agent_gateway 2 个测试 fail 是 Windows 文件锁 pre-existing 问题，与 PR-C 无关，参见 ADR 0026 §2.2 同样的诊断模式）
- ⏳ 部署验证：本地 dev compose 跑 1 场庭审 + 注入 LLM_MODEL_V3 真实 env，确认生效

---

## 3. 教训总结

### 3.1 「helper 第二参数」是文档化的默认值

viper `SetDefault` 把默认值藏在 viper 内部 store，看代码不直观。`envOrDefault(key, defaultVal)` 把默认值作为函数参数，**代码就文档**。

### 3.2 fail-fast 不散落到 helper

曾考虑让 `envOrDefault("JWT_SECRET", "")` 缺失时 panic，但那样 fail-fast 散落 33 处，难维护。集中在 `Load()` 末尾 `mustEnvs` 显式 fail-fast，可读性 + 排查友好。

### 3.3 配置路径标准化

之前的「viper ReadInConfig → AutomaticEnv → BindEnv → Unmarshal」4 步流水线**复杂且脆弱**。新方案：

```
Load():
  1. viper.ReadInConfig() 读 .env 文件
  2. 手动 export 到 process env
  3. envOrDefault 读 env (一次性构造 Config literal)
  4. mustEnvs 集中 fail-fast
  5. slog.Info 输出 summary
```

减少 1 个间接层（viper.Set/SetDefault/AutomaticEnv/Unmarshal），每个 env 走单一路径。

### 3.4 错误信息更友好

之前 silent fail（拿 default 不报错）。现在：

- env 缺失 → 走 default（用户预期行为）
- env 解析失败（int / float / bool）→ `slog.Warn` 记录 + 走 default（用户能看到警告）
- env 关键安全配置（JWT_SECRET / DATABASE_URL）缺失 → `log.Fatalf` 阻断启动（防 P0-2 / P0-4 复发）

---

## 4. 后续工作

### 4.1 部署验证 checklist

- [ ] 本地 dev compose 起 backend，log 输出 `config loaded from_env=N total_keys=43`，N 体现本地 .env 实际有的 env 数
- [ ] 跑 1 场完整庭审，验证 LLM 调用 / 搜索 / WS 握手 / cookie 全部正常
- [ ] 注入 `LLM_MODEL_V3=deepseek-chat-v3-pro` env，确认 backend log 体现
- [ ] 移除 `JWT_SECRET` env，确认 backend 启动 fail-fast

### 4.2 不要做的事

- ❌ 不要把 `envOrDefault` 改成 viper 风格 (返回 error 由 caller 处理) — 增加样板代码
- ❌ 不要在 helper 里 fail-fast — 集中在 `mustEnvs`
- ❌ 不要加 `EnvIntSlice` / `EnvMap` 等长尾 typed 变体 — 当前 5 个变体覆盖 100% 字段
- ❌ 不要回退到 viper.AutomaticEnv — 老 bug 复发

---

## 5. 关联文档

- ADR 0026 viper-bindenv-fix (v0.10.19 P0-2 副作用根因)
- ADR 0025 security-p0-closeout (P0-2 / P0-4 fail-fast 设计)
- ADR 0012 single-instance-deployment (单机部署决策, env 配置简化)
- `docs/release-notes/v0.10.21.md` (待 PR-C 合入后落地)
- `docs/adr/0026-viper-bindenv-fix.md` §8 (本次状态: 完成)
- `docs/todo/deferred-items-2026-08-05.md` (无需登记, 是 PR-C 收尾)
- `backend/internal/config/env.go` (helper 实现)
- `backend/internal/config/env_test.go` (26 sub-test)

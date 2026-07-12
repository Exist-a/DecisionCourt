# Skill: viper-env-fix

> **触发场景**：Viper 配置加载报错（如 `FATAL: required config XXX is empty`）但容器内 env 实际有值

## 1. Root Cause

**Viper 1.21.0 `AutomaticEnv()` 对 UPPERCASE env var 有 lowercase 转换 bug**：

```go
viper.AutomaticEnv()
```

Viper 内部查 env var 时：
1. 接收 viper key（如 `"JWT_SECRET"`）
2. 转 lowercase → `"jwt_secret"`
3. 在 process env 查 `"jwt_secret"` → **不存在**（容器 env 是 `"JWT_SECRET"`）
4. 拿 `viper.SetDefault(...)` 默认值

**之前有 SetDefault** → viper 拿到默认值，看起来"工作"，bug 被掩盖。
**删除 SetDefault** → bug 暴露 → fail-fast 启动失败。

## 2. 4 个"读"渠道验证

| 渠道 | 命令 | 期望 |
|------|------|------|
| host .env | `grep '^JWT_SECRET=' .env` | 有值 |
| docker compose config | `docker compose config \| grep JWT_SECRET` | 有值 |
| 容器 env | `docker compose run --rm backend env \| grep JWT_SECRET` | 有值 |
| docker inspect | `docker inspect dc_backend --format '{{range .Config.Env}}{{println .}}{{end}}'` | 有值 |

**4 个都说"有"但 backend log 说"empty" = viper bug。**

## 3. 修复方案（BindEnv 显式绑定）

在 `config.go Load()` 加 5 行：

```go
viper.SetEnvPrefix("")
viper.AutomaticEnv()

// 显式 BindEnv 跳过 viper 内部 lowercase 转换
for _, key := range []string{
    "JWT_SECRET",
    "DATABASE_URL",
    "LLM_API_KEY",
    "BOCHA_API_KEY",
    "ALLOWED_ORIGINS",
} {
    _ = viper.BindEnv(key, key) // 显式绑定到同名 env var
}
```

**原理**：`viper.BindEnv(key, envName)` 第二个参数把 viper key 锁定到 env var 名，**绕过 lowercase 自动转换**。

## 4. 为什么只绑 5 个（不全绑）

| 类别 | 处理 |
|------|------|
| **关键 env**（fail-fast） | BindEnv ✅ 必绑 |
| **有 SetDefault 兜底** | 不绑，viper 拿默认值，不会 fail-fast 但可能配置不生效 |
| **可选 env**（API key 可为空） | 不绑 |

**已知遗留**：其他 25+ env（`PORT` / `COOKIE_SECURE` / `AGENT_GATEWAY_*`）仍受 viper lowercase bug 影响。SetDefault 提供兜底，**不会启动失败**但**可能用错配置**。

## 5. 完整修复（v0.11+ 候选）

重写 Load() 用 `envOrFatal()` helper 绕开 viper 全部自动绑定：

```go
func envOrFatal(key string) string {
    val := os.Getenv(key)
    if val == "" {
        log.Fatalf("FATAL: required env %s is empty", key)
    }
    return val
}

func Load() {
    // ... SetDefault ...
    AppConfig.JWTSecret = envOrFatal("JWT_SECRET")
    AppConfig.DatabaseURL = envOrFatal("DATABASE_URL")
    // ...
}
```

**优点**：所有 env 从 process env 真实读取，不依赖 SetDefault 兜底。
**缺点**：需要重写 Load() 函数体（约 50 行），工作量大。

## 6. 单测建议

```go
func TestLoad_FailFastOnMissingJWTSecret(t *testing.T) {
    viper.Reset()
    t.Setenv("JWT_SECRET", "") // 清空 env
    
    // Load() 会调 log.Fatalf → os.Exit(1)
    // 测试需 subprocess pattern 或重构 Load() 暴露纯算法函数
}
```

按 AGENTS.md "避免 over-engineering"，**本次不强制要求单测**（5 行 BindEnv + 注释已足够，未来 v0.11+ 重构时再加）。

## 7. 关联文档

- [ADR 0025 §2.1 P0-2 JWT_SECRET 默认值移除](../../docs/adr/0025-security-p0-closeout.md)
- [ADR 0026 §1.3 viper lowercase bug root cause](../../docs/adr/0026-viper-bindenv-fix.md)
- [config.go L161-184](../../../backend/internal/config/config.go)
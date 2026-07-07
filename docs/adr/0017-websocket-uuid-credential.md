# ADR 0017:WebSocket 鉴权改为"UUID 即凭证"

| | |
|---|---|
| 状态 | Accepted(2026-07-06) |
| 取代 | ADR 无前置(原逻辑在 [handler.go:183-211](../internal/api/handler.go#L183-L211) + [websocket.go:124-130](../internal/api/websocket.go#L124-L130)) |
| 影响范围 | `backend/internal/api/websocket.go` |

## 1. 背景

v0.8.3 上线时(P0-1 / P0-5 安全加固),为了防止"匿名 session 被别人连 WS",
WebSocket handler 也加了 `owner_id == viewer` 的硬校验:

```go
// 旧逻辑(2026-07-06 前)
var n int64
model.DB.Table("court_sessions").
    Where("session_uuid = ? AND owner_id = ?", sessionUUID, viewer).
    Count(&n)
if n == 0 { return 403 }
```

这条逻辑在生产环境直接踩坑(2026-07-06 09:47 上海时间):

```
dc_frontend  | WebSocket connection to 'wss://decisioncourt.cn/ws/courtrooms/c7691ea3-...?token=eyJ...' failed:
dc_frontend  |   Error during WebSocket handshake: Unexpected response code: 403
dc_backend   | state transition session_uuid=c7691ea3-... from=idle to=opening round=0
dc_backend   | [LLM] provider=deepseek ...  ← 庭审已经在跑
```

后台日志显示庭审正常推进(state transition + LLM 调用),但前端 WS
连续 403。

## 2. 决策

把 WebSocket 的 owner_id 校验改为"软校验":

| 场景 | 旧行为 | 新行为 |
|---|---|---|
| session 不存在 | 403(not the owner) | 404(庭审不存在) |
| session 存在,owner_id == ""(legacy) | 403 | ✅ 允许 |
| session 存在,owner_id == viewer | ✅ 允许 | ✅ 允许 |
| session 存在,owner_id != viewer | 403 | ⚠️ WARN + audit,**允许** |

新的鉴权链:

1. `?token=xxx` / Cookie → JWT 验签 → viewer_id(必须有,失败 401)
2. `session_uuid` 必须存在(404 if not)
3. owner_id 不匹配时 **WARN 日志 + 写 audit_log(action=ws.connect.foreign_owner)**,但允许连接

## 3. 理由

### 3.1 安全性不降低
- session UUID 本身是 122-bit 不可枚举凭证(URL 即权限)。
- 攻击者要连 WS,必须先拿到 session UUID(等同拿到 URL)。owner_id
  校验没有增加任何 attack surface,只是增加了合法用户的连接失败率。
- 当前所有 `hub.Broadcast` 推送都是"庭审对外可见"内容(opening speeches /
  cross-exam / public evidence),没有私有 payload 走 WS。

### 3.2 UX 痛点
- 用户清掉 localStorage(隐私模式 / 跨浏览器 / 调试清理)→ 新 anon_id
  → JWT user_id 变了 → 旧 session 的 owner_id 不再匹配 → 永久 403。
- 庭审已经在跑,用户却被锁在外面,只能新开一个 case 重来。

### 3.3 HTTP API 不变
- POST /evidences、POST /actions 等写操作仍要 owner 校验,防止
  "拿到 URL 的人替原用户提交证据 / 推进 phase"。
- WS 只读(单向收 broadcast),放宽影响最小。

## 4. 取舍

- ❌ 失去"不同用户之间隔离 session"的强保证。如果未来要做 B2B 多租户,
  需要再加一层"tenant_id"在 JWT 里,WS 也要校验 tenant_id。
- ⚠️ audit_log 必须开,这样能 grep 所有"跨 owner 连接"的会话,事后
  排查泄露风险。
- ✅ 兼容 legacy session(OwnerID = "")。

## 5. 实施细节

[backend/internal/api/websocket.go](../internal/api/websocket.go) 的改动:

1. 拆"session 存在性检查"(404)和"owner 鉴权"(软)两步
2. owner 不匹配时:`slog.Warn` + `model.AuditLog{Action: "ws.connect.foreign_owner"}`
3. JWT 校验失败时加 `slog.Debug` 输出原始 error(便于排查)
4. 引入 `gorm.ErrRecordNotFound` 区分"不存在"和"DB 错误"

构建 + 测试:

```bash
cd backend
go build ./...        # ✓
go test ./internal/api/...   # ok 7.7s
```

## 6. 后续观察

部署到 ECS 后,**必须**关注:

```bash
# ECS 上
sudo docker exec dc_postgres psql -U decisioncourt -d decisioncourt \
  -c "SELECT user_id, target, reason, created_at FROM audit_log WHERE action='ws.connect.foreign_owner' ORDER BY created_at DESC LIMIT 50"
```

- 如果同一 session 出现大量"不同 user_id 连入",说明 URL 真的被分享了
  或被爬 → 需要再收紧(回到硬校验 + 用 token 取代 owner_id)
- 如果都是 1-2 次(用户清缓存重连),说明决策正确 ✅

## 7. 相关文档

- ADR 0016-deployment-lessons-learned(本次踩坑属于"鉴权 UX 教训")
- ADR 0002-a2a-private-channel(private message 不走 hub,故放宽不影响 private)
- [backend/internal/api/handler.go](../internal/api/handler.go) 中 `checkSessionAccess`
  仍走硬校验,WS 与 HTTP 的鉴权差异需要写进技术规范。

---

## 8. 2026-07-07 复盘:本 ADR 解决了一半问题

**实际生产复现**(2026-07-07 16:00 +0800):即使 v0.9.2 修了 owner 软校验,WS 握手 403 **依然存在**,只是换了一个来源。

| 修复 | 解决 | 未解决 |
|---|---|---|
| 本 ADR(§2 软校验) | owner_id 不匹配的 403 | upgrader 阶段的 CheckOrigin 拒掉的 403 |
| [ADR 0018](./0018-websocket-origincheck-init-timing.md) | upgrader.CheckOrigin 在 init 阶段锁死 `allowedSet` 的 403 | — |

### 8.1 教训

1. **本 ADR 的"决策对比表"(§2)只覆盖 owner_id 这一层,没列 upgrader.CheckOrigin 这一层**。下次写类似鉴权 ADR 时,要把"前置中间件链"也列出来,逐层验证。
2. **dev 环境用 localhost 永远测不出 CheckOrigin 的问题**。CI 必须跑真域名冒烟测试。
3. **真域名回归是这次 fix 的真正触发条件**,跟 ADR 0016 §"白盒 SSH 运维"形成完整闭环:用白盒 SSH 登 ECS,在 caddy 容器内用 `wget --header="Origin: https://decisioncourt.cn" ...` 复现 403,才能确认是 upgrader 阶段而不是 owner 阶段。

### 8.2 完整修复路径

- **2026-07-06**:v0.9.2(本 ADR)owner 软校验 commit `056f508` 上线 → 旧 403 消失,新 403 出现
- **2026-07-07**:v0.9.3 第一部分 config split fix `2c876f0` 上线 → 仍然 403
- **2026-07-07**:**v0.9.3 第二部分 init-timing fix `e097d7c`(ADR 0018)上线 → 101 Switching Protocols ✅**

详见 [docs/observability/case-study-2026-07-07.md](../observability/case-study-2026-07-07.md)。
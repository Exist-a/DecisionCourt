# DecisionCourt 运维速查

> **目标**：本文档汇总"系统内部到底在做什么"的常用查询命令。
> 配套 [security-audit-v0.8.3.md](security-audit-v0.8.3.md) 看"做了什么防护"，
> 配套 [decisioncourt-tech-spec.md](decisioncourt-tech-spec.md) 看"架构为什么这么设计"。

---

## 0. 三种模式（先选这个）

| 场景 | 用什么 | 命令 |
|---|---|---|
| **改代码调试** | dev mode（带热重载） | `docker compose -f docker-compose.dev.yml up -d --build` |
| **部署上线** | prod mode（用预构建镜像） | `docker compose up -d` |
| **看运行状态** | 任一种 + 下面章节的命令 | |

dev 与 prod 可同时跑（容器名前缀不同 `dc_dev_*` vs `dc_*`，端口不冲突）。

---

## 1. 看实时日志（最常用）

### 1.1 命令行版（生产首选）

```bash
# 实时跟踪后端日志（最常用）
docker logs dc_backend --tail 200 -f

# 同时看多个服务
docker logs dc_backend -f &
docker logs dc_frontend -f &
docker logs dc_caddy -f &
wait

# 看最近 100 行就退出
docker logs dc_backend --tail 100

# 按时间过滤（v0.9.2 起，json-file 驱动默认轮转，单容器最多 30MB 历史）
docker logs dc_backend --since 5m   # 最近 5 分钟
docker logs dc_backend --since 2026-07-06T08:00:00

# 找错误（grep）
docker logs dc_backend 2>&1 | grep -i 'error\|panic' | tail -50
```

### 1.2 浏览器版（dev 推荐）

dev compose 自带 **Dozzle**（7MB 容器日志面板）：

```bash
# 起 dev 栈
docker compose -f docker-compose.dev.yml up -d

# 浏览器打开
open http://localhost:9999
```

能看到：
- 所有 `dc_dev_*` 容器实时日志流
- 搜索框（支持 regex）
- 多容器分屏（点屏幕右上角）
- 关键字高亮（点行号）

### 1.3 日志在哪存

每个容器 stdout → Docker daemon → `/var/lib/docker/containers/<id>/<id>-json.log`。
v0.9.2 起每个文件最大 10MB、最多保留 3 个文件 = 单容器最多 30MB 历史。

```bash
# 看实际磁盘占用
sudo du -sh /var/lib/docker/containers/*/
```

---

## 2. 看业务指标（metrics）

后端启动时初始化了一个内存中的 metrics registry，**`/metrics` 端点以 JSON 返回**。

```bash
# 本机直接查（需 SSH 进服务器，或通过 Caddy 反代；连接信息见 AGENTS.md §9）
curl -s http://localhost:8080/metrics | jq .

# 或通过域名
curl -s https://yourdomain.com/metrics | jq .

# 找特定指标
curl -s http://localhost:8080/metrics | jq '.counters | to_entries | .[] | select(.key | contains("llm"))'

# 看最近一次错误
curl -s http://localhost:8080/metrics | jq '.gauges.error_total'
```

**当前 metrics 包含**（[observability/metrics.go](../../backend/internal/observability/metrics.go)）：
- LLM 调用次数 / 延迟 / 失败率
- Cache 命中 / 未命中
- Circuit breaker 状态
- 用户 trial 计数（按用户限流用）
- A2A 消息总数

> **注意**：metrics 是**内存**的，重启清零。重要指标（如 trial 总数）已落 `audit_logs` 表。

---

## 3. 看决策事件 + 审计（decision_events / audit_logs）

业务级 span 和审计日志落 Postgres。用 docker exec 进 psql 查询：

```bash
# 一次性进去交互
docker exec -it dc_postgres psql -U decisioncourt -d decisioncourt

# 或单条命令
docker exec dc_postgres psql -U decisioncourt -d decisioncourt -c "SELECT COUNT(*) FROM decision_events;"
```

### 3.1 常用查询

```sql
-- 最近 20 条决策事件（庭审状态变化、Agent 发言等）
SELECT
  created_at,
  session_uuid,
  event_type,
  actor,
  payload->>'phase' AS phase,
  payload->>'role'  AS role
FROM decision_events
ORDER BY created_at DESC
LIMIT 20;

-- 某次庭审的完整事件链
SELECT
  created_at,
  event_type,
  actor,
  jsonb_pretty(payload) AS payload
FROM decision_events
WHERE session_uuid = '把-uuid-填这里'
ORDER BY created_at ASC;

-- 某用户的操作记录（审计）
SELECT
  created_at,
  user_id,
  action,
  status,
  ip,
  user_agent
FROM audit_logs
WHERE user_id = '把-userid-填这里'
ORDER BY created_at DESC
LIMIT 50;

-- 今天 trial 总数（用来算用户级 trial 限流还剩几次）
SELECT
  user_id,
  COUNT(*) AS trials_today
FROM decision_events
WHERE event_type = 'trial_start'
  AND created_at >= date_trunc('day', NOW())
GROUP BY user_id
ORDER BY trials_today DESC;

-- 最近 10 分钟错误数（健康度粗指标）
SELECT
  DATE_TRUNC('minute', created_at) AS min,
  COUNT(*) AS errs
FROM audit_logs
WHERE status = 'error'
  AND created_at >= NOW() - INTERVAL '10 minutes'
GROUP BY min
ORDER BY min;
```

### 3.2 表结构速记

| 表 | 存什么 | 关键字段 |
|---|---|---|
| `decision_events` | 庭审业务事件（状态机 / Agent 发言 / 证据） | `session_uuid`, `event_type`, `actor`, `payload` (jsonb) |
| `audit_logs` | 鉴权 / API 调用审计 | `user_id`, `action`, `status`, `ip` |
| `users` | 用户（含 anon_id） | `id`, `anon_id`, `email`, `created_at` |
| `sessions` | 庭审会话 | `uuid`, `user_id`, `status`, `case_title` |
| `messages` | Agent 消息 | `session_uuid`, `role`, `content`, `created_at` |
| `evidence` | 证据 | `session_uuid`, `kind`, `summary`, `source` |

完整 schema 见 [decisioncourt-db-design.md](decisioncourt-db-design.md)。

---

## 4. 看健康度 / 容器状态

```bash
# 容器状态（是否健康 / 重启了几次）
docker ps -a --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# 只看健康度
docker inspect dc_backend --format '{{.State.Health.Status}}'

# 资源占用
docker stats dc_backend dc_frontend dc_postgres dc_redis dc_caddy --no-stream

# 看磁盘空间（防止日志/DB 把磁盘吃满）
df -h /
```

---

## 5. trace_id 串联

v0.8 起所有请求带 trace_id，slog 日志里能看到。

```bash
# 拿一次庭审的 trace_id
TRACE_ID="abc123..."

# 拉这个 trace_id 的所有相关日志（前后端都覆盖）
docker logs dc_backend 2>&1 | grep "$TRACE_ID"
docker logs dc_frontend 2>&1 | grep "$TRACE_ID"
```

---

## 6. 常见故障速查

| 现象 | 看什么 | 命令 |
|---|---|---|
| 网页打开 502 | Caddy 日志 | `docker logs dc_caddy --tail 100` |
| WebSocket 连不上 | 前端 CSP / 后端 listen | `curl http://localhost:8080/health` + 浏览器 console |
| LLM 没回应 | LLM API key + metrics | `curl -s localhost:8080/metrics \| jq '.gauges \| with_entries(select(.key\|test("llm")))'` |
| trial 限流了 | 用户 trial 计数 | 见 §3.1 第 4 条 SQL |
| 磁盘满了 | 容器日志 + DB 大小 | `du -sh /var/lib/docker/containers/*/` + `docker exec dc_postgres psql ... -c "SELECT pg_database_size('decisioncourt');"` |
| 容器一直重启 | 健康检查失败 | `docker inspect dc_backend --format '{{json .State.Health}}'` |

---

## 7. 备份与恢复

```bash
# 备份 Postgres（一个文件）
docker exec dc_postgres pg_dump -U decisioncourt decisioncourt > backup-$(date +%F).sql

# 恢复
cat backup-2026-07-06.sql | docker exec -i dc_postgres psql -U decisioncourt -d decisioncourt
```

---

## 8. 数据可得性实测（2026-10-09，第二次上线当天）

> 起因：上线后问「现在有用户用，我们能不能拿到前端埋点数据和提示词优化数据」。
> 下面每条都是**在生产上实测**的结论，不是读代码推测。**动过权限 / 镜像 / compose 后请重跑本节命令** —— 结论会随环境变化。

### 8.1 前端埋点：✅ 通（写 + 读都实测过）

| 环节 | 证据 |
|---|---|
| 写入 | `POST /api/v1/courtrooms/:uuid/events` → `{"code":0,"data":{"recorded":true}}` HTTP 200 |
| 落库 | `decision_events` 表；前端事件 `event_type` 以 `fe.` 开头（后端 span 是 `span.` / `state_`），同一个字段可一起查 |
| 读取 | `GET /api/v1/courtrooms/:uuid/events`（v2.11 D12 补的读端点，支持 `limit`/`offset` + `event_type_prefix`） |
| 实测 | 建会话 → 写一条 `fe.deploy_verify` → 读回 payload 完整（`{"source":"curl-check"}`、`duration_ms=42`） |

三个前提，缺一条就看不到数据：

1. **必须有真实庭审** —— 事件按 session 归属，`checkSessionAccess` 是 **owner-only**（防他人灌垃圾事件）。拿假 UUID 会 404。
2. **CSRF 必须过** —— v2.13 之前前端漏发 `X-XSRF-TOKEN`，`fe.*` 全部 403（静默丢了三周）。手工 curl 复现时的编码坑见 §8.5。
3. **前端得真的跑** —— `fe.*` 由浏览器 `track()` 发出，纯后端调用不产生。

### 8.2 LLM 审计 `llm_calls`：✅ 通

`g.recorder.Record(...)` 在 `gateway.Chat` 里是**无条件调用**（不受 `AGENT_GATEWAY_ENABLED` 总开关影响），recorder 自身的 `Enabled` 只看 `llmClient != nil`。字段：`model` / `prompt_tokens` / `completion_tokens` / `cost_usd|cny` / `latency_ms` / `agent_type` / `request_id` / `status`。

### 8.3 FileLogger 详细日志：❌ 当前写不进去（连带 trace 端点为空）

`AGENT_GATEWAY_FILE_LOGGER=true`、`AGENT_GATEWAY_LOG_DIR=logs`，但**容器里 `/app/logs` 不可写**：

```bash
docker compose exec -T backend sh -c 'touch /app/logs/.wtest'
# → touch: /app/logs/.wtest: Permission denied
```

根因是**两个问题叠加**：

| 层 | 现状 | 问题 |
|---|---|---|
| compose | `user: 1001:1001` | — |
| Dockerfile | `adduser -u 10001 -S appuser` | **uid 与 compose 不一致** → 容器实际跑在 uid 1001，而镜像里根本没有这个用户（`id` 直接报错） |
| 宿主机 | `logs/` 与 `logs/backend/` 属主 `root:root` 0755 | uid 1001 无写权限 |

**丢的是什么**：FileLogger 的 JSON 里有 **`system_prompt` + `input_messages` + `output_content`**（按 `FileLoggerPromptsMaxBytes` 截断），外加压缩 / 节流 / 预算 / 重试明细 —— 也就是改 prompt 最需要的「喂进去什么、出来什么」。

**连带影响**：`GET /api/v1/courtrooms/:uuid/traces/*` 读的就是同一个文件（`logs/agent_gateway_YYYY-MM-DD.log`，见 `trace/store.go:logFilePath`），所以 trace 端点必然是空的。

**不是完全静默**：写失败会打 `slog.Warn("agent_gateway: fileLogger.Write failed")`（v0.10.21 PR-A 加的）。**排查入口就是这条 WARN** —— 看到它先查本节权限。

修法（二选一，需授权）：

```bash
# 快速修：让现有容器能写
sudo chown -R 1001:1001 /opt/DecisionCourt/logs

# 推荐：统一 uid（compose 的 user 改成 10001，与 Dockerfile 的 appuser 对齐）后
sudo chown -R 10001:10001 /opt/DecisionCourt/logs
```

验证：`docker compose exec -T backend sh -c 'touch /app/logs/.wtest && echo OK'` 打印 OK；然后跑一次真实庭审，看 `logs/backend/agent_gateway_<日期>.log` 是否出现。

### 8.4 提示词版本归因：❌ 缺口（设计层，非环境问题）

**没有任何表记录「这次发言用的是哪版 prompt」**：`llm_calls` 没有 prompt 版本列，`messages` / `a2a_messages` 也没有。`GET /api/v1/prompts/version` 返回 `semver` + `loaded_at`（实测 `git_sha` 为空），而 semver 要手工改 YAML 才变。

后果：改完 `prompts/base.yaml` 之后**无法按版本归因做前后对比**，只能靠时间戳 + `loaded_at` 手工对齐。

建议修法：给 `llm_calls` 加 `prompt_version` 列，recorder 写入时带上当前 `promptlab` 版本 —— 这样 Prompt Lab 的 A/B 才有数据基础。

### 8.5 手工验证 CSRF 保护端点（curl）的编码坑

token 里含 `%3D%3D`（URL 编码的 `==`）。服务端对 **cookie 值做 URL 解码**、对 `X-XSRF-TOKEN` **按原样比较**：

```bash
RAW=$(awk '$6=="XSRF-TOKEN"{print $7}' /tmp/cj.txt | tail -1)   # cookie 用原值（含 %3D%3D）
DEC=$(printf '%s' "$RAW" | sed 's/%3D/=/g')                     # header 用解码值（==）
curl -X POST "$B/prompts/eval" -b "dc_session=$SESS; XSRF-TOKEN=$RAW" -H "X-XSRF-TOKEN: $DEC" ...
```

不这么做恒 `CSRF_TOKEN_MISMATCH`（连试 4 次才定位）。浏览器端天然正确 —— `readCookie` 走 `decodeURIComponent`。

---

## 9. 不在本文档范围

- **Prometheus + Grafana**：重，MVP 不需要。需要再说。
- **ELK / Loki**：同上加。
- **Dozzle for prod**：dev compose 自带，prod 暂不集成（用 `docker logs` + Dozzle on dev 模式查看更安全）。

---

## 10. 改完之后

任何"看不到"的痛点 → 先看 §1（logs）→ 再看 §3（DB）→ 还不够再看 §8（数据可得性）→ 再说。
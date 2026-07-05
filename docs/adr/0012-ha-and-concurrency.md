# ADR 0012: v0.9 单机部署(含公网)的高可用与并发防护

> **状态**:✅ Accepted (2026-07-04)
> **决策日期**:2026-07-04
> **影响范围**:`internal/courtroom/service.go` · `internal/api/handler.go` · `internal/api/websocket.go` · `internal/agent_gateway/gateway.go` · `internal/agent/react_runner.go` · `cmd/server/main.go` · `frontend/lib/api.ts` · `frontend/lib/websocket.ts`

## 背景

DecisionCourt v0.8.3 已完成 20 项安全加固 + 4 项 smoke bug,达到"内网 + 受控公网"基线。但 `docs/README.md §6` 明确指出的下一个议题 —— **后端白盒化 / 高可用 / 并发防护** —— 仍待解决。当前最关键的两类生产隐患:

1. **并发崩溃**:
   - `sessionLocks` 已写但仅覆盖 4 个路径;`SubmitEvidence` / `ProcessUserAction.continue_cross_exam` / `dispatch_investigator` 三处未加锁,容易撞车
   - HTTP/WS 层无幂等;网络重试或 F5 狂点 → 重复 evidence / 重复 round 触发
   - LLM 调用无 per-call timeout;hang 30 分钟不会自退出,只能等用户手取消
   - `runCrossExamRound` 内部单步 LLM 失败 → 整轮失败,phase 不迁移 → 用户卡死

2. **故障恢复缺位**:
   - backend 重启后无"复活卡住 trial"机制;sessions 停在 `cross_exam` 中间态,内存里无人在跑
   - 长期不动的庭审没有 watchdog,只能等用户主动点"下一轮"

**目标定位**(已与用户确认,2026-07-04):**单机部署(含公网)**。不引入 Redis Pub/Sub / 多实例 / K8s,仅针对单台 ECS / 内网 demo 场景。

部署形态涵盖:

- **本地内网开发**:单机 docker-compose,主要用于功能验证
- **阿里云单 ECS 公网部署**(后续规划):1 台 ECS(2C2G,¥56/月)+ 公网域名 + SSL

两种形态在架构层面都属"单机",所以 ADR 0012 的所有方案(`sync.Mutex` / `sync.Map` / 启动扫描 / per-call timeout)**通用**。但阿里云公网部署有几个**额外考量**:

- 启动恢复触发更频繁(运维重启 / OOM / 镜像升级)→ PR5 必须
- 阿里云 → DeepSeek 跨网延迟更高 → PR3 timeout 应从 60s 提到 **90s**(详见决策 3 修订)
- 资源更受限(2C2G)→ PR5 限并发 ≤5 必须严格生效
- 公网攻击面已由 v0.8.3 的 20 项加固覆盖,本 ADR 不再重复

改动量小(约 150 行代码 + 9 项测试),能装进当前单实例 docker-compose。

**白盒化相关工作**(Phase A 数据采集 / Phase B 增量埋点)留给独立子项目,在本 ADR 之外推进。

## 选项对比

5 个子项独立讨论、各自决策。详见下方"决策明细"。

## 决策明细

### 决策 1:session 互斥采用"局部补锁"

**背景**:`sessionLocks map[string]*sync.Mutex` 已在 `service.go` 写好。已覆盖 4 个路径(`direct_verdict` / `reopenTrial` / `resumeCrossExam` / `runCrossExamRound`),但缺 5 个:
- `SubmitEvidence`
- `ProcessUserAction.continue_cross_exam`
- `ProcessUserAction.start_cross_exam`
- `ProcessUserAction.skip_agent`(goroutine 启动 + transitionPhase 之间有 race)
- `ProcessUserAction.dispatch_investigator`

| 维度     | A. 局部补锁         | B. handler 层统一锁            | C. `sync.Map` 重构 |
| -------- | ------------------- | ------------------------------ | ------------------ |
| 改动量   | ~30 行 + 5 测试     | ~80 行 + 1 测试                | ~40 行 + 1 测试    |
| 漏锁风险 | 靠 code review 防漏 | 集中管理不会漏                 | 与 A 等价          |
| 锁粒度   | 细(按 session)      | 粗(同一 session 所有 API 排队) | 同 A               |
| 性能     | 优                  | 同 session 请求排队            | 同 A               |

**决策**:方案 A —— 局部补锁到 5 个剩余热路径。理由:
- 改动最小,沿用现有 `getSessionLock()` 设计
- 锁粒度细,不影响其他 session 的并发
- 后续 ADR 列出"必须加锁的路径清单",code review 时核对

**2026-07-04 实施发现**:原 ADR 写作时只列出 3 个热路径,实施 PR1 时发现 `start_cross_exam` 和 `skip_agent` 同样有 `transitionPhase` 之前的 race(详见代码注释),需要一并加锁。已扩展到 5 个路径。

**2026-07-04 实施发现 #2**:第一次实现 `ProcessUserAction.continue_cross_exam / start_cross_exam` 时用了 `defer lock.Unlock()` 包住整个分支,导致外层持锁时 `runCrossExamRound` 内部又 `lock.Lock()` 同一把锁 → 死锁(GO 测试 panic timeout)。修复方式:**锁范围缩到只覆盖 `transitionPhase` 临界区(round 自增判断 + 写 DB),调 `runCrossExamRound` 前必须 `lock.Unlock()`**,让 `runCrossExamRound` 自己重新加锁。同样的原则适用于所有"调 `runCrossExamRound` 之前"的路径(包括未来 `resumeCrossExam` 也存在的同类隐患,留作后续单独修复)。

### 决策 2:用户点击幂等采用"客户端 idempotency_key"

**背景**:状态机层已有 start / finish / round 内同 agent 重复发言的幂等。HTTP/WS 层未做,网络重试 / F5 狂点会重复写入。

| 维度       | A. idempotency_key | B. action + 时间窗 | C. DB 唯一约束     | D. 现状不补 |
| ---------- | ------------------ | ------------------ | ------------------ | ----------- |
| 挡网络重试 | ✅                 | ❌(只挡 <500ms)    | ✅                 | ❌          |
| 改动量     | 2 文件 + 1 测试    | 1 函数             | schema 改动        | 0           |
| 复杂度     | 中                 | 低                 | 中(改 schema)      | 0           |
| 误伤风险   | 无                 | 无                 | 用户输入错也算重复 | —           |

**决策**:方案 A —— 客户端生成 UUID v4,通过 HTTP header `Idempotency-Key` 携带;后端内存 `sync.Map[string]Result` 记录 24h,重复请求直接返回首次结果。理由:

- 业界标准(Stripe 模式)
- 内存 map 足够(单机 demo);后续上 Redis Pub/Sub 时可直接换 backend
- 不改 schema
- 关键 API:`POST /api/v1/courtrooms/:uuid/evidences` + `POST /api/v1/courtrooms/:uuid/actions`

### 决策 3:LLM 超时采用"per-call timeout"

**背景**:`retryer.go` 已实装,默认 `500ms / 1s / 2s` 退避,3 次重试;但 **每次调用本身无 timeout**,LLM hang 不会自退出。

| 维度      | A. per-call timeout | B. 错误类型分级                        | C. 仅监控 |
| --------- | ------------------- | -------------------------------------- | --------- |
| 解决 hang | ✅                  | ❌                                     | ❌        |
| 改动量    | 1 函数 + 1 测试     | 需 LLM client 返回结构化错误(2-3 文件) | 1 文件    |
| 账单优化  | 弱                  | 强                                     | —         |

**决策**:方案 A —— 每次 `Gateway.Complete` / `StreamComplete` 入口套 `context.WithTimeout(parent, 90s)`;超时后 Retryer 拿到 `DeadlineExceeded` 走 fallback。理由:

- 解决用户最痛的"卡 30 分钟"问题
- 改动最小
- **2026-07-04 修订**:timeout 从 60s 调到 **90s** —— 阿里云 ECS → DeepSeek 跨网调用实测延迟 P50 ≈ 8s、P95 ≈ 25s,留出 R1 推理模型 + 长 prompt 的余量。本地内网 demo 可保持 60s,但统一设 90s 便于一个 .env 配置
- B 留给后续"账单优化"专项,等日均 LLM 调用 > 100 再启动(见 [whitebox-roadmap §Phase C](../roadmap/whitebox-roadmap.md))

**2026-07-04 进一步修订**:PR3 (LLM per-call timeout) 已迁出本 ADR,集中到 [ADR 0013 决策 1](./0013-llm-gateway-engineering.md)。本 ADR 现在只覆盖 4 个子项(session 互斥 + 启动恢复 + 错误兜底 + RateLimit),**LLM Gateway 三个能力(timeout + cache + breaker)在 ADR 0013 统一规划**。

**可调参数**:`.env` 加 `AGENT_GATEWAY_LLM_TIMEOUT_SEC=90`,默认值 90。本地部署可调成 60。

### 决策 4:agent 死锁采用"轮内错误兜底"

**背景**:ReAct runner 已有 `MaxIterations=4` / `MaxReflects=3` / `Timeout=30s` 三层兜底。但 `runCrossExamRound` 内单步 LLM 失败 → runner 返回 error → `runCrossExamRound` 返回 error → phase 不迁移 → 用户卡死。

| 维度     | A. 轮内错误兜底         | B. fallback Speaker     | C. 现状不补 |
| -------- | ----------------------- | ----------------------- | ----------- |
| 避免卡死 | ✅                      | ❌(N 轮空辩论)          | ❌          |
| 庭审质量 | verdict 写明 LLM 不可用 | 庭审能走完但 N 轮空辩论 | 不可控      |
| 改动量   | 1 函数 + 1 测试         | runner 改 + 1 测试      | 0           |

**决策**:方案 A —— `runCrossExamRound` 顶部加 error handler:任意 LLM 调用失败 → 立即调 `finishTrial` + 广播 `trial.error` 事件 + verdict 标注"LLM 暂时不可用,基于历史信念直接裁决"。理由:

- 用户体验确定:不会卡死
- 庭审仍能产出 verdict,虽然降级
- B 会让庭审质量"看似能走完实际空转",反而更糟糕

### 决策 5:异常恢复采用"启动扫描 + 限并发重跑"

**背景**:backend 重启后,sessions 停在 `cross_exam` / `opening` / `closing` / `deliberation` 中间态,内存里无人在跑。用户只能点"下一轮"主动恢复。

| 维度     | A. 启动扫描                    | B. 前端 healthcheck | C. watchdog |
| -------- | ------------------------------ | ------------------- | ----------- |
| 透明恢复 | ✅                             | ❌                  | 部分        |
| 改后端   | 1-2 函数                       | 0                   | 1 函数      |
| 资源风险 | 同时重启 100 个 trial 资源耗尽 | 无                  | 无          |
| 防忘关   | ❌                             | ❌                  | ✅          |

**决策**:方案 A —— backend 启动时扫 `status=active` 且 `phase ∈ {opening, cross_exam, closing, deliberation}` 的 session,放入恢复队列,**限并发 ≤ 5** 重跑挂起工作流。理由:

- 透明恢复,用户不感知
- 限并发避免资源耗尽(阿里云 2C2G ECS 必备)
- C "防忘关"留给后续 PRD(目前单用户 demo 场景下,可由用户自己点"直接判决"兜底)

**2026-07-04 阿里云部署额外要求**:为便于公网部署后排查,recovery 模块必须埋 3 个 metric:

- `recovery_count_total`(counter)—— 本次启动恢复的 session 数
- `recovery_duration_seconds`(histogram)—— 单个 session 恢复耗时
- `recovery_failed_total`(counter)—— 恢复失败的 session 数(单独告警)

这些 metric 通过现有 `observability.Metrics` 接口埋,Phase C Prometheus exporter 接入后可写 dashboard。

## 整体方案汇总

| #        | 子项                 | 改动文件                                                          | 改动量                    | 测试数       |
| -------- | -------------------- | ----------------------------------------------------------------- | ------------------------- | ------------ |
| 1        | session 互斥补锁     | `internal/courtroom/service.go`                                   | ~30 行                    | 3            |
| 2        | idempotency_key      | `internal/api/handler.go` + `frontend/lib/api.ts` + 新 middleware | ~50 行 + 20 行            | 1 + 1        |
| 3 → ADR 0013-1 | LLM per-call timeout | `internal/agent_gateway/gateway.go`（迁移到 ADR 0013） | ~10 行(并入 0013) | (并入 0013) |
| 4        | 轮内错误兜底         | `internal/courtroom/service.go` (`runCrossExamRound`)             | ~25 行                    | 1            |
| 5        | 启动扫描             | 新增 `internal/courtroom/recovery.go` + `cmd/server/main.go` 装载 | ~80 行 + 3 个 metric 指标 | 2            |
| **合计** |                      |                                                                   | **~280 行**               | **14 项测试** + ADR 0013 12 项 |

## 实施拆分(5 个 PR)

按子项独立 PR,便于 revert 与 code review:

```
PR1 fix(courtroom): session 互斥补锁到 3 个剩余热路径
PR2 feat(api): Idempotency-Key 中间件 + 前端 header 注入
PR3 fix(gateway): LLM 调用 per-call timeout(90s,可配)
PR4 fix(courtroom): runCrossExamRound 轮内错误兜底 → finishTrial
PR5 feat(courtroom): backend 启动扫描恢复卡住的 trial(限并发 ≤5)
```

## 后果

### 收益

- ✅ **避免并发崩溃**:同 session 并发请求串行化,不再撞车
- ✅ **避免网络重试副作用**:客户端 idempotency_key 兜住所有"重发"场景
- ✅ **避免 LLM hang**:90s 超时自动降级,账单可预测(本地可调 60s)
- ✅ **避免庭审卡死**:LLM 连续失败立即进 verdict,用户体验确定
- ✅ **backend 重启自愈**:透明恢复,用户不感知

### 代价

- ⚠️ **每条新 handler 必须遵守"加锁 + 幂等"清单**:在 ADR 中列出,code review 时核对。漏一处就漏一处防护。
- ⚠️ **idempotency 内存 map 有内存压力**:单机 24h 100 个 session × 每个 5KB ≈ 500KB,可忽略;后续接 Redis 时换 backend 即可
- ⚠️ **启动恢复增加启动时间**:100 个 active session × 限并发 5 ≈ 20 轮恢复,可能慢 30-60s。可接受,因为仅启动时执行一次
- ⚠️ **未覆盖横向扩展**:仍是单机;多实例 + Redis Pub/Sub 留给下一轮(见 docs/roadmap/whitebox-roadmap.md §Phase D 之后)

### 仍存的限制

| 限制                 | 影响                      | 何时升级               |
| -------------------- | ------------------------- | ---------------------- |
| 单实例 backend       | 进程挂 = 庭审停(虽然自愈) | 多实例 + Redis Pub/Sub |
| 内存 idempotency map | backend 重启 = 全部清空   | Redis 持久化           |
| 启动恢复无优先级     | 所有 session 一视同仁恢复 | 长尾用户场景再优化     |

## 测试计划

每个 PR 必须包含:

- 单元测试:`go test ./internal/courtroom/... -run TestSessionLock -v`(锁 + 并发场景)
- 集成测试:`go test ./internal/api/... -run TestIdempotency -v`(重复请求)
- 启动恢复测试:`go test ./internal/courtroom/... -run TestRecovery -v`(模拟重启)

总计预计 9 项新增测试,与现有 167+ 项并存,**目标 176+ 项全部通过**。

## 关联

- 主文档:[`../decisioncourt-tech-spec.md`](../decisioncourt-tech-spec.md)(高可用与并发防护章节)
- 主文档索引:[`../README.md`](../README.md)(状态矩阵更新)
- Roadmap:[`../decisioncourt-roadmap.md`](../decisioncourt-roadmap.md) §6 议题清单
- 白盒化路线:[`../roadmap/whitebox-roadmap.md`](../roadmap/whitebox-roadmap.md)(未来配合)
- 代码(实施后):
  - `internal/courtroom/service.go`(决策 1 + 决策 4)
  - `internal/api/handler.go` + `internal/api/middleware.go`(决策 2,新增 idempotency middleware)
  - `internal/agent_gateway/gateway.go`(决策 3)
  - `internal/courtroom/recovery.go`(决策 5,新增)
  - `cmd/server/main.go`(决策 5 装载)
  - `frontend/lib/api.ts` + `frontend/lib/websocket.ts`(决策 2 前端)

## 复审

下次复审时间:**2026-08-04**(实施完成时)。复审问题:

- 9 项测试是否全过?
- 启动恢复实际跑 5 场真实庭审是否稳定?
- idempotency_key 是否在前端所有写操作都用上了?
- 是否有漏锁的 handler?(跑 grep `s.Service.` 检查所有 service 调用是否在锁内)

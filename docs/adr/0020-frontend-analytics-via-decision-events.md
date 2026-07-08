# ADR 0020:前端埋点复用 v0.8 decision_events 基础设施

| | |
|---|---|
| 状态 | Accepted(2026-07-08) |
| 配套 | [ADR 0010](./0010-whitebox-observability.md)(v0.8 三支柱 metrics / decision_events / slog)· [ADR 0017](./0017-websocket-uuid-credential.md)(auth 模式)· [whitebox-roadmap.md Phase B](../roadmap/whitebox-roadmap.md) |
| 影响范围 | 后端：`backend/internal/api/handler_events.go`(新增)· `handler.go`(WithEventRecorder + 路由)· `cmd/server/main.go`(装配) <br> 前端：`frontend/lib/transport.ts`(新增)· `frontend/lib/analytics/index.ts`(新增)+ 配套 `.test.ts` |

## 1. 背景

后端 v0.8 已建立完整的"业务事件可观测性"三支柱(ADR 0010)：

- **metrics**：内存版 Prometheus-like 计数器/直方图,JSON 暴露
- **decision_events**：GORM 落库的结构化事件表
- **slog**：结构化日志

但**前端完全是黑盒** —— 用户行为漏斗、各阶段停留时长、WebSocket 重连次数、流式 chunk 间隔等数据**只有前端能看到**,后端 metrics 无法覆盖。

### 1.1 想要回答的问题

| 问题 | 现有数据源 | 缺失 |
|---|---|---|
| 立案→启动庭审转化率 | 无 | 前端"启动庭审按钮点击" |
| 用户在 cross_exam 阶段平均停留多久 | 后端 phase.changed 时间戳 | **用户在每个阶段的实际停留**(后端只知道切到下一阶段的时刻) |
| 用户提交了几条证据 / 何时直接判决 | 后端 messages / evidences | 用户参与度(直接判决 = 用户主动跳过) |
| WebSocket 重连影响多少用户 | 后端无 | 前端 reconnect_attempt 计数 + 平均重连间隔 |
| 流式发言首字延迟 | 后端 metrics 有 LLM 调用耗时 | **从 chunk 到 chunk 的间隔 / 首字延迟**只有前端能看 |
| 判决反馈有用率 | 后端无 | 用户 thumbs up/down 比例 |

### 1.2 v0.8 白盒化的延伸

[whitebox-roadmap.md](../roadmap/whitebox-roadmap.md) Phase B 计划是**后端增量埋点**,本 ADR 是**前端配套** —— 把前端可见的用户行为补到同一张 `decision_events` 表,复用现有指标 / 审计 / dashboard 三件套。

## 2. 决策

### 决策 #1:数据去向后端 `decision_events`(不复用新表 / 第三方 SaaS)

**选定方案**：前端 `POST /api/v1/courtrooms/:session_uuid/events`,handler 直接调 `GormEventRecorder.Record()`。

| 方案 | 优点 | 缺点 |
|---|---|---|
| **A. 打到现有 decision_events(选定)** | 零新依赖、复用 metrics / audit / 后续 Prometheus 接入 | 污染业务表(需要 `EventType` 加 `fe.` 前缀区分) |
| B. 新表 `frontend_events` | 干净分离 | 重复 schema、破坏 v0.8"三支柱"叙事 |
| C. PostHog / Sentry SaaS | 自带漏斗 / session replay | 数据出境、隐私合规、依赖外部 |

**理由**：v0.8 三支柱的核心价值是**统一面板**。前端事件如果走第三方,会形成"后端用决策事件 / 前端用 SaaS"的双面板运维割裂,白盒化的可观测性承诺打折。

### 决策 #2:埋点失败绝不阻塞前端(best-effort)

- recorder nil(生产装配失败兼容)→ 仍返 200,不写库
- recorder.Record 返错(DB down / 超时)→ 仍返 200,仅 slog 警告
- 前端 `flush()` 抛错 → 静默 console.warn,不冒泡

**理由**：埋点是 observability,不是 critical path。用户点"启动庭审"按钮后,埋点上报失败不应该阻断后续 WebSocket 连接 / 庭审启动。

### 决策 #3:前端事件类型用 `fe.<name>` 前缀

后端业务级 span 用 `span.<name>`、状态机用 `state_transition`、收敛用 `convergence_triggered`。前端新增 `fe.*` 命名空间:

- `fe.trial_started` / `fe.phase_entered` / `fe.evidence_submitted` / `fe.verdict_feedback` / ...
- 任何后端看到 `EventType LIKE 'fe.%'` 的行都能立刻识别为前端事件

### 决策 #4:EventType 长度 50 在 handler 层前置校验

后端 DB schema `decision_events.event_type VARCHAR(50)`。在 handler 层加前置校验,避免无效事件打到 DB 才触发 GORM 报错。

### 决策 #5:Payload schema-less(`map[string]interface{}`)

后端 schema `payload JSONB`。前端事件维度差异大(有的带 `phase`、有的带 `helpful`、有的带 `attempt`),强 schema 会成为迭代负担。schema-less 让前端快速加新事件不用等后端改 schema。

**权衡**：下游查询方需要按字段类型断言(JSON 解码到 `map[string]interface{}` 时 `int` 变 `float64`,这是 Go 标准库行为)。

### 决策 #6:PII 守卫在前端做(默认拒绝,白名单正向)

前端 `containsPII(payload)` 递归扫描,任何一层含敏感字段就拒绝:

```ts
const PII_FIELDS = new Set([
  "content", "message", "messages", "raw_results", "summary",
  "verdict", "verdict_content", "trial_summary", "context", "title",
]);
```

**理由**：埋点维度已经够用,真要记"用户证据"应该走后端业务事件(有完整鉴权),不该走前端埋点。前端一律黑名单,简单粗暴。

### 决策 #7:批量窗口 5s + 关键事件立即 flush

- 普通事件：进入队列,5s 后 flush(可减少请求数 80%+)
- 关键事件(verdict_feedback / ws_reconnect / ws_missed_pong / trial_completed):立即 flush,不容丢失

**理由**：5s 窗口对"用户停留时长"分析精度足够(±2.5s 误差),但能省 80% 请求。关键事件一案发生 ≤5 次,可接受。

### 决策 #8:失败重试用 in-queue 回填,不做指数退避

失败的事件回填到队尾,下次 flush 重试。无指数退避(埋点不阻塞主流程,重试到成功即可)。

**队列上限 100 / payload 字节上限 32KB**:防止弱网下队列爆内存。

## 3. 备选方案(不选)

### 3.1 用 `navigator.sendBeacon` 替代 fetch

- 优点:page unload 时也能发送
- 缺点:不能自定义 header(bearer token)、不能读响应、payload 大小限制 64KB
- 选定 fetch + `keepalive: true` 选项等价于 sendBeacon(且支持自定义 header)

### 3.2 实时事件流(WebSocket 多开一个 channel)

- 优点:服务端可主动 push
- 缺点:需要 WS 握手鉴权 + 后端额外 endpoint + 增加 WS 复杂度
- 选定:复用现有 REST endpoint,后端复杂度最小

### 3.3 在后端每个业务 endpoint(`POST /evidences` 等)同时落 decision_events

- 优点:前端不用另开 endpoint
- 缺点:业务 endpoint 复杂化(主路径 + 埋点耦合)、无法埋"前端独立事件"(如 tab 切换 / 模式切换)
- 选定:独立 `/events` endpoint,职责清晰

### 3.4 前端埋点直接 POST 到后端 metrics 端点

- 不行。`/metrics` 是 GET-only 输出,不是 ingest endpoint

## 4. 实施细节

### 4.1 后端

新增 [backend/internal/api/handler_events.go](../../backend/internal/api/handler_events.go):

```go
type frontendEventRequest struct {
    EventType  string                 `json:"event_type" binding:"required,max=50"`
    Payload    map[string]interface{} `json:"payload"`
    DurationMs int64                  `json:"duration_ms"`
    Status     string                 `json:"status"`
    ErrorMsg   string                 `json:"error_msg"`
}

func (h *Handler) PostFrontendEvent(c *gin.Context) {
    sessionUUID := c.Param("session_uuid")
    if _, ok := h.checkSessionAccess(c, sessionUUID); !ok { return }

    var req frontendEventRequest
    if err := c.ShouldBindJSON(&req); err != nil { /* 400 */ return }
    if len(req.EventType) > 50 { /* 400 */ return }

    status := req.Status
    if status == "" { status = "ok" }

    if h.eventRecorder == nil {
        c.JSON(200, gin.H{"code": 0, "data": gin.H{"recorded": false}})
        return
    }

    _ = h.eventRecorder.Record(c.Request.Context(), observability.DecisionEventRecord{
        SessionUUID: sessionUUID, EventType: req.EventType, Payload: req.Payload,
        DurationMs: req.DurationMs, Status: status, ErrorMsg: req.ErrorMsg,
    })
    c.JSON(200, gin.H{"code": 0, "data": gin.H{"recorded": true}})
}
```

`Handler` struct 新增字段 `eventRecorder observability.EventRecorder`,通过 `WithEventRecorder(rec)` 注入(main.go 装配时复用同一个 `eventRecorder`)。

### 4.2 前端

新增 [frontend/lib/transport.ts](../../frontend/lib/transport.ts):

- `createTransport(config, deps) → { enqueue, flush, size }`
- 工厂模式 + 依赖注入 fetcher / scheduleFlush / onWarn,测试可注入 fake
- 默认 `defaultDeps(batchIntervalMs)` 返回生产配置(SSR safe)

新增 [frontend/lib/analytics/index.ts](../../frontend/lib/analytics/index.ts):

- `createAnalytics(deps) → { track, trackPhaseChange, trackVerdictFeedback, trackEvidenceSubmitted }`
- PII 守卫 `containsPII(payload)` 递归扫描
- 便捷函数封装常用事件 schema,降低调用方出错概率

### 4.3 测试

| 文件 | 测试数 | 覆盖 |
|---|---|---|
| `backend/internal/api/handler_events_test.go` | 10 | 成功 / 404 / 403 / 400 / recorder nil / recorder 失败 / 字段透传 |
| `frontend/lib/transport.test.ts` | 12 | 入队 / flush / 失败降级 / 关键事件 / 容量上限 / mock 模式 / SSR |
| `frontend/lib/analytics/analytics.test.ts` | 10 | track / 自动 sessionUUID / PII 守卫(顶层 + 嵌套) / 便捷函数 |

合计 32 个新测试,全部 TDD(先写测试,实现让测试从红变绿)。

### 4.4 调用方接入(后续 PR)

Step 1 只交付基础设施 + 后端端点,实际事件触发在后续 PR 接入:

- Step 2(漏斗 + 业务指标):在 `CourtroomScene.tsx` 接入 `trackPhaseChange` / `trackEvidenceSubmitted` / `trackVerdictFeedback`,在 `EvidenceBoard.tsx` 接入 `direct_verdict_triggered`
- Step 3(性能):web-vitals 包 + WS reconnect 监听器接入 `fe.web_vital` / `fe.ws_reconnect`

## 5. 部署验证

```bash
cd backend
go test ./internal/api/... -run TestPostEvent -v   # 10/10 PASS
go test ./...                                       # 全部 OK

cd frontend
node --experimental-strip-types --test lib/transport.test.ts lib/analytics/analytics.test.ts  # 22/22 PASS
```

部署到 ECS 后手工 curl 验证:

```bash
curl -X POST https://decisioncourt.cn/api/v1/courtrooms/$SESSION/events \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"event_type":"fe.smoke_test","payload":{"phase":"opening"}}'
# 期望:{"code":0,"data":{"recorded":true}}
```

然后 `SELECT * FROM decision_events WHERE event_type LIKE 'fe.%' ORDER BY created_at DESC LIMIT 5;` 应能看到刚才的事件。

## 6. 后续工作(不在本 ADR 范围)

- Step 2 接入实际事件触发位置(CourtroomScene / EvidenceBoard 等 5 处)
- Step 3 web-vitals + WS reconnect 监听器
- 数据仓库 ETL 把 decision_events 同步到 ClickHouse / DuckDB,做漏斗分析
- Prometheus exporter 暴露 fe.* 事件计数为 metric

## 7. 相关文档

- [ADR 0010](./0010-whitebox-observability.md) —— v0.8 三支柱(metrics / decision_events / slog)
- [whitebox-roadmap.md Phase B](../roadmap/whitebox-roadmap.md) —— 后端增量埋点计划,本 ADR 是它的前端配套
- [decisioncourt-tech-spec.md](../decisioncourt-tech-spec.md) —— 待更新"前端埋点"章节(本 ADR 落地后)

---

## 8. 面试讲解附录(2026-07-08 加)

> **Cross-link**: 本节是 ADR 层面的"面试讲解附录",侧重讲技术决策。
> 第一人称 / 故事化 / 30-3-10 分钟话术见 [`docs/interview/10-frontend-analytics.md`](../interview/10-frontend-analytics.md)。

> 这一节的目的不是讲技术,而是讲**怎么讲**。
> 把"我做了一个前端埋点"讲到能让面试官眼前一亮,需要三件事:
> 1. 一句话能说清价值
> 2. 关键决策每个都能展开
> 3. 被追问时心里有底

### 8.1 30 秒电梯演讲

> "我们项目后端 v0.8 已经有一套业务事件可观测性基础设施(metrics / decision_events / slog 三支柱),但**前端完全是黑盒** —— 用户在每个庭审阶段停留多久、WebSocket 重连几次、流式 chunk 延迟多少,**只有前端能看到**。
>
> 所以我做了一个**复用现有基础设施**的前端埋点:前端打到同一张 `decision_events` 表,EventType 用 `fe.` 前缀和后端 span 区分。关键设计是**埋点失败绝不阻塞前端**(best-effort)和**PII 守卫**(payload 含 content / message / verdict 就拒绝)。结果是漏斗转化率、各阶段停留时长、判决反馈率这些**只有前端能给的指标**现在能直接查 SQL 了。"

### 8.2 设计哲学 / Why this matters

**核心叙事:可观测性应该横向扩展,而非垂直重建**

很多项目做埋点的思路是"前端用 SaaS(GA / Sentry / PostHog),后端自己一套 metrics" —— 这形成两个面板、两套查询、两套告警、两次合规审计。

我们反其道而行:**前端和后端事件进同一张表、用 EventType 前缀区分**。这不是"省事",是**主动选择** —— 因为这个项目的核心价值是"AI 可观测",**用户行为事件和 AI 决策事件必须能 join 查询**(例:某用户在 cross_exam 阶段停留 > 60 秒后,法官的信念轨迹是不是发生了剧烈变化?)。

把这两类数据放同一张表,才能回答这种问题。

### 8.3 关键技术决策清单(8 个,每个一句)

| # | 决策 | 一句话理由 |
|---|---|---|
| 1 | 数据去向后端 `decision_events` | 让前端事件和 AI 决策事件能 join 查询 |
| 2 | 埋点失败 best-effort 不阻塞 | 观测性 ≠ critical path,用户点按钮不该被埋点失败阻断 |
| 3 | `fe.<name>` 前缀 | 一行 SQL `WHERE event_type LIKE 'fe.%'` 区分前后端 |
| 4 | EventType 长度 50 在 handler 层前置校验 | 避免 DB GORM 报错才拒绝,省一次 DB round-trip |
| 5 | Payload schema-less | 快速迭代,新事件不用先改 schema |
| 6 | PII 守卫前端黑名单 | 简单粗暴,前端一律不带 content / message / verdict |
| 7 | 批量 5s + 关键事件立即 flush | 减少 80% 请求,但 verdict_feedback / ws_reconnect 不容丢失 |
| 8 | 失败回填队列尾 + 容量上限 | 弱网下不爆内存,事件最终会送达 |

### 8.4 面试官可能追问的 Q&A

#### Q1:"为什么不直接用 PostHog / Sentry?它们现成的漏斗分析不是更好?"

**推荐答案**(展示权衡能力):

"现成的 SaaS 在 demo 期确实方便,但这个项目的核心叙事是 **AI 可观测** —— 用户行为事件必须能和 AI 决策事件 join 查询。例:某用户在 cross_exam 阶段停留 > 60 秒,法官的信念轨迹是不是剧烈变化?这种 cross-source 查询,如果数据在两个系统里就只能 ETL 同步,延迟 + 复杂度都上去了。

另一个考量:**数据出境合规**。这个项目用户会提交案件原文,属于敏感内容。前端埋点如果走 SaaS,等于用户敏感数据出境,合规风险大。

所以选**打到自己后端的 decision_events 表**,用 EventType 前缀区分。代价是漏斗分析要自己写 SQL,好处是数据在自己手里、能 join。"

#### Q2:"批量 5s 窗口,如果用户在 5s 内 page unload 怎么办?"

**推荐答案**(展示细节):

"这是个真实风险。5s 窗口内的普通事件可能丢失。**缓解方案有三层**:

1. **fetch + keepalive**:我用 `fetch(url, { keepalive: true })` 等价于 `sendBeacon`,page unload 期间浏览器会保留请求直到完成。
2. **`flushNow()` API**:runtime.ts 暴露了 `flushNow()`,可以在 pagehide / visibilitychange 事件里主动调,触发一次同步 flush。
3. **关键事件不走批量**:`verdict_feedback` / `ws_reconnect` 这些不容丢失的事件在 transport 里就走立即 flush 路径,不存在 5s 窗口风险。

实际上,我建议面试时**主动指出这个 trade-off**,而不是被问到才说 —— 体现对生产环境的考量。"

#### Q3:"PII 守卫用黑名单是不是不够稳?怎么保证新人不小心加了 `verdict_text` 字段?"

**推荐答案**(展示演进能力):

"你说得对,黑名单天然有遗漏风险。**当前阶段**(MVP)够用,理由是:
1. 我们项目的事件清单小(8 个),PII 黑名单在 [analytics/index.ts](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/analytics/index.ts) 集中维护,code review 一眼能看出。
2. 守卫失败兜底是 `console.warn`,不是静默丢弃 —— 至少会被看到。

**演进路径**(面试时展示前瞻性):
- 短期:在 PR template 加 checkbox"如果新增 fe.* 事件,payload 是否过 PII 守卫?"
- 中期:把 PII 黑名单换成白名单 —— payload 只允许预定义的 key。
- 长期:把守卫提到 ESLint rule / commit hook,新增敏感字段就 CI 报警。

我选黑名单是因为白名单会把快速迭代能力削弱太多,得不偿失。"

#### Q4:"EventType 用 `fe.` 前缀,会不会出现有人写错写成 `frontend.` 或 `FE.` ?"

**推荐答案**(展示工程规范意识):

"这是个真实问题。**当前缓解**:
- 后端 handler 没用前缀白名单,只是简单 `binding:"required,max=50"`,所以写错不会报错。
- 文档(db-design.md §9.6)明确写了命名空间约定。

**长期方案**:
- 后端 handler 加 `binding:"required,startswith=fe."` 验证前缀(gin validator 支持自定义规则)。
- 或者把 EventType 改成强类型 enum(Go 端 + TS 端共享 schema),编译期就报错。

这次没做是因为 v0.10 还在 MVP 阶段,先把流程跑通。"

#### Q5:"你怎么验证埋点是真的在跑?有测试吗?"

**推荐答案**(展示工程纪律):

"三层验证:

1. **单元测试**(TDD 32 个):transport.ts 测了批量 / 失败 / 容量 / mock 模式,analytics 测了 PII 守卫(顶层 + 嵌套)+ 便捷函数。
2. **集成测试**(手工 curl):ADR §5 列了命令,部署后 `curl -X POST .../events -d '{"event_type":"fe.smoke_test",...}'` 然后 `SELECT * FROM decision_events WHERE event_type LIKE 'fe.%'` 验证。
3. **真实使用验证**:这 8 个埋点接入到了 8 个实际用户操作(开 庭、提交证据、切 tab、判决反馈等),**每次前端组件 mount 都会触发**。

**没做的事**(展示取舍):
- **没写 React 组件测试**:`node --test` 是项目测试基建,引入 RTL 会让测试基建膨胀。改用 dev 环境 curl 冒烟。
- **没写跨包 e2e**:前端真发 fetch + 后端真落 DB 算"鸡肋 e2e",值不大。

这种取舍本身就是工程判断 —— 我可以详细讲为什么。"

### 8.5 5 分钟现场演示建议

如果面试有现场演示环节(白板或现场跑代码),建议节奏:

1. **(30s)** 打开项目结构图,指 [ADR 0020](file:///d:/源码/FullStack/DecisionCourt/docs/adr/0020-frontend-analytics-via-decision-events.md) + [analytics/index.ts](file:///d:/源码/FullStack/DecisionCourt/frontend/lib/analytics/index.ts),讲清"复用现有基础设施"的叙事。
2. **(60s)** 跑 `node --experimental-strip-types --test lib/transport.test.ts lib/analytics/analytics.test.ts`,展示 32 个测试全过 —— 证明"我没光说不练"。
3. **(60s)** 打开 [CourtroomScene.tsx](file:///d:/源码/FullStack/DecisionCourt/frontend/components/courtroom/CourtroomScene.tsx#L100-L120) 看 6 处 `getAnalytics().track(...)` 调用,讲每个埋点的业务价值(漏斗 / 转化 / 反馈)。
4. **(60s)** 展示 [db-design.md §9.6](file:///d:/源码/FullStack/DecisionCourt/docs/decisioncourt-db-design.md#L517-L591) 的 SQL 查询 —— 用 `WHERE event_type LIKE 'fe.%'` 一句话过滤前端事件,展示 join 查询能力。
5. **(30s)** 收尾:这个项目的核心叙事是 **AI 可观测**,前端埋点是 v0.8 的**横向延伸**(不是新方向),让"用户行为"和"AI 决策"在同一张表 join 查询 —— 这是和"用 PostHog"的根本区别。

**核心动作**:把鼠标停在 `db-design.md §9.6` 的漏斗 SQL 上,让面试官看到"前端 + AI 决策"在一条 SQL 里 join —— 这是**故事的高潮**。

### 8.6 "如果只能记一句话"的浓缩

> **可观测性应该横向扩展(前端事件进同一张表),而不是垂直重建(用 SaaS),这样用户行为和 AI 决策才能在同一张表 join 查询 —— 这是 DecisionCourt v0.10 前端埋点的核心决策。**
# ADR 0033: LLM Trace 后端聚合 + 前端可视化架构

| | |
|---|---|
| **编号** | 0033 |
| **标题** | v1.0.4 LLM Trace 架构：FileLogger JSON Lines 读端聚合 + REST + 前端时间轴 |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-22 |
| **触发** | V1-ROADMAP M3 v1.0.4 "LLM Trace 可视化 + Framer Motion 微动效" |
| **依赖** | v0.10.22 PR-A `agent_gateway.FileLogger` JSON Lines 落盘 + v0.8.3 `observability.Trace` trace_id 注入 + v0.6 `belief_diffs` 表 |
| **替代决策** | (a) LangSmith / Langfuse SDK / (b) 自建 clickhouse + 实时流式 / (c) 仅 console.log 不落盘 |
| **影响** | `backend/internal/trace/` (NEW) + `backend/internal/api/handler_trace.go` (NEW) + `frontend/components/trace/` (NEW) + `frontend/lib/trace.ts` (NEW) + `frontend/lib/animations/` (NEW) + `frontend/components/courtroom/animations/` (NEW) |

---

## 1. 决策

### 1.1 背景

v0.10.22 PR-A 在 `agent_gateway.FileLogger` 把每次 LLM 调用元数据落盘到 `logs/agent_gateway_YYYY-MM-DD.log` JSON Lines 文件，但**没有任何读端聚合**。DevOps 排查问题只能 `grep` + `jq`，看不到：
- 单次 trial 的"LLM 调用链"（某次发言→触发 retry→judge 触发→retry judge...）
- 信念变化的时间轴（控方/辩方/法官在不同 round 的 belief_a 走势）
- 反驳链的触发与翻盘（v1.0.2 rebuttal_links 静态展示）

v1.0.0 落地后用户反复反馈"想看 LLM 到底在想什么"，没有可视化层只有 debug 日志。

### 1.2 选项对比

| 维度 | A. LangSmith SDK | B. ClickHouse + 实时流式 | **C. 自建 FileLogger 聚合 + REST (本决策)** |
|---|---|---|---|
| 集成成本 | 高（外部依赖 + token） | 高（运维 + 集群） | 低（复用现有 FileLogger 落盘） |
| 隐私 | 差（数据出云） | 优 | 优（本地文件） |
| 延迟 | 远程 API 100-300ms | 实时 ~10ms | 离线读 ~5ms |
| vendor lock-in | 高 | 低 | 无 |
| 当前 DAU | 个人项目，无需 GA | 个人项目，无需 PB 级 | 个人项目，1 trial ~50 runs 5KB |
| 实现复杂度 | 中（SDK 接入 + 后端改） | 高（集群运维） | 低（读 JSON Lines + LRU） |

### 1.3 决策

采用 **方案 C** — 复用现有 `agent_gateway.FileLogger` JSON Lines 落盘，新建 `internal/trace/` 包做**读端聚合**，新增 REST `/traces/*` 端点，前端用 React 组件可视化。

### 1.4 关键理由

1. **零写入路径变更**：FileLogger 已在 hot path 上稳定运行（v0.10.22 PR-A 实装，本 PR 不动写入路径）
2. **不引入新存储**：1 trial ~50 runs × 5KB/entry = 250KB/day，1GB/年，远低于本地磁盘容量
3. **离线优先**：符合个人项目"长期本地开发模式"（ECS 2026-08-05 终止后转入本地 dev）
4. **零外部依赖**：不引 LangSmith SDK，避免 vendor lock-in 与 token 成本
5. **可逐步升级**：未来若需要 PB 级 / 实时流式，可在不破坏 API 的前提下替换 Store 实现

---

## 2. 设计要点

### 2.1 数据流

```
LLM call → gateway.writeFileLog → logs/agent_gateway_YYYY-MM-DD.log (JSON Lines)
                                          ↓ (read)
                                  trace.ParseFile / ParseReader
                                          ↓ (group by RequestID)
                                  SessionTrace{Traces: map[trace_id][]Run}
                                          ↓ (LRU cache)
                                  FileTraceStore / InMemoryTraceStore
                                          ↓ (REST GET)
                                  /api/v1/courtrooms/:uuid/traces[/:trace_id]
                                          ↓ (fetch)
                                  frontend lib/trace.ts (fetchTraces / fetchTrace)
                                          ↓ (render)
                                  TrialReplay Dialog (3 tabs)
                                    ├── AgentTraceNode (tree)
                                    ├── BeliefDiffTimeline (recharts)
                                    └── RebuttalTraceNode (groups)
```

### 2.2 Run.TraceID 字段映射

LogEntry 字段名是 `request_id`（v0.10.22 PR-A 沿用 HTTP middleware `X-Request-ID`），本包对外 API 用 `trace_id` 字段命名（与 OTel 标准对齐，便于未来切换 OTel 时不改 API）：

| LogEntry 字段 | Run 字段 | 说明 |
|---|---|---|
| `request_id` | `Run.TraceID` | HTTP trace_id，从 X-Request-ID 注入 |
| `request_id + "-" + retry_count` | `Run.RunID` | 同 trace 内 unique |
| `timestamp - latency_ms` | `Run.StartedAt` | 从 log 落盘时间反推调用开始时间 |
| `timestamp` | `Run.EndedAt` | log 落盘时间 |
| `session_uuid` | `Run.SessionID` | session 过滤 |
| `retry_count` | `Run.RetryCount` | 父子关系构造（RC=N 是 RC=N-1 的 child） |
| `agent_type / task_type / model / status / error_msg` | 同名 | 一一映射 |
| ❌ | `Run.Input / Output / Tags` | LogEntry 不含，留空（后续 PR 可补"全量 prompt 落盘"开关） |

### 2.3 文件级 LRU 缓存

`FileTraceStore` 缓存 key = `"file:" + date`（**文件级**），不是 `"sessionID:" + date`：

- 同一天文件被多 session 复用解析结果
- LRU 默认 100 entries ≈ 5MB 内存，2C2G ECS 够用
- 文件级缓存失效语义清晰：同 session 二次查询同 date 直接命中缓存

### 2.4 Tree 构造算法

`BuildTree` 按 `RetryCount` 单链构造父子关系：

```
RetryCount=0 → root
RetryCount=N (N>0) → RetryCount=N-1 的 child
```

- retry 实现是 sequential（不是 tree），多分支场景不出现
- 单链 DFS 递归深度 ≤ 5（实测 max retry 3），不爆栈

### 2.5 前端 SSR 路径

`lib/trace.ts` 与 `lib/rebuttal.ts` 同模式：

- `typeof window === "undefined" || useMock` → 返空/null（避免 Next.js build 报错）
- 浏览器 fetch 失败 → console.warn + 返空数组（UI 容错降级到"无数据"渲染）
- 不在 Node --test 环境 mock fetch（避免 ESM + .ts 后缀 + fetch mock 链路复杂度）

### 2.6 Framer Motion 渐进接入

PR-C3 不全替换现有 CSS 动画：

- **接管**：avatar scale/y/rotate（motion.div）+ 气泡 fade in/out（AnimatePresence）
- **保留**：CSS `animate-spin`（调查员 spinner ring）+ `animate-pulse`（流式光标 + 搜索图标）

理由：CSS 装饰元素无状态切换需求，motion 接管"有状态的"头像+气泡，性能 + 维护性权衡。

---

## 3. 后果

### 3.1 收益

- ✅ **零写入路径变更**：FileLogger 稳定运行，PR-C1 只动"读端"
- ✅ **可观测性升级**：dev / verdict 页可看 LLM 调用链 + 信念曲线 + 反驳链
- ✅ **审计 trail 完整**：trial 结束后用户可看"为什么 judge 这么判"
- ✅ **未来 OTel 迁移友好**：trace_id 字段命名与 OTel 对齐

### 3.2 代价

- ⚠️ **前端 bundle +30KB**：framer-motion 11.x 体积
- ⚠️ **Input/Output 留空**：plan §2 设计了但 LogEntry 没存，后续 PR 补"全量 prompt 落盘"开关
- ⚠️ **A/B Test 不区分 version prompt**：本 PR 只交付评分比较框架，"重新跑 LLM 拿 A/B 输出"留后续 PR
- ⚠️ **前端测试仅 SSR 路径**：Node --test 测不了 fetch 路径（ESM + .ts 后缀 + fetch mock 复杂度），文档化在 test 注释里

---

## 4. 关联

### 主文档

- [V1-ROADMAP.md M3 v1.0.4](../V1-ROADMAP.md)
- [V1.0.4-PLAN.md](../V1.0.4-PLAN.md) — 完整 4 PR 拆分
- [v1.0.4 release notes](../release-notes/v1.0.4.md) — 14 章节模板

### 代码

- 后端： `backend/internal/trace/{parser.go, aggregator.go, store.go}`
- 后端 handler： `backend/internal/api/handler_trace.go`
- 前端 lib： `frontend/lib/trace.ts` + `frontend/lib/animations/{easing.ts, variants.ts}`
- 前端 components： `frontend/components/trace/{TrialReplay, AgentTraceNode, BeliefDiffTimeline, RebuttalTraceNode}.tsx`
- 前端 animations： `frontend/components/courtroom/animations/{AvatarAnimations, ThinkingDots, GavelAnimation, SpeechBubbleAnimated}.tsx`
- 改： `frontend/components/courtroom/AgentAvatar.tsx` + `CourtroomScene.tsx`

### 测试

- 后端： `internal/trace/{parser_test.go, aggregator_test.go, store_test.go}` + `internal/api/handler_trace_test.go` 共 20 sub-test
- 前端： `frontend/lib/trace.test.ts` 4 sub-test + `frontend/lib/animations/variants.test.ts` 6 sub-test

### 复用

- v0.10.22 PR-A `agent_gateway.FileLogger` JSON Lines 落盘
- v0.8.3 `observability.Trace` trace_id HTTP middleware 注入
- v0.6 `belief_diffs` 表（BeliefDiffTimeline 数据源）
- v1.0.2 `evidence_rebuttal_links` 表（RebuttalTraceNode 数据源）
- v1.0.3 PR-B1 `promptlab` YAML 化（不冲突，独立）

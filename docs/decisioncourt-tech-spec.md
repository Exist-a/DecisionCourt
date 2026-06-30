# 决策庭（DecisionCourt）技术选型文档

> 版本：v0.5  
> 状态：v0.5 增补 Episodic Memory via A2A 私有通道 + ContextView 投影层 + Expand-Contract 迁移策略  
> 目标：为决策庭 MVP 提供清晰、可落地的技术栈选择与架构设计。  
> 设计演进：[memory-a2a-redesign.md](../../.trae/documents/memory-a2a-redesign.md)

---

## 1. 选型原则

1. **简历导向**：技术栈要能体现全栈工程能力，同时避免过度炫技。
2. **本地可跑**：MVP 必须能在本地 Docker Compose 一键启动。
3. **国内友好**：优先支持国内可稳定访问的 LLM API。
4. **可扩展性**：架构留有扩展空间，第二阶段可以加专家证人、陪审团等。
5. **成本控制**：开发期 WebSearch 免费，LLM 调用量可控。

---

## 2. 整体架构

```
┌─────────────────────────────────────────────────────────────┐
│                     Frontend Layer                          │
│  Next.js 14 (App Router) + React + TypeScript               │
│  Tailwind CSS + shadcn/ui + React-Flow                      │
│  Socket.io-client（WebSocket 客户端）                        │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTPS / WebSocket
┌──────────────────────────▼──────────────────────────────────┐
│                      API Gateway                            │
│  Go + Gin                                                   │
│  - RESTful API（庭审、证据、Agent 编排）                     │
│  - WebSocket 网关（实时庭审流）                             │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                   Agent Orchestration                       │
│  自定义状态机 + Prompt 模板 + 信念引擎                        │
│  - A2A 消息总线（Agent-to-Agent）                           │
│  - 私有记忆池（按 Agent 隔离）                              │
│  - ProsecutorAgent（控方）                                  │
│  - DefenderAgent（辩方）                                    │
│  - InvestigatorAgent（调查员）                              │
│  - ClerkAgent（书记员）                                     │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│              Agent Gateway（v0.5+ 已实装）                   │
│  - 统一接入 + 调用审计（llm_calls 表）                       │
│  - Prompt 压缩（可开关）                                     │
│  - Token 预算（可开关）                                      │
│  - 限流与降级（可开关）                                      │
│  - Fallback 退避重试（可开关）                                │
│  - JSON 文件日志（按日期切分）                                │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                    LLM Provider                             │
│  DeepSeek API（OpenAI 兼容格式）                            │
│  - V3 处理常规轮次                                          │
│  - R1 处理关键轮次（如最终判决、复杂质证）                 │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                    Data & Search Layer                      │
│  PostgreSQL：庭审、证据、Agent 状态、判决书                 │
│  PostgreSQL：A2A 消息日志、私有记忆                         │
│  Redis：会话状态、WebSocket 订阅、限流                      │
│  SearXNG（Docker）：开发期免费搜索                          │
│  Tavily API：部署期高质量搜索（可选切换）                   │
└─────────────────────────────────────────────────────────────┘
```

**说明**：Agent Gateway 位于 Agent Orchestration 与 LLM Provider 之间。v0.5+ 已实装白盒子集（统一接入、审计、trace 关联）与高级能力（Prompt 压缩、Token 预算、限流、Fallback 退避重试、JSON 文件日志），均可通过环境变量开关。模型路由与响应缓存仍留到第二阶段。

---

## 3. 前端技术栈

### 3.1 框架：Next.js 14 App Router

| 维度 | 选择 | 理由 |
|---|---|---|
| 框架 | Next.js 14 | App Router 提供 Server Component、Streaming、API Route |
| 语言 | TypeScript | 类型安全，便于维护 |
| 样式 | Tailwind CSS | 实用优先，白底极简法庭风格可快速实现 |
| 组件库 | shadcn/ui | 基于 Radix UI，可定制，无样式锁定 |
| 可视化 | React-Flow | 观点地图、证据关系图首选 |
| 图表 | Recharts / Visx | 立场变化曲线 |
| 状态管理 | Zustand | 轻量，适合庭审客户端状态 |
| 实时通信 | Socket.io-client | 自动重连、事件命名空间 |

### 3.2 关键页面

| 路由 | 说明 |
|---|---|
| `/` | 首页 / 立案入口 |
| `/court/[id]` | 庭审主界面 |
| `/verdict/[id]` | 判决书展示页（可分享） |
| `/history` | 历史庭审（第二阶段） |

### 3.3 前端目录结构（暂定）

```
frontend/
├── app/
│   ├── page.tsx              # 首页
│   ├── court/
│   │   └── [id]/
│   │       └── page.tsx      # 庭审界面
│   ├── verdict/
│   │   └── [id]/
│   │       └── page.tsx      # 判决书页
│   ├── layout.tsx
│   └── globals.css
├── components/
│   ├── courtroom/
│   │   ├── CourtroomScene.tsx    # 法庭场景布局
│   │   ├── AgentAvatar.tsx       # Agent 头像
│   │   ├── EvidenceBoard.tsx     # 证据板
│   │   ├── StanceChart.tsx       # 立场变化曲线
│   │   ├── ArgumentMap.tsx       # 观点地图
│   │   └── VerdictPanel.tsx      # 判决书面板
│   ├── ui/                       # shadcn/ui 组件
│   └── forms/                    # 表单组件
├── lib/
│   ├── api.ts                    # API 客户端
│   ├── websocket.ts              # WebSocket 封装
│   └── utils.ts
├── store/
│   └── courtroomStore.ts         # Zustand 状态
├── public/
└── package.json
```

---

## 4. 后端技术栈

### 4.1 框架与语言

| 维度 | 选择 | 理由 |
|---|---|---|
| 语言 | Go 1.22+ | 性能高、并发模型适合 WebSocket、部署简单 |
| Web 框架 | Gin | 社区大、中间件丰富、文档完善 |
| ORM | GORM | 开发效率高，PostgreSQL 支持好 |
| 配置 | Viper | 支持环境变量、配置文件 |
| 日志 | Zap / Slog | 结构化日志 |
| 验证 | go-playground/validator | 请求参数验证 |
| UUID | google/uuid | 庭审 ID 生成 |

### 4.2 实时通信

| 维度 | 选择 | 理由 |
|---|---|---|
| WebSocket 库 | gorilla/websocket | 最成熟，支持广播、连接管理 |
| 事件格式 | JSON | 前后端统一 |
| 房间隔离 | session_id 作为 room | 不同庭审互不干扰 |

### 4.3 LLM 调用

| 维度 | 选择 | 理由 |
|---|---|---|
| SDK | github.com/sashabaranov/go-openai | 支持 OpenAI 兼容 API，DeepSeek 可用 |
| 模型 | DeepSeek-V3（默认）| 中文强、便宜、响应快 |
| 模型 | DeepSeek-R1（关键轮次）| 推理强，适合复杂质证和判决 |
| 调用方式 | HTTP API | 简单直接，无需本地 GPU |

### 4.4 后端目录结构（暂定）

```
backend/
├── cmd/
│   └── server/
│       └── main.go              # 入口
├── internal/
│   ├── api/
│   │   ├── handler.go           # HTTP handler（含 GET /investigations）
│   │   ├── middleware.go        # 中间件
│   │   ├── websocket.go         # WebSocket handler
│   │   └── hub.go               # Hub + Room 广播（Broadcast sleep 30ms 保流式 spacing）
│   ├── courtroom/
│   │   ├── service.go           # 庭审业务逻辑 + DispatchInvestigator 单条 entry 状态机 + speakWithReAct 流式
│   │   └── statemachine.go      # 庭审状态机
│   ├── agent/
│   │   ├── orchestrator.go      # Agent 编排（基于 A2A 消息）
│   │   ├── prosecutor.go        # 控方
│   │   ├── defender.go          # 辩方
│   │   ├── investigator.go      # 调查员
│   │   ├── clerk.go             # 书记员
│   │   ├── prompts.go           # Prompt 模板
│   │   ├── react_runner.go      # ReAct 循环 + OnIterStart / OnSpeakChunk 钩子
│   │   └── tools/
│   │       └── investigator_search.go  # 调查员搜索工具（新签名）
│   ├── a2a/
│   │   ├── message.go           # A2A 消息结构（含 dispatch_investigator / investigation_report）
│   │   ├── bus.go               # 消息总线与路由
│   │   └── access.go            # 访问控制（上下文隔离）
│   ├── private_memory/
│   │   ├── model.go             # 私有记忆数据模型
│   │   ├── store.go             # 按 Agent 隔离的 CRUD
│   │   └── injector.go          # 记忆注入与相关性筛选
│   ├── evidence/
│   │   ├── model.go             # 证据模型
│   │   ├── service.go           # 证据服务（**仅**用户证据专用）
│   │   └── rules.go             # 证据规则
│   ├── investigation/           # v0.3 新增：调查发现独立模块
│   │   ├── types.go             # Finding struct + Repository interface
│   │   ├── service.go           # RecordFinding / ListBySession
│   │   ├── gorm_repository.go   # GORM 实现
│   │   └── inmemory_repository.go # InMemory（测试用）
│   ├── belief/
│   │   └── engine.go            # 信念引擎
│   ├── search/
│   │   ├── provider.go          # 搜索接口
│   │   ├── bocha.go             # Bocha API 实现（生产默认）
│   │   ├── searxng.go           # SearXNG 实现（开发可选）
│   │   ├── duckduckgo.go        # DuckDuckGo 实现（fallback）
│   │   └── mock.go              # MockProvider（测试用）
│   ├── agent_gateway/
│   │   ├── gateway.go           # 网关入口
│   │   ├── router.go            # 模型路由
│   │   ├── compressor.go        # Prompt 压缩
│   │   ├── budget.go            # Token 预算
│   │   ├── cache.go             # 响应缓存
│   │   └── audit.go             # 调用审计
│   ├── llm/
│   │   ├── client.go            # LLM 客户端（含 StreamComplete）
│   │   └── config.go            # 模型配置
│   ├── model/
│   │   └── db.go                # GORM 模型（含 InvestigationFinding）
│   └── config/
│       └── config.go            # 配置管理
├── migrations/                  # 数据库迁移
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

### 4.5 A2A 与情节记忆架构（v0.5 重设计）

> **状态**：截至 2026-06-29 已实装代码。`internal/a2a`（Bus + Repository 接口 + InMemory/GORM 实现 + 12 项隔离测试）+ `internal/private_memory`（9 项隔离测试）已落地，但 v0.5 计划把私有记忆底层迁移到 A2A 私有通道（PR 1-4）。详见 [memory-a2a-redesign.md](../../.trae/documents/memory-a2a-redesign.md)。

#### 4.5.1 A2A 消息总线（v0.5 扩展）

Agent 之间不直接通信，也不共享完整 prompt 上下文。所有 Agent 输入/输出都通过 `a2a.Bus` 包装为标准消息：

```go
type Message struct {
    ID          uuid.UUID
    MessageUUID string
    SessionID   uuid.UUID
    Round       int
    Phase       string
    From        string                 // agent_type 或 "orchestrator"
    To          string
    MessageType MessageType            // 见下方 v0.5 全量列表
    Visibility  Visibility             // public / private
    Payload     map[string]interface{}
    MemoryRefs  []string
    CreatedAt   time.Time
}
```

`a2a.Bus` 的核心职责：

1. **路由**：根据 `To` 字段将消息分发给目标 Agent；持久化写入 `a2a_messages` 表。
2. **可见性隔离**：`Visibility = private` 的消息只能被 `To` 与 `From` 看到，orchestrator 看到全部用于审计。`SanitizedPayload()` 方法在投影给对手方前剥离 `reasoning` 字段。
3. **审计广播**：每次 `Send` 触发 `a2a.message` WebSocket 事件；公开消息广播 payload，私有消息仅广播 envelope（不带 payload，避免泄漏）。
4. **消息类型**（v0.5 扩展 4 个 private 类型）：
   - **public**：`speech` / `evidence` / `challenge` / `inquiry` / `verdict_task` / `dispatch` / `report` / `dispatch_investigator` / `investigation_report`
   - **private** 🆕：`strategy_note` / `opponent_weakness` / `self_correction` / `evidence_eval`（v0.5 PR 1 实装）

Repository 抽象（`Repository` interface）允许测试用 `InMemoryRepository`，生产用 `GormRepository`。

#### 4.5.2 情节记忆池（v0.5 重设计：基于 A2A 私有通道）

> **v0.5 重大变更**：私有记忆底层从独立 `private_memories` 表迁移到 **A2A 私有消息通道**。

v0.5 之前：私有记忆存储在独立的 `private_memories` 表（schema 见 [§4.5.2 v0.3 历史](#v03-历史架构仅供迁移参考)）。

v0.5 之后：所有情节记忆条目作为 A2A 私有消息存储，复用 Bus 的隔离/审计/路由能力：

```go
// 存储形式（v0.5）
a2a.Message{
    SessionID:   sessionID,
    From:        "prosecutor",  // from=selfAgent
    To:          "prosecutor",  // to=selfAgent
    MessageType: a2a.MessageTypeStrategyNote,  // 或 OpponentWeakness / SelfCorrection / EvidenceEval
    Visibility:  a2a.VisibilityPrivate,
    Payload: map[string]interface{}{
        "memory_type": "opponent_weakness",
        "round":       1,
        "content":     "辩方没反驳 E001 的数据来源，是核心弱点",
        "linked_evidence_ids": ["E001"],
    },
}
```

**访问控制**（v0.5 复用 A2A Bus 隔离）：

- `Bus.ListVisibleTo(sessionID, viewer)` 中 `viewer` 仅接受 `selfAgent`（看自己的私有消息）或 `AddressOrchestrator`（看全部用于审计）；其他 viewer 看不到 `visibility=private` 消息。
- 写入时机 3 处：
  - **每次 speak 后自动**：`a2aBus.Send(MessageTypeStrategyNote, visibility=private)`
  - **ReAct reflect 步骤**：LLM 显式输出 `memory_type` + `memory_note`，ReActRunner 自动 `a2aBus.Send` 写入对应类型
  - **ReAct tool_call(search) 后**：自动 `MessageTypeEvidenceEval`
- 读取时机：`Orchestrator.BuildContextView(sessionID, selfAgent)` 一次性拉取自己的私有消息全文，注入 system prompt。
- 注入策略：**全文注入**（单场庭审 ≤ 50 条 × 200 token = 10K tokens，远低于 128K context window）。
- 书记员**不调用** `BuildContextView`，只读取公共庭审记录。

#### 4.5.2.1 v0.5 ContextView 投影层（PR 1 核心）

Orchestrator 在构造 LLM prompt 前调用 `BuildContextView`，按 selfAgent 过滤 + 剥离对方 reasoning：

```go
// internal/a2a/context_view.go（v0.5 PR 1 新增）
type LLMContext struct {
    SystemPromptPrefix string                  // "## 你之前的策略笔记" 段落
    ArgumentSummary    string                  // "## 本轮对话摘要" 段落（sanitized）
    PublicMessages     []model.A2AMessage      // sanitized payloads
    PrivateMemory      []model.A2AMessage      // self-only, full payloads
    Beliefs            map[string]float64
}

func (b *Bus) BuildContextView(
    ctx context.Context,
    sessionID uuid.UUID,
    selfAgent string,
) (*LLMContext, error) {
    msgs := b.ListVisibleTo(ctx, sessionID, selfAgent)
    
    var pub []model.A2AMessage
    var priv []model.A2AMessage
    for _, m := range msgs {
        if m.Visibility == string(VisibilityPrivate) {
            priv = append(priv, m)
            continue
        }
        // public：对方消息剥 reasoning
        if m.FromAgent != selfAgent && m.FromAgent != AddressOrchestrator {
            sanitized := Message{
                ...
                Payload: Message{ID: m.ID, Payload: map[string]interface{}{...}}.SanitizedPayload(),
            }
            pub = append(pub, sanitized)
        } else {
            pub = append(pub, m)
        }
    }
    // 按 round + created_at 排序，按 type 拼接 narrative
    ...
}
```

**测试要求**（PR 1 验收）：
- [ ] 控方收到辩方 public speech → payload.reasoning 为空
- [ ] 控方收到自己的 private memory → payload 完整
- [ ] 辩方 ListVisibleTo 控方 private → 返回空
- [ ] orchestrator ListVisibleTo → 返回全部（含 private）

#### 4.5.3 数据迁移（v0.5 双写过渡）

`private_memories` 表在 v0.5 PR 4 完成时启动**双写过渡期**，按业界 Expand-Contract 三阶段零停机迁移：

```go
// Phase 1: 双写（PR 4 完成时启动）
func (o *Orchestrator) recordSideEffects(...) {
    // 双写到两个存储
    o.a2aBus.Send(ctx, memoryNoteA2A)  // 新
    o.memoryRepo.Append(ctx, memoryEntry)  // 旧（保留 1 版本周期）
}

// Phase 2: 迁移历史 + 影子读
// 后台脚本：SELECT * FROM private_memories WHERE session_id=X
//   → INSERT INTO a2a_messages (visibility='private', payload=...)
// 影子读对比：每场庭审 assertion：strategy_note 数量两表相同

// Phase 3: 全量切读 a2a_messages → drop 旧表
```

**详细迁移时间线** 参见 [memory-a2a-redesign.md §3](../../.trae/documents/memory-a2a-redesign.md)。

#### 4.5.3 派遣调查员（dispatch_investigator）

> **v0.3 修订**：原 v0.2 描述的"private 通道、双方隔离"语义已弃用。现行为：dispatch / report 都用 `Visibility = public`，写入**独立表** `investigation_findings`，通过 `GET /investigations` 端点暴露给前端 InvestigatorPanel。**不**再写 `evidences` 表。

控辩方 LLM 在 ReAct 思考中通过 `agent.cot_step(tool_call, tool=investigator_search)` 内部决策触发。流程：

```
Prosecutor/Defender   courtroom.Service   a2a.Bus          investigation.Service      SearchProvider
       │                    │               │                       │                       │
       │ tool_call dispatch │               │                       │                       │
       ├───────────────────►│               │                       │                       │
       │                    │ search.started (ws broadcast)        │                       │
       │                    │ broadcast a2a.message(dispatch_investigator, public)         │
       │                    ├──────────────►│                       │                       │
       │                    │               │ RecordFinding(query)  │                       │
       │                    ├───────────────┼──────────────────────►│                       │
       │                    │               │                       │ Search()              │
       │                    │               │                       ├──────────────────────►│
       │                    │               │                       │◄──────────────────────┤
       │                    │               │                       │ raw_results           │
       │                    │               │ repo.Create(finding)  │                       │
       │                    │               │ broadcast a2a.message(investigation_report, public)│
       │                    │               │ search.completed (ws broadcast, 含 finding_id)   │
       │◄───────────────────│               │                       │                       │
```

**关键不变量**：

- Dispatch 和 Report 都用 `Visibility = public`（类比正常庭审记录公开）
- 调查结果写入独立表 `investigation_findings`，**不**进 `evidences` 表
- 前端 InvestigatorPanel 通过 `GET /api/v1/courtrooms/:uuid/investigations` 拉历史，WebSocket 通过 `search.started` / `search.completed` + `a2a.message` 三类事件实时推送
- `search.completed` 用 `defer` 包裹，**保证**成功 / 失败都发出（payload 含 `success` + `error` 字段）

---

## 5. 数据存储

### 5.1 PostgreSQL

用于持久化存储：
- `court_sessions`：庭审会话
- `agents`：Agent 配置与状态
- `evidences`：用户提交的证据（**仅**用户输入）
- `investigation_findings`：v0.3 新增 — 调查员被派遣后的搜索结果（与 evidences 严格分离）
- `messages`：庭审消息记录
- `verdicts`：判决书
- `belief_states`：信念状态快照
- `a2a_messages`：Agent 间消息日志
- `private_memories`：各 Agent 私有记忆

### 5.2 Redis

用于：
- 庭审会话缓存
- WebSocket 连接订阅管理
- 限流（如搜索调用频率）
- 临时任务队列

### 5.3 数据模型原则

- 庭审状态以数据库为准，Redis 只做缓存。
- 每场庭审有一个 `session_id` 作为主键。
- Agent 输出、A2A 消息、证据全部记录，保证可审计。
- 私有记忆按 `agent_id` 严格隔离，任何查询必须同时携带 `session_id` 与 `agent_id`。

---

## 6. LLM 选型详细说明

### 6.1 为什么选 DeepSeek

| 维度 | DeepSeek-V3 | DeepSeek-R1 |
|---|---|---|
| 中文能力 | 强 | 强 |
| 价格 | 低 | 较低 |
| 上下文长度 | 64K | 64K |
| OpenAI 兼容 | 是 | 是 |
| 推理能力 | 良好 | 强（有思维链）|
| 速度 | 快 | 较慢 |

### 6.2 模型使用策略

| 场景 | 模型 | 原因 |
|---|---|---|
| 开庭陈述、常规质证 | DeepSeek-V3 | 成本低、响应快 |
| 复杂反驳、最终判决 | DeepSeek-R1 | 推理更深入 |
| 证据可信度评估 | DeepSeek-V3 | 结构化输出足够 |
| 判决书生成 | DeepSeek-R1 | 需要综合推理 |

### 6.3 备选方案

如果 DeepSeek 不稳定或不可用：
1. **阿里通义千问 Qwen-Max**：中文和工具调用稳定。
2. **智谱 GLM-4-Flash**：价格低，适合快速轮次。
3. **OpenAI GPT-4o-mini**：如果你后续拿到 API，可直接切换。

代码层面通过 `llm.Client` 接口抽象，切换模型只需改配置。

### 6.4 Agent Gateway 设计

> **v0.5+ 状态**：白盒子集（统一接入 + 审计 + trace 关联）与高级能力（Prompt 压缩 / Token 预算 / 限流 / Fallback / 文件日志）均已实装，模块位于 `backend/internal/agent_gateway/`，由 `agent_gateway.NewWithConfig(llmClient, recorder, model, cfg)` 装饰。模型路由 / 响应缓存仍留到第二阶段。
>
> 装饰链：`llm.NewClient() → agent_gateway.NewWithConfig(...) → agent.NewOrchestrator / evidence.NewService / agent.NewReActRunner`。
>
> ctx 注入点：orchestrator 的 `traceFor` 函数 + react_runner 的 `AgentGatewayTrace` RunnerConfig 字段。
>
> 环境变量开关：`AGENT_GATEWAY_ENABLED` 总开关，为 true 时默认开启所有子能力；`AGENT_GATEWAY_PROMPT_COMPRESSION` / `TOKEN_BUDGET` / `THROTTLING` / `FALLBACK` / `FILE_LOGGER` 可单独关闭；关闭所有开关时只保留审计落库（白盒子集行为）。

Agent Gateway 是位于 Agent Orchestration 与 LLM Provider 之间的中间层，专门解决多 Agent 系统中的**成本、效率、可观测性**问题。

#### 6.4.1 核心职责

| 职责 | 说明 | 状态 |
|---|---|---|
| **模型路由** | 根据任务复杂度选择模型：开场/质证用轻量模型，最终判决/复杂推理用强模型 | ⏳ 第二阶段 |
| **Prompt 压缩** | 预算达到阈值时，保留 system + 最近 5 条消息；超长单条截断到 1500 字符；开关控制 | ✅ 已实装 |
| **Token 预算** | 按 `session_uuid` 维护内存已用 token；默认 20000/庭审；开关控制 | ✅ 已实装 |
| **响应缓存** | 缓存相似请求的响应，减少重复调用 | ⏳ 第二阶段 |
| **限流与降级** | 超预算时降低 `max_tokens` / `temperature`；API 失败时 500ms/1s/2s 退避重试 3 次 | ✅ 已实装 |
| **调用审计** | 写 `llm_calls` 表：model / provider / token / latency / status / error / trace | ✅ 已实装 |
| **文件日志** | `backend/logs/agent_gateway_YYYY-MM-DD.log`，JSON 每行，含压缩/限流/重试/预算字段，用于对比实验 | ✅ 已实装 |

#### 6.4.2 可量化优化指标

Agent Gateway 需要输出可量化的成本优化数据：

| 指标 | 定义 | 示例目标 |
|---|---|---|
| **Token 节省率** | （未优化 Token - 实际 Token）/ 未优化 Token | ≥ 30% |
| **模型降级率** | 简单任务使用轻量模型的比例 | ≥ 60% |
| **缓存命中率** | 命中缓存的请求占比 | ≥ 20% |
| **平均单次庭审成本** | 完整庭审的 LLM 调用成本 | ≤ 0.5 元 |
| **P95 延迟** | 95% 请求的响应延迟 | ≤ 3s |
| **模型切换成功率** | 主模型失败时切换到备用模型的成功率 | ≥ 99% |

#### 6.4.3 路由策略示例

```go
func RouteModel(task TaskType, complexity float64, budget TokenBudget) ModelConfig {
    switch task {
    case TaskOpeningStatement, TaskRoutineRebuttal:
        if complexity < 0.6 {
            return DeepSeekV3Lite  // 轻量、便宜
        }
        return DeepSeekV3
    case TaskFinalVerdict, TaskComplexReasoning:
        return DeepSeekR1         // 强推理
    default:
        return DeepSeekV3
    }
}
```

#### 6.4.4 压缩策略

- **历史摘要**：超过 N 轮后，将早期对话压缩为摘要。
- **证据去重**：相同证据不重复注入上下文。
- **结构化上下文**：用 JSON/表格替代自然语言描述，减少 Token。
- **动态截断**：按 Token 预算截断低相关性内容，优先保留证据和信念状态。

---

## 7. WebSearch 方案

### 7.1 抽象接口

```go
type SearchProvider interface {
    Name() string
    Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error)
}

type SearchResult struct {
    Query   string        `json:"query"`
    Results []SearchItem  `json:"results"`
}

type SearchItem struct {
    Title       string  `json:"title"`
    URL         string  `json:"url"`
    Content     string  `json:"content"`
    Score       float64 `json:"score"`
    SourceType  string  `json:"source_type"`
}
```

### 7.2 开发与部署切换

| 环境 | Provider | 说明 |
|---|---|---|
| 本地开发 | **Bocha API**（默认）/ SearXNG（Docker） | Bocha 国内可稳定访问；SearXNG 免费本地 |
| 单元测试 | MockProvider | 零依赖，可预测输出 |
| 云服务器测试 | Bocha / SearXNG / Tavily | 根据网络情况选择 |
| 生产部署 | **Bocha**（默认）/ Tavily | Bocha 国内友好；Tavily 海外质量高 |

> v0.3 修订：原 v0.2 的 DuckDuckGo Provider 因经常触发反爬已弃用。Bocha 成为默认（国内可稳定访问 + API 简洁）。

### 7.3 SearXNG 配置

在 `docker-compose.yml` 中增加：

```yaml
searxng:
  image: searxng/searxng:latest
  ports:
    - "8080:8080"
  environment:
    - BASE_URL=http://localhost:8080
    - INSTANCE_NAME=DecisionCourtSearx
```

后端通过 `http://searxng:8080/search?q=xxx&format=json` 调用。

---

## 8. 实时通信设计

### 8.1 WebSocket 事件类型

```typescript
type CourtEvent =
  // 用户证据
  | { type: 'evidence.added'; evidence: Evidence }
  | { type: 'evidence.challenged'; evidence_id: string; agent_id: string; reason: string }
  // Agent 发言
  | { type: 'agent.thinking_started'; agent_id: string; agent_type: AgentType }   // v0.3
  | { type: 'agent.cot_step'; agent_id: string; agent_type: AgentType; step: CotStep }  // v0.3
  | { type: 'agent.speak_chunk'; agent_id: string; agent_type: AgentType; accumulated: string }  // v0.3
  | { type: 'agent.thinking_finished'; agent_id: string; agent_type: AgentType }  // v0.3
  | { type: 'agent.speak'; agent_id: string; agent_type: AgentType; content: string; evidence_refs: string[] }
  | { type: 'belief.updated'; option_a: number; option_b: number }
  // 调查员
  | { type: 'search.started'; dispatcher: 'prosecutor' | 'defender'; query: string }  // v0.3 修订
  | { type: 'search.completed'; dispatcher: 'prosecutor' | 'defender'; query: string; success: boolean; finding_id?: string; result_count?: number; summary?: string; raw_results?: SearchItem[]; error?: string }
  | { type: 'a2a.message'; message_uuid: string; from: string; to: string; message_type: 'speech' | 'evidence' | 'challenge' | 'inquiry' | 'verdict_task' | 'dispatch_investigator' | 'investigation_report'; visibility: 'public' | 'private'; payload?: any }
  // 阶段 & 判决
  | { type: 'phase.changed'; phase: CourtPhase }
  | { type: 'verdict.ready'; verdict: Verdict }
  | { type: 'user.action.required'; action: UserAction }
```

### 8.2 连接管理

- 用户进入 `/court/[id]` 时建立 WebSocket 连接。
- 后端按 `session_id` 将连接加入 room。
- Agent 输出通过 room 广播给所有旁观者（MVP 只有用户自己）。

---

## 9. 部署方案

### 9.1 本地开发

```bash
git clone <repo>
cd decisioncourt
cp .env.example .env
# 填写 DEEPSEEK_API_KEY
docker-compose up -d
```

启动服务：
- 前端：http://localhost:3000
- 后端 API：http://localhost:8080
- SearXNG：http://localhost:8081
- PostgreSQL：localhost:5432
- Redis：localhost:6379

### 9.2 云服务器部署

1. 准备一台 Linux 服务器（推荐 2核4G 以上）。
2. 安装 Docker + Docker Compose。
3. 拉取代码，配置 `.env`。
4. `docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d`

### 9.3 环境变量示例

```bash
# LLM
LLM_PROVIDER=deepseek
LLM_API_KEY=sk-xxx
LLM_BASE_URL=https://api.deepseek.com/v1
LLM_MODEL_V3=deepseek-chat
LLM_MODEL_R1=deepseek-reasoner

# Search
SEARCH_PROVIDER=searxng
SEARXNG_URL=http://searxng:8080
# 部署时切换
# SEARCH_PROVIDER=tavily
# TAVILY_API_KEY=tvly-xxx

# Database
DATABASE_URL=postgres://user:pass@postgres:5432/decisioncourt
REDIS_URL=redis://redis:6379

# Server
PORT=8080
JWT_SECRET=xxx
```

### 9.4 第二阶段分布式与高可用架构（非 MVP）

当系统需要支持更高并发或作为公开服务运行时，引入以下架构升级。

#### 9.4.1 目标场景

- 同时在线庭审数 > 100
- 单节点 WebSocket 连接数达到上限
- LLM 调用需要削峰填谷
- 需要 99.9% 可用性

#### 9.4.2 分布式架构图

```
                    ┌─────────────┐
                    │   CDN       │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  Nginx/     │
                    │  Traefik    │
                    │  (LB)       │
                    └──────┬──────┘
                           │
            ┌──────────────┼──────────────┐
            │              │              │
     ┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
     │  backend-1  │ │ backend-2  │ │ backend-n  │
     │  (Gin + WS) │ │  (Gin+WS)  │ │  (Gin+WS)  │
     └──────┬──────┘ └─────┬──────┘ └─────┬──────┘
            │              │              │
            └──────────────┼──────────────┘
                           │
                    ┌──────▼──────┐
                    │ Redis       │
                    │ Pub/Sub     │
                    │ + Queue     │
                    └──────┬──────┘
                           │
            ┌──────────────┼──────────────┐
            │              │              │
     ┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
     │  worker-1   │ │  worker-2  │ │  worker-n  │
     │ (LLM Agent) │ │ (LLM Agent)│ │ (LLM Agent)│
     └─────────────┘ └────────────┘ └────────────┘

┌─────────────────────────────────────────────────────────────┐
│  PostgreSQL 主从 + Redis Sentinel / Cluster                 │
└─────────────────────────────────────────────────────────────┘
```

#### 9.4.3 关键改造点

| 改造点 | 方案 | 收益 |
|---|---|---|
| **负载均衡** | Nginx / Traefik 反向代理多个 backend 实例 | 水平扩展 |
| **WebSocket 分布式广播** | Redis Pub/Sub 跨实例同步庭审事件 | 用户连任意节点都能收到消息 |
| **LLM 调用异步化** | Redis Stream / RabbitMQ 任务队列 + Worker 消费 | 削峰填谷、避免阻塞主服务 |
| **数据库高可用** | PostgreSQL 主从复制 + 读写分离 | 读性能提升、故障切换 |
| **缓存高可用** | Redis Cluster 或 Sentinel | 会话状态不丢失 |
| **限流熔断** | 令牌桶限流 + 熔断器保护 LLM API | 防止雪崩 |

#### 9.4.4 异步 LLM 调用流程

```
1. 用户触发 Agent 发言
2. backend 将任务写入 Redis Stream / RabbitMQ
3. worker 消费任务，调用 LLM
4. worker 将结果写回 PostgreSQL
5. backend 通过 Redis Pub/Sub 广播到对应 room
6. 前端收到 WebSocket 事件
```

#### 9.4.5 实施时机

- **MVP 不做**：单节点 + 同步调用足以支撑开发和演示。
- **公开测试前做**：当需要邀请多用户测试时，先做 WS 分布式广播。
- **商业化前做**：完整的高可用架构在商业化前实现。

---

## 10. 开发工具链

| 工具 | 用途 |
|---|---|
| VS Code / Cursor | IDE |
| Air（Go）| 后端热重载 |
| pnpm | 前端包管理 |
| golangci-lint | Go 代码检查 |
| Prettier + ESLint | 前端代码格式化 |
| Makefile | 统一命令 |
| GitHub Actions（可选）| CI/CD |

---

## 11. 测试架构与运行规范

### 11.1 测试分层

| 层 | 文件后缀 | build tag | 依赖 | 运行命令 |
|---|---|---|---|---|
| 单元测试 | `*_test.go` | （无） | in-memory fake / interface stub | `go test ./...` |
| 集成测试 | `*_integration_test.go` | `//go:build integration` | 真实 PostgreSQL + 真实 LLM API key（来自 .env） | `go test -tags integration ./...` |
| 前端测试 | （暂未配置） | — | — | — |

集成测试默认在 `internal/courtroom/` 包内，模拟完整庭审流程（opening → cross_exam → closing → deliberation），每个场景会把状态 JSON 写到 `backend/test-output/<scenario>-<sessionUUID>.json` 供复盘。

### 11.2 测试文件组织约定（courtroom 包参考）

| 类型 | 文件命名 | 内容 |
|---|---|---|
| 单元测试 | `service_*_test.go` / `dispatch_*_test.go` | 业务断言，使用 `fakes_test.go` 中的 fake |
| 共享 fake | `fakes_test.go` | `stubSearcher` / `streamingLLM` / `reactScriptedLLM` / `buildDispatchService` / `buildStreamingSpeakService` 等可复用 fixture |
| 集成公共 | `integration_helpers_test.go` | `testFixture` / `testState` / 公共断言 helpers |
| 集成场景 | `integration_full_flow_test.go` / `integration_evidence_test.go` / `integration_round_test.go` | 单个端到端场景，每个对应一种 JSON 产物前缀 |

> **原则**：不要在多个测试文件里重复实现 fake。新建场景前先检查 `fakes_test.go` 是否已经有可复用 fixture。

### 11.3 新增测试 checklist

1. **优先单测**：能用 fake 验证的，不要上升到集成测试。
2. **集成测试场景**：放 `integration_*_test.go` 后缀，遵守 AGENTS.md §3.2。
3. **Fake 复用**：放到 `fakes_test.go`，避免散落到具体业务测试文件。
4. **测试输出**：写到 `backend/test-output/`，该目录已在 `.gitignore`。

### 11.4 运行命令速查

```bash
# 单元测试（CI 友好，无需 PG / LLM）
go test ./...

# 单元 + 集成（在 docker 启动 PG 之后）
go test -tags integration ./...

# 集成子集（例如只跑"中途提交证据"那个 case）
go test -tags integration -run TestSubmitEvidence ./internal/courtroom/

# 跳过集成
go test -short ./...
```

---

## 12. 风险与备选方案

| 风险 | 影响 | 备选方案 |
|---|---|---|
| DeepSeek API 不稳定 | 庭审中断 | 快速切换到 Qwen / GLM-4 |
| SearXNG 搜索结果差 | 证据质量低 | 开发期也接 Tavily 免费层 |
| Agent 输出过长 | Token 成本高、前端渲染慢 | 限制输出长度，关键轮次才用 R1；Agent Gateway Prompt 压缩 / Token 预算 / 限流已实装，可开关对比 |
| Agent 互相附和或叛变 | 辩论失去对抗性 | A2A 上下文隔离 + 私有记忆池 + 信念引擎 + 立场一致性检查 |
| 私有记忆越权泄露 | 策略污染、信息博弈 | Orchestrator 访问控制 + 审计日志 + 单元测试 |
| WebSocket 连接数多 | 服务器压力大 | MVP 限单会话，后期加连接池 |
| 前端可视化性能差 | 复杂图表卡顿 | 用 Canvas 替代 SVG，或分页渲染 |

---

## 13. 已确认的技术选型总结

| 层级 | 最终选择 |
|---|---|
| 前端框架 | Next.js 14 App Router + TypeScript |
| 前端样式 | Tailwind CSS + shadcn/ui |
| 前端可视化 | React-Flow + Recharts |
| 前端状态 | Zustand |
| 后端语言/框架 | Go 1.22+ + Gin |
| ORM | GORM |
| 数据库 | PostgreSQL 15+ |
| 缓存 | Redis 7+ |
| WebSocket | gorilla/websocket |
| Agent 通信协议 | A2A（Agent-to-Agent）消息总线 |
| 记忆模型 | 公共证据板 + 按 Agent 隔离的私有记忆池 |
| LLM | DeepSeek-V3（默认）+ DeepSeek-R1（关键轮次）|
| LLM SDK | go-openai（兼容 OpenAI 格式）|
| Agent Gateway（v0.5+） | 统一接入、审计、Prompt 压缩、Token 预算、限流、Fallback、文件日志 |
| 搜索（开发）| SearXNG Docker |
| 搜索（部署）| Tavily API |
| 部署 | Docker Compose |
| 认证 | 无需登录，匿名会话 |

---

## 14. 当前状态与下一步

### 14.1 已落地到文档的架构

- ✅ A2A 消息总线：消息结构、路由职责、上下文隔离规则。
- ✅ 私有记忆池：数据表设计、按 Agent 隔离的访问控制、注入逻辑。
- ✅ 后端目录结构：新增 `internal/a2a` 与 `internal/private_memory` 模块。
- ✅ 风险与缓解：Agent 附和/叛变、私有记忆泄露的应对方案。

### 14.2 代码现状（截至 2026-06-29）

| 设计点 | 状态 | 代码位置 |
|---|---|---|
| A2A 消息总线 | ✅ 已实装 | `internal/a2a/` + `a2a_messages` 表 + 12 项隔离测试 |
| 私有记忆池 | ✅ 已实装 | `internal/private_memory/` + `private_memories` 表 + 9 项隔离测试 |
| 调查员派遣 | ✅ 已实装 | `internal/investigation/` + `investigation_findings` 表 + 10 项测试 |
| ReAct runner | ✅ 已实装 | `internal/agent/react_runner.go` + OnIterStart / OnSpeakChunk 钩子 |
| LLM 流式 | ✅ 已实装 | `internal/llm/client.go` StreamComplete + 后端 hub.Broadcast sleep 30ms + 前端 flushSync |
| Bocha 搜索 | ✅ 已实装 | `internal/search/bocha.go` |
| A2A `dispatch_investigator` / `investigation_report` 公开可见 | ✅ 已实装 | `internal/a2a/message.go` MessageType 常量 |
| WebSocket ReAct 事件 | ✅ 已实装 | `agent.thinking_started/finished`、`agent.cot_step`、`agent.speak_chunk` |
| **v0.5 私有 4 个 MessageType**（PR 1 ✅ 已实装） | ✅ 已实装 | `internal/a2a/types.go` 加 `MessageTypeStrategyNote` / `MessageTypeOpponentWeakness` / `MessageTypeSelfCorrection` / `MessageTypeEvidenceEval` |
| **v0.5 ContextView 投影层**（PR 1 ✅ 已实装） | ✅ 已实装 | `internal/a2a/context_view.go` 加 `LLMContext` struct + `Bus.BuildContextView()` + `Bus.SanitizeForViewer()` + `ErrNotVisible` / `ErrMalformedPayload`，10 项单测覆盖 |
| **v0.5 Orchestrator Prompt 注入**（PR 3 ✅ 已实装） | ✅ 已实装 | `internal/agent/orchestrator_context.go` 加 `buildEpisodicMemoryBlock()` + `extractMemoryPayload()`；`orchestrator.lawyerSpeakReAct` 在 `withArgumentSummaryText` 之后注入 `## 你之前的策略笔记` 段落；10 项单测覆盖（含 3 项集成测试） |
| **v0.5 ReAct reflect 自动写记忆**（PR 2 ✅ 已实装） | ✅ 已实装 | `internal/agent/reflect_classifier.go` + `MemoryHook` + RunnerConfig.MemoryHook + orchestrator.makeMemoryHook；14 项单测覆盖（含 4 种 memory_type 全跑通的 3 轮集成测试） |
| **v0.5 前端 MemoryAuditPanel**（PR 4 ✅ 已实装） | ✅ 已实装 | 新增 `components/courtroom/MemoryAuditPanel.tsx` + `MemoryTimeline.tsx` + `BehindTheScenesPanel.tsx`；`types/index.ts` 加 `MemoryKind` + `MemoryEntry`；`store/courtroomStore.ts` 加 `memoryEntries` + `realCourthouseMode` + 4 个 reducer + `applyCourtEvent` 的 `a2a.message` handler hydrate；`court/[id]/page.tsx` 加第三个 tab「策略笔记」+ toggle；`verdict/[id]/page.tsx` 加幕后视角面板（强制 unredacted）；`next build` + `tsc --noEmit` 通过 |
| **v0.5 数据迁移**（PR 4 后启动） | ⏳ 计划 | Expand-Contract 双写过渡期 |
| **v0.5+ SessionUUID 房间钥匙修复** | ✅ 已实装 | `a2a/types.go` 加 `SessionUUID` 字段 + `a2a/bus.go` 优先用 `SessionUUID` 当 room key + `orchestrator.go` `recordSideEffects` 2 处填字段 + `bus_test.go` 加 2 项 schema freeze 测试 |
| **v0.5+ MemoryEntry 结构化字段** | ✅ 已实装 | 后端 `recordSideEffects` payload 拆 `stance` / `confidence` / `reasoning`；前端 `MemoryEntry` type 加 3 个可选字段 + `MemoryTimeline` 渲染结构化卡片 |
| **v0.5+ AgentAvatar 思考脉冲动画** | ✅ 已实装 | `globals.css` `@keyframes ring-think-pulse` 1.6s 呼吸；`.thinking-prosecutor` 绛红 / `.thinking-defender` 深青 |
| **v0.5+ 前端 envelope 字段名修正** | ✅ 已实装 | `courtroomStore.ts` 读 `p.from`（后端字段名），加 `mapFromToAgentType()` 归一化函数处理 agent UUID 残留 |

### 14.3 后续可改进（不在 MVP 范围）

1. **Agent Gateway 第二阶段**：模型路由、响应缓存（v0.5+ 已实装统一接入 / 审计 / Prompt 压缩 / Token 预算 / 限流 / Fallback / 文件日志）
2. **智能收敛**：连续两轮信念度变化 < 5% 提前进入 closing
3. **信念引擎动态更新**：当前初始化 + snapshot 已做，未基于证据实时更新
4. **Bocha 结果引用图谱**：在 InvestigatorPanel 里把搜索结果按相关性排序
5. **ReAct 多轮反思**：当前 LLM 自带反思，MaxReflects=3 已预留 hook 可继续调优

详细 UX 与数据模型细化：[decisioncourt-ux-refinement.md](./decisioncourt-ux-refinement.md)

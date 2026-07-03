# 决策庭 · DecisionCourt

> **让 AI 像法庭一样帮你把复杂决策看全、看透、看出可执行结论。**

[![MVP Status](https://img.shields.io/badge/status-MVP%20Complete-brightgreen)](./docs/README.md)
[![Backend Tests](https://img.shields.io/badge/backend%20tests-167%2B%20passing-success)](./backend)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#许可证)
[![Next.js](https://img.shields.io/badge/Next.js-14-black)](https://nextjs.org)
[![Go](https://img.shields.io/badge/Go-1.22-00ADD8)](https://go.dev)

面对重大决策（跳槽/创业/投资/路线选择）时，你不再需要求单一 AI 给一个"顺滑但片面"的答案。**决策庭**以"法庭"为隐喻，让多个专业化 AI Agent 分别扮演**控方、辩方、调查员、书记员**，围绕候选选项进行结构化对抗辩论；你作为"法官/当事人"可以实时提交证据、传唤调查、打断质询；最终系统输出一份可审计、可执行的《决策判决书》。

---

## 一图流：决策庭怎么工作

```
                       你（法官）
                          │
                  立案 │ 提交证据 │ 继续/直接判决
                          ▼
   ┌──────────────────────────────────────────────┐
   │         庭审（按"法庭"形式编排）             │
   │                                              │
   │   ┌──────────┐  ┌──────────┐                 │
   │   │ 控方律师  │←→│ 辩方律师  │  ← ReAct 循环  │
   │   │  Pro-A    │  │  Pro-B    │   多轮对抗辩论 │
   │   └────┬─────┘  └─────┬────┘                 │
   │        │   信念引擎 ↓ │                      │
   │        │   belief_diffs │（每条证据审计 trail）│
   │        └──────┬─────────┘                    │
   │               ▼                              │
   │         ┌──────────┐                         │
   │         │ 调查员   │  ← Bocha / SearXNG      │
   │         └─────┬────┘                         │
   │               ▼                              │
   │         ┌──────────┐                         │
   │         │ 书记员   │  ← 结构化判决书          │
   │         └──────────┘                         │
   └──────────────────────────────────────────────┘
                          ▼
              《决策判决书》+ trial_summary
                  + 导出 JSON / PDF
```

---

## 🌟 核心亮点

### 1. 多 Agent 对抗辩论，而非单一回答

四类专业 Agent 围绕你的决策展开结构化辩论：

| 角色 | 职责 |
|---|---|
| **控方** | 强力支持选项 A |
| **辩方** | 强力反对 / 维护选项 B |
| **调查员** | 主动检索外部信息（Bocha / SearXNG） |
| **书记员** | 整理庭审 + 生成判决书 |
| **法官** | **你自己** —— 实时插证据、做最终裁决 |

### 2. 三层防线防止 Agent 互相附和

1. **信念引擎（Belief Engine）** v0.6 —— 贝叶斯 log-odds + 锚定 + weaken 边；每条证据变化都写 `belief_diffs` 审计 trail
2. **A2A 消息总线 + ContextView 投影** —— 控辩双方**互相看不到对方的私有推理链**（`payload.reasoning`）
3. **情节记忆（Episodic Memory）** —— 每方 Agent 维护自己的私有策略池，通过 A2A 私有通道写入，对方永远看不到

### 3. 流式 + 可审计的实时体验

- **逐字流式发言**：LLM token 实时推送到前端气泡
- **完整可回放**：所有 A2A 消息、ReAct 步骤、私有策略笔记均持久化 + WebSocket 广播
- **幕后视角**：判决书页解锁"幕后视角" Tab，展示双方 Agent 私有策略演化路径

### 4. 智能收敛

每轮质证后计算信念度变化 Δ，多信号按优先级触发提前进入结案 —— **避免拖沓，节省 Token**。

### 5. Agent Gateway：白盒化的 LLM 调用

v0.5+ 实装"白盒子集"（统一接入 + 审计落库 + trace 关联），v0.6 实装"高级能力"（Prompt 压缩 / Token 预算 / 限流 / Fallback / JSON 文件日志）。v2 实装 **Smart Compression**（评分 + 原子组 + 贪心打包 + 兜底摘要）和 **Token Budget Reject**。

> 压缩策略对比详见 [`docs/adr/0006-smart-prompt-compression.md`](./docs/adr/0006-smart-prompt-compression.md)

---

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
git clone https://github.com/<your-username>/DecisionCourt.git
cd DecisionCourt

cp .env.example .env
# 编辑 .env，至少填入 LLM_API_KEY（DeepSeek / Kimi）

docker compose up -d

# 访问
# 前端:    http://localhost:3000
# 后端:    http://localhost:8080/health
# SearXNG: http://localhost:8081
```

### 方式二：本地开发

前置要求：**Node.js 20+** / **pnpm** / **Go 1.22+** / **PostgreSQL 15+**

```bash
# 1. 启动 PostgreSQL
brew services start postgresql@15
createdb decisioncourt

# 2. 启动后端
cd backend
go run cmd/server/main.go
# 监听 http://localhost:8080

# 3. 启动前端（新终端）
cd frontend
pnpm install
pnpm dev
# 访问 http://localhost:3000
```

---

## 架构总览

```
┌─────────────────────────────────────────────┐
│       Frontend (Next.js 14 + shadcn/ui)     │
│  Zustand · WebSocket · React-Flow · Recharts │
└──────────────┬──────────────────────────────┘
               │ HTTP + WebSocket
┌──────────────▼──────────────────────────────┐
│         API Gateway (Go + Gin)              │
│    REST handler + Hub (room-based)          │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│     Courtroom Service  (状态机)             │
│  idle→opening→cross_exam→closing→verdict    │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│      Agent Orchestrator + ReAct Runner      │
│                                              │
│  ┌────────┐ ┌────────┐ ┌────────────┐ ┌────┐│
│  │控方    │ │辩方    │ │调查员      │ │书记││
│  │+ReAct  │ │+ReAct  │ │+WebSearch  │ │员  ││
│  └───┬────┘ └───┬────┘ └──────┬─────┘ └────┘│
│      │          │             │             │
│  ┌───▼──────────▼─────────────▼─────────────▼┐
│  │  A2A Bus  +  Episodic Memory  + Belief  │
│  │  ContextView 投影 · 私有通道隔离          │
│  └──────────────────────────────────────────┘
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│  Agent Gateway (白盒子集 + v2 高级能力)    │
│  审计 + Prompt 压缩 + Token 预算 + 限流     │
└──────────────┬──────────────────────────────┘
               │
┌──────────────▼──────────────────────────────┐
│  LLM Client · Search Providers · PostgreSQL │
│  DeepSeek / Kimi · Bocha / SearXNG / Mock   │
└─────────────────────────────────────────────┘
```

---

## 核心机制

### A2A 消息总线 + ContextView 投影

每个 Agent 只能收到它应该收到的消息。Orchestrator 通过 `BuildContextView(selfAgent)` 在生成对方 prompt 前剥离 `reasoning` 字段 —— **控辩双方永远看不到对方的内部推理链**。

```json
{
  "from": "prosecutor",
  "to": "orchestrator",
  "message_type": "speech",
  "visibility": "public",   // 或 "private"
  "payload": {
    "content": "正式发言...",
    "reasoning": "内部推理（仅审计可见）",
    "stance": "pro_a",
    "confidence": 0.82,
    "evidence_refs": ["E001"]
  }
}
```

### 信念引擎（Belief Engine）

每个 Agent 维护对两个选项的信念度 `belief_A + belief_B = 1`：

| Agent | 初始 | 更新规则 |
|---|---|---|
| 控方 | `belief_A = 0.7` | 锚定 0.7 + 支持 A 的证据 → 提升 `belief_A` |
| 辩方 | `belief_A = 0.3` | 锚定 0.7 + 支持 A 的证据 → 降低 `belief_A` |
| 调查员 | `belief_A = 0.5` | 按搜索结果动态调整 |

v0.6 升级为**贝叶斯 log-odds + 锚定**，抗单条证据翻转；详见 [`docs/adr/0004-bayesian-belief-engine.md`](./docs/adr/0004-bayesian-belief-engine.md)

### 调查发现 vs 用户证据

| 维度 | 用户证据 | 调查发现 |
|---|---|---|
| 来源 | 用户手动提交 | LLM 派遣调查员搜索 |
| 落地表 | `evidences` | `investigation_findings` |
| UI 位置 | EvidenceBoard | InvestigatorPanel（独立 Tab） |
| 是否影响信念 | ✅ 是 | ❌ 否（仅作 LLM 上下文） |
| 是否引用进发言 | ✅ 是 | ❌ 否 |

---

## 项目结构

```
DecisionCourt/
├── docs/                          # 完整设计文档
│   ├── README.md                  # 文档索引 + 实装状态矩阵
│   ├── adr/                       # 9 份架构决策记录
│   ├── archive/                   # 已完成的详细设计文档
│   └── decisioncourt-*.md         # 8 份主项目文档
│
├── backend/                       # Go + Gin 后端
│   ├── cmd/server/main.go         # 装配入口
│   ├── internal/
│   │   ├── a2a/                   # A2A Bus + ContextView
│   │   ├── agent/                 # Orchestrator + ReAct + Prompts
│   │   ├── agent_gateway/         # 白盒子集 + v2 高级能力
│   │   ├── api/                   # REST + WebSocket Hub
│   │   ├── belief/                # v0.6 贝叶斯引擎
│   │   ├── courtroom/             # 状态机 + Service
│   │   ├── evidence/              # 用户证据
│   │   ├── investigation/         # 调查发现
│   │   ├── private_memory/        # 私有记忆池
│   │   ├── search/                # Bocha / SearXNG / DuckDuckGo
│   │   ├── llm/                   # DeepSeek / Kimi 客户端
│   │   └── model/                 # GORM models
│   └── test-output/               # 端到端测试 JSON 样本
│
├── frontend/                      # Next.js 14 前端
│   ├── app/                       # 立案页 / 庭审页 / 判决书页
│   ├── components/courtroom/      # AgentAvatar · ArgumentMap · EvidenceBoard
│   ├── hooks/                     # usePhaseUI
│   ├── lib/                       # api · websocket · mock
│   ├── store/                     # Zustand store
│   └── types/
│
├── docker-compose.yml             # PG + Redis + SearXNG + 前后端
├── .env.example                   # 环境变量模板
└── README.md                      # 本文件
```

---

## 环境变量

复制 `.env.example` 为 `.env` 后按需修改：

| 变量 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `LLM_PROVIDER` | 是 | `deepseek` | `deepseek` / `kimi`（OpenAI 兼容） |
| `LLM_API_KEY` | 是 | - | LLM API 密钥 |
| `LLM_BASE_URL` | 否 | DeepSeek 官方 | LLM API 基础地址 |
| `LLM_MODEL_V3` | 否 | `deepseek-chat` | 常规轮次模型 |
| `LLM_MODEL_R1` | 否 | `deepseek-reasoner` | 关键轮次推理模型 |
| `SEARCH_PROVIDER` | 否 | `searxng` | `mock` / `searxng` / `bocha` / `tavily` |
| `BOCHA_API_KEY` | 视 provider | - | Bocha 搜索 key（国内友好） |
| `DATABASE_URL` | 是 | - | PostgreSQL 连接字符串 |
| `REDIS_URL` | 否 | - | Redis 连接字符串（高可用时使用） |
| `PORT` | 否 | `8080` | 后端端口 |
| `JWT_SECRET` | 否 | `decisioncourt-secret` | JWT 密钥 |

### 切换 LLM 厂商

```bash
# DeepSeek（默认）
LLM_PROVIDER=deepseek
LLM_API_KEY=sk-xxx
LLM_BASE_URL=https://api.deepseek.com/v1
LLM_MODEL_V3=deepseek-chat
LLM_MODEL_R1=deepseek-reasoner

# Kimi（Moonshot）
LLM_PROVIDER=kimi
LLM_API_KEY=sk-xxx
LLM_BASE_URL=https://api.moonshot.cn/v1
LLM_MODEL_V3=moonshot-v1-8k
LLM_MODEL_R1=moonshot-v1-32k
```

### Agent Gateway 高级能力（可选）

```bash
AGENT_GATEWAY_ENABLED=true
AGENT_GATEWAY_PROMPT_COMPRESSION=true
AGENT_GATEWAY_SMART_COMPRESSION=true      # v2 Smart 评分压缩
AGENT_GATEWAY_TOKEN_BUDGET=true
AGENT_GATEWAY_REJECT_WHEN_EXHAUSTED=true  # v2 默认 true，账单可预测
AGENT_GATEWAY_THROTTLING=true
AGENT_GATEWAY_FALLBACK=true
AGENT_GATEWAY_FILE_LOGGER=true
```

---

## 测试

```bash
cd backend
go test ./internal/... -v
```

**当前状态**：167 项测试全部通过，覆盖：

- `internal/a2a`：12 项（Bus 路由 + ContextView 投影 + SessionUUID 房间钥匙回归测试）
- `internal/private_memory`：9 项（隔离性 + Orchestrator 集成）
- `internal/investigation`：10 项（dispatch + 持久化 + 公开广播）
- `internal/agent`：ReAct Runner + Reflect Classifier + Prompt 注入
- `internal/courtroom`：State Machine + DispatchInvestigator + Speak Streaming
- `internal/search`：Bocha / DuckDuckGo Provider
- `internal/api`：Hub 流式时序
- `internal/agent_gateway`：63 项（Token Budget v2 / Smart Compression v2 / Gateway reject + 三策略压缩对比 baseline）

端到端样本位于 `backend/test-output/`（每个集成测试场景对应一份 JSON 状态）。

---

## 文档

完整设计文档位于 [`docs/`](./docs)：

- **索引**：[`docs/README.md`](./docs/README.md) — 文档结构 + 实装状态矩阵
- **8 份主文档**：[`docs/decisioncourt-prd.md`](./docs/decisioncourt-prd.md) · [`decisioncourt-tech-spec.md`](./docs/decisioncourt-tech-spec.md) · [`decisioncourt-agent-design.md`](./docs/decisioncourt-agent-design.md) · [`decisioncourt-api-design.md`](./docs/decisioncourt-api-design.md) · [`decisioncourt-db-design.md`](./docs/decisioncourt-db-design.md) · [`decisioncourt-roadmap.md`](./docs/decisioncourt-roadmap.md) · [`decisioncourt-ux-refinement.md`](./docs/decisioncourt-ux-refinement.md) · [`project-ideas.md`](./docs/project-ideas.md)
- **9 份 ADR**：[`docs/adr/`](./docs/adr/) — 每个关键决策的"为什么"
- **6 份归档**：[`docs/archive/`](./docs/archive/) — 已完成的详细设计文档

---

## 路线图

### ✅ MVP 已完成

- 四 Agent 对抗辩论 + 信念引擎 + 智能收敛
- A2A 消息总线 + ContextView 投影 + 私有通道
- 情节记忆（Episodic Memory via A2A）
- ReAct 循环 + 流式 LLM + 调查员实时搜索
- 用户证据 + 调查发现独立表
- 庭审模式选择（快速 / 标准 / 深度）
- 极简白底法庭风格 UI + 凹陷输入框
- MemoryAuditPanel（前端可审计）+ 幕后视角页
- Agent Gateway v0.5+（白盒子集）+ v0.6（高级能力）+ v2（Smart + Reject）
- 文档整合（8 份主文档 + 9 份 ADR + 6 份归档）

### 🚧 第二阶段（不在 MVP）

- 后端白盒化（trace-id 串联 / 决策日志 / 性能指标）
- 后端高可用（多实例 + WS 分布式广播 + Redis Pub/Sub + LLM 异步化 + 数据库主从 + 熔断降级）
- 并发防护（同一 session 互斥 / 用户快速点击幂等 / LLM 超时与重试 / agent 死锁检测）
- Agent Gateway 模型路由 / 响应缓存
- 强制立场一致性检查（LLM-as-judge 打回重生成）
- 新意度检查 / 发言长度硬截断 / 已反驳证据集合跟踪
- 专家证人 / 陪审团 / 历史庭审 / PDF 导出
- 预设决策模板库

---

## 许可证

本项目以 **MIT License** 开源，详见 [LICENSE](./LICENSE)。

---

## 安全说明

### ⚠️ 推送至公开仓库前必做

1. **检查 `.env`**：仓库根目录的 `.env` 文件包含本地真实 API Key，**已被 `.gitignore` 排除**，但请确认没有手动 `git add -f` 强制加入。
2. **轮换已泄露的 Key**：任何曾在本地明文存放过 LLM / Search provider 的 API Key，**强烈建议**去对应控制台轮换（rotate）后再推送。
3. **生产部署**：务必使用强随机 `JWT_SECRET`（`openssl rand -hex 32`），不要沿用 `decisioncourt-secret` 默认值。
4. **不要提交真实庭审数据**：清理 `backend/test-output/` 中可能含敏感内容的样本。

如发现潜在安全问题，请邮件联系维护者（不要公开 Issue）。

---

## 致谢

- LLM：[DeepSeek](https://www.deepseek.com/) · [Kimi (Moonshot)](https://www.moonshot.cn/)
- 搜索：[Bocha AI Search](https://bochaai.com/) · [Tavily](https://tavily.com/)
- 前端：[Next.js](https://nextjs.org/) · [shadcn/ui](https://ui.shadcn.com/) · [Tailwind CSS](https://tailwindcss.com/) · [React Flow](https://reactflow.dev/) · [Recharts](https://recharts.org/) · [Zustand](https://zustand-demo.pmnd.rs/)
- 后端：[Gin](https://gin-gonic.com/) · [GORM](https://gorm.io/) · [gorilla/websocket](https://github.com/gorilla/websocket)

---

<p align="center">
  <sub>Built with ⚖️ for anyone who has ever faced a decision too complex for a single AI answer.</sub>
</p>
# 决策庭 · DecisionCourt

> **让 AI 像法庭一样帮你把复杂决策看全、看透、看出可执行结论。**

[![MVP Status](https://img.shields.io/badge/status-MVP%20Complete-brightgreen)](./docs/decisioncourt-prd.md)
[![Backend Tests](https://img.shields.io/badge/backend%20tests-167%20passing-success)](./backend)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#许可证)
[![Next.js](https://img.shields.io/badge/Next.js-14-black)](https://nextjs.org)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8)](https://go.dev)

面对重大决策（跳槽/创业/投资/路线选择）时，你不再需要求单一 AI 给一个"顺滑但片面"的答案。**决策庭**以"法庭"为隐喻，让多个专业化 AI Agent 分别扮演**控方、辩方、调查员、书记员**，围绕候选选项进行结构化对抗辩论；你作为"法官/当事人"可以实时提交证据、传唤调查、打断质询；最终系统输出一份可审计、可执行的《决策判决书》。

> **MVP 已完成** —— 端到端跑通立案 → 开庭 → 举证 → 质证 → 调查 → 结案 → 判决 全流程。[Roadmap §15](./docs/decisioncourt-prd.md) 列出全部已实装模块。

---

## 🌟 亮点：Agent Gateway 压缩策略对比

> **一句话**：DecisionCourt 的 Agent Gateway 用 v2 **"评分 + 原子组 + 贪心打包 + 兜底摘要"** 替代 v0.5+ 的"硬丢最近 5 条"，在多角色庭审场景下既省钱又保住了因果链。

### 实验数据（同一条 12 轮庭审 transcript：19 条消息，4763 字符 ≈ 1190 tokens）

跑 [`TestCompressionEval_StrategyComparison`](./backend/internal/agent_gateway/compression_eval_test.go) 三种配置对比：

| 策略 | applied | 消息 19 → | chars 4763 → | tokens 节省 | 节省比 | 信息密度 |
|---|---|---|---|---|---|---|
| **禁用压缩**（baseline） | ❌ | 19 | 4763 | 0 | 0.0% | — |
| **Legacy keep-5**（v0.5+） | ✅ | 6 | 1415 | 837 | **70.3%** | 235 chars/msg |
| **Smart 评分压缩**（v2） | ✅ | **14** | 3264 | 374 | 31.4% | 233 chars/msg |

### 关键发现

1. **Legacy 数字更漂亮，但代价是丢证据、丢法官推理**。
   Legacy 把 19 条砍到 6 条看似省钱，但把 cross-exam 中的关键 evidence、judge assess、tool_call 整组都一起扔了。**下一轮 LLM 调用就会产生幻觉或自相矛盾**。

2. **Smart 留更多条，但每条都是"高价值"**。
   - 识别了 **17 个原子组**（基于 tool_call_id / evidence_id Metadata），保留其中 **10 个**
   - 强制保留最近 **3 条**（防"丢光"）
   - 丢弃 > 5 条时插入 **1 条 earlier-context 摘要 note**，列出 evidence_id / @prosecutor 等可信锚点
   - 信息密度 = 233 chars/msg ≈ Legacy（235 chars/msg）—— 区别仅在**保留消息数**

3. **DecisionCourt 庭审场景推荐 Smart**；客服/coding 这类上下文价值均匀的场景推荐 Legacy。

### 工程意义（单庭审估算，假设 10 次需要压缩的 LLM 调用）

| 策略 | 单庭审节省 tokens | DeepSeek 现行价折算 |
|---|---|---|
| Legacy | ~8,370 | ≈ ¥0.84 |
| Smart | ~3,740 | ≈ ¥0.37 |

智能体多花的"成本"换来不丢事实链 —— 对一份可执行判决书而言，这笔账是值得的。

### 复现方法

```bash
cd backend
go test ./internal/agent_gateway/... -run TestCompressionEval_StrategyComparison -v
```

### 完整文档链

- 设计文档：[`.trae/documents/compression-eval-baseline.md`](./.trae/documents/compression-eval-baseline.md) — 完整基线 benchmark
- 压缩策略：[`.trae/documents/prompt-compression-courtscenario.md`](./.trae/documents/prompt-compression-courtscenario.md)
- 预算设计：[`.trae/documents/token-budget-design.md`](./.trae/documents/token-budget-design.md)
- 总体设计：[`.trae/documents/agent-gateway-advanced.md`](./.trae/documents/agent-gateway-advanced.md)

---

## 目录

- [🌟 亮点：Agent Gateway 压缩策略对比](#亮点agent-gateway-压缩策略对比)
- [项目特色](#项目特色)
- [快速开始](#快速开始)
- [架构总览](#架构总览)
- [核心机制](#核心机制)
- [项目结构](#项目结构)
- [环境变量](#环境变量)
- [开发指南](#开发指南)
- [测试](#测试)
- [文档](#文档)
- [路线图](#路线图)
- [许可证](#许可证)

---

## 项目特色

### 🎭 多 Agent 对抗辩论，而非单一回答

四类专业 Agent 围绕你的决策展开结构化辩论：

| 角色 | 职责 | 目标 |
|---|---|---|
| **控方** | 强力支持选项 A | 用证据说服你 |
| **辩方** | 强力反对 / 维护选项 B | 用证据反驳 |
| **调查员** | 主动检索外部信息 | 找到你没看到的证据 |
| **书记员** | 整理庭审 + 生成判决书 | 中立、完整、可执行 |
| **法官** | **你自己** | 实时插证据、做最终裁决 |

### 🧠 三层防线防止 Agent 互相附和

1. **信念引擎（Belief Engine）**：量化每个 Agent 对选项的信念度，强制发言方向与信念度一致（不一致则 LLM-as-judge 打回重生成）。
2. **A2A 消息总线 + ContextView 投影**：控辩双方**互相看不到对方的私有推理链**（`payload.reasoning`），看不到对方的私有策略笔记。
3. **情节记忆（Episodic Memory）**：每方 Agent 维护自己的私有策略池，通过 A2A 私有通道写入，对方永远看不到。

### 🔍 ReAct 协议 + 调查员实时搜索

控辩方 LLM 在 ReAct 循环中可主动派遣调查员搜索外部证据：
```
思考 → 工具调用（search.started）→ 调 Bocha/SearXNG API → 写入 investigation_findings 表 → 回报（search.completed）
```
调查发现与用户证据在 UI 和数据库层面严格分离，互不污染。

### 🎬 流式体验 + 可审计

- **逐字流式发言**：LLM token 实时推送到前端气泡（`hub.Broadcast` sleep 30ms + 前端 `flushSync`）。
- **完整可回放**：所有 A2A 消息、ReAct 步骤、私有策略笔记均持久化 + WebSocket 广播，庭审后可逐条审计。
- **幕后视角**：判决书页解锁"幕后视角" Tab，展示双方 Agent 私有策略演化路径。

### 🚦 智能收敛

每轮质证后计算信念度变化 Δ，连续两轮 Δ < 5% 即认为辩论收敛，提前进入结案陈词 —— **避免拖沓，节省 Token**。

---

## 快速开始

### 方式一：Docker Compose（推荐）

```bash
# 1. 克隆
git clone https://github.com/<your-username>/DecisionCourt.git
cd DecisionCourt

# 2. 配置 LLM Key
cp .env.example .env
# 编辑 .env，至少填入 LLM_API_KEY（DeepSeek / Kimi）

# 3. 一键启动
docker compose up -d

# 访问
# 前端:    http://localhost:3000
# 后端:    http://localhost:8080/health
# SearXNG: http://localhost:8081
```

### 方式二：本地开发

**前置要求**：Node.js 20+ / pnpm / Go 1.26+ / PostgreSQL 15+

```bash
# 1. 启动 PostgreSQL
brew services start postgresql@15
createdb decisioncourt

# 2. 启动后端
cd backend
cp ../.env.example .env   # 或直接用根目录 .env
go run cmd/server/main.go
# 监听 http://localhost:8080

# 3. 启动前端（新终端）
cd frontend
pnpm install
cp .env.example .env.local
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
│  LLM Client · Search Providers · PostgreSQL │
│  DeepSeek / Kimi · Bocha / SearXNG / Mock   │
└─────────────────────────────────────────────┘
```

---

## 核心机制

### A2A 消息总线（Agent-to-Agent）

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
| 控方 | `belief_A ≥ 0.7` | 支持 A 的证据 → 提升 `belief_A` |
| 辩方 | `belief_A ≤ 0.3` | 支持 A 的证据 → 降低 `belief_A` |
| 调查员 | `belief_A = 0.5` | 按搜索结果动态调整 |

发言方向与信念度不一致时 → LLM-as-judge 打回重生成。

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
├── docs/                          # 产品与设计文档
│   ├── decisioncourt-prd.md       # 产品需求文档（含 MVP 进度）
│   ├── decisioncourt-roadmap.md   # 实施路线图
│   ├── decisioncourt-api-design.md
│   ├── decisioncourt-db-design.md
│   ├── decisioncourt-tech-spec.md
│   ├── decisioncourt-agent-design.md
│   └── decisioncourt-ux-refinement.md
│
├── backend/                       # Go + Gin 后端
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── a2a/                  # A2A Bus + ContextView
│   │   ├── agent/                # Orchestrator + ReAct + Prompts
│   │   ├── agent/tools/          # 调查员搜索工具
│   │   ├── api/                  # REST + WebSocket Hub
│   │   ├── belief/               # 信念引擎
│   │   ├── courtroom/            # 状态机 + Service
│   │   ├── evidence/             # 用户证据
│   │   ├── investigation/        # 调查发现
│   │   ├── private_memory/       # 私有记忆池
│   │   ├── search/               # Bocha / SearXNG / DuckDuckGo
│   │   ├── llm/                  # DeepSeek / Kimi 客户端
│   │   └── model/                # GORM models
│   └── test-output/              # 端到端测试 JSON 样本
│
├── frontend/                      # Next.js 14 前端
│   ├── app/                      # 立案页 / 庭审页 / 判决书页
│   ├── components/
│   │   ├── courtroom/            # AgentAvatar · ArgumentMap · EvidenceBoard
│   │   │                         # InvestigatorPanel · MemoryAuditPanel ...
│   │   └── ui/                   # shadcn/ui 组件
│   ├── hooks/                    # usePhaseUI
│   ├── lib/                      # api · websocket · mock
│   ├── store/                    # Zustand store
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
| `SEARXNG_URL` | 否 | `http://searxng:8080` | SearXNG 地址 |
| `TAVILY_API_KEY` | 视 provider | - | Tavily 搜索 key |
| `DATABASE_URL` | 是 | - | PostgreSQL 连接字符串 |
| `REDIS_URL` | 否 | - | Redis 连接字符串 |
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

---

## 开发指南

### 前端：切换 Mock / 真实后端

前端默认连接真实后端。如需使用 Mock 数据演示前端：

```bash
# frontend/.env.local
NEXT_PUBLIC_USE_MOCK=true
```

### 后端：重置数据库

```bash
dropdb decisioncourt && createdb decisioncourt
# GORM 会在后端启动时自动 AutoMigrate
```

### 后端：热重载

```bash
cd backend
go install github.com/cosmtrek/air@latest
air
```

### 添加新的 Agent 类型

1. 在 `internal/agent/prompts.go` 中定义 system prompt
2. 在 `internal/a2a/types.go` 中注册 message_type
3. 在 `internal/courtroom/service.go` 中编排发言顺序
4. 在 `frontend/components/courtroom/AgentAvatar.tsx` 中注册视觉样式

---

## 测试

### 后端单元测试

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
- `internal/agent_gateway`：**63 项**（Token Budget v2 多维 / sliding / OnWarning / Reject + Prompt 压缩 v2 评分 / 原子组 / GreedyPack / 兜底摘要 + Gateway reject-when-exhausted 集成 + 三策略压缩对比 baseline）

### 端到端样本

`backend/test-output/` 收录了完整庭审的 WebSocket 事件 JSON 样本，可直接用于回归对比。

---

## 文档

完整设计文档位于 [`docs/`](./docs)：

- [产品需求文档 (PRD)](./docs/decisioncourt-prd.md) — 业务需求 + MVP 进度 + 简历叙事
- [实施路线图](./docs/decisioncourt-roadmap.md) — 从 0 到 MVP 的阶段划分
- [API 接口设计](./docs/decisioncourt-api-design.md)
- [数据库设计](./docs/decisioncourt-db-design.md)
- [技术规范](./docs/decisioncourt-tech-spec.md)
- [Agent 状态机与 Prompt 设计](./docs/decisioncourt-agent-design.md)
- [UX 细节规范](./docs/decisioncourt-ux-refinement.md)

进行中的设计演进位于 [`.trae/documents/`](./.trae/documents)：

- [memory-a2a-redesign.md](./.trae/documents/memory-a2a-redesign.md) — v0.5 记忆系统 + A2A 重设计
- [agent-gateway-advanced.md](./.trae/documents/agent-gateway-advanced.md) — v0.5+/v2 Agent Gateway 装饰器详细设计
- [token-budget-design.md](./.trae/documents/token-budget-design.md) — Token Budget 重设计决策稿（D1-D6 全锁定）
- [prompt-compression-courtscenario.md](./.trae/documents/prompt-compression-courtscenario.md) — Prompt 压缩 v2 重设计（针对庭审 5 大痛点）
- [compression-eval-baseline.md](./.trae/documents/compression-eval-baseline.md) — v2 vs v0.5+ 压缩策略基线 benchmark

---

## 路线图

### ✅ 已完成（MVP）

- 四 Agent 对抗辩论 + 信念引擎 + 智能收敛
- A2A 消息总线 + ContextView 投影 + 私有通道
- 情节记忆（Episodic Memory via A2A）
- ReAct 循环 + 流式 LLM + 调查员实时搜索
- 用户证据 + 调查发现独立表
- 庭审模式选择（快速 / 标准 / 深度）
- 极简白底法庭风格 UI + 凹陷输入框
- Docker Compose 一键启动
- MemoryAuditPanel（前端可审计）+ 幕后视角页
- **Agent Gateway v0.5+**（白盒子集：统一接入 / 审计落库 / trace 关联 + 高级能力：Prompt 压缩 / Token 预算 / 限流 / Fallback / 文件日志）
- **Agent Gateway v2 升级**（Token Budget 多维 + sliding 5min + OnWarning + RejectWhenExhausted；Prompt Compression 评分+原子组+贪心打包+兜底摘要）

### 🚧 第二阶段（不在 MVP）

- Agent Gateway 模型路由（v2 已落地压缩 + 预算，路由仍待）
- Agent Gateway Redis 多实例存储
- Agent Gateway Retry jitter + 错误分类
- 问题澄清与选项生成
- Agent 主动提问
- 专家证人 / 陪审团
- 历史庭审 + PDF 导出
- 预设决策模板库

---

## 许可证

本项目以 **MIT License** 开源，详见 [LICENSE](./LICENSE)。

---

## 安全说明

### ⚠️ 推送至公开仓库前必做

1. **检查 `.env`**：仓库根目录的 `.env` 文件包含本地真实 API Key（DeepSeek / Bocha），**已被 `.gitignore` 排除**，但请确认没有手动 `git add -f` 强制加入。
2. **轮换已泄露的 Key**：任何曾在本地明文存放过 LLM / Search provider 的 API Key，**强烈建议**去对应控制台轮换（rotate）后再推送 —— 万一机器被克隆过、IDE 自动备份过、或 `.env` 被同步到云端，Key 都有可能泄露。
3. **生产部署**：务必使用强随机 `JWT_SECRET`（`openssl rand -hex 32`），不要沿用 `decisioncourt-secret` 默认值。
4. **不要提交真实庭审数据**：`backend/test-output/` 中的样本是开发测试用的合成决策问题（"辞职创业"、"出国留学"），但若你曾用真实个人决策做过端到端测试，请在推送前 review 该目录并清空任何敏感内容。

### 密钥管理最佳实践

- 本地开发：`.env` 文件（已在 `.gitignore` 内）
- 团队协作：使用 [Doppler](https://www.doppler.com/) / [1Password CLI](https://developer.1password.com/docs/cli) 等密钥管理工具
- CI/CD：GitHub Actions Secrets / GitLab CI Variables
- 部署：云平台 Secret Manager（AWS Secrets Manager / GCP Secret Manager）

如发现潜在安全问题，请邮件联系维护者（不要公开 Issue）。

---

## 致谢

- LLM：[DeepSeek](https://www.deepseek.com/) · [Kimi (Moonshot)](https://www.moonshot.cn/)
- 搜索：[Bocha AI Search](https://bochaai.com/) · [SearXNG](https://searxng.org/) · [Tavily](https://tavily.com/)
- 前端：[Next.js](https://nextjs.org/) · [shadcn/ui](https://ui.shadcn.com/) · [Tailwind CSS](https://tailwindcss.com/) · [React Flow](https://reactflow.dev/) · [Recharts](https://recharts.org/) · [Zustand](https://zustand-demo.pmnd.rs/)
- 后端：[Gin](https://gin-gonic.com/) · [GORM](https://gorm.io/) · [gorilla/websocket](https://github.com/gorilla/websocket)

---

<p align="center">
  <sub>Built with ⚖️ for anyone who has ever faced a decision too complex for a single AI answer.</sub>
</p>
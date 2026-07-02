# ADR 0001: MVP 技术栈

> **状态**：✅ Accepted (2026-06)  
> **决策日期**：2026-06  
> **影响范围**：全栈

## 背景

DecisionCourt 是多 Agent 决策辅助平台 MVP，需要在 4-6 周内跑通立案 → 庭审 → 判决全流程。技术栈需满足：

1. **本地可跑**：Docker Compose 一键启动
2. **国内友好**：LLM API 国内可稳定访问
3. **全栈工程能力展示**：覆盖前端、后端、数据库、实时通信
4. **可扩展**：第二阶段支持水平扩展

## 选项对比

| 维度 | 选 A | 选 B | 选 C |
|---|---|---|---|
| 后端语言 | **Go 1.22+** ✅ | Python (FastAPI) | Node.js (NestJS) |
| Web 框架 | **Gin** ✅ | Echo | chi |
| 数据库 | **PostgreSQL 15+** ✅ | MySQL | MongoDB |
| 缓存 | **Redis 7+** ✅ | Memcached | 无 |
| ORM | **GORM** ✅ | sqlx | Ent |
| 前端 | **Next.js 14 App Router** ✅ | Nuxt 3 | Remix |
| 前端样式 | **Tailwind + shadcn/ui** ✅ | MUI | Chakra |
| 前端可视化 | **React-Flow + Recharts** ✅ | Cytoscape.js | D3 |
| 前端状态 | **Zustand** ✅ | Redux Toolkit | Jotai |
| WebSocket | **gorilla/websocket** ✅ | melody | nhooyr.io/websocket |
| LLM SDK | **go-openai（OpenAI 兼容）** ✅ | 直接 HTTP | langchaingo |
| LLM 默认 | **DeepSeek-V3 / R1** ✅ | GPT-4o-mini | Qwen-Max |
| 搜索 | **Bocha（开发）+ Tavily（部署）** ✅ | SearXNG 自托管 | Google Custom Search |

## 决策

采用**选 A 列全套**。

### 关键理由

- **Go 1.22 + Gin**：性能高、并发模型契合 WebSocket（gorilla）、部署简单（单一静态二进制）。Go 生态对 OpenAI 兼容客户端成熟（go-openai）。
- **PostgreSQL + Redis**：PG 已是事实标准，支持 JSONB（evidences / messages / a2a_messages 都不需要额外 MongoDB）；Redis 仅用于缓存和 WebSocket 订阅管理。
- **Next.js 14 App Router**：支持 React Server Components + 流式渲染，与 shadcn/ui 契合度高。
- **DeepSeek-V3 / R1**：中文能力 + 价格 + OpenAI 兼容。V3 处理常规轮次，R1 处理判决等关键推理轮次。
- **Bocha**：国内可稳定访问 + API 简洁（替代反爬严重的 DuckDuckGo）。

### 后续可替换

- LLM Provider 抽象在 `internal/llm/`，切换厂商只需改配置（Kimi / Qwen / OpenAI 已实测可行）。
- Search Provider 抽象在 `internal/search/`，当前实现 Bocha + SearXNG + DuckDuckGo + Mock。

## 后果

### 收益

- ✅ 单 Docker Compose 启动，开发体验佳
- ✅ Go 性能可承载 100+ 在线庭审（单节点 MVP）
- ✅ 国内网络友好，LLM / Search 都不依赖 VPN
- ✅ 简历叙事完整（全栈 + AI Agent + 实时通信）

### 代价

- ⚠️ Go 生态对 LLM 多 Agent 框架不如 Python 丰富（langchain 等），需要自实现 Orchestrator
- ⚠️ DeepSeek API 在 R1 模型上有时延较高，关键轮次需要更长超时
- ⚠️ Bocha 仍需付费 key（免费额度有限），开发期 SearXNG 自托管是 fallback

## 关联

- 主文档：[`../decisioncourt-tech-spec.md`](../decisioncourt-tech-spec.md) §3 §4
- 配置文件：[`backend/internal/config/config.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/config/config.go)
- 装配入口：[`backend/cmd/server/main.go`](file:///d:/源码/FullStack/DecisionCourt/backend/cmd/server/main.go)
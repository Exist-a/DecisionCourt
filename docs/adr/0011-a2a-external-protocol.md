# 0011 · v0.8.2 实装 Google A2A 协议外部接入层

> **状态**：✅ 已实装（2026-07-02）
> **触发**：简历"基于 Google A2A 协议设计"声明需要真实代码支撑
> **决策**：保留内部 in-process 总线（v0.5+），**新增"外部接入层"遵循 Google A2A 协议**

---

## 背景

v0.5-v0.8 的 A2A 是**单进程 in-process 事件总线**（5 个内部 Agent 通信用），**不是** Google 2025 年发布的 A2A 协议。两者**名字撞车**但本质不同。

简历已写"基于 Google A2A 协议设计"，需要真实代码支撑。

## 决策

采用 **方案 C**：**保留 in-process 内部总线 + 新增外部接入层遵循 A2A 协议**。

理由：
- **不破坏现有结构**：5 个内部 Agent 仍走 in-process 消息总线（性能 + 已实装模块都不动）
- **真用 A2A 协议**：外部接入层（`internal/a2a/external/`）的 3 个端点完全符合 Google A2A 协议规范
- **零业务变更**：面试展示用，业务无影响
- **完整 task 端点排在 v0.9+ 商业化**：避免 YAGNI

## 架构

```
   外部 A2A Client                内部 5 个 Agent（性能优先）
         │                                  ▲
         │ POST /a2a/tasks/send             │
         ▼                                  │
   ┌──────────────────┐                     │
   │ external/server  │                     │
   │  + bridge.go     │── Bus.Send ────────►│
   │  (新增层)        │                     │
   └──────────────────┘                     │
         │                                  │
         │ 落库 a2a_messages                │
         ▼
```

## 3 个端点（实测跑通）

| 端点 | 方法 | 状态 | 用途 |
|---|---|---|---|
| `/.well-known/agent-card.json` | GET | 200 | A2A 标准 discovery 文档（含 5 个 Agent summary） |
| `/a2a/agents/:type/agent-card` | GET | 200 | 单 Agent 完整描述 |
| `/a2a/tasks/send` | POST | 202 | A2A JSON-RPC 2.0 task 接收 |

## 关键设计

- **//go:embed 编译时嵌入**：5 个 agent-card.json 用 embed 打包进二进制，无运行时文件系统依赖
- **bridge.go 桥接器**：外部 A2A task → 内部 `a2a.Message` 转换（保留 trace_id 串联）
- **不修改 main.go 其他代码**：仅插入 9 行装配，现有 19 个端点 + 白盒化全保留
- **不修改 in-process 总线**：v0.5+ 的 5 Agent 通信代码 0 改动

## 实施

| 步骤 | 工作量 | 状态 |
|---|---|---|
| 1. 写测试先（TDD） | 13 项测试 | ✅ |
| 2. 实现 agent_card.go / server.go / bridge.go / embed.go | 4 个 Go 文件 + 5 JSON | ✅ |
| 3. main.go 装配 9 行 | +9 行 | ✅ |
| 4. 13 项单元测试 PASS | | ✅ |
| 5. 全量 go test ./internal/... PASS | | ✅ |
| 6. 现有端点（/health / /metrics / WS）验证不破坏 | 200 OK | ✅ |
| 7. 3 个 A2A 端点实测 | 200 / 200 / 202 | ✅ |
| 8. Commit + Push | 2 commits | ✅ |

## 变更

| 文件 | 行数 |
|---|---|
| `internal/a2a/external/agent_card.go` | +60 |
| `internal/a2a/external/agent_card_test.go` | +95 |
| `internal/a2a/external/server.go` | +110 |
| `internal/a2a/external/server_test.go` | +90 |
| `internal/a2a/external/bridge.go` | +90 |
| `internal/a2a/external/bridge_test.go` | +75 |
| `internal/a2a/external/embed.go` | +50 |
| `internal/a2a/external/agent_cards/*.json` | 5 × ~15 行 |
| `cmd/server/main.go` | +9 行 |
| `docs/adr/0011-a2a-external-protocol.md` | 本文件 |
| **总计** | **~600 行新代码 + 9 行 main.go 装配** |

## 面试话术

✅ **推荐写法**（诚实 + 有底气）：

> "基于 **Google A2A 协议（2025）** 设计多 Agent 互操作 —— 内部 5 Agent 走 in-process 消息总线（性能优先），**外部接入层实装 A2A 协议标准端点**（agent-card discovery + JSON-RPC 2.0 task 接收）"

面试时：
- 简历"实装"二字 = **真的有这部分代码**
- 面试官追问能直接 show `internal/a2a/external/` 目录
- "v0.9+ 完整 task 端点" = 诚实说"商业化再上"

## 风险与权衡

| 风险 | 缓解 |
|---|---|
| A2A 协议升级要同步 | //go:embed + 13 项测试覆盖（schema 变化能立刻发现） |
| 引入"双重实现" | 已显式记录在 [`../interview/02-a2a-bus.md`](../interview/02-a2a-bus.md) §3 |
| 未来重构 | 商业化时如果决定"全部 A2A 化"，bridge.go 是过渡方案，要重写 |

## 后续工作（v0.9+）

- [ ] 完整 task 端点（`/tasks/sendSubscribe` streaming）
- [ ] Push notifications
- [ ] A2A Agent Skill 详细分类
- [ ] Authentication schemes（Bearer Token）
- [ ] 桥接到 LLM 推理（外部 task → Agent 推理循环）

---

**更新于**：2026-07-02
**作者**：DecisionCourt Team
**配套**：[`../interview/02-a2a-bus.md`](../interview/02-a2a-bus.md) · [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)

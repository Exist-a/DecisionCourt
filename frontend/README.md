# DecisionCourt — Frontend

> Next.js 14 前端 + shadcn/ui + Tailwind CSS

本目录是 [DecisionCourt](../README.md) 项目的前端。

## 快速开始

参见根目录 [README.md](../README.md) 的「快速开始」章节 —— 前端作为 Docker Compose 一部分启动，或本地 `pnpm dev`。

## 技术栈

| 层 | 技术 |
|---|---|
| 框架 | Next.js 14 (App Router) + TypeScript |
| 样式 | Tailwind CSS + shadcn/ui |
| 状态 | Zustand |
| 实时 | 原生 WebSocket |
| 可视化 | React Flow · Recharts |

## 目录结构

```
frontend/
├── app/                      # App Router 页面
│   ├── page.tsx              # 立案页
│   ├── court/[id]/page.tsx   # 庭审主界面
│   └── verdict/[id]/page.tsx # 判决书页
├── components/
│   ├── courtroom/            # 庭审相关组件
│   │   ├── AgentAvatar.tsx
│   │   ├── ArgumentMap.tsx
│   │   ├── BehindTheScenesPanel.tsx
│   │   ├── CotStepsPanel.tsx
│   │   ├── CourtroomScene.tsx
│   │   ├── EvidenceBoard.tsx
│   │   ├── InvestigatorPanel.tsx
│   │   ├── JudgeBiasMeter.tsx
│   │   ├── MemoryAuditPanel.tsx
│   │   ├── MemoryTimeline.tsx
│   │   ├── MessageHistory.tsx
│   │   ├── PhaseGuide.tsx
│   │   ├── StanceChart.tsx
│   │   └── ThinkingBubble.tsx
│   └── ui/                   # shadcn/ui 原子组件
├── hooks/                    # usePhaseUI 等
├── lib/                      # api · websocket · mock
├── store/                    # Zustand store
└── types/
```

## 切换 Mock / 真实后端

```bash
# frontend/.env.local
NEXT_PUBLIC_USE_MOCK=true   # 演示前端时使用 Mock
NEXT_PUBLIC_USE_MOCK=false  # 默认：连接真实后端
```

详见 [前端开发指南](../README.md#开发指南)。
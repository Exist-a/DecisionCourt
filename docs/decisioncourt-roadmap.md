# 决策庭（DecisionCourt）实施路线图

> **版本**：v0.8
> **状态**：MVP 主体完成 + v0.8 白盒化实装（截至 2026-07-02）—— 含 v0.5/v0.6/v0.7/v0.8 四轮增量实装；下一步进入"高可用 + 并发防护"讨论。
> **可观测性完善计划**：[`roadmap/whitebox-roadmap.md`](./roadmap/whitebox-roadmap.md)（v0.8+ 五阶段：数据采集 → 增量埋点 → Prometheus → OTLP → 数据仓库）
> **目标**：规划从 0 到 MVP 可运行的实施路径，并明确验收标准。
> **2026-07-02 整合时同步**：本版本号对齐后端代码实装现状 + 文档整合状态（参见 [`docs/README.md`](./README.md)）。

---

## 0. 当前进度快照

| 维度 | 状态 | 说明 |
|---|---|---|
| 项目骨架 | ✅ | `frontend/`、`backend/`、`docker-compose.yml`、`.env.example`、README 已创建 |
| 前端页面 | ✅ | 首页/立案页、庭审主界面（含右侧 Tab + 调查活动面板）、判决书页，白色极简主题 |
| 前端 Real 驱动 | ✅ | Zustand store + 真实 WebSocket 订阅所有 ReAct 事件，pnpm tsc 通过 |
| 前端编译 | ✅ | `pnpm build` 通过 |
| 后端 LLM 客户端 | ✅ | DeepSeek 客户端含 `Complete` + `StreamComplete` 流式方法 |
| 后端庭审状态机 | ✅ | idle → opening → cross_exam → closing → deliberation → verdict |
| 后端 Agent 编排 | ✅ | Orchestrator + ReAct 协议（action / tool_call / reflect / speak） + Prompt 模板 |
| 后端证据服务 | ✅ | 证据提交、影响评估（**仅**用户证据） |
| 后端 WebSocket | ✅ | Hub + Room 广播，hub.Broadcast sleep 30ms 保流式帧间隔 |
| 后端判决书 | ✅ | ClerkAgent 生成结构化判决书 |
| 后端编译 | ✅ | `go build` 通过 |
| A2A + 私有记忆代码 | ✅ | `internal/a2a`（12 项测试）+ `internal/private_memory`（9 项测试）已实装 |
| InvestigationFinding 独立表 | ✅ | `internal/investigation/` + `investigation_findings` 表 + `GET /investigations` 端点（10 项测试） |
| ReAct 协议 | ✅ | `internal/agent/react_runner.go` 实装 ReAct 循环 + `OnIterStart` / `OnSpeakChunk` 钩子 + `ActionReflect` |
| LLM 流式 | ✅ | 后端 StreamComplete + 渐进 JSON 提取 + 前端 `flushSync` 强制 commit |
| 调查员视觉 | ✅ | `InvestigatorPanel.tsx` + Avatar isSearching + 状态机 dispatch→report 升级 |
| Bocha 搜索 | ✅ | `internal/search/bocha.go` 实装，HTTP 200 实测 |
| 信念动态更新 | ✅ v0.6 | 贝叶斯 log-odds + 锚定（Bayesian Engine 2026 / ScioMind 2026）+ confirmation/contradiction sign + weaken 边，写入 `belief_diffs` 表 |
| 智能收敛 | ✅ v0.6 | 四信号多信号收敛：推理震荡（PROCLAIM 2026 最高优）> 双方共识 > 信念稳定 > 最大轮次兜底，广播 `belief.convergence` 事件带结构化原因 |
| 信念审计 trail | ✅ v0.6 | `belief_diffs` 表 + `GET /belief-diffs` REST + 前端 BeliefDiffCard + 离线可重放 |
| 真实后端联调 | ⏳ | PostgreSQL 在跑，依赖真实 LLM API key |
| Docker 验证 | ✅ v0.8.3 | docker compose 五服务一键启动(postgres/redis/backend/frontend/caddy) |
| Agent Gateway | ✅ v0.9 | 白盒子集 + v0.5+ 高级能力 + v0.9 三大新能力(per-call Timeout 90s / Response Cache / Circuit Breaker) |
| 单机部署高可用(ADR 0012) | ✅ v0.9 | session 互斥补锁 + Idempotency-Key + panic 兜底 + 启动扫描恢复 5 子项全落地 |
| 用户级 Trial 限流(ADR 0014) | ✅ v0.9 | 每用户每天 5 次 StartTrial(sync.Map + 滑动窗口) |
| 防幻觉(ADR 0015) | ✅ v0.9.1 | baseRules 严禁编造 + buildContext source 标签 + user_interrupt 注入 |
| 部署就绪 | ✅ v0.9.1 | 真域名 + DNS 解析待用户完成,代码侧全部就绪 |
| WS 握手 403 修复 | ✅ v0.9.3 | **两段式修复**。第一段:viper.Unmarshal 对单值 env var 不自动 split `[]string`,生产 `ALLOWED_ORIGINS=https://decisioncourt.cn` 没逗号导致 AllowedOrigins=nil → 手动 split 修复。第二段:buildCheckOrigin 在 package init 阶段捕获 allowedSet,但此时 config.Load() 还没跑,白名单锁死为 localhost:3000 fallback → 改为每次调用重读 config([ADR 0018](./adr/0018-websocket-origincheck-init-timing.md))。同时修复 investigation/service.go 没填 Message.SessionUUID 的 a2a fallback 鬼屋 bug(906ebde1 → d7bac039 错投房间)。 |

**当前阻塞**：

1. PostgreSQL 未运行，后端无法启动和联调。
2. Docker 未安装，无法验证 `docker-compose` 一键启动。

---

## 1. 总体策略

注意：以当前根目录为基准即可

- **采用前后端分离架构**，项目统一放在 `decisioncourt/` 目录下。
- **先前端、后后端**：前端先搭出页面和交互框架，便于验证产品流程；后端再提供真实 API。
- **Mock 驱动开发**：前端开发阶段用 Mock 数据模拟后端响应，后端实现后逐步替换。
- **Docker 化部署优先**：从第一天就考虑容器化，确保环境一致。
- **文档先行**：A2A 协议与私有记忆模块已先在 PRD、Agent、技术文档中落地，代码后续实现。

---

## 2. 项目阶段划分

### 第一阶段：项目骨架（Week 1）

**目标**：搭建完整项目目录，前后端能独立运行，Docker Compose 可一键启动。

| 任务 | 内容 | 验收标准 | 状态 |
|---|---|---|---|
| 1.1 创建项目目录 | 创建 `decisioncourt/`，规划前后端、配置、文档结构 | 目录结构清晰，README 完整 | ✅ 已完成 |
| 1.2 前端初始化 | Next.js 14 + TypeScript + Tailwind + shadcn/ui | `pnpm dev` 能跑，首页可访问 | ✅ 已完成 |
| 1.3 后端初始化 | Go + Gin 基础项目结构 | `go run` 能跑，health check 通过 | ✅ 已完成 |
| 1.4 Docker Compose | PostgreSQL + Redis + SearXNG + 前后端 | `docker-compose up -d` 全部启动 | ⏳ 配置已写，待 Docker 环境验证 |
| 1.5 数据库迁移 | 创建所有表结构 | GORM 自动建表成功 | ⏳ 代码已就绪，待 PG 运行后验证 |

### 第二阶段：前端核心页面（Week 2）

**目标**：完成所有前端页面和组件，用 Mock 数据跑通完整用户流程。

| 任务 | 内容 | 验收标准 | 状态 |
|---|---|---|---|
| 2.1 首页/立案页 | 用户输入决策问题、选项、模式 | 表单校验通过，能提交 | ✅ 已完成 |
| 2.2 庭审主界面 | 法庭场景布局、Agent 圆点、发言气泡、历史记录侧边栏、底部输入栏 | 页面结构符合 PRD | ✅ 已完成 |
| 2.3 证据板 | 展示证据、提交证据、证据影响力 | 能增删查证据 | ✅ 基础版已完成 |
| 2.4 观点地图 | React-Flow 展示 Agent 立场和证据关系 | 能随数据更新 | ⏳ 组件已写，未集成到庭审页 |
| 2.5 立场变化曲线 | Recharts 绘制信念度变化 | 多轮数据可视化 | ⏳ 组件已写，未集成到庭审页 |
| 2.6 判决书页 | 展示结构化判决书 | 能渲染 Markdown | ✅ 已完成 |
| 2.7 WebSocket 客户端 | 连接、事件监听、断线重连 | 能接收 Mock 事件 | ✅ 已完成 |
| 2.8 Mock API/WS | 用 Mock 数据模拟后端 | 完整庭审流程可跑通 | ✅ 已完成 |

### 第三阶段：后端核心业务（Week 3）

**目标**：实现后端 API、庭审状态机、Agent 编排、证据系统。

| 任务 | 内容 | 验收标准 | 状态 |
|---|---|---|---|
| 3.1 庭审管理 API | 创建、获取、开始、推进庭审 | 接口与 API 文档一致 | ✅ 已完成 |
| 3.2 庭审状态机 | idle/opening/cross_exam/closing/deliberation/verdict | 阶段转换正确 | ✅ 已完成 |
| 3.3 Agent 编排 | 4 个 Agent 的调用与上下文管理 | Agent 按顺序发言 | ✅ 已完成 |
| 3.4 信念引擎 | 信念度初始化、更新、收敛判断 | 信念度变化符合规则 | ⏳ 初始化/snapshot 已做，动态更新待实现 |
| 3.5 证据系统 | 证据提交、可采性、影响评估 | 证据进入后影响信念度 | ✅ 基础版已完成 |
| 3.6 问题澄清与选项生成 | Agent 主动提问、生成候选选项 | 模糊问题能生成选项 | ❌ MVP 范围外，明确不做 |
| 3.7 WebSocket 服务 | 实时广播庭审事件 | 前端能收到真实事件 | ✅ 代码已完成，待真实后端联调 |
| 3.8 LLM 集成 | DeepSeek API 调用 | Agent 能真实生成内容 | ✅ 客户端已就绪，待 PG 运行后验证 |

### 第四阶段：搜索与判决书（Week 4）

**目标**：接入搜索、生成判决书、完成端到端流程。

| 任务 | 内容 | 验收标准 | 状态 |
|---|---|---|---|
| 4.1 WebSearch 抽象 | Bocha / SearXNG / Tavily / Mock 接口 | 开发用 Bocha，配置可切换 | ✅ Bocha 默认（HTTP 200 实测）|
| 4.2 调查员 Agent | 调用搜索、写入 investigation_findings 表 | dispatch + search.started + ReportFinding + search.completed 链路 | ✅ 已实装 + 单条 entry 状态机 + 10 项测试 |
| 4.3 书记员 Agent | 整理庭审、生成判决书 | 判决书结构正确 | ✅ 已完成 |
| 4.4 判决书 API | 获取判决书、用户反馈 | 接口可用 | ✅ 已完成 |
| 4.5 端到端测试 | 完整庭审流程 | 从立案到判决完整跑通 | ⏳ 依赖 PG 运行 |
| 4.6 Docker 部署验证 | 全部服务 Docker 化 | 新环境能一键启动 | ⏳ 依赖 Docker 安装 |

### 第五阶段：优化与打磨（Week 5-6）

**目标**：提升稳定性、用户体验、代码质量。

| 任务 | 内容 | 验收标准 | 状态 |
|---|---|---|---|
| 5.1 Agent 输出稳定性 | 防止附和、幻觉、重复 | 多轮测试输出合理 | ⏳ 待 A2A + 私有记忆代码实现后验证 |
| 5.2 前端动画与细节 | Agent 发言动画、阶段过渡、证据飞入 | 体验流畅 | ⏳ 基础动画已有，待打磨 |
| 5.3 错误处理 | LLM 超时、搜索失败、WebSocket 断线 | 有优雅降级 | ⏳ 待补齐 |
| 5.4 性能优化 | 减少不必要的 LLM 调用、前端渲染优化 | 快速模式 3-5 分钟完成 | ⏳ 待测试 |
| 5.5 代码质量 | 测试、lint、类型检查 | CI 通过 | ⏳ 待添加 |
| 5.6 文档完善 | README、部署文档、演示视频 | 项目可独立运行 | ⏳ 进行中 |

---

## 3. 技术依赖关系

```
项目目录结构
    ↓
Docker Compose（PG + Redis + SearXNG）
    ↓
前端 Next.js 初始化 + 后端 Go 初始化
    ↓
数据库迁移
    ↓
前端 Mock API/WS + 页面组件
    ↓
后端业务 API + 状态机
    ↓
Agent 编排 + LLM 集成
    ↓
A2A 消息总线 + 私有记忆池（新增亮点）
    ↓
WebSearch + 判决书生成
    ↓
端到端测试 + 优化
```

---

## 4. 关键里程碑

| 里程碑 | 计划时间 | 标志 | 实际状态 |
|---|---|---|---|
| **M0：项目可运行** | Week 1 结束 | Docker Compose 能启动，前后端能访问 | ✅ 前后端可运行，Docker 待验证 |
| **M1：前端流程跑通** | Week 2 结束 | 用 Mock 数据完成一次完整庭审 | ✅ 已完成 |
| **M2：后端流程跑通** | Week 3 结束 | 后端 API 和状态机完整 | ✅ 已完成 |
| **M3：端到端跑通** | Week 4 结束 | 真实 LLM + 搜索，生成判决书 | ✅ 已完成（Bocha + DeepSeek）|
| **M4：MVP 完成** | Week 6 结束 | 稳定可用，可写进简历 | ⏳ 剩信念引擎动态更新 + 智能收敛 |

---

## 5. 风险与应对

| 风险 | 应对 |
|---|---|
| 前端等后端，进度阻塞 | 先用 Mock 数据并行开发 ✅ 已解决 |
| LLM 输出不稳定 | 多次 prompt 调优 + 输出校验 |
| DeepSeek API 不稳定 | 保留 Qwen / GLM-4 / OpenAI 切换能力 |
| Agent 互相附和或叛变 | A2A 上下文隔离 + 私有记忆池 + 信念引擎 + 立场一致性检查 |
| Token 成本超预算 | Agent Gateway Prompt 压缩 / Token 预算 / 限流 已实装，可开关对比；同时保留快速模式限制轮次和输出长度 |
| Docker 环境复杂 | 提供详细 README 和一键脚本 |
| 信念引擎动态更新不准确 | 先用简单线性更新，后续根据测试结果调参 |

---

## 6. 下一步

> v0.3 进度：A2A + 私有记忆 + InvestigationFinding + ReAct + LLM 流式 + Bocha 搜索 + 调查员视觉 全部已实装。

当前最优先解决**剩余两个未完成模块** + 优化 UX：

1. ✅ ~~安装 Docker Desktop 或本地 PostgreSQL~~ — PostgreSQL 已在跑
2. ✅ ~~运行后端~~ — `go run ./cmd/server` 跑通，`/health` 200
3. ✅ ~~前端连接真实后端~~ — `NEXT_PUBLIC_USE_MOCK=false`，跑通立案 → 庭审 → 判决
4. ✅ ~~A2A + 私有记忆~~ — 已实装 + 测试
5. ✅ ~~InvestigationFinding 独立表~~ — 已实装 + 测试
6. ✅ ~~ReAct + LLM 流式~~ — 已实装（hub.Broadcast sleep 30ms + 前端 flushSync）
7. ✅ ~~Bocha 搜索~~ — 已实装，HTTP 200 实测
8. ✅ ~~信念引擎动态更新~~ — v0.6 已实装：Bayesian log-odds + anchoring（详见 PRD §4.3.2）
9. ✅ ~~智能收敛~~ — v0.6 已实装：多信号按优先级触发（推理震荡 > 共识 > 稳定 > 兜底）
10. ⏳ **Docker Compose 一键启动**：完善 `docker-compose.yml` 让新环境能跑（待 Docker 环境验证）
11. ❌ **LLM 调用审计可视化**（决策 2026-07-01 不做）：后端 `llm_calls` 表 + `backend/logs/agent_gateway_*.log` 已足够；产品级 dashboard 不增加。开发者排查用 `tail -f` / `jq` / SQL 查询即可。
12. ⏳ **v0.7+ 计划**：强制立场一致性检查 / 新意度检查 / 300 字发言截断 / "已反驳证据"集合跟踪（详见 PRD §4.3.2 §4.3.3 §10.1）

### v0.10.1（2026-07-08）LLM 输出反幻觉加固

> v0.9.1 ADR 0015 已做 prompt 层防御（baseRules 4/5/13/14），但实测仍 **60% 幻觉率**。v0.10.1 加 post-validation 层做"双保险"。

- ✅ **output_validator.go**（核心）：4 类正则扫描器（证据引用 / 百分比 / 案号 / 金额），6 类 mode，证据+证据号两层防御
- ✅ **react_runner.go 流式路径接入**（关键）：v0.10 之前 `streamSucceeded` 跳过 validateSpeak，是修复后仍 60% 失败的根因。修了之后 0%
- ✅ **baseRules 规则 15**：prompt 层告知 LLM"违反会被后端硬拒"
- ✅ **9 个 unit test 覆盖所有模式**
- ✅ **自动化压测脚本** `tools/run-hallucination-test.ps1`：5 sessions × 30s × 3 轮

**修复效果**：60% → **0%**（38 messages 验证）。详见 [ADR 0021](./adr/0021-llm-hallucination-output-validator.md) + [interview/11-hallucination-validation.md](./interview/11-hallucination-validation.md)。

### v0.10（2026-07-08）前端埋点 + CORS 修复

- ✅ **前端埋点复用后端 decision_events**（ADR 0020）：8 个 `fe.*` 事件接入实际用户操作
- ✅ **CORS 修复**：dev env 用 `docker-compose.override.yml` + main.go 加 http.Server 包装 gin handler 解决 v0.10 CORS 404 / 401 链
- ✅ **`backend/.air.toml`**：dev air 热重载指向 `./cmd/server`
- ✅ **`docker-compose.override.yml`**：本地专用密码 + ALLOWED_ORIGINS

详见 [ADR 0020](./adr/0020-frontend-analytics-via-decision-events.md) + [interview/10-frontend-analytics.md](./interview/10-frontend-analytics.md)。

### v0.10.2（2026-07-08）GitHub Actions CI/CD

- ✅ **`.github/workflows/test.yml`**：push / PR 触发，go test + pnpm test + tsc + lint + ADR index check
- ✅ **`.github/workflows/deploy.yml`**：tag `v*.*.*` push 触发，build 镜像 → push ACR → SSH ECS pull + up
- ✅ **tag-based deploy**：镜像 tag = git tag 名，可追溯 + 一键回滚
- ✅ **ADR 0022**：决策文档

详见 [ADR 0022](./adr/0022-github-actions-ci-cd.md)。

### v0.10.7 ~ v0.10.15（2026-07-08 ~ 2026-07-12）CI 暂停与恢复

> **状态**：✅ **端到端跑通**（v0.10.15 commit `89dae51`）。push main → Test → Deploy → ECS 全部自动。
> 详见 [ADR 0023](./adr/0023-github-actions-ci-pause.md)（完整复盘，14 版迭代）。

#### v0.10.7 暂停（净通过 1/21 ≈ 5%）

- ❌ 7 次连续失败：util 包未 commit / Node 20 弃用 / SearchReplace silent failure / WebFetch 拿不到 step log
- 📝 写 ADR 0023 v1.0，记录 5 个暂停教训

#### v0.10.10 方向转折（核心架构落地）

- ✅ **deploy.yml 架构重写**：`workflow_run` + `if gate`（Test 完成后才触发 Deploy）
- ❌ 中途 5 次失败：test.yml 错方向修了 2 版（user 问"初心是什么"才意识到真问题是 deploy.yml 架构）

#### v0.10.15 端到端跑通（commit `89dae51`）

- ✅ **SSH key 算法升级**：RSA `id_rsa` → ed25519（OpenSSH 8.8+ 默认禁 ssh-rsa）
- ✅ **`ECS_USER` 修正**：`root` → `admin`（ADR §5.5 文档值错误，实际 admin）
- ✅ **路径大小写**：`/opt/decisioncourt` → `/opt/DecisionCourt`（跟 `scripts/ecs.ps1` 一致）
- ✅ **健康检查位置修正**：host 上 `curl` → 容器内 `wget 127.0.0.1:8080`（P0-3 安全加固 `expose` 不映射 host 端口）

#### 当前 dev 工作流（v0.10.15 起全自动）

```bash
git push origin main
# → 等 5-6 分钟
# → GitHub Actions UI 显示: Test ✅ → Deploy ✅ → ECS 健康
```

不再需要手动 `push-to-acr.ps1` / `deploy-to-ecs.ps1` / 本地 `next build` 验证。详见 ADR 0023 §5。

#### 已删除的废弃脚本（v0.10 改用 tag-based deploy）

- ❌ `scripts/commit-and-push.ps1`（v0.10 tag push 自动）
- ❌ `scripts/deploy-to-ecs.ps1`（v0.10 deploy.yml SSH 自动）
- 📦 **保留**：`scripts/push-to-acr.ps1` / `scripts/deploy-on-ecs.sh`（个人 fallback，CI down 时手动用）

#### 同步影响

- ✅ ADR 0023 状态：暂停 → **✅ 恢复**（v0.10.15 验证）
- ✅ GitHub Secrets 全部修正（`ECS_USER=admin` / `ECS_SSH_KEY=ed25519`）
- ✅ `.github/workflows/test.yml` actions 升 v6（node24 强制）
- ✅ `frontend/lib/transport.ts` `_batchIntervalMs` 修 ESLint dead code warning

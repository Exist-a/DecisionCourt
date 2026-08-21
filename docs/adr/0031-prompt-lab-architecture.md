# ADR 0031: Prompt Lab 架构 (LLM Prompt 版本管理 + Eval + A/B)

| | |
|---|---|
| **编号** | 0031 |
| **标题** | Prompt Lab 架构：baseRules YAML 化 + 热加载 + LLM-as-judge Eval + A/B Test |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-20 |
| **触发** | 用户 2026-08-20 "接下来做什么"+ "动画升级最后，其他随便"+ "做之前先产出规划文档等文档" |
| **依赖** | v0.10.24 候选 1 stance judge (LLM-as-judge 模式借鉴) + v1.0.2 PR-3 baseRules retry 通路 |
| **替代决策** | (a) LangSmith / Langfuse SDK / (b) 继续 hardcoded baseRules / (c) Web UI prompt 编辑器 |
| **影响** | `backend/prompts/base.yaml` (NEW) + `internal/promptlab/` (NEW) + `internal/agent/prompts.go` (MODIFIED) + `internal/api/handler_promptlab.go` (NEW) |

---

## 1. 决策

### 1.1 背景

`backend/internal/agent/prompts.go:16-77` 把 baseRules 写在 Go 字符串字面量里（~60 行），任何 prompt 调整都要：
- 改代码
- `go build`
- `docker build`
- `docker compose up`
- 完整链路重测

**单次"改 2 个字试效果"的迭代周期 ~30 分钟**，严重阻碍 prompt 调优。

v0.10.23 (候选 2) + v0.10.24 (候选 1) + v1.0.2 (候选 4) 累计加了 3 类 retry guard（novelty / stance / rebuttal），每类都涉及 prompt 调整。每次都要 commit + deploy 才能试。

### 1.2 矛盾诊断

调 prompt 是 LLM 应用的核心迭代活动，但当前流程与**迭代速度诉求**不匹配：

| 痛点 | 现状 | 影响 |
|---|---|---|
| 改 prompt 速度 | ~30 分钟/次 | prompt 调优慢 10x |
| 版本对比 | 无量化工具 | 不知道哪个版本更好 |
| 自动化评估 | 无 | 评估靠人工跑 trial |
| Audit trail | git log 但 diff 不直观 | 不知道 prompt 历史 |

### 1.3 替代方案对比

| 方案 | 优点 | 缺点 | 决策 |
|---|---|---|---|
| (a) LangSmith / Langfuse SDK | 生态完整, 自动 trace | 外部依赖, 账户管理, 上传 prompt 到第三方 | ❌ 不采纳 |
| (b) 继续 hardcoded baseRules | 简单 | 迭代慢 10x, 无法 A/B | ❌ 不采纳 |
| (c) Web UI prompt 编辑器 | 用户友好 | 当前用户只有 1 人 (Exist), 过度工程 | ❌ 不采纳 (后续 v1.1+ 视用户量决定) |
| **(d) YAML + 热加载 + Eval + A/B 自实现** | 无外部依赖, 内部基建, 复用现有 LLM | 工作量 1.5-2 周 | ✅ **采纳** |

### 1.4 实施决策

**采用 (d)：YAML + 热加载 + LLM-as-judge Eval + A/B 自实现**。

**关键设计**：
- **YAML 存储**：baseRules 提取到 `backend/prompts/base.yaml`，Go 代码保留 fallback hardcoded 字符串（YAML 加载失败时降级）
- **热加载**：文件 mtime > lastLoad → 自动 reload（`sync.RWMutex` 保护并发）
- **Eval 用现有 LLM**：用 DeepSeek v4 客户端做二次判定，温度 0.2（与 v0.10.24 stance judge 同）
- **A/B Test**：同时跑 2 个版本, 对每个 trial output eval, 求平均 score, 选赢家
- **REST API**：`/api/v1/prompts/{eval,abtest,version,reload}` 4 个端点

### 1.5 兼容性

| 维度 | 影响 |
|---|---|
| **现有 LLM 调用链** | 无影响（仅 baseRules 来源变了） |
| **现有 ReAct Runner** | 无影响（prompts.go 改动向后兼容） |
| **测试** | `prompts.go:baseRules()` 仍可接受 `(toolsBlock string)` 参数, 旧测试不需要改 |
| **生产部署** | YAML 文件随 docker image 打包（`prompts/` 目录 COPY 进 image） |
| **rollback** | YAML 文件删除 → 启动回退 hardcoded 字符串 |

---

## 2. 实施内容 (3 PR, 当前 ADR 文档)

### 2.1 Commit 表

| commit | 内容 | 当前 |
|---|---|---|
| (本 ADR 文档) | `docs(adr): Prompt Lab 架构 ADR 0031` | ✅ |
| (后续 PR-B1) | `feat(promptlab): baseRules YAML 化 + 热加载` | ⏸ |
| (后续 PR-B2) | `feat(promptlab): Eval + A/B Test + REST` | ⏸ |
| (后续 PR-B3) | `docs(release-notes): v1.0.3 release notes + tag` | ⏸ |

### 2.2 文件清单

**PR-B1 计划**:
- `backend/prompts/base.yaml` (NEW)
- `backend/internal/promptlab/loader.go` (NEW)
- `backend/internal/promptlab/store.go` (NEW)
- `backend/internal/promptlab/version.go` (NEW)
- `backend/internal/promptlab/loader_test.go` (NEW, 5 sub-test)
- `backend/internal/agent/prompts.go` (MODIFIED)
- `backend/cmd/server/main.go` (MODIFIED)
- `backend/.gitignore` (MODIFIED)

### 2.3 验证 (PR-B1 完成时)

| 验证项 | 期望 |
|---|---|
| `go test ./internal/promptlab/` | 5/5 PASS |
| `go test ./...` | 全包 PASS (0 退化) |
| 启动时 YAML 加载 | 日志 "loaded promptlab version 1.0.3-pr1" |
| 改 YAML 文件 mtime | 30s 内自动 reload (无需重启) |
| YAML 文件不存在 | 启动 fallback 到 hardcoded 字符串 + 警告日志 |
| YAML 格式错 | 启动 fallback + 错误日志 |

---

## 3. 教训总结

1. **prompt 调优是 LLM 应用的核心活动** — 应该有专门的工具链，不能寄希望于"改代码 + commit"
2. **热加载是基本要求** — 30s 内看到效果 vs 30 分钟等待 commit flow，迭代效率 ×60
3. **LLM-as-judge 自动评分** — 与 v0.10.24 stance judge 同模式，已实装过，无新风险
4. **不引入外部 SDK** — LangSmith / Langfuse 是大项目用的，单人维护的自实现最小集够用
5. **YAML > JSON** — YAML 支持注释和人类可读，version control diff 更直观

---

## 4. 后续工作

- [ ] PR-B1: YAML + Loader + 热加载 + fallback
- [ ] PR-B2: Eval + A/B + REST API
- [ ] PR-B3: v1.0.3 release notes + tag
- [ ] v1.0.4: Trace 可视化（基于 v1.0.3 Eval 数据 + 复用 baseRules YAML）
- [ ] v1.0.5: 剪影小人替换（基于 v1.0.4 Framer Motion）
- [ ] v1.0.x+ (待讨论): human feedback 收集 UI / per-trial 动态 prompt / Web UI 编辑器

---

## 5. 关联文档

- [V1.0.3-PLAN.md](../V1.0.3-PLAN.md) (本 ADR 的实施计划)
- [PRD §4.3.2 信念引擎](../decisioncourt-prd.md) (Eval 借鉴 LLM-as-judge 模式)
- [ADR 0021 LLM Hallucination Output Validator](../adr/0021-llm-hallucination-output-validator.md) (Eval 设计对齐)
- [v0.10.24 候选 1 stance judge](../release-notes/v0.10.24.md) (LLM-as-judge 模板)
- [V1-ROADMAP.md M2 v1.0.3 ✅](../V1-ROADMAP.md) (本里程碑)
- [v1.0.2 release notes](../release-notes/v1.0.2.md) (前置版本)
- [tech-spec.md §9 部署](../decisioncourt-tech-spec.md) (YAML 文件随 docker image 打包)
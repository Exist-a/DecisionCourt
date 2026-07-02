# ADR 0005: 调查发现独立表（与用户证据严格分离）

> **状态**：✅ Accepted & Implemented (2026-06-29)  
> **决策日期**：2026-06-29  
> **影响范围**：`internal/investigation/`、新增 `investigation_findings` 表、`GET /api/v1/courtrooms/:uuid/investigations` 端点

## 背景

v0.2 之前，调查员 WebSearch 产物直接写入 `evidences` 表。这带来严重的语义混淆：

1. **用户证据是"事实/约束"** —— 用户主动提交，控制权在用户
2. **调查发现是"LLM 派遣搜索结果"** —— 来源是外部 web，控制权在 LLM

两者混在同一张表 + 同一套 API + 同一份 UI 上，导致：

- 用户看到自己没提交的"证据 ID"出现在消息流里
- 无法区分"我提交的事实" vs "AI 找的引用"
- 调查发现能进入公共证据板影响信念 → 用户失去对信念引擎的部分控制权

## 选项对比

| 维度 | A. 同一表 + `source` 字段区分 | B. 独立表 `investigation_findings` | C. 调查发现 = 临时上下文，不入库 |
|---|---|---|---|
| UI 区分 | ⚠️ 需要按 source 过滤渲染 | ✅ 完全独立面板 | ❌ 不可回放 |
| 审计 | ⚠️ 同表查询 | ✅ 独立查询 | ❌ 丢失 |
| 用户控制信念 | ❌ 调查发现仍影响 | ✅ 不影响 | ✅ 不影响 |
| 复用 A2A Bus | ✅ | ✅ | ❌ |
| 代码复杂度 | ✅ 低 | ⚠️ 新表 + 新 service | ✅ 低 |

## 决策

采用 **方案 B** —— 调查发现写入独立表 `investigation_findings`。

### 关键不变量

1. **`evidences` 表仅由用户 `POST /evidences` 端点写入** —— LLM / 调查员都不能写
2. **`investigation_findings` 表仅由 `investigation.Service.RecordFinding` 写入**
3. **调查发现不更新信念** —— 仅作为 LLM 上下文（不影响 BeliefEngine.Update）
4. **调查发现不进入 `evidence_refs`** —— 律师发言只能引用 `evidences` 表里的 ID

### A2A 可见性

- `dispatch_investigator` / `investigation_report` 两条公开（**修正了 v0.3 错误的"private"语义**）
- 类比正常庭审记录对所有旁观者公开
- 前端 InvestigatorPanel 独立 Tab 展示

### 关键理由

- 用户对"事实"的控制权和 LLM 对"搜索结果"的控制权语义不同，必须分离
- 审计 + 回放需要独立表（不能让调查发现消失在 `evidences` 表里）
- 复用 A2A Bus 公开通道（`search.started` / `search.completed` 三事件齐全）

## 后果

### 收益

- ✅ 用户语义清晰（"我提交的证据" vs "AI 调查的结果"）
- ✅ 前端 InvestigatorPanel 独立展示调查活动
- ✅ 用户对信念引擎的控制权完整保留
- ✅ 10 项 service_test.go 测试覆盖 dispatch + 持久化 + 公开广播

### 代价

- ⚠️ 律师发言时**不**能引用调查发现 —— 可能错过一些有价值的搜索结果
- ⚠️ `investigation_findings` 表的 `raw_result` JSONB 体积可能较大（取决于搜索引擎返回）

## 关联

- 主文档：[`../decisioncourt-prd.md` §5.6](../decisioncourt-prd.md)
- UX 决策：[`../decisioncourt-ux-refinement.md`](../decisioncourt-ux-refinement.md)
- 代码：[`backend/internal/investigation/`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/investigation/)
- 模型：[`backend/internal/model/investigation_finding.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/model/investigation_finding.go)
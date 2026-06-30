# PR 1 Todo List：A2A ContextView 投影 + 私有 MessageType 扩展

> PR 范围：v0.5 重设计的第一步 — 为后续 PR 2-4 打地基  
> 预计工作量：1-2 天  
> 风险等级：**低**（纯新增能力，不破坏现有 12 项 a2a 测试 + 9 项 private_memory 测试）  
> 设计文档：[memory-a2a-redesign.md](./memory-a2a-redesign.md)

---

## 任务分解

### Phase A：基础设施层（新增，无破坏性）

- [x] **A1. 扩展 `MessageType` 常量**（[internal/a2a/types.go](file:///Users/zhuanzhuanmima0000/fullStack/DecisionCourt/backend/internal/a2a/types.go#L13-L31)）
  - 新增 4 个常量：
    - `MessageTypeStrategyNote MessageType = "strategy_note"`
    - `MessageTypeOpponentWeakness MessageType = "opponent_weakness"`
    - `MessageTypeSelfCorrection MessageType = "self_correction"`
    - `MessageTypeEvidenceEval MessageType = "evidence_eval"`
  - **验收**：`go build ./...` 通过 ✅

- [x] **A2. 验证现有 a2a 测试仍绿**（不破坏 API）
  - 执行 `go test ./internal/a2a/...`
  - **验收**：11/11 顶层 + 4/4 子测试 全绿 ✅

### Phase B：ContextView 投影层（核心新增）

- [x] **B1. 定义 `LLMContext` 数据结构**（`internal/a2a/context_view.go` 新文件）
  ```go
  type LLMContext struct {
      WorkingMemory   []model.A2AMessage  // public，对方消息已 sanitized
      PrivateMemory   []model.A2AMessage  // self-only，全部
      Beliefs         map[string]float64
  }
  ```
  - **验收**：struct 定义编译通过 ✅

- [x] **B2. 实现 `Bus.BuildContextView()` 方法**
  - 输入：`ctx, sessionID, selfAgent`
  - 步骤：
    1. `bus.ListVisibleTo(ctx, sessionID, selfAgent)` 拉所有可见消息
    2. 按 `visibility == private` 分流 → PrivateMemory 列表
    3. 对 public 消息判断 `FromAgent != selfAgent && FromAgent != AddressOrchestrator` → 调用 `SanitizedPayload()` 剥离 `reasoning`
    4. 返回 `LLMContext`
  - **验收**：编译通过 + 8 项 ContextView 测试通过 ✅

- [x] **B3. 实现 `Bus.SanitizeForViewer()` 辅助方法**
  - 输入：`message model.A2AMessage, viewerAgent string`
  - 逻辑：
    - 如果 `viewerAgent == AddressOrchestrator` 或 `viewerAgent == FromAgent` → 返回原消息（自己可见自己发的 reasoning）
    - 否则：调用 `SanitizedPayload()` 返回脱敏后的 message
  - **验收**：编译通过 + `TestSanitizeForViewer_Rules` 通过 ✅

### Phase C：测试层（PR 1 全部测试在这里定义）

- [x] **C1. 创建 `context_view_test.go`**（新文件，**10 项** 测试，覆盖 8 项必做 + 2 项 bonus）

  | # | 测试名 | 场景 | 期望 | 结果 |
  |---|---|---|---|---|
  | 1 | `TestContextView_Sanitize_PublicFromOtherSide_StripsReasoning` | 控方查辩方 public speech | `Payload.reasoning == ""` | ✅ PASS |
  | 2 | `TestContextView_Sanitize_PublicFromSelf_KeepsReasoning` | 控方查自己 public speech | `Payload.reasoning` 完整 | ✅ PASS |
  | 3 | `TestContextView_PrivateOnlySelfSees` | 控方查自己 private memory | 返回 1 条 | ✅ PASS |
  | 4 | `TestContextView_PrivateFromOtherSideHidden` | 控方查辩方 private memory | 返回 0 条 | ✅ PASS |
  | 5 | `TestContextView_OrchestratorSeesAll` | orchestrator 查 | 返回所有 public + private | ✅ PASS |
  | 6 | `TestContextView_RoundOrdering` | 多轮消息乱序插入 | 按 `created_at asc` 排序 | ✅ PASS |
  | 7 | `TestContextView_PrivateMemoryMessageTypes_AllVisible` | 4 种 private MessageType 都能识别 | 列表包含全部 4 个 | ✅ PASS |
  | 8 | `TestContextView_EmptySession` | 空 session | 返回空 LLMContext，nil error | ✅ PASS |
  | 9 (bonus) | `TestSanitizeForViewer_Rules` | SanitizeForViewer 单行 API 全规则 | orchestrator/自己/对方/private/empty viewer | ✅ PASS |
  | 10 (bonus) | `TestContextView_MalformedPayload_SkippedButEnvelopeKept` | 损坏 JSON 不导致崩溃 | envelope 保留 + payload fallback `{}` | ✅ PASS |

  - **验收**：10/10 通过 ✅

- [x] **C2. 运行 `go test ./internal/a2a/... -v`** 确认无回归
  - **验收**：11（旧顶层）+ 4（旧子）+ 10（新）= **25 项 全绿** ✅

- [x] **C3. 运行 `go test ./...`** 全仓库回归
  - **验收**：8 个测试包（a2a / agent / agent/tools / api / courtroom / investigation / private_memory / search）全绿 ✅

### Phase D：构建 + 静态检查

- [x] **D1. `go build ./...`**
  - **验收**：无编译错误 ✅

- [x] **D2. `go vet ./...`**
  - **验收**：无警告 ✅

- [x] **D3. `gofmt -l internal/a2a/{context_view.go,context_view_test.go,types.go}`** 确认 PR 1 新增/修改文件格式一致
  - **验收**：空输出 ✅
  - 备注：pre-existing 的 bus.go / bus_test.go / inmemory_repository.go 在 PR 之前就有 trailing newline 不规范问题，本次 PR 范围不动它们以保持 diff 聚焦

### Phase E：提交 + 文档同步

- [ ] **E1. 提交代码**（不直接 git commit，由用户决定）
  - 建议 commit message：`feat(a2a): PR1 ContextView projection + 4 private message types`
  - 涉及文件：
    - `internal/a2a/types.go`（+18 行 4 个常量）
    - `internal/a2a/context_view.go`（+240 行新增）
    - `internal/a2a/context_view_test.go`（+270 行新增）
    - `docs/decisioncourt-tech-spec.md` §13.2 PR 1 状态更新
    - `docs/decisioncourt-prd.md` §15.2 PR 1 状态更新

- [x] **E2. 更新 `docs/decisioncourt-tech-spec.md` §13.2 状态**
  - PR 1 的 2 项从 ⏳ 计划 → ✅ 已实装 ✅
  - 增加代码位置引用 ✅

---

## 验收门禁（PR 1 通过标准）

| # | 门禁 | 命令 | 期望 |
|---|---|---|---|
| 1 | 单元测试 | `go test ./internal/a2a/...` | 20/20 通过 |
| 2 | 全仓库回归 | `go test ./...` | 全部通过 |
| 3 | 编译 | `go build ./...` | 无错误 |
| 4 | 静态检查 | `go vet ./...` | 无警告 |
| 5 | 格式 | `gofmt -l internal/a2a/` | 空输出 |

**任何一项未通过 → PR 1 不通过。**

---

## 风险与回滚

| 风险 | 概率 | 缓解 |
|---|---|---|
| SanitizedPayload 副作用 | 低 | 已有方法 `Message.SanitizedPayload()` 是纯函数，零副作用 |
| BuildContextView 性能（50 条 × SanitizedPayload） | 低 | 纯 JSON map 拷贝，μs 级；如需优化可加 cache |
| 与现有 private_memory 包的命名冲突 | 低 | ContextView 用 `a2a.LLMContext`，private_memory 用 `private_memory.Entry`，命名空间隔离 |

**回滚策略**：
- PR 1 全部新增代码 + 测试，删除 `context_view.go` + `context_view_test.go` 即可回滚
- `MessageType` 新增 4 个常量是纯加法，无破坏性

---

## PR 1 完成后 → 启动 PR 2

PR 2 任务清单（仅预览，本文件不展开）：
- 扩展 `ReflectAction` JSON schema（加 `memory_type` + `memory_note`）
- `internal/agent/reflect_classifier.go` 自动 `a2aBus.Send(private)`
- ReActRunner 集成
- 集成测试：3 轮 cross-exam 验证 4 种 memory_type 都出现

---

## 进度跟踪

- Phase A：⬜ 未开始
- Phase B：⬜ 未开始
- Phase C：⬜ 未开始
- Phase D：⬜ 未开始
- Phase E：⬜ 未开始

**完成 PR 1 后更新本节为 ✅**。
# ADR 0043 — v2.9 PR-4 判决书考虑 rebuttal 状态 (Verdict Adoption Summary + belief_diffs Source=rebuttal)

| | |
|---|---|
| **状态** | ✅ Accepted |
| **日期** | 2026-09-21 |
| **作者** | ZCode Agent |
| **关联版本** | v2.9 |
| **触及 §2.1 裁决逻辑** | **✅ 是**（法官判决书 / 证据采纳 / 论证权重 — AGENTS.md §2.1 严格范畴） |
| **取代** | ADR 0030 §4 line 110 ("v1.0.3 候选: 法官判决书是否考虑 rebuttal 状态 — 后续讨论, 不在本 ADR 范围") |
| **被取代** | — |

---

## 1. 背景

ADR 0030 (v1.0.2) 实施 evidence_rebuttal_link 状态机时,明确**defer** 了一个开放问题:

> v1.0.3 候选: 法官判决书是否考虑 rebuttal 状态 (后续讨论, 不在本 ADR 范围)

理由: 候选 4 (rebuttal state machine) 的目的是"禁止引用被反驳证据",但**只到 agent 发言引用前**:
- Agent-side guard (`applySpeakerRebuttalRetryLoop`) — 律师引用 standing evidence 时硬拒
- ❌ 法官 final decision **没有**这条 guard
- 法官 LLM 只能从 `messages` 历史 + belief 值**间接推断**rebuttal state
- 翻盘证据 (overturned) 律师可以引用,但**法官看不到显式信号**

真实症状:
- 律师 A 反驳 E001 → standing (后台已记,但法官 LLM 不可见)
- 律师 B 翻盘 → overturned (后天 state,可 LLM 仍看不见)
- 法官判决 "为何采信/不采信" 时,这个 metadata 信息是**丢失**的

用户 2026-09-21 在 ExitPlanMode 后授权 v2.7+v2.8+v2.9 计划,本 ADR 实施 v2.9 PR-4。

---

## 2. 决策

### 2.1 用户决策 (已通过 AskUserQuestion 对齐)

| 维度 | 决策 | 实现约束 |
|---|---|---|
| **scope (全链路)** | prose + 结构化字段 (Verdict.EvidenceAdoption jsonb) + belief 回填 (Source=rebuttal 行) | 涉及数据模型 + LLM prompt + 渲染 |
| **standing 证据** | **硬指令: 完全忽略** | LLM hard-instructed "treat as not submitted" — 不许在 reasoning 引用 |
| **overturned 证据** | **引用带 caveat** | LLM 可引用但 reason 写"已被某方反驳 round N, 翻盘 round M" |
| **auto-overturn** | **暂不实装** | UpdateStatus 写路径不在 PR-4 范围 (deferred-items-2026-09-21 §D5) |

### 2.2 方案对比

| 维度 | A. 仅 verdict prose | **B. 全链路 prose + struct + belief (采用)** | C. + 重算 BeliefA/B |
|---|---|---|---|
| 数据新字段 | 0 | 1 (Verdict.EvidenceAdoption jsonb) | 1 + 重算 BeliefA/B |
| LLM prompt 改动 | 加 rebuttal 节 | 加 rebuttal 节 + 输出 schema 加 evidence_adoption 字段 | 同 B |
| belief 回溯 trail | 无 | 1 Source=rebuttal row per standing/withdrawn | + 回算历史 rows |
| 影响面 | 小 | **§2.1 范畴** + 数据模型 (GORM jsonb) | 同 B + 重算 CPU + flip verdict 风险 |
| 用户决策 | (放弃) | **采用** | (拒绝) |

**B 方案采用**: 用户 §2.1 决策认可。理由: schema 增量 + LLM prompt 增量, blast radius 最小, 不动 BeliefA/B (那已经是 judge LLM 直接给的值,重算会 flip verdict)。

### 2.3 设计要点

#### 2.3.1 数据模型增量

**Verdict.EvidenceAdoption jsonb** (`backend/internal/model/db.go`):

```go
type Verdict struct {
    ...existing fields...
    // v2.9 PR-4 (ADR 0043): per-evidence 采纳状态
    EvidenceAdoption *EvidenceAdoptionJSONB `gorm:"type:jsonb" json:"evidence_adoption,omitempty"`
}

type EvidenceAdoptionEntry struct {
    EvidenceID    string  `json:"evidence_id"`     // uuid
    DisplayID     string  `json:"display_id"`      // E001
    Status        string  `json:"status"`          // standing | overturned | withdrawn | adopted
    WeightApplied float64 `json:"weight_applied"`  // 0.0 = hard-ignored, 1.0 = full weight
    Reason        string  `json:"reason"`          // 中文说明
}

type EvidenceAdoptionJSONB []EvidenceAdoptionEntry
// + driver.Valuer + sql.Scanner 实现 (GORM jsonb 持久化 / hydration)
```

**Reuses 现有数据**: `evidence_rebuttal_links` 表 + `evidence_rebuttal_links.status` 状态机已在 v1.0.2 (ADR 0030) 实装。本 PR 完全复用, 不改 schema。

#### 2.3.2 belief_diffs Source 常量

`backend/internal/model/belief_diff.go`:

```go
const (
    BeliefSrcEvidence    = "evidence"
    BeliefSrcWeaken      = "weaken"
    BeliefSrcAnchorPull  = "anchor_pull"
    BeliefSrcStance      = "stance"
    BeliefSrcRebuttal = "rebuttal"  // v2.9 PR-4 NEW
)
```

**Trail 用途**: 每个 standing / withdrawn 证据写一条 `belief_diffs` row, `phase="verdict", round=0`, `EvidenceWeight=0`, `Direction=neutral`。让 audit trail 涵盖 verdict 阶段"法官判决时忽略该证据" 这条事实。

**不重算 BeliefA/B**: verdict 阶段的 BeliefA/B 由 judge LLM 直接输出,本 PR 不动。

#### 2.3.3 BuildAdoptionSummary 纯函数

`backend/internal/courtroom/rebuttal_adoption.go`:

```go
type AdoptionSummary struct {
    VerdictAdoption []model.EvidenceAdoptionEntry  // 给 Verdict.EvidenceAdoption
    BeliefDiffsRows []model.BeliefDiff              // 给 belief_diffs 表
    PromptRender    string                          // 给 JudgeFinalPrompt + ClerkPromptWithJudgeDecision 的 markdown section
    Counts          AdoptionCounts                  // log + dev observability
}

func BuildAdoptionSummary(sessionID, links, evidences) AdoptionSummary
```

**Latest-wins 语义**: 同一 evidence 有多条 link → 取最新一条 status。
- standing → overturned: 翻盘,最终 overturned
- overturned → standing: 退化,最终 standing (异常 case)
- 任何 overturned beats standing
- withdrawn beats "adopted"

**PromptRender markdown 模板** (`renderAdoptionPromptSection`):
- ## 证据反驳状态 (强制规则) — 4 类 status 的硬指令
- | Evidence ID | Status | 备注 | — markdown 表格, by display_id 排序
- 统计行: standing=X, overturned=Y, withdrawn=Z, adopted=W, 总计=N

`adoptionTableMaxRows = 200` 防止 session 有 500+ evidence 时 prompt 撑爆。

#### 2.3.4 Prompt 增量 (§2.1)

`backend/internal/agent/prompts.go`:

**JudgeFinalPrompt**:
- 签名加 `adoptionSummary string` (空字符串时跳过该 section,后向兼容 v2.8 + 未触发 rebuttal flow)
- "## 裁决原则" 后插入 "`adoptionSummary`" 渲染 (markdown)
- 输出 schema 不变 (JudgeDecision JSON)

**ClerkPromptWithJudgeDecision**:
- 签名加 `adoptionSummary string`
- "## 原则" 增 "原则 5: evidence_adoption 字段必须填" + 渲染表格
- 输出 schema 增 `evidence_adoption: [{...}]` 数组
- 二、证据认定 章节明确"按 evidence_adoption 字段写每条 evidence 采纳情况"

#### 2.3.5 Orchestrator 透传

`backend/internal/agent/orchestrator.go`:
- `JudgeFinalDecision` 签名加 `adoptionSummary string`
- `GenerateVerdict` 签名加 `adoptionSummary string`
- 透传给 prompts

#### 2.3.6 Service.finishTrial 集成 (courtroom package)

`backend/internal/courtroom/service.go`:

```go
// 在 JudgeFinalDecision 调用前
var adoptionSummary AdoptionSummary
if s.rebuttalRepo != nil {
    rebuttalLinks, _ := s.rebuttalRepo.ListBySession(ctx, session.ID)
    adoptionSummary = BuildAdoptionSummary(session.ID, rebuttalLinks, evidences)
}

judgeDecision, err = s.orchestrator.JudgeFinalDecision(verdictCtx, *judge, session, evidences, messages, adoptionSummary.PromptRender)

// 优先采用 LLM "evidence_adoption" 输出, fallback 到 BuildAdoptionSummary 硬填值
verdict.EvidenceAdoption = &model.EvidenceAdoptionJSONB(adoptionSummary.VerdictAdoption)
if rawAdop, ok := result["evidence_adoption"]; ok {
    if arr, ok := rawAdop.([]interface{}); ok && len(arr) > 0 {
        if llAdop, convErr := convertToEvidenceAdoptionEntries(arr); convErr == nil {
            llJSON := model.EvidenceAdoptionJSONB(llAdop)
            verdict.EvidenceAdoption = &llJSON
        }
    }
}

// belief_diffs rows 落库 (非 fatal)
if len(adoptionSummary.BeliefDiffsRows) > 0 {
    for i := range adoptionSummary.BeliefDiffsRows {
        if adoptionSummary.BeliefDiffsRows[i].ID == uuid.Nil {
            adoptionSummary.BeliefDiffsRows[i].ID = uuid.New()
        }
    }
    if err := s.db.Create(&adoptionSummary.BeliefDiffsRows).Error; err != nil {
        log.Printf("[finishTrial] v2.9 PR-4 belief_diffs Source=rebuttal insert failed (non-fatal): %v", err)
    }
}
```

**关键决策**: LLM 输出优先。LLM ClerkAgent 也能按 schema 填 `evidence_adoption`,如果输出有效就采用它,BuildAdoptionSummary 兜底。这种"两条来源,LLM 优先,程序兜底"模式与 v2.6 D3 文案类似。

#### 2.3.7 Frontend 证据采纳卡

`frontend/types/index.ts`:
- `Verdict.evidence_adoption?: EvidenceAdoptionEntry[]`
- 新建 `EvidenceAdoptionEntry` type

`frontend/app/verdict/[id]/page.tsx` (L563 起,庭审纪要之后,判决书正文之前):
- `<section>` + `border-l-2 border-clerk` style (与庭审纪要 border-judge 对称)
- `<table>` 4 列: 证据 / 状态 / 权重 / 备注
- 状态颜色 chip:
  - standing → `反驳有效` (bg-rose-900)
  - withdrawn → `撤回` (bg-stone-700)
  - overturned → `已翻盘` (bg-emerald-900)
  - adopted → `采纳` (bg-stone-500)
- 底部提示小字说明规则 (硬忽略 vs caveat)
- 仅 verdict.evidence_adoption 非空时显示 — 老 verdict (v2.9 之前) 完全跳过

---

## 3. 关键设计决策

1. **§2.1 prompt 硬指令而非 RAG**: 法官判决书采纳 rebuttal 状态最严格做法是 RAG (把 status 信息注入 prompt 的特定 query format),但这会引入新基础设施 (向量库 + 检索)。LLM hard-instructed rule + 证据采纳表已经覆盖 §2.1 决策,且 blast radius 最小。
2. **BuildAdoptionSummary 纯函数**: 不接 ctx, 不接 I/O,只算 3 组数据。Service 在调用前从 DB 读,调用后写 DB。便于单测 (TestBuildAdoptionSummary_* 9 个 sub-test PASS)。
3. **LLM 输出优先,程序兜底**: 两条来源合并 `evidence_adoption` 字段,LLM ClerkAgent 按 schema 填是首选 (LLM 能写出自然语言 reason), BuildAdoptionSummary 兜底 (程序不会出错)。这种"双源合并"避免 LLM 拒答或格式错时整个 v2.9 流程失效。
4. **不改 BeliefA/B 数值**: verdict 阶段的 BeliefA/B 是 judge LLM 直接给,不再重算。Source=rebuttal rows 是 audit trail,不是数值回溯。换言之"法官判决时**意识到** E001 standing"这条事实,不会改变"法官最终倾向选项 X"那条结论。这是 §2.1 用户决策"全链路 但不重算"的边界。
5. **Latest-wins link 状态语义**: 多条 link 时取最新一条。这是简化设计 (没有 git-like rebase), plan 阶段已与用户对齐。
6. **空 PromptRender 后向兼容**: 老 verdict path (rebuttalRepo 为 nil) 走不到 BuildAdoptionSummary, adoptionSummary.PromptRender 保持空字符串, JudgeFinalPrompt / ClerkPromptWithJudgeDecision 跳过 section。LLM 输出 schema 不变 (仅触发 rebuttal flow 的 session 输出 evidence_adoption)。
7. **EvidenceAdoptionJSONB driver.Valuer/sql.Scanner 模式**: GORM jsonb 持久化需要自定义 Value + Scan 方法。bunt 类型 slice 直接给 GORM 有序列化不可控问题, wrapper type 让 nil = NULL (老 verdict 行证据)。Frontier align 现有 `ConsensusPoints / DivergencePoints jsonb` 模式 (后者用 `string` 存 JSON,本 PR 用 wrapper type 是更现代做法)。
8. **adoptionTableMaxRows=200**: 经验值, 1 trial 通常 ≤ 20 evidence, 200 是 10x 上限。超出 truncate + "(省略...)" 提示。保护 LLM prompt 不过大。

---

## 4. 改动清单

### 4.1 Backend (8 files)

| 文件 | 改动 |
|---|---|
| `backend/internal/model/db.go` | Verdict + EvidenceAdoptionJSONB + EvidenceAdoptionEntry type, GORM jsonb wrapper |
| `backend/internal/model/belief_diff.go` | 新增 `BeliefSrcRebuttal = "rebuttal"` 常量 |
| `backend/internal/courtroom/rebuttal_adoption.go` (NEW) | BuildAdoptionSummary + AdoptionSummary type + renderAdoptionPromptSection + AdoptionCounts |
| `backend/internal/courtroom/convert_evidence_adoption.go` (NEW) | LLM `evidence_adoption` JSON → model.EvidenceAdoptionEntry 转换, 失败不 panic |
| `backend/internal/courtroom/service.go` | finishTrial: load rebuttal_links + BuildAdoptionSummary + 传 PromptRender 给 Judge/Generate + 写 Verdict.EvidenceAdoption + belief_diffs rows 落库 |
| `backend/internal/agent/prompts.go` | JudgeFinalPrompt + ClerkPromptWithJudgeDecision 加 adoptionSummary 参数 + 渲染 section + 输出 schema 加 evidence_adoption |
| `backend/internal/agent/orchestrator.go` | JudgeFinalDecision + GenerateVerdict 加 adoptionSummary 参数, 透传给 prompts |
| `backend/internal/agent/orchestrator_verdict_retry_test.go` | retry hook tests 加 "" 占位参数 (后向兼容) |
| `backend/internal/courtroom/rebuttal_adoption_test.go` (NEW) | 9 个 sub-test (Empty / NoLinks / Standing / Overturned / Withdrawn / LatestWins / MixedStates / PromptRender / TableMaxRows) |

### 4.2 Frontend (2 files)

| 文件 | 改动 |
|---|---|
| `frontend/types/index.ts` | Verdict 加 evidence_adoption?: 新建 EvidenceAdoptionEntry |
| `frontend/app/verdict/[id]/page.tsx` | 庭审纪要 + 判决书正文之间 新 card "证据采纳" — `<table>` 4 列 + 状态 chip 颜色 (rose/stone/emerald) + 说明小字 |

### 4.3 Docs (this file + others)

- `docs/adr/0043-rebuttal-aware-verdict.md` (NEW)
- `docs/release-notes/v2.9.md` (NEW)
- `docs/V1-ROADMAP.md §0 + §6` 更新
- `docs/todo/deferred-items-2026-09-21.md §D5` (auto-overturn wiring, 维持 deferred 状态但范围明确)
- `docs/adr/0030 §4` line 110 deferred 项 → ✅ "v2.9 PR-4 落地"

---

## 5. 测试覆盖

### 5.1 Backend (9 new sub-test)

`rebuttal_adoption_test.go` (9):
- `TestBuildAdoptionSummary_EmptyEvidences` — 空 evidences 返空 summary
- `TestBuildAdoptionSummary_NoLinks_AllAdopted` — 无 link 全 adopted, weight=1.0, no belief_diffs
- `TestBuildAdoptionSummary_Standing_HardIgnored` — standing → weight=0, 1 belief_diffs row Source=rebuttal Direction=neutral
- `TestBuildAdoptionSummary_Overturned_CiteWithCaveat` — overturned → weight=1, no belief_diffs
- `TestBuildAdoptionSummary_Withdrawn_HardIgnored` — withdrawn → weight=0, 1 belief_diffs row
- `TestBuildAdoptionSummary_LatestWins` — 同一 evidence 多条 link,取最新 status
- `TestBuildAdoptionSummary_MixedStates_CountsCorrect` — 4 类状态混合, Counts 正确
- `TestBuildAdoptionSummary_PromptRender_ContainsMandateSection` — markdown 包含 "## 证据反驳状态" / 强制规则 / 表格头
- `TestBuildAdoptionSummary_TableMaxRows` — 250 evidence → markdown truncate + "省略"

### 5.2 全包回归

`go test ./...` 22 包全 OK, 0 regression。
老 retry hook 测试 (`orchestrator_verdict_retry_test.go` 7 sub-test) 加 "" 占位参数保持 PASS。

### 5.3 Frontend

无新增 test (样式 + 数据展示,SSR-safe fixture 覆盖)。

---

## 6. 不做的事 (明确边界)

- ❌ 不动 BeliefA/B 数值 — verdict 阶段还是 judge LLM 直接给 (用户决策 §2.1)
- ❌ 不实装 UpdateStatus auto-overturn wire — deferred-items-2026-09-21 §D5 (用户决策 §2.1)
- ❌ 不做 EvidenceAdoption 二级索引 (jsonb 没法直接 index) — verdict 渲染走 application-level 扫描
- ❌ 不引入 RAG / 向量库 — prompt 硬指令 + 证据采纳表已经覆盖 §2.1 决策
- ❌ 不动 evidence_rebuttal_link 表 schema — v2.9 完全复用 v1.0.2 (ADR 0030)
- ❌ 不写前端 SSR test — 现有 TrialReplay.test.ts SSR-safe 覆盖,样式调整无需测试
- ❌ 不动 promptlab sanitize helper — adoptionSection markdown content 没经过 sanitization (内嵌纯模板,不是 user input)

---

## 7. 文档同步

- `docs/release-notes/v2.9.md` (NEW)
- `docs/V1-ROADMAP.md §0 + §6` v2.9 进度行
- `docs/adr/0030 §4` line 110 → ✅ "v2.9 落地"
- `docs/todo/deferred-items-2026-09-21.md §D5` 范围明确 (auto-overturn wiring, 仅写路径层面)

---

## 8. 关联文档

- [ADR 0030 候选 4 已反驳证据集合跟踪](./0030-evidence-rebuttal-state-machine.md) — v1.0.2 实装状态机, 本 ADR 实施其 §4 deferred 项
- [release-notes/v2.9.md](../release-notes/v2.9.md) — v2.9 实施细节 + 测试 + 时间线
- [todo/deferred-items-2026-09-21.md §D5](../todo/deferred-items-2026-09-21.md) — auto-overturn 维持 deferred
- [V1-ROADMAP.md §0 + §6](../V1-ROADMAP.md) — v2.9 进度 + 持续维护
- [PRD §4.3.3](../decisioncourt-prd.md) — 候选 4 原始定义

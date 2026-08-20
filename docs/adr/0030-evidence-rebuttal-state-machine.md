# ADR 0030: 已反驳证据集合跟踪状态机 (EvidenceRebuttalLink)

| | |
|---|---|
| **编号** | 0030 |
| **标题** | 候选 4: 已反驳证据集合跟踪状态机 |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-20 |
| **触发** | 用户 2026-08-20 授权 v1.0.2 roadmap §0 "下一步进入候选4 讨论" + PRD §4.3.3 ⏳ 项落地 |
| **依赖** | ADR 0004 (贝叶斯信念引擎) + ADR 0008 (v0.6 异构论辩图谱 weaking 边) + v0.10.23/24 候选 2/1 (retry 通路模式) |
| **替代决策** | (a) Evidence 表加字段 / (b) 复用 EvidenceWeakenLink / (c) prompt 软提示 |
| **影响** | `backend/internal/model/evidence_rebuttal_link.go` (NEW) + GORM AutoMigrate + 后续 5 PR |

## 1. 决策

### 1.1 背景

PRD §4.3.3 (决策 2026-07-01 后) L163:

> ⏳ **v0.7+ 计划**：禁止引用已经被反驳且未翻盘的证据 —— MVP 阶段 LLM 自由引用，未做"已反驳证据"集合跟踪。

roadmap §0 顶部 2026-08-20 状态行:

> 下一步进入"候选 4 已反驳证据集合跟踪"讨论。

PRD §4.3.2 L155-157 + §4.3.3 L162-164 已经实装"强制立场一致性检查" (v0.10.24 候选 1) + "新意度检查" (v0.10.23 候选 2) + "300 字硬截断" (v0.10.21 PR-B), 但**候选 4 (已反驳证据) 一直未实装**。

### 1.2 矛盾诊断

候选4 的核心是 3 个相互耦合的设计选择:

1. **数据模型**: Evidence 表加字段 (简单) vs 独立关系表 (可扩展)
2. **状态机粒度**: 单一状态 vs 严格状态机 (standing / overturned / withdrawn)
3. **执行约束**: prompt 软提示 (依赖 LLM) vs 后端硬拒 (强制)

三者必须一起设计。如果选 prompt 软提示,数据模型简单 (Evidence 加字段) 即可;如果选后端硬拒,需要关系型表 + 状态机 + Service 集成。

### 1.3 替代方案对比

| 方案 | 数据模型 | 状态机 | 约束 | 优点 | 缺点 |
|---|---|---|---|---|---|
| **(a) Evidence 表加字段** | `RebuttedBy *uuid` + `Status` | 单一状态 | prompt 软提示 | 改动最小 | 不支持多轮链;无法 overturn |
| **(b) 复用 EvidenceWeakenLink** | 加 RebuttalType 字段 | 单状态 | prompt 软提示 | 不建新表 | 语义混杂 (weaken 削弱传播, rebuttal 反驳内容) |
| **(c) 独立表 + 严格状态机 + 后端硬拒** | NEW `evidence_rebuttal_links` | standing/overturned/withdrawn | 2次 retry + fallback rejected | 与 existing weaken 对称, 支持多轮链, 强制 guard | 改动大 (~8h) |

### 1.4 实施决策

**采用 (c)**: 严格状态机 + 后端硬拒。

理由:
1. **PRD 原文"禁止引用"是强制语义**, 不是提示。prompt 软提示实测 (v0.10.17 静默错误历史) 不可靠
2. **与 existing 候选 1/2 同等级**: stance judge (v0.10.24) 和 novelty check (v0.10.23) 都用 "2 次 retry + fallback reject" 模式, 候选4 应一致
3. **多轮反驳链是真实需求**: 律师 A 反驳 E001, 律师 B 想"翻盘"(指 E001 是误读) → 需要 standing→overturned 状态机
4. **关系型表与 weaken_link 对称**: 后续如果有"已质疑 vs 已反驳"双重语义, 也能扩展

### 1.5 兼容性

- **Evidence 表**: 完全不动, **0 schema 变更**
- **BeliefDiff 表**: 加 `BeliefSrcRebuttalState` 常量 (后续 PR-3), 但不改列
- **Speaker 结构**: 加 `RebuttalRejected bool` + `RebuttalViolations []string`, **JSON omitempty 向后兼容** (旧后端 / 旧前端不显示)
- **AgentOutput**: 加 `Rebut []RebuttalDeclaration` + `HasRebuttal()` + `ValidRebuttalDeclarations()`, 旧 LLM 不输出 rebut 字段 → 后端默认空数组 → 行为兼容
- **REST API**: 加 `GET /api/v1/courtrooms/:uuid/rebuttal-links` 端点, 旧客户端不感知

## 2. 实施内容 (6 PR, 1 commit 当前)

### 2.1 Commit 表

| commit | 内容 | 当前 |
|---|---|---|
| (本 commit) | `feat(model): EvidenceRebuttalLink 表 + AutoMigrate (ADR 0030 PR-1)` | ✅ |

后续 5 PR (在本 ADR 后续阶段实施):
- PR-2: AgentOutput RebuttalDeclaration + Speaker 字段 + RebuttalHook
- PR-3: applySpeakerRebuttalCheck 后端硬拒
- PR-4: Service 集成 + GORM repo + REST 端点
- PR-5: baseRules rebut schema 引导 + Frontend EvidenceBoard chip
- PR-6: 测试 + 文档 + tag

### 2.2 修改文件 (当前 PR-1)

| 文件 | 类型 | 改动 |
|---|---|---|
| `backend/internal/model/evidence_rebuttal_link.go` | NEW | 完整 struct + TableName + 3 status 常量 |
| `backend/internal/model/evidence_rebuttal_link_test.go` | NEW | 4 sub-test (TableName / StatusConstants / DefaultStatus / FieldsRequired) |
| `backend/internal/model/db.go` | 修改 | AutoMigrate 列表加 `&EvidenceRebuttalLink{}` |

### 2.3 验证

| 验证项 | 结果 |
|---|---|
| `go test ./internal/model/` | ✅ 4/4 PASS |
| `go build ./...` | ✅ 通过 |
| `go test ./...` | ✅ (其他包不受影响) |

## 3. 教训总结

1. **roadmap §0 顶部钦定的"下一步"是高质量优先级信号**: 用户 v1.0.1 修完测试后立刻启动候选4, 这是 roadmap 设计意图的体现
2. **PRD ⏳ 项与代码实装的距离 ≠ 简单**: 候选4 在 PRD 只是一句话, 但实际涉及 数据模型 + 状态机 + 后端硬拒 + prompt + UI + 测试 6 个 PR
3. **状态机命名选择**: standing/overturned/withdrawn 是法律术语 (法庭辩论常见词), 比 open/closed/deleted 更表意
4. **PR 拆分原则**: 6 PR 按"数据 → 协议 → guard → service → UI → 文档"分层, 任何 PR 都可以独立 revert, 风险可控

## 4. 后续工作

- [ ] PR-2: AgentOutput.Rebut + Speaker.RebuttalRejected (输入协议)
- [ ] PR-3: applySpeakerRebuttalCheck (与 stance/novelty 同等级 guard)
- [ ] PR-4: Service.defaultRebuttalHook + GORM RebuttalRepository + REST 端点
- [ ] PR-5: baseRules rebut schema + Frontend chip
- [ ] PR-6: ecs_regression_v102_test + v1.0.2 release notes + tag
- [ ] v1.0.3 候选: 法官判决书是否考虑 rebuttal 状态 (后续讨论, 不在本 ADR 范围)

## 5. 关联文档

- [PRD §4.3.3 防止诡辩与重复](../decisioncourt-prd.md) - 候选4 原始定义
- [PRD §4.3.2 信念引擎](../decisioncourt-prd.md) - 现有 belief_diffs 不变
- [roadmap §0 顶部 2026-08-20 状态行](../decisioncourt-roadmap.md) - "下一步进入候选4 讨论"
- [V1-ROADMAP M1 v1.0.2 候选 4](../V1-ROADMAP.md) - M0.5 v1.0.1 → M1 v1.0.2 衔接
- [release-notes/v1.0.2.md](../release-notes/v1.0.2.md) - 完整发版说明 (PR-6 写)
- [ADR 0004 贝叶斯信念引擎](./0004-bayesian-belief-engine.md) - 信念引擎不直接感知 rebuttal 状态
- [v0.10.23 候选 2 新意度 Jaccard](../release-notes/v0.10.23.md) - retry 模式借鉴
- [v0.10.24 候选 1 stance judge](../release-notes/v0.10.24.md) - guard 等级对齐
- [EvidenceWeakenLink](../model/evidence_weaken_link.go) - v0.6 引入, 与本表对称
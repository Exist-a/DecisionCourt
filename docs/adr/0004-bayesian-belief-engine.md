# ADR 0004: v0.6 信念引擎升级（贝叶斯 log-odds + 锚定）

> **状态**：✅ Accepted & Implemented (2026-07-01)  
> **决策日期**：2026-06-30  
> **影响范围**：`internal/belief/engine_v06.go`、`internal/belief/anchoring.go`、`internal/belief/convergence.go`、新增 `belief_diffs` / `evidence_weaken_links` 表

## 背景

MVP 版的信念更新公式是"加法 + clip"（[`decisioncourt-agent-design.md` §4.3`](../decisioncourt-agent-design.md)），存在 3 个问题：

1. **不对称性问题**：新证据对信念的影响不随当前信念度变化而调整 —— 即使 belief_a 已达 0.95，单条支持 a 的证据仍能再加 0.15（违反常识）
2. **锚定缺失**：律师角色（控方/辩方）的初始信念倾向（prior 0.7/0.3）容易被一次反向证据推翻
3. **审计缺失**：信念变化没有 trail，无法回答"为什么法官最终判 A"

业界参考：Belief Engine 2026 / ScioMind 2026 的贝叶斯 log-odds + 锚定公式；PROCLAIM 2026 关于"高一致可能是错的"的警示（推理震荡信号）。

## 选项对比

| 维度 | A. 加法+clip（MVP） | B. 贝叶斯 log-odds + 锚定 | C. 神经网络建模 |
|---|---|---|---|
| 抗单条证据翻转 | ❌ 差 | ✅ 锚定保 | ✅ |
| 计算成本 | ✅ 低 | ✅ 低 | ❌ 高 |
| 信念审计 trail | ❌ 无 | ✅ logit 域可重放 | ❌ 黑盒 |
| 可解释性 | ✅ | ✅ | ❌ 差 |
| 业界映射 | ❌ 无 | ✅ Belief Engine 2026 | ⚠️ 实验性 |

## 决策

采用 **方案 B** —— 贝叶斯 log-odds + 锚定 + weaken 边。

### 核心公式

```
logit(p)_{t+1} = (1 - Anchor) · [logit(p)_t + Uptake · w · sign · ln(2)]
                 + Anchor · logit(PriorA)
w              = cred · relevance · |impact| · (1 - maxWeaken)
sign           = +1 (confirmation) | -1 (contradiction) | 0 (neutral)
new_p          = sigmoid(logit(p)_{t+1}), clamp 到 [0.05, 0.95]
```

### 角色参数表

| 角色 | PriorA | Uptake | Anchor | 设计意图 |
|---|---|---|---|---|
| 控方 | 0.7 | 0.4 | 0.7 | 强锚定 A，受确认放大但难被反向推翻 |
| 辩方 | 0.3 | 0.4 | 0.7 | 对称：强锚定 B |
| 调查员 | 0.5 | 0.8 | 0.2 | 弱锚定（吃 evidence 方向） |
| 书记员 | 0.5 | 0.0 | 1.0 | Uptake=0 完全不更新 |
| 法官 | 0.5 | 0.6 | 0.3 | 弱锚定（综合双方） |

### 智能收敛（多信号按优先级触发）

1. **推理震荡**（PROCLAIM 2026 最高优）：最近两条来自不同 agent 的发言 Jaccard > 0.6
2. **双方共识**：控方+辩方都偏向同一侧（都 ≥0.85 或都 ≤0.15）
3. **信念稳定**：连续 N 轮单轮最大 Δ < 0.05
4. **最大轮次兜底**：达到 5 轮强制结束

### 新增数据表

- `belief_diffs` —— 每次 update 写一行（含 prior/posterior/Δ/logit/weight/weaken_factor/reason），用于审计 + 回放
- `evidence_weaken_links` —— 律师可主动质疑某条 evidence 对某目标 agent 的影响（异构论辩图谱）

### 关键理由

- 贝叶斯公式有成熟数学保证，信念随证据积累自然收敛
- 锚定参数让律师角色保持对抗性（即使强证据也不能让辩方瞬间叛变）
- `belief_diffs` trail 让前端 BeliefDiffCard 渲染 + 离线回放 / 合规审计
- PROCLAIM 2026 警示"高一致可能是错的" —— 推理震荡信号优先于共识

## 后果

### 收益

- ✅ 抗单条证据翻转（锚定保证律师角色稳定性）
- ✅ 信念变化可审计 / 可回放（logit 域重放）
- ✅ 智能收敛从单信号升级到 4 信号多优先级，更稳健
- ✅ 控辩双方互相 weaken 边形成"异构论辩图谱"（专利点）

### 代价

- ⚠️ 引擎参数表（PriorA / Uptake / Anchor）需要调优 —— 当前值参考 Belief Engine 2026，尚未在大量真实庭审数据上回归
- ⚠️ `belief_diffs` 表每次 update 写一行，量级约 20-50 行 / 庭审（可接受）

## 关联

- 主文档：[`../decisioncourt-prd.md` §4.3.2](../decisioncourt-prd.md)
- 代码：[`backend/internal/belief/engine_v06.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/belief/engine_v06.go)、[`backend/internal/belief/anchoring.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/belief/anchoring.go)、[`backend/internal/belief/convergence.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/belief/convergence.go)
- 模型：[`backend/internal/model/belief_diff.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/model/belief_diff.go)、[`backend/internal/model/evidence_weaken_link.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/model/evidence_weaken_link.go)
# 03 · 贝叶斯信念引擎（v0.6）—— 让 AI 法官"相信"用数学说话

> **目标**：用第一人称 + 工程视角，讲清楚 v0.6 贝叶斯 log-odds 信念引擎**为什么这么设计**、**怎么实装**、**如何在面试中讲出来**。
> **配套**：[`../architecture/link-overview.md`](../architecture/link-overview.md) §3 · [`../adr/0007-belief-engine-v06.md`](../adr/0007-belief-engine-v06.md) · [`../observability/case-study-2026-07-02.md`](../observability/case-study-2026-07-02.md)
> **更新于**：2026-07-02
> **版本**：v1.0

---

## 0. 一句话总结

> AI 法官在庭审中"对双方主张的相信度"**不用 0-100 主观分**，**用贝叶斯 log-odds（logit）数学模型**。每条证据触发一次 `Bayesian Update`，写入 `belief_diffs` 审计表，**让"信念变化"是可追溯、可解释、可回放的**。

---

## 1. 为什么不用 0-100 评分？

### 1.1 业内常见做法（我调研过）

多数 AI Judge 系统用：
- LLM 直接 prompt "给 option_a 打 60 分 option_b 打 40 分"
- 或者用一个 0-1 的标量
- 然后取大者当推荐

### 1.2 这个做法的 3 个本质问题

| 问题 | 后果 |
|---|---|
| **主观** | LLM 凭"感觉"打分，无法解释"为什么 60 不是 70" |
| **不可回放** | "60→70 是哪条证据引起的？"答不上 |
| **不可审计** | 法庭场景（推荐 = 推荐买房 / 投资 / 离婚）需要**可追溯** |

**举例**：用户提交"借款 5 万"证据后，LLM 说"控方 60 分 → 65 分"。**为什么是 65 不是 64？凭什么？用什么数学？**—— 0-100 评分答不上。

### 1.3 我的设计选择：**贝叶斯 log-odds**

让"相信度"是**数学运算的结果**，不是 LLM 凭空给分。

**核心思想**：把"我相信 A 还是 B"抽象成一个**对数优势比**（logit）：
```
logit(p) = log(p / (1 - p))
```

- `p = 0.5` → `logit = 0`（中性，不偏不倚）
- `p = 0.7` → `logit ≈ +0.847`（倾向 A）
- `p = 0.9` → `logit ≈ +2.197`（强烈倾向 A）

**贝叶斯更新规则**（简化版）：
```
new_logit = old_logit + evidence_logit
```

每条证据相当于在"信念天平"上加一个砝码，**叠加线性可解释**。

---

## 2. 工程实装（v0.6 怎么落地的）

### 2.1 数据结构

[`backend/internal/belief/diff.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/belief/diff.go)：每条 `belief_diff` 是**一次 Bayesian 更新步骤的完整审计行**。

```go
type BeliefDiff struct {
    ID                uuid.UUID   // 审计行 ID
    SessionID         uuid.UUID
    Round             int
    Phase             string      // "evidence" / "weaken" / "anchor_pull"
    AgentType         AgentType   // 谁更新了信念
    EvidenceID        *uuid.UUID  // 哪条证据触发（weaken 不需要）
    Source            string      // 来源（evidence / weaken / anchor_pull）
    Direction         string      // supports_a / supports_b / neutral
    PriorBeliefA      float64     // 更新前 (0-1)
    PosteriorBeliefA  float64     // 更新后 (0-1)
    DeltaBeliefA      float64     // 变化量
    PriorLogit        float64     // 更新前 logit
    PosteriorLogit    float64     // 更新后 logit
    EvidenceWeight    float64     // 证据权重（0-1）
    WeakenFactor      float64     // 被对方 weaken 的折扣
    Reason            string      // 人类可读原因（截断 80 字）
    CreatedAt         time.Time
}
```

**为什么用 logit 字段**：数学可加性 = 可解释性 + 可审计。LLM 论证里看到 `prior_logit=1.098, posterior_logit=0.847, delta=-0.251` 比 `60→53.8 分` **可解释 100 倍**。

### 2.2 更新公式（v0.6 简化版）

[`backend/internal/belief/engine_v06.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/belief/engine_v06.go:90)：

```go
// 1. 计算 evidence 在 logit 空间的"权重"
//    weight = log(credibility * relevance * |impact|)
//    （简化：3 个 0-1 因子乘积 + 对数化）
w := math.Log(1 + evidence.CredibilityScore*evidence.RelevanceScore*math.Abs(impact))

// 2. 方向（支持 A 还是 B）
direction := sign(impact_on_option_a)  // +1 / -1 / 0

// 3. 应用 weaken 折扣（对方质疑此证据）
effective_w := w * (1 - weaken_factor)  // 0.7 weaken → 30% 折扣

// 4. 更新
new_logit := prior_logit + direction*effective_w
new_p := sigmoid(new_logit)  // = 1 / (1 + e^-logit)
```

**关键设计**：
- **3 个 0-1 因子乘积** —— 任何一个接近 0，证据"几乎无效"
- **weaken 折扣** —— 对方质疑此证据时，折扣按比例衰减
- **logit 空间加法** —— 多条证据**独立可加**，合并更新为一次

### 2.3 4 个 Agent × 每个 evidence = 4 条 belief_diff

每次用户提交 1 条 evidence，**4 个 AI Agent（控方 / 辩方 / 调查员 / 法官）独立跑贝叶斯更新**，生成 4 条 belief_diff 行。

**为什么每个 Agent 独立算？**：每个 Agent 有"自己的视角"和"自己的 prior"。**即使同一条证据，控方和辩方会从不同角度解读**，得到不同的 delta。

**用户案例**（来自 case-study）：
```
evidence "有点累了" submitted
  → 控方 logit: +1.098 → +0.918 (Δ -0.18, supports_b / "累了影响学习")
  → 辩方 logit: -1.098 → -0.918 (Δ +0.18, supports_b / 同上但从辩方视角)
  → 调查员 logit: 0 → -0.024 (Δ -0.024, supports_b / 中性偏辩)
  → 法官   logit: 0 → -0.039 (Δ -0.039, supports_b / 中性偏辩)
```

**4 条不同 belief_diff** —— 同一证据，4 个解读，全部审计。

---

## 3. 多信号收敛（v0.6 核心数学）

庭审的"收敛" = **AI 法官什么时候判定"够了，做出判决"**。

### 3.1 业内常见做法（不严谨）

- 跑满 N 轮强制结束
- LLM 说"我判完了"
- 阈值一刀切

### 3.2 我的做法：3 个信号 + 1 个 OR 逻辑

[`backend/internal/belief/convergence.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/belief/convergence.go)：

| 信号 | 含义 | 阈值 |
|---|---|---|
| **variance 阈值** | 4 个 Agent 信念方差 | < 0.01（高度一致） |
| **delta 阈值** | 双方信念差异 | logit 差异 > 2.0（一面倒） |
| **时间窗口** | 多少轮没新证据 | ≥ 2 轮没新影响（停滞） |

**OR 逻辑**：任一信号触发即收敛。

### 3.3 为什么用 OR 而不是 AND

- AND：要求"高度一致 + 一面倒 + 停滞"同时满足 → 可能永远不收敛
- OR：任一信号足够 → **至少给用户"可解释的"收敛理由**

**业务例子**：
```
3 轮 cross_exam 后，4 Agent 信念方差 = 0.008（< 0.01）→ 收敛
  收敛原因: "variance_below_threshold (4 个 Agent 信念已高度一致)"
  → 不需要等到"双方差异 > 2.0"
```

---

## 4. audit trail = `belief_diffs` 表

`belief_diffs` 表是 v0.6 的**核心交付物**。

### 4.1 表 schema（关键列）

| 列 | 用途 |
|---|---|
| `id` | 唯一 ID（v0.8.3 修复——见 case-study） |
| `session_id` | 关联到庭审 |
| `round` / `phase` | 发生在哪轮 / 哪阶段 |
| `agent_type` | 谁更新 |
| `evidence_id` | 哪条证据触发 |
| `prior_logit` / `posterior_logit` | 更新前后的 logit |
| `delta_belief_a` | 信念变化量 |
| `reason` | 人类可读原因 |

### 4.2 4 个核心能力

1. **可追溯**：给定 `session_uuid`，SELECT 全部 belief_diffs 即得完整信念变化 timeline
2. **可解释**：每个 delta 都源自具体 evidence + 具体 weaken + 具体 agent，**LLM 不能"凭感觉"**
3. **可回放**：用 belief_diffs 重新跑出最终 verdict（不需要再调 LLM）
4. **可校验**：法庭场景下，"为什么法官判 A 不判 B？"答："E001-E004 共 16 条 belief_diff，平均 logit 倾向 A 为 +0.847"——**可引用、可复核**

---

## 5. 法官 verdict 怎么用 belief？

[`backend/internal/courtroom/service.go`](file:///d:/源码/FullStack/DecisionCourt/backend/internal/courtroom/service.go) 的 `GenerateVerdict` 函数：

### 5.1 流程

1. **聚合 belief** —— SELECT AVG(posterior_belief_a) FROM belief_diffs WHERE ...
2. **构造 prompt** —— 把 16 条 belief_diff + 4 段庭审 transcript + 收敛原因塞给 LLM
3. **LLM 调用** —— 让 LLM 写判决书（Markdown 格式）
4. **存 verdict** —— 包含 option_a_score / option_b_score / recommendation / trial_summary

### 5.2 真实案例（来自 [case-study](../observability/case-study-2026-07-02.md) §2）

```
session = "我要学习吗？"
verdict:
  option_a_score: 0.56  (学习)
  option_b_score: 0.44  (不学)
  recommendation: "建议选择学"
  trial_summary: "控方引入E004及科学证据反驳辩方主观证据，最终比分在第3轮后拉开"

LLM 接受了 belief engine 计算的 ~0.56 vs 0.44，**没有推翻算式结果**。
```

**关键**：LLM 写判决书的**输入是数学算出来的**（不是 LLM 凭空打分），所以判决书**总是有"为什么"**。

---

## 6. 防质疑思考（面试常问）

### Q1: "为什么不直接 LLM 输出 0-100 分？"

> 业内做法简单（一个 prompt），但**主观**、**不可审计**、**不可回放**。我的设计**让数学解释数学**，**让审计追溯每一处变化**，**让法庭（高 stakes 场景）的判决有据可查**。

### Q2: "贝叶斯太学术，能不能用更简单的？"

> 可以。但**v0.6 我选贝叶斯因为**：
> 1. 加性：logit 空间加法 = 数学可解释
> 2. 概率自然有界（0-1）
> 3. weaken / anchor 边都能进同一个算式
> 4. 教科书级别，面试加分

### Q3: "4 个 Agent 独立算，效率不高吧？"

> 每次 1 evidence → 4 belief_diff 数据库写 + 4 broadcast。是 4 次 SQLite-level 写，不是 4 次 LLM 调用。**纯数学 + 数据库 IO**，**纳秒级**。零 LLM 成本。**这是"用工程化换可解释性"的合理代价**。

### Q4: "多个证据冲突时怎么算？"

> v0.6 简化为**逐条 add logit**。冲突 = 后一条覆盖前一条的 logit 增量方向相反。**测试覆盖 50+ 测试用例**（含双方冲突、weaken 抵消等场景）。

---

## 7. 关键代码位置（面试可指）

| 模块 | 文件 | 行数 |
|---|---|---|
| `BeliefDiff` 结构 | `backend/internal/belief/diff.go` | ~85 |
| 贝叶斯更新引擎 | `backend/internal/belief/engine_v06.go` | ~250 |
| 收敛判断 | `backend/internal/belief/convergence.go` | ~150 |
| DiffRepo (DB) | `backend/internal/belief/gorm_repository.go` | ~95 |
| InmemRepo (test) | `backend/internal/belief/inmemory_repository.go` | ~80 |
| 法官 verdict 调用 | `backend/internal/courtroom/service.go:1949` | ~30 |
| 前端 BeliefDiffCard | `frontend/components/courtroom/BeliefDiffCard.tsx` | ~140 |
| 前端 BeliefTrajectoryTab | `frontend/components/courtroom/BeliefTrajectoryTab.tsx` | ~100 |
| 测试覆盖 | `backend/internal/belief/engine_v06_test.go` | 50+ 用例 |

---

## 8. 面试话术（背 3 句就够）

### 30 秒版（自我介绍末尾）

> "我在 [项目名] 设计了一个**贝叶斯信念引擎**——AI 法官在庭审中**对双方主张的相信度**用贝叶斯 log-odds 数学建模，不是 LLM 凭空打 0-100 分。每条证据触发一次 Bayesian Update 写入 `belief_diffs` 审计表，让'信念变化'**可追溯、可解释、可回放**。法庭场景（推荐买房 / 投资 / 离婚）需要这种**可审计的 AI**，不是主观分。"

### 3 分钟版（深入问）

> 我们调研过业内常见 AI Judge 做法，多数用 LLM 直出 0-100 分。但这有 3 个问题：**主观、不可回放、不可审计**。
>
> 我选**贝叶斯 log-odds** 数学建模：
> 1. `logit(p) = log(p / (1 - p))` —— 把"信念"变成线性可加的量
> 2. 每条 evidence = 一个 logit 增量，**叠加线性可解释**
> 3. 4 个 Agent（控方 / 辩方 / 调查员 / 法官）**独立**更新 belief（不同视角 = 不同 prior = 不同 delta）
> 4. 每次更新写一条 `belief_diff` 审计行，含 `prior_logit` / `posterior_logit` / `delta` / `reason`
> 5. 多信号收敛（variance / delta / 时间窗口）OR 逻辑
>
> 实装 250 行 Go（`engine_v06.go`）+ 50+ 测试用例 + 前端 BeliefDiffCard 时间线。
>
> 我印象最深的真实案例：用户跑了一场"我要学习吗"庭审，4 条证据触发 **16 条 belief_diff**（4 evidence × 4 agent），最终法官判决 A=0.56 vs B=0.44，**LLM 没推翻算式结果**，**用户能看到每一处信念变化**。**这是工程化让 AI 可解释的价值**。

---

## 9. 【反思】

### 反思 1：选数学模型时**先问"业务需要什么样的可解释性"**，**再选模型**

我的需求：法庭场景必须能回答"为什么判 A 不判 B"。贝叶斯的好处是**线性可加 = 数学可解释**。如果需求是"快、模糊、对就行"，用 LLM 评分或逻辑回归就够了。**模型选型 = 业务需求 驱动，不是技术崇拜**。

### 反思 2：**让"审计追溯"成为表 schema 的 first-class 字段**

`belief_diffs` 表的 schema 不是"随便设计"——`prior_logit / posterior_logit / reason` 都是为了"法庭可审计"。**表的列 = 业务的回答**。**先写"用户会问什么问题"，再设计表**。**这是反范式设计的好例子**。

### 反思 3：**v0.8.3 bug 4 教会我**：**每一行 audit 都要有 unique id**

`belief_diff.ID` 在 v0.6 设计时是数据库默认值（`gen_random_uuid()`），**engine 创建时未主动分配**。结果 v0.8 WS 推送时所有 belief_diff 共享 `uuid.Nil` 零值。**前端 store 的幂等检查把后续 belief_diff 全部去重**。**修一行 `uuid.New()`**。

**教训**：**审计行的 ID 是跨层 trace 的关键**——从 DB insert 到 API 序列化到 WS broadcast 到前端 store dedupe。**任何一环丢了 ID，下游全乱**。这是 v0.6 设计时没想到的，**v0.8 白盒化帮我发现**。

---

## 10. 名词速查（首次出现已加注，此处汇总）

| 名词 | 含义 |
|---|---|
| logit | `log(p / (1 - p))`，把 0-1 概率转换为线性可加的量 |
| sigmoid | `1 / (1 + e^-logit)`，logit 的逆运算 |
| Bayesian Update | 给定 evidence，更新 belief 的数学规则 |
| weakening | 对方律师质疑此证据，降低其权重 |
| anchoring | 把 Agent 信念锁回 prior，抵抗漂移 |
| variance 收敛 | 4 个 Agent 信念高度一致时算收敛 |
| belief_diffs | 审计表，每条 = 一次 Bayesian Update |
| log-odds | logit 的另一叫法 |
| A2A 私内存 | Agent 私有策略笔记（不广播，但入库） |

---

**下一步**：
- 接着看 [`04-agent-gateway-v2.md`](04-agent-gateway-v2.md) —— Gateway 装饰器如何用工程化平衡质量/成本
- 或跳到 [`05-whitebox-observability.md`](05-whitebox-observability.md) —— 面试杀手锏


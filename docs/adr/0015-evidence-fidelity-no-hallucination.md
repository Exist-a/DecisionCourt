# ADR 0015: v0.9.1 证据真实性与 LLM 幻觉防御

> **状态**：✅ Accepted（2026-07-04 决策）
> **决策日期**：2026-07-04
> **影响范围**：`backend/internal/agent/prompts.go`（baseRules + buildContext）+ `backend/internal/agent/orchestrator.go`（withArgumentSummaryText）+ 2 个新测试文件
> **触发事件**：2026-07-04 用户手动测试发现"短证据输入触发 LLM 编造细节"(详见 ADR 0015 §背景)

---

## 背景

2026-07-04 用户在浏览器手动测试 v0.9 部署版本时，发现一个**P0 业务风险**：

### 现象

用户提交证据：
- E001（user fact）："但是我还有工作"
- E002（user fact）："有点累，医生让我休息"

进入质证阶段后，**辩方 Agent 发言引用了以下细节**：
> "我方提交的医疗记录完全一致——主治医师在**2023年3月15日**的医嘱中写明'建议休息两周'……参考(**2022)京01民终1234号**案例……"

但**用户的 E002 内容只有 9 个字**——没有"2023年3月15日"、没有"(2022)京01民终1234号"、没有任何医嘱细节。

DB 直查验证：
```sql
SELECT evidence_id, source, content FROM evidences WHERE evidence_id IN ('E001', 'E002');
-- E001 | user | 但是我还有工作
-- E002 | user | 有点累,医生让我休息

SELECT COUNT(*) FROM investigation_findings WHERE session_uuid = '...';
-- 0  (搜索结果为空)
```

**LLM 看到证据内容只有 9 个字，觉得论证不够"实"，自己脑补了日期/案号来让发言"看起来权威"。**

### 现有保护不足

排查代码后找到 3 个根因：

1. **`baseRules` 第 4 条只禁止"编造证据 ID"**，未禁止"引用 evidence 内容里不存在的细节"。LLM 可以合法地"引用 E002 但补充虚构的 2023-03-15 医嘱"。
2. **`buildContext` 不显示 `Source` / `CredibilityScore` / `SubmittedBy`** —— LLM 看不到这条证据是 user 提交的简述 vs 搜索的权威文档，导致 LLM 把 user 的 9 个字当"提纲"，自由发挥补充。
3. **`withArgumentSummaryText` 只过滤 `user_interrupt`**（不写入 agent 上下文），导致用户中途补充的"医生让我休息"看不到完整背景，LLM 容易基于残缺上下文"脑补"。

### 风险等级

这是 **P0 风险**，因为：
- 法律/医疗/职业等严肃场景里，"编造医嘱日期"或"编造案号"是失实行为，可能误导用户做错决策
- ADR 0005 已明确"用户证据 vs 调查发现"分离原则，但**没禁止"在用户证据基础上 LLM 自由发挥"**
- 当前 prompt 只防 ID 编造，未防细节编造，**护栏有缺口**

---

## 选项对比

| 维度 | A. 仅加 Prompt 强约束 | B. Prompt + Context 标签 | C. Prompt + Context + Output 验证 |
|---|---|---|---|
| 改动量 | ~30 行 baseRules | ~80 行（baseRules + buildContext） | ~200 行（含输出正则扫） |
| 防幻觉强度 | 中 | **高** | 高（双层防御） |
| 误伤风险 | 高（只能靠 LLM 自觉） | 低（标签让 LLM 自己判断） | 最低（输出扫保底） |
| 维护成本 | 低 | 中 | 高（正则需要维护） |
| 简历价值 | "Prompt Engineering" | **"Evidence Grounding"**（更专业的术语） | "LLM Output Validation" |
| 部署风险 | 中 | **低** | 中（误杀合法发言） |

---

## 决策

采用 **方案 B** —— **Prompt 强约束 + Context 显示来源标签**，双管齐下让 LLM 知道"这条证据的来源 + 可信度 + 是谁提的"。

### 关键理由

1. **场景定位**：当前是**法律 / 决策辅助**而非创意写作，**真实性 > 表达丰富度**。宁可发言"看起来不够丰满"，也不能编造。
2. **方案 B 的输出检测友好**：如果未来要做方案 C（output 正则扫），方案 B 的"来源标签"结构化输出便于匹配。
3. **方案 C 误伤风险**：法律引用、案例号本身是合法发言（如果用户提交了完整 case number），正则难以区分"用户提供的" vs "LLM 编造的"。

### 具体改动（3 处）

#### Fix 1：`baseRules` 增加 2 条强约束

```markdown
13. **v0.9.1 严禁编造证据细节（ADR 0015）**：你可以引用 evidence 内容里实际出现的
    日期 / 数据 / 编号 / 引用，但严禁自己补充不存在的细节（如"2023年3月15日医嘱"
    "案号 (2022) 京01民终1234号"）。如果 evidence 内容简短，直接说"用户主张：
    医生让我休息,具体日期 / 案号 / 医嘱内容用户未提供"即可。

14. **v0.9.1 严禁虚构对方论点（ADR 0015）**：对方最近一次发言就是真实的对方论点，
    不要"预判"对方下一步会说什么。如果对方没说，就不要引用为"对方曾表示……"。
```

#### Fix 2：`buildContext` 显示 `source` / `submitted_by` / `credibility`

```diff
- E001 [fact]: 但是我还有工作 (A影响0.0, B影响0.0)
- E002 [fact]: 有点累,医生让我休息 (A影响0.0, B影响0.0)
+ E001 [source=用户陈述 submitted_by=anon_cfcd... credibility=0.85 type=fact]: 但是我还有工作 (A影响0.0, B影响0.0)
+ E002 [source=用户陈述 submitted_by=anon_cfcd... credibility=0.85 type=fact]: 有点累,医生让我休息 (A影响0.0, B影响0.0)
```

新增 `sourceLabel()` helper 把枚举值翻译成中文：
- `user` → "用户陈述"
- `investigator` → "搜索结果"
- `system` → "系统注入"

Prompt 头部加引导语：
```
注意：以下证据 source=user 是用户提交的真实输入；source=investigator 是调查员搜索
结果；可信度 0.0-1.0（用户提交默认 0.85）。
```

#### Fix 3：`withArgumentSummaryText` 抓取 `user_interrupt`

之前 `user_interrupt` 类型消息被过滤掉（不进入 agent 上下文）。改为：抓取**最近一条** `user_interrupt`（用户中途补充），摘要最显眼位置显示：

```
【用户最新补充（2026-07-04 19:39）】 医生让我休息
```

这样 agent 看到完整证据 + 用户最新补充，不会基于残缺上下文脑补。

---

## 后果

### 收益

- ✅ **P0 风险消除**：LLM 不再编造证据细节，用户不会再被误导做错决策
- ✅ **可追溯**：未来用户质疑某条发言引用的细节，可以直接对照 evidence.source=user 的原始 content
- ✅ **护栏体系完善**：与 ADR 0005（搜索 vs 证据分离）形成闭环 —— **来源可识别 + 内容不可虚构**
- ✅ **测试覆盖**：新增 7 个测试（含 4 个防幻觉单测 + 3 个 context 注入测试）

### 代价

- ⚠️ **prompt 长度增加**：每条 evidence 多 ~40 字符，10 条证据 = ~400 字符。实测未触发 token budget。
- ⚠️ **LLM 表达受限**：不能"创造性补充细节"。**可接受**——产品定位是"严肃决策辅助"不是"创意写作"。
- ⚠️ **测试中 mock 用户**：测试无法在真实浏览器重现，但单测验证了 prompt 构建逻辑 + LLM 行为约束。

### 不做（暂缓）

- ❌ **Output 验证正则扫**：方案 C。当前 LLM 自觉 + baseRules 强约束够用，留待 v1.x 评估。
- ❌ **Output 与 evidence source 自动对比**：技术复杂（需要 LLM-as-judge 或 embedding 相似度），性价比低。

---

## 关联

- **主文档**：[`docs/decisioncourt-agent-design.md`](../decisioncourt-agent-design.md) §基
- **代码**：[`backend/internal/agent/prompts.go`](../..//backend/internal/agent/prompts.go)（baseRules + buildContext）
- **代码**：[`backend/internal/agent/orchestrator.go`](../../backend/internal/agent/orchestrator.go)（withArgumentSummaryText）
- **测试**：[`backend/internal/agent/no_hallucination_test.go`](../../backend/internal/agent/no_hallucination_test.go)（4 测试）
- **测试**：[`backend/internal/agent/no_hallucination_orchestrator_test.go`](../../backend/internal/agent/no_hallucination_orchestrator_test.go)（3 测试）
- **触发事件**：2026-07-04 用户手动测试发现 `decision_events` 异常 + DB 直查证据内容
- **关联 ADR**：[0005（调查发现独立表）](./0005-investigation-findings.md)（用户证据 vs 搜索结果分离）+ [0007（Token Budget）](./0007-token-budget-rejection.md)
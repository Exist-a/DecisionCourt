# ADR 0032: 移除 ArgumentMap（叙事流优先于信息图重写）

| | |
|---|---|
| **编号** | 0032 |
| **标题** | v1.0.3 移除 ArgumentMap 观点地图组件 |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-21 |
| **触发** | 用户 2026-08-21 在 v1.0.3 PR-B1 启动验证期间反馈："ArgumentMap 完全没用……我觉得根本不会看他，上面地图都有倾向，那看这个干什么？所以我建议直接删掉" |
| **关联** | ADR 0009（2026-07 选方案 B"删 StanceChart 保留 ArgumentMap 精简版"）— 本次实质上是采纳 ADR 0009 当时讨论过的方案 C |
| **影响** | 删除 `frontend/components/courtroom/ArgumentMap.tsx`；移除 `frontend/components/courtroom/CourtroomScene.tsx` 中的 import + 引用；移除 `package.json` 的 `reactflow` 依赖；更新 PRD / tech-spec / roadmap / README |

---

## 1. 决策

### 1.1 背景

ADR 0009（2026-07）当时从 3 个选项中选了 **方案 B**（删 StanceChart，保留 ArgumentMap 精简版）。ArgumentMap 自 v0.5 集成到庭审页起，作为"观点地图"承载 5 个节点（选项 A/B + 控方/辩方/法官）+ 边（agent → 选项的信念倾向）。

### 1.2 矛盾诊断

用户 2026-08-21 启动 v1.0.3 PR-B1 验证时明确反馈："用户根本不会看他，上面地图都有倾向，那看这个干什么？"

展开 ArgumentMap 的实际问题：

1. **信息冗余** —— 节点仅显示当前 belief_a / belief_b，但同一信息已在庭审消息流的 stances（pro_a / pro_b / challenge / neutral）+ EvidenceBoard（证据影响）+ 庭审记录（律师发言的 stance 标注）多处可见。ArgumentMap 是"信息图重写"同一数据，未提供额外洞察。
2. **静态感强** —— 庭审过程最有价值的信息是"信念度怎么随论据变化"，但 ArgumentMap 只显示终态，不显示时序。这正是 v0.6 belief engine 应该做的 — 已通过 BeliefTrajectoryTab 在 verdict 页提供完整时序。
3. **认知负荷 vs 信息密度不成比例** —— 280-320px 高的固定容器承载 3 个数字（每个 agent 一个核心指标），需要用户理解"连线代表立场倾向"的隐喻 + 颜色编码 + 线宽公式 `(belief - 0.5) * 5`。
4. **ReactFlow 是重型框架** —— 引入 ~50KB+ 依赖 + 节点/边/布局算法 + 容器尺寸检测，但承载的信息用 React 简单条件渲染就能显示。
5. **触发"完全没用"的真实原因**（用户反馈推断）：
   - 用户已经把"立场倾向"和庭审叙事绑定（"上面地图都有倾向"）
   - 信息图重写同一数据不增加新信息维度
   - 用户期望看到"过程变化"，ArgumentMap 只显示终态 → 用户怀疑图在干嘛

### 1.3 替代方案对比

| 方案 | 优点 | 缺点 | 决策 |
|---|---|---|---|
| (a) 保留 ArgumentMap + 加 tooltip / 动画 / 时序 | 信息图更丰富 | 加深认知负担（ADR 0009 当年的核心矛盾），代码量 +200行 | ❌ 不采纳 |
| (b) 换成实时信念进度条（BeliefGauge） | 简单，动态，占用空间小 | 不如 ReactFlow "可视"；用户没明确要 | ❌ 不采纳 |
| **(c) 完全删除 ArgumentMap，依赖庭审叙事流承载立场信息** | 页面极简，无冗余，叙事连贯 | 庭审过程看不到信念时序变化（已在 verdict 页 BeliefTrajectoryTab 提供） | ✅ **采纳** |

### 1.4 实施决策

**采用 (c)：删除 ArgumentMap**。

**关键变更**：
- 删除 `frontend/components/courtroom/ArgumentMap.tsx`（不再保留 dead code — 与 StanceChart 处理不同，因 StanceChart 是 v0.6 信念审计源头组件不删）
- `frontend/components/courtroom/CourtroomScene.tsx` 去掉 import 和 `<ArgumentMap>` JSX 块
- 移除 `package.json` 的 `reactflow` 依赖（grep 确认仅 ArgumentMap 用）
- 不动 `frontend/components/courtroom/StanceChart.tsx`（ADR 0009 已说明该组件是 v0.6 BeliefTrajectoryTab 的源头）
- 不动 verdict 页 `BeliefTrajectoryTab`（庭审过程看不到信念时序变化，但 verdict 页仍提供完整信念审计）

### 1.5 兼容性

| 维度 | 影响 |
|---|---|
| 庭审页 UI | 右侧少 280-320px 高度；信息图重写消失；用户依赖叙事流获取立场 |
| Verdict 页 | 不变（BeliefTrajectoryTab + BeliefDiffCard 仍提供完整信念审计）|
| 前端依赖 | 移除 `reactflow` ^11.11.4（间接依赖 `dagre`、`@reactflow/core` 等也一起卸载）|
| 现有测试 | ArgumentMap 无单元测试（v0.5 起一直未补），无破坏 |
| PRD §4.3.4 | 标记观点地图项为 "v1.0.3 移除"，保留作为历史决策轨迹 |

---

## 2. 教训

1. **信息图重写同一数据 ≠ 新维度** —— ArgumentMap 节点信息（belief_a / belief_b）已被庭审消息流的 stance + EvidenceBoard 覆盖，信息图是"展示形式"变化不是"信息"变化。
2. **用户视角比设计者视角重要** —— 设计者（ADR 0009）认为"ReactFlow 框架更具交互性"，但用户实际使用中根本不会点开。设计判断要事后被用户反馈校准。
3. **重型框架引入要谨慎** —— ReactFlow 引入 ~50KB+ 依赖只为渲染 5 个节点，是过度工程。React 条件渲染 + Tailwind 足够。
4. **决策要在 PRD 层做而不仅在代码层** —— ArgumentMap 已经在 PRD §4.3.4 标记为本期完成，移除时要同步改 PRD（不只是删代码）。

---

## 3. 后续工作

- ✅ 同步更新 PRD / tech-spec / roadmap / README（commit `<此 commit>`）
- ✅ ADR 0032 入库（`docs/adr/README.md` 索引同步）
- ✅ reactflow 依赖从 package.json 移除
- ⏸ pnpm install 移除 lockfile（前端开发者本地跑 `pnpm install` 时自动清理）
- ⏸ **D2 / D3**（docs/todo/deferred-items-2026-08-21.md） — cross-exam silent error + direct_verdict fallback round=0 修复仍是 pre-existing，与本次移除 ArgumentMap 无关

---

## 4. 关联文档

- ADR 0009（庭审页面可视化简化，2026-07）—— 当时选了方案 B，本次实质采纳方案 C
- `frontend/components/courtroom/ArgumentMap.tsx`（**已删除**）
- `frontend/components/courtroom/CourtroomScene.tsx`（去掉 import + JSX 引用）
- `frontend/components/courtroom/StanceChart.tsx`（保留，BeliefTrajectoryTab 源头）
- `frontend/components/courtroom/BeliefTrajectoryTab.tsx`（verdict 页信念审计）
- `docs/decisioncourt-prd.md §4.3.4`（PRD 标记项更新）
- `docs/decisioncourt-tech-spec.md §3.1`（技术栈移除 React-Flow）
- `docs/decisioncourt-roadmap.md §2.4`（roadmap 状态更新）
- `docs/README.md`（ADR 索引 + 5.2 表格更新）
- `docs/todo/deferred-items-2026-08-21.md`（D2 / D3 仍 deferred）
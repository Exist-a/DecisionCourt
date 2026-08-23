# ADR 0034-archive: v2.0 REDESIGN 3D 路线二次归档

| | |
|---|---|
| **编号** | 0034-archive |
| **标题** | v2.0 REDESIGN 3D r3f 重构路线二次归档（PR-D2.9 放弃）|
| **状态** | ✅ **Archived** |
| **作者** | Exist + ZCode Agent |
| **归档日期** | 2026-08-23 |
| **supersede** | [ADR 0034-supersede-2-5d-r3f.md](./0034-supersede-2-5d-r3f.md)（状态 ⚠️ Archived by this）|
| **supersede chain** | [0034-silhouette-architecture.md](./0034-silhouette-architecture.md)（剪影原版）→ 0034-supersede（r3f 重构）→ **0034-archive（r3f 二次归档）** |
| **Postmortem** | [../postmortem/v2.0-redesign-3d-pivot.md](../postmortem/v2.0-redesign-3d-pivot.md) |

---

## 1. 决策

**v2.0 REDESIGN 3D r3f 重构路线正式归档**——3D 路线**不再尝试**。

### 触发（2026-08-23）
- 用户反馈 "太他妈丑了" / "切记，你做出来的东西都死丑的"
- 9 个 PR-D2 commit 反复试错失败（深木 → 浅橡木 → 游戏风格）
- Agent 承认能力边界：缺少 3D 资产（GLTF/贴图/HDR）+ 审美判断 + 工具

### 用户决定（2026-08-23）
> "那就算了，不做3d了" / "代码回退到开始庭审现场重构之前把"

### 操作（2026-08-23）
- ✅ `git reset --hard 5fd803b` —— 完全回退 v2.0 REDESIGN 11 个 commit
- ✅ 项目状态回到 v1.0.4 PR-C4（v2.0 剪影原版已合并保留）

---

## 2. v2.0 REDESIGN 三次决策链

| ADR | 状态 | 决策 |
|---|---|---|
| [0034-silhouette-architecture.md](./0034-silhouette-architecture.md) | ⚠️ Superseded | 剪影小人方案（已落地，commit `50e1746` + `6e0588b`）|
| [0034-supersede-2-5d-r3f.md](./0034-supersede-2-5d-r3f.md) | ⚠️ **Archived** by this | supersede 剪影 → 2.5D r3f 重构（2026-08-22）|
| **0034-archive-3d-pivot.md**（本文档）| ✅ **Archived** | 二次归档 r3f 路线（2026-08-23）|

---

## 3. v2.0 当前最终方案

- ✅ **v1.0.4 PR-C3 圆点角色**（AgentAvatar + framer-motion 6 状态动画）
- ✅ **v1.0.4 PR-C4 案卷封面**（"Case File" 标签 + 当事人陈述对比条）
- ❌ **3D r3f 重构**（永不实施）

---

## 4. 关键经验（详情见 Postmortem）

1. **Agent 能力边界**——3D 需要资产 + 工具 + 审美，Agent 缺所有
2. **"再调一下"是陷阱**——用户说"丑"是定性判断，不是参数调整
3. **3 次失败换方向**——PR-D2.0 → PR-D2.5 已经是 5 次失败，应早就 stop
4. **抽象 > 模拟**——法庭的"语义"（天平 / 金色 / 对比）能用 CSS 表达

---

## 5. 关联文档

- [Postmortem](../postmortem/v2.0-redesign-3d-pivot.md) —— 完整复盘（5 Whys + 教训 + 行动项）
- [V1-ROADMAP.md](../V1-ROADMAP.md) §0 —— v2.0 REDESIGN 状态表
- [V2.0-PLAN.md](../V2.0-PLAN.md) —— 剪影原版（已合并保留）
- [V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md) —— 3D 重构规划（**已 SUPERSEDED**）
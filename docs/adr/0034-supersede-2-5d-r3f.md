# ADR 0034-supersede: v2.0 剪影小人 supersede by 2.5D r3f 重构

| | |
|---|---|
| **编号** | 0034-supersede |
| **标题** | v2.0 厕所标识剪影小人 → 2.5D r3f 角色动画重构 (用户反馈"简陋，要重做") |
| **状态** | ⏳ Proposed (待 PR-D1 启动时变 Accepted) |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-22 |
| **触发的反馈** | 用户 2026-08-22 "目前的 v2.0 简陋，要 2.5D 体积感 + 走路 + 角色专属动作 + 调查员跑位 + 面部无细节" |
| **supersede** | [ADR 0034-silhouette-architecture.md](./0034-silhouette-architecture.md)（状态从 ✅ Accepted → ⚠️ Superseded by this）|
| **被取代原因** | 用户明确反馈：v2.0 剪影小人"简陋，未达到大版本颠覆过去UI的预期" |
| **影响** | 删除 `frontend/components/courtroom/silhouettes/` 整个目录 (~300 行) + AgentAvatar 改接 r3f + 新增 ~700 行 2.5D 代码 |

---

## 1. 决策

ADR 0034 (剪影小人) **作废**，由本 ADR supersede：

**采用 react-three-fiber + drei + three-pathfinding** 做 2.5D 角色动画重构。

## 2. 触发反馈（用户原话 2026-08-22）

> "我看到你已经修改完了 2.0，但是我觉得非常简陋，我们拿出一个大版本目的就是为了颠覆过去的 ui，你这样基本没变。我说一下我想要的。你可以引入其他的库，我要的是 2.5d 的、有视觉体积的 ui，小人可以走动。法官的脑袋可以左右看双方、正反方可以有点头叉腰等动作、调查员可以跑到对应对应正反方告诉他们证据等等动作，人物可以没有细节，就是眼睛等等都不要有，你理解？"

## 3. 为什么剪影方案失败

| 维度 | 剪影 (原 ADR 0034) | 用户期望 |
|---|---|---|
| **体积感** | 平面 SVG，CSS keyframes 缩放 | 真 2.5D 透视 + 描边 + 阴影 |
| **走路** | 双脚交替 4-frame CSS | 多角色独立走路循环 + 节奏 |
| **法官转头** | ❌ 未实现 | 头缓慢跟随发言者位置 |
| **正反方点头叉腰** | ❌ 未实现 | 点头节奏 + 双手姿势 |
| **调查员跑位** | ❌ 未实现 | 沿 pathfinding 路径移动到目标角色 |
| **视觉冲击力** | "厕所标识剪影" 类似极简风 | "颠覆过去 UI" 的产品级视觉飞跃 |

剪影方案满足"无依赖、零 bundle、极速交付"，但**视觉保真度低**。原 ADR 0034 §3.2 已写"代价:首屏调试周期长"，实际用户验收发现**整套方案定位偏低端**。

## 4. 新方案关键决策

| 维度 | 决策 | 理由 |
|---|---|---|
| **技术栈** | r3f v8 + drei v10.7.8 + three-pathfinding | 唯一真 2.5D（MeshToonMaterial + Outlines + AccumulativeShadows 内置）|
| **角色面部** | 完全无 mesh（连眼睛都不画）| 匹配用户"无细节"明确要求 + 反而简化 |
| **走路** | `useFrame` + `Math.sin` 驱动骨骼摆动 | 无需 .glb 资产，dev 自写 |
| **跑位** | three-pathfinding NavMesh + 沿 path lerp | 5 角色 + 简单 5 节点全连接，手写寻路足够 |
| **状态机** | 现有 6 framer-motion variants 保留为驱动层 + r3f `useFrame` 渲染 | 不重写 framer-motion 基础设施 |
| **向后兼容** | 不需要（旧 v2.0 还未正式发布，已落地但用户验收失败）| 直接替换 |

## 5. 多阶段实施路线

| 阶段 | PR | 内容 | 工作量 |
|---|---|---|---|
| 阶段 1 | PR-D1 | 装包 + transpilePackages + Canvas wrapper | 0.5d |
| 阶段 2 | PR-D2 | CourtroomFloor + Character + 5 角色 + 替换 CourtroomScene + 删 silhouettes/ | 2d |
| 阶段 3 | PR-D3 | characterStateMachine + animationController + 6 状态 useFrame 驱动 | 2d |
| 阶段 4 | PR-D4 | NavMesh + PathController + 调查员跑位 + 灯光/阴影/OrbitControls 打磨 | 2d |
| 阶段 5 | PR-D5 | release notes + V1-ROADMAP 同步 + tag | 0.5d |

总：~7d / 5-6 周

## 6. 旧 ADR 0034 处理

**状态变更**：
- 旧 ADR 0034 (剪影小人)：`✅ Accepted` → `⚠️ Superseded by [0034-supersede-2-5d-r3f.md](./0034-supersede-2-5d-r3f.md)`

**git 历史保留**：
- 旧剪影代码保留在 git history (commit `50e1746` + `6e0588b`)，需要时 `git revert` 可恢复
- 旧 `docs/V2.0-PLAN.md` + `docs/release-notes/v2.0.md` 保留作为历史决策记录
- 旧 `docs/adr/0034-silhouette-architecture.md` 顶部加 "Superseded by" 指针（本 ADR）

**tag 策略**：
- 不叠 tag `v2.0`
- 旧 tag `v2.0` (commit `6e0588b`) 保留作为里程碑记录
- 新 v2.0 重做版完成后 commit message 内说明"v2.0 重做版（r3f + drei）"

## 7. 关联文档

- [V2.0-REDESIGN-PLAN.md](../V2.0-REDESIGN-PLAN.md) — 顶层规划（用户原话 + 技术选型 + 5 阶段路线图）
- [V2.0-REDESIGN-PLAN.md §3 各阶段详细](../V2.0-REDESIGN-PLAN.md#3-各阶段详细文档) — 4 个 stage 文件链接
- [v2-redesign/stage-1-foundation.md](../v2-redesign/stage-1-foundation.md) — 装包 + Canvas wrapper
- [v2-redesign/stage-2-static-characters.md](../v2-redesign/stage-2-static-characters.md) — 5 角色 + 庭审场地
- [v2-redesign/stage-3-animation-system.md](../v2-redesign/stage-3-animation-system.md) — 6 状态 useFrame
- [v2-redesign/stage-4-pathfinding-polish.md](../v2-redesign/stage-4-pathfinding-polish.md) — NavMesh + 视觉打磨

## 8. ADR 编号说明

**编号冲突解决**：
- 旧 ADR 0034（剪影小人）保留编号，顶部加 "Superseded by" 指针
- 本 supersede ADR 文件名为 `0034-supersede-2-5d-r3f.md`，**不**用新编号
- 理由：保留与旧 ADR 的关联，git history 清晰
- ADR README 索引加 `[0034-supersede]` 行

## 9. 用户授权节点

| 节点 | 内容 |
|---|---|
| 启动前 | 本 ADR + V2.0-REDESIGN-PLAN 审核（当前 ExitPlanMode 已批准）|
| 阶段 1 完成 (PR-D1) | 用户浏览器验证 r3f 基础跑通 → 批准阶段 2 |
| 阶段 2 完成 (PR-D2) | 用户验证 5 角色 2.5D 场景 → 批准阶段 3 |
| 阶段 3 完成 (PR-D3) | 用户验证 6 状态动作 → 批准阶段 4 |
| 阶段 4 完成 (PR-D4) | 用户最终验收调查员跑位 + 视觉打磨 → 批准阶段 5 |
| 阶段 5 完成 (PR-D5) | v2.0 重做版正式发布 + tag |
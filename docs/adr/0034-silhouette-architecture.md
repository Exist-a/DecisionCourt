# ADR 0034: 厕所标识剪影小人架构

| | |
|---|---|
| **编号** | 0034 |
| **标题** | v2.0 视觉升级：厕所标识剪影小人 SVG + CSS keyframes + 向后兼容 fallback |
| **状态** | ✅ Accepted |
| **作者** | Exist + ZCode Agent |
| **决策日期** | 2026-08-22 |
| **触发** | V2.0-PLAN.md (升级自 v1.0.5-PLAN.md) + 用户 2026-08-21 "v1.0.5 原方案工作量+调试周期都大，升格为大版本 v2.0" |
| **依赖** | v1.0.4 PR-C3 Framer Motion 微动效 (AvatarAnimations motion.div 接管 scale/y/rotate) + tailwind.config.ts 案卷·印章 token (颜色) |
| **替代决策** | (a) Lottie/Rive 设计稿 / (b) 设计师手绘 SVG / (c) 维持圆形头像 + 加 Framer Motion 动效 |
| **影响** | `frontend/components/courtroom/silhouettes/` (NEW) + `frontend/app/globals.css` (MODIFIED, + 90 行) + `frontend/components/courtroom/AgentAvatar.tsx` (MODIFIED) |

---

## 1. 决策

### 1.1 背景

v1.0.4 PR-C3 接入 Framer Motion 后，圆形头像已经能"点头 / 上下浮 / 微旋转"，但**视觉本身仍是圆点 + 角色名首字**，缺角色辨识度。

用户 2026-08-20 原话："小人初步就是像是厕所标识上的那种小人就行，不要各种细节，颜色待定，动作就是走路双脚、手、点头啥的。"

v1.0.5-PLAN.md 设计了剪影小人 SVG + CSS keyframes 方案。原计划 v1.0.5 = 2 PR / 1-1.5 周。2026-08-21 用户重新评估后认为工作量+调试周期都大，**升格为大版本 v2.0**（破坏性变更预期 + 3 PR + 2-3 周 + 视觉打磨 PR）。

### 1.2 选项对比

| 维度 | A. Lottie/Rive 设计稿 | B. 设计师手绘 SVG | **C. 自写剪影 SVG + CSS keyframes（本决策）** |
|---|---|---|---|
| 设计师依赖 | 高（必须出图） | 高 | **无**（Dev 自己写） |
| bundle 体积 | Lottie ~150KB / Rive ~300KB | ~10KB/角色 | **~2KB/角色（原生 SVG）** |
| 动画性能 | JS 引擎驱动（中等） | CSS keyframes（GPU 加速） | **CSS keyframes（GPU 加速）** |
| 隐私 / 离线 | 无影响 | 无影响 | 无影响 |
| 用户原话匹配度 | "不需要细节" | "不需要细节" | **"剪影即可"（V2.0-PLAN.md §0.2）** |
| 实现复杂度 | 中（接入 Lottie 库） | 中（设计师出图） | **低（< 200 行 SVG + CSS）** |

### 1.3 决策

采用 **方案 C** — 自写 5 角色剪影 SVG + CSS keyframes，**不引入 Lottie/Rive**。

### 1.4 关键理由

1. **零外部依赖**：原生 SVG `<rect>/<circle>` + CSS `@keyframes`，无 bundle 增量（仅 90 行 CSS）
2. **匹配用户原话**："厕所标识剪影"风格本身就是简单剪影，复杂设计稿反而违背需求
3. **GPU 加速**：`will-change: transform` + CSS keyframes 比 Lottie JS 引擎更顺滑
4. **Dev 可控**：颜色 + 姿势全部由代码控制，未来调色 / 加姿势无需走设计师
5. **可逐步升级**：CSS 动画 → Framer Motion → Lottie 渐进路径保留，未来想升级可换 Silhouette 内部实现

---

## 2. 设计要点

### 2.1 文件结构

```
frontend/
├── components/
│   └── courtroom/
│       └── silhouettes/                  # NEW
│           ├── Silhouette.tsx            # 5 角色 SVG 组件 + 入口
│           └── RoleSilhouette.tsx        # 包装 + mode=circle fallback
├── app/
│   └── globals.css                       # MODIFIED, + 90 行 silhouette CSS
└── lib/
    └── silhouettes.test.ts              # 5 sub-test
```

### 2.2 5 角色 SVG 设计原则

- **风格**：纯色剪影，无渐变、无细节、左右对称
- **viewBox**：64x64（紧凑可缩放）
- **颜色解耦**：`fill="currentColor"` + `silhouette-{role}` CSS class 应用 CSS var
- **5 角色特征**（V2.0-PLAN.md §1.2）：
  - 控方律师：圆形头 + 站立 + 举手指控（左侧手臂）
  - 辩方律师：圆形头 + 站立 + 双手打开辩护（左右对称）
  - 调查员：圆形头 + 蹲姿 + 拿放大镜（右侧）
  - 法官：圆形头 + 坐姿 + 拿法槌（右上角）
  - 书记员：圆形头 + 低头打字 + 笔记本

### 2.3 5 类 CSS Keyframes

| Keyframe | 触发条件 | 周期 | 触发方式 |
|---|---|---|---|
| silhouette-walk | isSpeaking | 0.8s | 双脚交替（CSS class `silhouette-leg-left/right`） |
| silhouette-nod | isThinking | 1.2s | 身体点头（head group） |
| silhouette-point | isSpeaking (律师) | 2s | 举手指控 |
| silhouette-magnify | isSearching (调查员) | 1.5s | 放大镜旋转 |
| silhouette-gavel | isJudging (法官) | 0.4s | 4 关键帧敲锤 |

**GPU 加速**：`will-change: transform` 启用硬件加速，避免 60fps 掉帧。

### 2.4 向后兼容 fallback

PR-D2 实现 `RoleSilhouette` 包装组件 + env var fallback：

- 默认 `mode="silhouette"` → 渲染剪影 SVG
- 设置 `NEXT_PUBLIC_USE_CIRCLE_AVATAR=true` → 渲染旧圆形 div（V2.0-PLAN.md §1.5 SilhouetteAvatar 设计）

**理由**：用户改回旧头像最常见场景是"我不喜欢新版本想看回圆形" — 改 env var 重启即可，0 改代码。

### 2.5 与 v1.0.4 Framer Motion 共存

剪影 SVG + CSS keyframes 与 Framer Motion 不冲突：

| 层 | 负责 |
|---|---|
| **Framer Motion** (motion.div) | 头像整体 scale / y / rotate (AvatarAnimations) |
| **CSS keyframes** | 头像内部组件（双脚/手臂/头）细节动画 |
| **Tailwind transition** | 状态切换（isSpeaking 时 ring-gold） |

PR-D2 保留所有 v1.0.4 PR-C3 集成，剪影只是替换"圆 div"为"SVG 元素"。

### 2.6 数据属性便于视觉回归测试

每个 SVG 含 `data-role` / `data-speaking` / `data-searching` / `data-judging` 属性：

```html
<svg data-role="prosecutor" data-speaking="true" ...>
```

未来 Playwright 视觉回归测试可直接 query + assert。

---

## 3. 后果

### 3.1 收益

- ✅ **零外部依赖**：仅 90 行 CSS + 5 角色 SVG，无 Lottie/Rive 库
- ✅ **GPU 加速**：CSS keyframes + will-change，60fps 流畅
- ✅ **零代码回退**：env var 一键回退到圆形头像
- ✅ **数据属性**：未来视觉回归测试可挂 Playwright

### 3.2 代价

- ⚠️ **破坏性视觉变更**：旧圆形头像升级后变成剪影小人，UX 突变
- ⚠️ **首屏调试周期长**：5 角色 × 5 动画 × 5 状态 = 125 种组合，需多轮视觉打磨（PR-D3.5 计划保留）
- ⚠️ **设计师未参与**：剪影姿势可能偏离"理想设计稿"（V2.0-PLAN.md §5 风险）

---

## 4. 关联

### 主文档

- [V1-ROADMAP.md M4 v2.0](../V1-ROADMAP.md)
- [V2.0-PLAN.md](../V2.0-PLAN.md) — 完整 3 PR 拆分（PR-D1 + PR-D2 + PR-D3）
- [v2.0 release notes](../release-notes/v2.0.md)

### 代码

- 前端： `frontend/components/courtroom/silhouettes/{Silhouette, RoleSilhouette}.tsx`
- 改： `frontend/components/courtroom/AgentAvatar.tsx` + `frontend/app/globals.css`
- 测试： `frontend/lib/silhouettes.test.ts`（5 sub-test）

### 复用

- v1.0.4 PR-C3 `AvatarAnimations` + `framer-motion` variants
- `tailwind.config.ts` 案卷·印章 token（prosecution / defense / judge / neutral）
- v1.0.4 `SpeechBubbleAnimated`（气泡独立于头像，本 PR 不动）

### 关联历史

- v1.0.5-PLAN.md (2026-08-20) — 原 2 PR 方案，本决策升级为大版本
- ADR 0032 Remove ArgumentMap — v1.0.3 用户反馈"完全没用"，本决策同样采用"用户原话驱动"原则

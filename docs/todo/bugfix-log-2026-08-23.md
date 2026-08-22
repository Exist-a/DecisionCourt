# v1.0-patch 修复记录与未解决问题（2026-08-22 ~ 08-23）

> **状态**：本文档记录 v1.0-patch 系列修复的过程与结果，并**标记 3 个未解决问题**（策略笔记相关）。
> **触发**：用户 2026-08-22 手动测试 v1.0 系列功能时连续反馈多个 bug，本 session 集中修复。
> **范围**：v1.0-patch（`d72f860` 起，共 7 个 commit），非新版本号。

---

## 一、已修复问题清单（按时间顺序）

### F1. 浏览器 back 从判决书返回庭审报错（commit `d72f860`）

- **现象**：用户从 `/verdict/<uuid>` 点浏览器 back 跳到 `/court/<uuid>`，庭审现场空白，backend 日志出现多次 `record not found`。
- **根因**：`/court/[id]/page.tsx` 直接渲染 `<CourtroomScene/>`，无任何 store hydrate 逻辑；浏览器 back 不重发 WS，store 为空。
- **修复**：抽共享函数 `frontend/lib/courtroomHydrate.ts`（session / agents / evidences / investigations / belief_diffs / memory 六段 REST hydrate），court + verdict 两页共用。
- **附带**：新增后端 `GET /api/v1/courtrooms`（ListMySessions，owner 过滤）+ 前端 `lib/trialHistory.ts`（localStorage 历史庭审列表）+ 首页 `TrialHistoryList` 折叠面板 + 判决书顶部"← 返回庭审现场"按钮。

### F2. React key 重复警告 `Encountered two children with the same key E001`（commit `696211c`）

- **现象**：点历史庭审"回看庭审"后，console 报 key 重复。
- **根因**：CourtroomScene 内部还残留 6 段老 REST hydrate（`addEvidence` 不去重），与外层 hydrateCourtroomStore 双跑 → 同 evidence_id 出现两次。
- **修复**：删除 CourtroomScene 内部重复 hydrate，拆成 3 个独立 useEffect（initAnalytics / memory 降级 / WS）。

### F3. 跨 session 证据污染 —— "点开不一样的庭审看到相同的证据"（commit `2dac4c4`）

- **根因**：hydrate 的 evidences 段用 `addEvidence`（追加）而非整体替换。切 trial 时 B 的 evidence（id 不同）追加进 A 的数组 → 混合显示。
- **修复**：store 新增 `setEvidences`（整体替换），hydrate 用 `setEvidences` 覆盖；0 evidence 时清空防残留。

### F4. 庭审记录 / 调查记录 Tab 没数据（commit `53acab7`）

- **现象**：用户反馈"庭审记录、调查记录等任何信息都没有成功渲染出来"。
- **根因**：抽 hydrate 函数时**漏了 messages 端点**（verdict 页有独立 loadMsgs 所以正常，court 页没有）。
- **修复**：store 新增 `setMessages`；hydrate 补 messages 段 + `setActiveInvestigation(null)` 重置；接口设为 optional（verdict 用本地 useState 不传）。

### F5. 信念轨迹渲染崩溃 —— `Element type is invalid ... got: undefined (BeliefDiffCard)`（commit `9b9bbb6`）

- **根因**：`BeliefDiffCard.tsx` 的 `const SourceIcon = sourceIcons[diff.source]`，当后端返回表外新 source 值时为 `undefined`，React 渲染 undefined 崩溃。
- **修复**：`sourceIcons[diff.source as keyof typeof sourceIcons] ?? null` 防御性 fallback。

### F6. 切换庭审闪"连接已断开 / 已恢复连接" toast + toast 层级不对（commit `ee7ac1b`）

- **根因**：
  1. `websocket.ts` 的 `disconnect()` 显式调 `onConnectionStateChange("closed")`，而 `onclose` 里该回调在 `closedByUser` 检查**之前** → 路由切换 cleanup 时弹"断开"，紧接着新连接弹"恢复"。
  2. ToastContainer `z-50` 与 Dialog `z-50` 同层，被遮挡。
- **修复**：`disconnect()` 不再显式广播 closed；`onclose` 把 `closedByUser` 检查移到回调之前；ToastContainer `z-50` → `z-[100]`。

### F7. 第一次质证"操作未能完成" —— prosecutor empty content 中断整轮（commit `f3a93e0`）

- **现象**：`saveAgentMessage: refusing to persist empty content ... phase=cross_exam round=1`，整轮质证中断。
- **根因链**（从日志 tokens=6253/6404/6715/6758 的多次 retry 定位）：
  1. 流式发言成功，但内容含 "15%" 等数字被 `ValidateAgainstHallucination` 硬拒（`percentRegex` 误伤正常辩论语言）
  2. `out.Content = ""` 清空走 retry → retry 数次仍过不了校验 → fall-through 空 content
  3. saveAgentMessage 的 empty guard（本 session 早前加的）返 error → 整轮 return err 中断
- **修复**：`streamedFallback` 软降级 —— hallucination 硬拒时先把流式内容存局部变量；retry 耗尽后 content 仍空时恢复 fallback 内容 + slog.Warn。反幻觉初衷保留（先 retry），但不再中断 trial。
- **回归测试**：`react_runner_fallback_test.go` 2 sub-test（T1 精确复现本 bug）。

### 调查但非 bug（记录备查）

- **中文 title 乱码**：后端 Go 处理 UTF-8 完全正常（httptest + DB hex 双重验证）；根因是**本机 Git Bash curl 把 UTF-8 转 OEM/GBK 后发送**。浏览器 fetch 不受影响。测试时应用 `--data-binary @file` 代替 `-d '字符串'`。

---

## 二、⏸ 未解决问题（用户 2026-08-23 确认，标记未解决）

### U1. 判决书中策略笔记显示为空 ⏸ UNRESOLVED

- **现象**：判决书页幕后视角（BehindTheScenesPanel）策略笔记列表为空。
- **已排除**（证据链完整）：
  - 后端**有**产生数据：`docker logs` 显示 `[a2a] prosecutor → prosecutor (strategy_note, visibility=private, round=0..3)` 大量正常
  - DB **有**数据：最新 session（`3fe835f8-...` "我该学习吗"）a2_a_messages 含 10 条 strategy_note + 4 opponent_weakness + 1 self_correction
  - REST 端点**正常**：`GET /courtrooms/<uuid>/memory` 以真实 owner 调用返 `count: 15`，payload 结构完整（`payload.content` / `payload.stance` / `payload.confidence` 均在）
- **可疑根因（已定位待修）**：`frontend/lib/courtroomHydrate.ts` 第 6 段 memory 映射读 `r.content`（顶层），但后端返回结构内容在 **`r.payload.content`** → 所有 entry 的 `content=""`。且该 `setMemoryEntries(错误映射)` 在 `applyCourtEvent(正确路径)` **之后**执行，用空内容**覆盖**了正确数据。
  - 对照：store 的 `applyCourtEvent` a2a.message handler（courtroomStore.ts ~L749-810）正确读 `payload.content`，两者不一致即为铁证。
- **影响范围**：判决书页 + 历史庭审回看的策略笔记 Tab（共用 hydrate）。

### U2. 历史庭审中策略笔记依旧为空 ⏸ UNRESOLVED

- 同 U1 根因（`/court/[id]/page.tsx` 走同一 hydrateCourtroomStore）。

### U3. 策略笔记点击显示"渲染此页面时遇到错误"并记录到日志 ⏸ UNRESOLVED

- **现象**：用户点击策略笔记（sidebar Tab 或判决书幕后面板）触发渲染错误页。
- **初步怀疑**：与 U1 同源 —— hydrate 产出的 entry `content=""` 且缺失 `stance/confidence/reasoning` 可选字段；`MemoryTimeline.tsx` L99-101 检测结构化字段存在时走结构化卡片路径，kind 样式表 `KIND_STYLES[entry.kind]` 对非法 kind 值（如 `report`/`dispatch` 不在 MemoryKind 内却被强制 cast）可能 undefined 导致级联渲染失败。
- **前端 docker logs 无对应报错输出**（错误被 error boundary 吞掉，只在浏览器 console），需浏览器 DevTools 复现取完整堆栈确认。

### 未解决事项汇总表

| # | 问题 | 根因定位 | 修复方向（下次 session） |
|---|---|---|---|
| U1 | 判决书策略笔记空 | hydrate 读 `r.content`，实际在 `r.payload.content` | hydrate memory 映射改读 payload 嵌套字段（对齐 store applyCourtEvent 写法），并补 `stance/confidence/reasoning` |
| U2 | 历史庭审策略笔记空 | 同 U1 | 同 U1 |
| U3 | 策略笔记点击渲染出错 | 疑似同 U1（空 content + 非法 kind cast）+ MemoryTimeline 防御缺失 | 修 U1 后复测；MemoryTimeline 的 `KIND_STYLES[entry.kind]` 加 `?? 默认样式` 防御；filter 时排除非 MemoryKind 的 message_type（report/dispatch） |

---

## 三、修复过程测试数据快照

| 时点 | Backend (go test) | Frontend (pnpm test) | 备注 |
|---|---|---|---|
| v1.0.4 (726af6e) | 19 包 / ~324 sub-test | 79 | 基线 |
| v1.0-patch d72f860 | 19 包 PASS | 86 | +3 ListMySessions, +2 trialHistory |
| v1.0-patch ee7ac1b | 19 包 PASS | 90 | +2 BeliefDiffCard fallback, +2 websocket disconnect |
| v1.0-patch f3a93e0 | 19 包 PASS（agent 包 +2） | 90 | +2 streamedFallback |

## 四、Commit 索引

| Commit | 内容 |
|---|---|
| `d72f860` | F1 浏览器 back bug + 历史庭审回看（hydrate 抽取 + list API + trialHistory） |
| `696211c` | F2 React key 重复（删双 hydrate） |
| `2dac4c4` | F3 跨 session evidence 污染（setEvidences） |
| `53acab7` | F4 hydrate 补 messages + activeInvestigation |
| `9b9bbb6` | F5 BeliefDiffCard SourceIcon fallback |
| `ee7ac1b` | F6 WS closedByUser 顺序 + toast z-[100] |
| `f3a93e0` | F7 hallucination 硬拒软降级（streamedFallback） |

---

## 五、遗留教训（沿用 AGENTS.md 规范）

1. 抽公共函数时必须**逐项核对**老代码的端点清单（F4 漏 messages 即此教训）。
2. 后端 REST 返回嵌套结构（payload.content）与 WS 事件路径（applyCourtEvent）**字段层级不同**，映射时必须分别对齐（U1 根因）。
3. 每次前端修改后必须浏览器实测，不能只靠 tsc + unit test（本 session 多个 bug 均由用户实测发现）。

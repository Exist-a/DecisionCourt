# 阶段落地记录 · 2026-10-12（压缩 M8-2 收口 + 技术债 F1–F4）

| | |
|---|---|
| **日期** | 2026-10-12 |
| **上一阶段** | [`STAGE-LANDING-2026-10-11.md`](STAGE-LANDING-2026-10-11.md)（Module A–D + M8-1 预算 300k） |
| **本阶段主题** | ① 压缩/预算 4 项已定位缺陷 + 度量（A1–A5）② M8-2 system 拆「契约块+素材块」主重构 + 真实 A/B ③ 调查员搜索失败补日志（M9）④ 技术债 F1–F4 |
| **提交** | 本地 **8 个 commit**（`13a12c7` → `ca68df2`），**未 push**（push main = 自动上线，用户 2026-10-12 决定「听我通知」） |
| **状态** | ✅ 全部代码 + 文档 + 真实验证完成；⏳ 服务器侧运维项与 push 待用户 |

---

## 1. 模块清单与落地

### 第 1 轮 · 压缩/预算 4 项缺陷 + 指标（`13a12c7`）

| 编号 | 缺陷 | 修复 | 文件 |
|---|---|---|---|
| A1 | 兜底截断保留**头部**、丢弃**尾部** → 砍掉的恰是最新庭审历史 | 新增 `truncateSystemContent`：保契约头、丢**最旧**历史行；`TranscriptHistoryMarker` 单一来源 | `agent_gateway/prompt_compressor.go`、`agent/react_runner.go` |
| A2 | 限流把 `react_think` 的 `max_tokens` 砍到 100 → 决策 JSON 截断 → 重试（疑似净负） | JSON 输出（`opts.JSONMode`）纳入"关键输出"豁免：保留 max_tokens、仅降 temperature | `agent_gateway/throttler.go` |
| A3 | 预算拒绝只写 DB、不进 FileLogger（流式分支连 DB 都不写） | 两个拒绝分支各补 `writeFileLog`（流式另补 `recorder.Record`） | `agent_gateway/gateway.go` |
| A4 | 兜底摘要阈值 5 与 ReAct droppable 上限 4 不匹配 → 分支**不可达** | 默认阈值 5 → **3**（三处默认 + `.env.example`） | `config/config.go`、`agent_gateway/gateway_config.go`、`prompt_compressor.go` |
| A5 | `prompt_compression_ratio` 是 after/before，"省 0%" 时 = 1.0，不直观 | 新增 `prompt_compression_saved_ratio`（= 1 − ratio，0 = 没省） | `observability/metrics.go`、`prompt_compressor.go` |

回归测试 6 个文件。

### 第 2 轮 · M8-2：system 拆「契约块 + 素材块」（`4871d8b`）

- `message_section.go`：区块标记（`MessageSectionContract` / `MessageSectionMaterial`）+ `MarkContract/MarkMaterial/IsContractSystemMessage/IsMaterialMessage`。**未标记的旧式 system 一律视作契约** → 保持"system 强制保留"旧行为，向后兼容。
- `react_runner.buildInitialMessages`：system 拆为 **契约块**（baseRules + 工具协议，永不压缩）+ **素材块**（历史区段标记 + 逐条历史行，可压）。
- `prompt_compressor.CompressScored`：契约永不评分/丢弃；素材与普通消息同台参与评分/原子组/贪心；新增 `MaterialKeepRecentN`（默认 **6**）素材下限保最新历史。`CompressLegacy` 改为保留**全部** system（旧实现只留第一条，拆分后会误删素材）。
- `prompt_scorer.roleWeightFor`：素材块给中性权重 1.0（否则沿用 system 的 2.0 会永不丢弃）。
- 配置 `AGENT_GATEWAY_MATERIAL_KEEP_RECENT_N` 贯通 config → gateway_config → compressor。
- 回归测试 `compression_section_split_test.go`（契约永不丢 / 素材下限保最近弃最旧 / legacy 保留全部 system）。

### M9 · 调查员搜索失败补后端日志（`9461345`）

搜索失败时 `RecordFinding` 在第一步就 `return`（尚未发 A2A dispatch/report → DB 无痕），错误只回灌给 LLM 的 `[tool_error]` 与前端 banner，**后端零日志**（AGENTS.md §6.2e 规则 3）。在 `courtroom.DispatchInvestigator` 的 defer 的 `success=false` 分支补 `slog.Warn`（session/dispatcher/query/err）。**不改搜索逻辑**。

### 第 3 轮 · 技术债 F1–F4（`ab60756`）

| 编号 | 来源 | 处置 |
|---|---|---|
| **F1** | R4 遗留 | 用户确认「那可以删了」→ **整体删除**后端 trace 读端：`internal/trace/`（5 文件）+ `handler_trace.go` + `Handler.traceStore`/`WithTraceStore` + `RegisterTraceRoutes` 调用 + `main.go` 注入 + 前端 `Trace*` 类型。**边界**：只删"把日志再解析成调用树"这层；trace 数据仍在 `llm_calls` 表 + FileLogger，可观测栈不受影响 |
| **F2** | R10 遗留 | 补 `TestHardcodedBaseRules_MatchesYAML`：加载 `prompts/base.yaml` 与 `HardcodedBaseRules()` **逐字节比较**（唯一允许差异=末尾换行；实测 yaml 7604B / hardcoded 7605B） |
| **F3** | R5-d 遗留 | ① 证据占位**超时兜底**（30s → 标 `failed` → 「提交超时」+ 可移除）；判定抽成纯函数 `lib/pendingEvidence.ts::markTimedOut` + 5 单测 ② 渲染此前**从未渲染**的死字段 `inputHelpText` ③ `appeal` 阶段输入栏改禁用（后端状态机本就禁止 `submit_evidence`） |
| **F4** | D4 | 启动期日志 retention：`PruneOldLogs(logDir, days)` + `AGENT_GATEWAY_LOG_RETENTION_DAYS`（默认 30，≤0 关）。无 cron、不在写热路径 |

**顺带修正**：第 2 轮遗漏了 `buildGatewayConfig.MaterialKeepRecentN` 映射（env 不生效），由 `TestBuildGatewayConfig_MapsEveryField` 暴露。⚠️ 教训：第 2 轮曾报"全量测试全绿"，实为 `| head` 掩盖了退出码；本阶段起以**真实退出码**复核。

---

## 2. 真实验证证据（dev compose + IAB 浏览器）

> 方法：临时 compose override 调小预算/阈值逼出压缩与限流（**验证后即删，未入仓**）。

### 2.1 A1–A5（第 1 轮）

| 项 | 证据 |
|---|---|
| A3 | 文件日志出现 `"status":"error"` + `error_msg="agent_gateway: token budget exhausted … ratio=1.44"`（修复前仅 DB 有） |
| A2 | `react_think`（ratio 0.617）/ `react_speak_stream`（ratio 0.975）日志 `throttle_exempted=true` + `throttle_exempt_reason="json_output: truncated JSON is unusable"`，max_tokens 保留 500/1000 |
| A5 | `/metrics` 出现 `prompt_compression_saved_ratio`（本次短调用节省率 ≈ 0，与审计一致） |
| A4 | 默认阈值 3 生效；可达性由单测护栏钉住 |
| 业务 | 标准庭审开场正常、零 ERROR、`retry_count=0`；`budget_total=300000`（确认 M8-1 生效） |

### 2.2 M8-2（第 2 轮）

full 文件日志的 `input_messages` 确认拆分结构：
```
[0] role=system len=6992   ← 契约块（开头即 baseRules）
[1] role=system len=17     ← 历史区段标记
[2] role=system len=314    ← 单条历史行
```
`compression_material_forced` 已入 LogEntry，真实庭审中出现（=1/2）。

### 2.3 M9

强制失败（临时用**非空但无效**的 `BOCHA_API_KEY`；置空会命中启动 fail-fast）后日志出现：
```
WARN investigator search failed  session_uuid=… dispatcher=prosecutor
     query="…" err="investigation: search: bocha: status 401: {\"message\":\"Invalid API KEY\"…}"
```

### 2.4 F1–F4（第 3 轮）

- **F1**：`go build ./...` + `go test ./...`（**exit 0**）+ `tsc --noEmit` 干净；`lib` 测试 **107/107**。
- **F4**：重启 dev 后端出现 `log retention: removed old log file path=logs/agent_gateway_2026-07-01.log age_days=103 retention_days=30`，目录文件 6 → 5。
- **F3**：浏览器实测提交证据正常落库、「排队中」占位正确清除、页面重载无运行时错误。
- **`next build`**：Windows 上代码阶段全过（`✓ Compiled successfully` + lint 通过 + 静态页 5/5），仅"写 standalone 输出"的 **symlink** 步骤因本地无符号链接权限失败（环境限制，CI/Linux 不受影响）。

### 2.5 M8-2 真实 A/B 同题对跑（本阶段核心数据）

同题（"团队是否应该引入 AI 代码评审工具？"）、同配置（standard / 预算 250k / compress@0.3 / throttle@0.7），**唯一差异** = 临时开关 `AGENT_SPLIT_SYSTEM`（跑完已删）。两臂**均跑完整场**：

| | Arm A（拆分开） | Arm B（拆分关） |
|---|---|---|
| 调用数 | 36 | 38 |
| **总 token** | **210,970** | **224,396** |
| 错误 / 重试 | 0 / 0 | 0 / 0 |
| 预算峰值 | 0.828 | 0.879 |
| 压缩调用数 | 14 | 17 |
| **实际丢弃素材** | ✅ 9→7, 11→9, 13→11, 8→6, 9→6, 10→7, 11→8, 13→8, **15→10** | ❌ 全部 before==after |
| 素材下限触发 | 有 | 无（无素材块） |
| 判决书 | 生成，建议「引入 AI 评审」 | 生成，建议「引入 AI 评审」 |

**结论**：拆分使同场庭审 **少约 6.0% token**（210,970 vs 224,396），机制 = 旧素材可被丢弃（Arm B 结构性做不到——素材埋在单条 system 里永不参与评分）。两臂 **0 错误 0 重试、判决同向同结构** → **未观察到质量回归**。
**边界**：仅进程级 + 判决方向/字段层面；**未做人工/LLM 盲评**。

---

## 3. 本阶段决策

| 决策 | 结论 |
|---|---|
| M8-2 压缩重构 | **立项做完整重构**（用户），已实施 + A/B 完成 |
| P2「可见面」 | ✅ **定稿为选项①「维持现状」**（对方可见=正文+立场+置信度+引用证据；不可见=`reasoning`+私有笔记） |
| 上线/push | 用户：「**听我通知**」→ 全部本地提交，未 push |
| 安全 P2/P3 剩余 7 项 | 用户：**暂不启动** |
| F1 `/traces` | 用户：「**那可以删了**」→ 整体删除后端读端 |

---

## 4. 遗留与下一步

### 🔴 本阶段**新发现的真缺陷（未修，建议优先）**

**UI 驱动的庭审在同一后端进程跑多场之后出现「静默停滞」**：后端**无任何日志**、前端显示"质证进行中"却不再有 LLM 调用；**重启后端即恢复**。触发时机在多次 LLM 报错/预算拒绝之后出现。疑似 **ADR 0012 的 session 互斥锁在某条错误路径未释放**。与 M8-2 **无关**（Arm A 拆分开也停滞过；全新进程可正常跑完整场）。已登记在 [`deferred-items-2026-10-09 §M8`](todo/deferred-items-2026-10-09.md)。

### 🟡 单测覆盖但真机未观察到（低风险）

- A4 兜底摘要**真的插入**（实测有丢弃，未见 summary 插入）；
- F3 的「提交超时 + 移除」态（纯函数已单测；正常「排队中」与落库清除已实测）。

### ⏳ 服务器/运维侧（需用户或运维执行）

- 生产 `.env` 手工同步：`AGENT_GATEWAY_SUMMARY_INSERT_THRESHOLD`(5→3)、`AGENT_GATEWAY_MATERIAL_KEEP_RECENT_N`(新)、`AGENT_GATEWAY_LOG_RETENTION_DAYS`(新)；
- 生产库 `private_memories` 表**未 DROP**，且必须**先部署新代码再 DROP**；
- 存量机器 `chown -R 10001:10001 /opt/DecisionCourt/logs`；
- ECS sshd 加固 + CI 专用部署密钥替换 GitHub Secret。

### 📋 待排（用户已定/未启动）

安全 P2/P3 剩余 7 项（暂不启动）、push 上线（听通知）、tag 推送；**P3 措辞现已解锁**（P2 已定稿）；主张级接地扩展（因果/实体类）。

---

## 5. 关联文档

- [`COMPRESSION-REDESIGN-PROPOSAL.md`](COMPRESSION-REDESIGN-PROPOSAL.md)（状态：✅ 已实施）
- [`adr/0006-smart-prompt-compression.md`](adr/0006-smart-prompt-compression.md)（A1–A5 + M8-2 + A/B 数据）
- [`adr/0033-llm-trace-architecture.md`](adr/0033-llm-trace-architecture.md)（状态：⚠️ 部分归档，读端已删）
- [`P2-VISIBILITY-PROPOSAL.md`](P2-VISIBILITY-PROPOSAL.md)（状态：✅ 已定稿）
- [`todo/deferred-items-2026-10-09.md`](todo/deferred-items-2026-10-09.md) §M8 / §M9
- [`todo/deferred-items-2026-10-07.md`](todo/deferred-items-2026-10-07.md) R4 / R5-d / R10
- [`todo/deferred-items-2026-09-21.md`](todo/deferred-items-2026-09-21.md) §D4

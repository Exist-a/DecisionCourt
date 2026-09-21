# Deferred Items（2026-09-21 新增）

| | |
|---|---|
| | |
|---|---|
| **生成日期** | 2026-09-21 |
| **状态** | 🟡 D4 deferred（后续 PR）/ ✅ D5 范围明确 / ✅ D6 已于 v2.10 收尾 |
| **触发** | v2.8 PR-3 (ADR 0042) call out + plan v2.x 累积 |
| **关联 PR** | v2.8 PR-3 = commit `4d0e3XX`（待定）；D6 = v2.10 (ADR 0044) |

---

## D4. logs/ retention helper（v2.8 PR-3 call out）

### 现象
- v2.8 PR-3 (ADR 0042) 引入 `AGENT_GATEWAY_FILE_LOGGER_PROMPTS=full` opt-in，让
  LogEntry 写 system prompt + input messages + output content。
- 风险等级：用户启用 `full` 后，每日 `logs/agent_gateway_YYYY-MM-DD.log` 大小：
  - metadata 模式（默认）：~50 KB/trial × 5 trial/day = **250 KB/day**, ~93 MB/year
  - full 模式：**~2.5 MB/trial × 5 trial/day = 12.5 MB/day**, ~4.6 GB/year
- 当前 `logs/` 无 retention policy — files 累积直到磁盘满
- 项目 `scripts/monitor-daily.sh` 只检查 DB tables + `df /opt/DecisionCourt`，不
  touch `logs/` 目录（grep `logrotate` / `find -mtime` / `MaxSize` 等关键字命中
  数量 = 0）

### 根因
- `agent_gateway.FileLogger` (backend/internal/agent_gateway/file_logger.go)
  单纯 append + 按日期切分，无 cleanup 逻辑
- 与 v0.10.22 时期设计保持一致（ADR 0013 §决策 X）
- 设计动机是 "small project, personal dev, 不需要 logrotate" — 但 v2.8 full
  mode 改变了前提

### 建议修复（不在 v2.8 PR-3 范围，下次 PR）

工作量 1 天，方案二选一：

**A. 简单 OS-level logrotate** — 用户自己在外层用 logrotate / docker
volume cleanup。零代码改动。

**B. Go-level retention helper** — 在 `cmd/server/main.go` 启动时跑：
```go
// 保留最近 30 天, 删除旧文件
filepath.Walk(fl.logDir, func(path string, info os.FileInfo, err error) error {
    if err != nil || info.IsDir() { return nil }
    if time.Since(info.ModTime()) > 30 * 24 * time.Hour {
        slog.Info("log rotation: deleting old file", "path", path, "age", time.Since(info.ModTime()))
        os.Remove(path)
    }
    return nil
})
```
默认 30 天 (config 可调 `AGENT_GATEWAY_LOG_RETENTION_DAYS`)。

### 关联文档
- [ADR 0042 §3.2](../adr/0042-llm-trace-prompt-persistence.md) — v2.8 PR-3
  call out 位置
- `scripts/monitor-daily.sh` — 当前 ops monitoring 工具，不动 logs/

### 不做什么（明确边界）
- ❌ v2.8 PR-3 不实现 retention（不在本 PR scope，避免与 LogEntry 改动交缠）
- ❌ 不引入 cron 依赖（保持 single-binary 部署模型）
- ❌ 不在 FileLogger.Write 路径上做 rotate（写热路径必须保持简单无副作用）
- ❌ 不做 file compression / gzip（避免 Round-trip 读复杂度）

### 触发条件（什么时候做 D4）
- 用户启用 `full` mode production-level 使用（每日 > 100 trial）
- 磁盘使用 > 80% (`monitor-daily.sh` DISK_USAGE_THRESHOLD=80)
- 用户显式要求 "fix logs filling disk"

---

## D5. auto-overturn wiring (v2.9 PR-4 §2.1 call out, deferred to next PR)

### 现象
- v2.9 PR-4 (判决书考虑 rebuttal 状态) 是 §2.1 范畴, 用户授权范围:
  - 全链路 (prose + evidence_adoption 结构化字段 + belief 回填)
  - standing 证据硬忽略 / overturned 证据引用带 caveat
  - **auto-overturn 暂不实装** (用户决策 2026-09-21 ExitPlanMode 后)
- 当前 `evidence_rebuttal_links` 的 `UpdateStatus(standing → overturned)`
  写路径只在测试 fixture 出现,生产路径需手动调用

### 建议修复（v2.9 PR-4 中或后续单独 PR）
1. 在 `EmitRebuttalFromOutput` (agent/rebuttal_emitter.go) 添加反向 hook:
   律师 B 说 "I rebut the rebuttal of E001" 时, 调 `UpdateStatus(standing → overturned)`
2. 在 verdict prompt 加 "for each standing rebuttal, has the original rebuttal
   been effectively countered in later arguments? If yes, mark overturned"
3. UI 加手动 flip button (admin only)

### 触发条件
- v2.9 PR-4 main 工作落地
- 用户授权 "继续做 auto-overturn"
- 律师用户体验反馈 "想翻盘被反驳的证据"

---

## D6. Agent Gateway Token 压缩策略改进 (2026-09-21 审查发现) — ✅ Done (v2.10)

> **2026-09-21 收尾**：7 项全部落地，见 [ADR 0044 §3.3](../adr/0044-token-compression-strategy-review.md) + [release-notes/v2.10](../release-notes/v2.10.md)。
> 其中 #5 的实施修正了原文方案：`chars / N` 与旧的字符数预算线性同构、不改变任何保留决策，
> 实际改为按字符类别加权的 `EstimateTokens`（CJK 1.5 tok/char、ASCII 0.25 tok/char）。

### 现象
- 2026-09-21 对 `backend/internal/agent_gateway/` 压缩策略进行代码审查，发现 7 个问题
- 涉及配置一致性、评分准确性、原子组识别、预算单位、摘要质量、度量闭环
- 按优先级分为 P0（2 个）、P1（2 个）、P2（2 个）、P3（1 个）

### 问题清单

| # | 问题 | 优先级 | 涉及代码 | 修复成本 |
|---|---|---|---|---|
| 1 | `IsSmartCompressionEnabled` 不继承 `isChildDefault()` | P0 | `gateway_config.go:220-222` | 1 行 |
| 2 | `ScoreThreshold` 是死配置 | P1 | `config.go:208`, `prompt_compressor.go:282` | 5 行 |
| 3 | 原子组识别漏掉 evidence_id 链 | P0 | `prompt_atomic.go:27-72`, `orchestrator.go:838-841` | 20 行 + 测试 |
| 4 | 评分没有 recency decay | P2 | `prompt_scorer.go:14-15` | 需调权重 |
| 5 | 按字符 ratio 而非 token ratio | P1 | `prompt_greedy.go:15-18`, `prompt_greedy.go:30-31` | 10 行 |
| 6 | Summary 走 extractive 缺推理 | P3 | `prompt_summary.go:23-76` | 需 LLM 成本评估 |
| 7 | 无"压缩后判决质量"回环度量 | P2 | `gateway.go:262-289`, ADR 0037 | 需新指标设计 |

### 根因分析

**#1 配置 footgun**：
- `IsSmartCompressionEnabled()` 要求 `c.Enabled && c.SmartCompression`，不调用 `isChildDefault()`
- 设计意图是"必须显式开启"（注释："破坏性升级，需用户在 .env 显式开启"）
- 但 viper.SetDefault 已经把 `SmartCompression` 默认设成 true，与设计意图矛盾
- 如果将来有人把 viper 默认改回 false，Smart Compression 会静默回落到 legacy

**#2 死配置**：
- `AGENT_GATEWAY_SCORE_THRESHOLD` 存在但不参与筛选逻辑
- 筛选完全由 `GreedyPack` 的 `keepRatio` 控制（70%/40%/20%）
- 配置存在但不起作用，违反"配置即代码"原则

**#3 evidence_id 链漏识别**：
- `BuildAtomicGroups` 只识别 `tool_call_id`，不识别 evidence_id 链
- `orchestrator.speak()` 转换时丢弃 `Metadata` 字段，导致评分器看到的 `m.Metadata["evidence_id"]` 永远是空的
- 证据链在压缩时可能被拆散，后续轮次引用时 LLM 看不到原始证据内容

**#4 无 recency decay**：
- 评分公式不考虑消息位置（除了首末各 +0.3）
- 极端 budget 下，早期高分消息可能挤掉近期关键推理

**#5 字符 ratio ≠ token ratio**：
- `GreedyPack` 用字符数计算 target，而非 token
- 中文/代码混合场景，代码块被过度保留（"看起来"占空间但实际 token 少）

**#6 Summary 缺推理**：
- 只提取 anchor（evidence_id + 角色 + 120 字 preview），不含推理过程
- LLM 看到结论但不知道依据，可能编造其他依据

**#7 无质量回环度量**：
- 所有 metrics 都是操作型指标（压缩比、耗时、token 用量）
- 无法评估压缩策略的实际效果

### 建议修复（v2.10 候选）

**P0（下次 PR 必做）**：
1. **#1 配置 footgun**：在 `isChildDefault()` 里加 `!c.SmartCompression`
2. **#3 evidence_id 链**：
   - 在 `orchestrator.speak()` 转换时，把 `model.Message.EvidenceRefs` 和角色信息写入 `llm.Message.Metadata`
   - 在 `BuildAtomicGroups` 第一遍加 evidence_id 分组

**P1（v2.10 候选）**：
3. **#2 死配置**：在 `GreedyPack` 里加一道筛：`if g.GroupScore < cfg.ScoreThreshold { continue }`
4. **#5 字符 ratio**：在 `GreedyPack` 入口加 token 估算（`chars / 3`），target 改用 token 算

**P2（需要独立 ADR）**：
5. **#4 recency decay**：引入 soft decay 公式 `final = (1-α) × score + α × recency_weight`，α=0.3
6. **#7 质量回环**：加 `verdict_evidence_accuracy` 指标，检查判决书引用的 evidence_id 是否存在于原 transcript

**P3（看预算）**：
7. **#6 abstractive summary**：加开关 `AGENT_GATEWAY_SUMMARY_ABSTRACTIVE=true`，开启后调轻量 LLM 生成 abstractive summary

### 触发条件
- ~~用户授权 "继续做 token 压缩改进"~~ ✅ 2026-09-21 已完成
- ~~v2.10 版本规划~~ ✅ v2.10
- 庭审场景出现证据链断裂或幻觉问题（`verdict_evidence_accuracy` 指标持续观察，<1.0 时回看）

---

## 关联文档
- [ADR 0042 §3.2](../adr/0042-llm-trace-prompt-persistence.md) — D4 来源
- [ADR 0030 §4](../adr/0030-evidence-rebuttal-state-machine.md) — D5 来源
- [ADR 0044](../adr/0044-token-compression-strategy-review.md) — D6 来源（Token 压缩策略审查）
- [release-notes/v2.8.md §🔜](../release-notes/v2.8.md) — 用户汇总可见
- [release-notes/v2.10.md](../release-notes/v2.10.md) — D6 收尾发版说明

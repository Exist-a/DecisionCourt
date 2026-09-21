# Deferred Items（2026-09-21 新增）

| | |
|---|---|
| **生成日期** | 2026-09-21 |
| **状态** | 🟡 部分 done (D4 deferred,后续 PR) |
| **触发** | v2.8 PR-3 (ADR 0042) call out + plan v2.x 累积 |
| **关联 PR** | v2.8 PR-3 = commit `4d0e3XX`（待定） |

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

## 关联文档
- [ADR 0042 §3.2](../adr/0042-llm-trace-prompt-persistence.md) — D4 来源
- [ADR 0030 §4](../adr/0030-evidence-rebuttal-state-machine.md) — D5 来源
- [release-notes/v2.8.md §🔜](../release-notes/v2.8.md) — 用户汇总可见

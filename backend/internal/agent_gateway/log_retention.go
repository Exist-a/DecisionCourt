package agent_gateway

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogFilePrefix / LogFileSuffix 界定 retention 只清理 FileLogger 自己写的日切文件，
// 不碰同目录下的其它产物（如 trace store 的文件）。
const (
	LogFilePrefix = "agent_gateway_"
	LogFileSuffix = ".log"
)

// PruneOldLogs 删除 logDir 下**超过 retentionDays 天**的 `agent_gateway_*.log`。
//
// v2.12 (F4 / deferred D4)：`AGENT_GATEWAY_FILE_LOGGER_PROMPTS=full` 打开后，单日
// 日志可达 ~12.5 MB/day（≈4.6 GB/year），而 logs/ 此前**没有任何 retention** ——
// 文件累积到磁盘满（2026-09-21 D 盘写满即为佐证）。这里做启动期一次性清理：
// 无 cron 依赖（保持 single-binary 部署），不在写热路径上做副作用（不违反 D4 边界）。
//
// 行为：
//   - retentionDays <= 0 → 视为**禁用**，直接返回（默认由 config 给 30）。
//   - 只删 `agent_gateway_*.log` 前缀/后缀匹配的**普通文件**（不递归子目录）。
//   - 单个文件删除失败只记 WARN，不影响其余文件与启动。
//
// 返回删除的文件数。
func PruneOldLogs(logDir string, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	dir := logDir
	if dir == "" {
		dir = defaultLogDir
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // 目录还不存在（尚未产生日志）—— 不是错误
		}
		return 0, fmt.Errorf("prune logs: read dir %q: %w", dir, err)
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, LogFilePrefix) || !strings.HasSuffix(name, LogFileSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			slog.Warn("log retention: stat failed", "file", name, "err", err)
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			slog.Warn("log retention: delete failed", "path", path, "err", err)
			continue
		}
		removed++
		slog.Info("log retention: removed old log file",
			"path", path,
			"age_days", int(time.Since(info.ModTime()).Hours()/24),
			"retention_days", retentionDays,
		)
	}
	return removed, nil
}

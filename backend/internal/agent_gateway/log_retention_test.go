package agent_gateway

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneOldLogs_RemovesOnlyOldAgentGatewayFiles 是 F4（deferred D4）的护栏：
// retention 只删超期的 `agent_gateway_*.log`，且不碰其它文件 / 子目录。
func TestPruneOldLogs_RemovesOnlyOldAgentGatewayFiles(t *testing.T) {
	dir := t.TempDir()

	old := filepath.Join(dir, "agent_gateway_2020-01-01.log")
	recent := filepath.Join(dir, "agent_gateway_2099-01-01.log")
	other := filepath.Join(dir, "some_other_file.log")
	for _, p := range []string{old, recent, other} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	// 把 old 与 other 的 mtime 拨到 100 天前。
	oldTime := time.Now().Add(-100 * 24 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(other, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	n, err := PruneOldLogs(dir, 30)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Errorf("应只删 1 个过期文件，got %d", n)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("过期的 agent_gateway_*.log 应被删除")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("未过期的日志不应被删除")
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("非 agent_gateway_*.log 的文件不应被碰")
	}
}

// TestPruneOldLogs_Disabled 当 retentionDays <= 0 时完全不动文件。
func TestPruneOldLogs_Disabled(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "agent_gateway_2020-01-01.log")
	if err := os.WriteFile(old, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	oldTime := time.Now().Add(-100 * 24 * time.Hour)
	_ = os.Chtimes(old, oldTime, oldTime)

	if n, err := PruneOldLogs(dir, 0); err != nil || n != 0 {
		t.Fatalf("retention=0 应禁用：n=%d err=%v", n, err)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("禁用时不应删除任何文件")
	}
}

// TestPruneOldLogs_MissingDirIsNotAnError 日志目录尚未创建时不算错误。
func TestPruneOldLogs_MissingDirIsNotAnError(t *testing.T) {
	if n, err := PruneOldLogs(filepath.Join(t.TempDir(), "nope"), 30); err != nil || n != 0 {
		t.Fatalf("目录不存在应返回 (0,nil)，got n=%d err=%v", n, err)
	}
}

package agent_gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileLogger_WritesJSON(t *testing.T) {
	dir := t.TempDir()
	fl := NewFileLogger(dir)
	defer fl.Close()

	entry := LogEntry{
		RequestID:   "req-1",
		SessionUUID: "sess-1",
		AgentType:   "prosecutor",
		TaskType:    "speak",
		Model:       "deepseek-chat",
		Provider:    "deepseek",
		TotalTokens: 123,
		Status:      StatusSuccess,
	}
	if err := fl.Write(entry); err != nil {
		t.Fatalf("write: %v", err)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 log file, got %d", len(files))
	}
	if !strings.HasPrefix(files[0].Name(), "agent_gateway_") {
		t.Errorf("unexpected filename: %s", files[0].Name())
	}

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var got LogEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RequestID != "req-1" || got.AgentType != "prosecutor" || got.TotalTokens != 123 {
		t.Errorf("entry mismatch: %+v", got)
	}
	if got.Timestamp.IsZero() {
		t.Errorf("timestamp should be auto-filled")
	}
}

func TestFileLogger_AppendsMultiple(t *testing.T) {
	dir := t.TempDir()
	fl := NewFileLogger(dir)
	defer fl.Close()

	for i := 0; i < 3; i++ {
		if err := fl.Write(LogEntry{RequestID: "req", Status: StatusSuccess}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	files, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("want 3 lines, got %d", len(lines))
	}
}

func TestFileLogger_DateRotation(t *testing.T) {
	fl := NewFileLogger(t.TempDir())
	defer fl.Close()

	// 无法直接改变时间，我们验证文件名格式即可。
	today := time.Now().Local().Format(dateFormat)
	name := filepath.Join(fl.logDir, "agent_gateway_"+today+".log")
	fl.Write(LogEntry{RequestID: "req"})
	if _, err := os.Stat(name); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist", name)
	}
}

// v0.10.22 PR-A: 显式命名 BasicWrite (而非已有 WritesJSON), 验证 LogEntry
// 38 字段中关键字段 (RequestID / Model / TotalTokens / Status) 正确序列化,
// 避免日后有人误改 LogEntry 字段导致本地 dev 看不到日志。
//
// 区别于 TestFileLogger_WritesJSON: BasicWrite 用 bytes.TrimSpace 处理
// 单行 JSON + 校验 timestamp.IsZero(), 强化 "时间戳自动填充" 这条契约。
func TestFileLogger_BasicWrite(t *testing.T) {
	dir := t.TempDir()
	fl := NewFileLogger(dir)
	defer fl.Close()

	entry := LogEntry{
		RequestID:   "req-basic",
		SessionUUID: "sess-basic",
		AgentType:   "prosecutor",
		Model:       "deepseek-chat",
		TotalTokens: 42,
		Status:      StatusSuccess,
	}
	if err := fl.Write(entry); err != nil {
		t.Fatalf("write: %v", err)
	}

	today := time.Now().Local().Format(dateFormat)
	name := filepath.Join(dir, "agent_gateway_"+today+".log")
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var got LogEntry
	if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.RequestID != "req-basic" || got.TotalTokens != 42 || got.Status != StatusSuccess {
		t.Errorf("entry mismatch: %+v", got)
	}
	if got.Timestamp.IsZero() {
		t.Errorf("timestamp should be auto-filled")
	}
}

// v0.10.22 PR-A: 验证 DirectoryCreate — 父路径不存在时 Write 自动 MkdirAll。
// 修 v0.9.2 旧注释说的"未打开功能"历史: 当时主要是 docker volume 不挂,
// 不是程序不创建目录。DirectoryCreate 强化对"目录自动创建"行为的测试。
//
// 3 层缺失目录 (root/missing/nested/logs) 验证嵌套创建 + 自动 MkdirAll。
func TestFileLogger_DirectoryCreate(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "missing", "nested", "logs")
	fl := NewFileLogger(dir)
	defer fl.Close()

	if err := fl.Write(LogEntry{RequestID: "req-dir", Status: StatusSuccess}); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("log dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("log path is not a directory: %s", dir)
	}

	today := time.Now().Local().Format(dateFormat)
	name := filepath.Join(dir, "agent_gateway_"+today+".log")
	if _, err := os.Stat(name); err != nil {
		t.Errorf("expected file %s to exist: %v", name, err)
	}
}

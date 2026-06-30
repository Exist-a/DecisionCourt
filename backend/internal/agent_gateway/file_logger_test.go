package agent_gateway

import (
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

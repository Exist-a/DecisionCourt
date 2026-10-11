package agent_gateway

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

func readTodayLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "agent_gateway_"+todayLocal()+".log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	return string(data)
}

func exhaustedRejectGateway(t *testing.T) (*Gateway, string) {
	t.Helper()
	dir := t.TempDir()
	inner := &fakeLLM{completeContent: "should-not-be-called"}
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, newFakeStore())
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  true,
		BudgetPerSession:     1000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
		FileLogger:           true,
		LogDir:               dir,
	}.Normalize()
	return NewWithConfig(inner, rec, "deepseek-v4-flash", cfg, nil), dir
}

// closeFileLogger 关闭 gateway 持有的文件日志句柄：Windows 下不关会阻止
// t.TempDir 清理（文件被占用）。
func closeFileLogger(t *testing.T, gw *Gateway) {
	t.Helper()
	t.Cleanup(func() {
		if gw.fileLogger != nil {
			_ = gw.fileLogger.Close()
		}
	})
}

// TestGateway_RejectWhenExhausted_WritesFileLog 是 M8-2 A3 的回归钉：预算拒绝
// 必须同时落 llm_calls 表**与** FileLogger。此前只写表不写文件，运维看
// "详细调用快照"文件会漏掉"判决被拒"这一最关键事件。
func TestGateway_RejectWhenExhausted_WritesFileLog(t *testing.T) {
	gw, dir := exhaustedRejectGateway(t)
	closeFileLogger(t, gw)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "a3-c1", AgentType: "judge", TaskType: "verdict"})
	gw.budget.AddUsage(ctx, "a3-c1", BudgetUsage{InputTokens: 500, OutputTokens: 600})

	_, _, err := gw.Complete(ctx, "sys", []llm.Message{{Role: "user", Content: "x"}}, llm.CompletionOptions{})
	if err == nil {
		t.Fatal("expected ErrBudgetExhausted")
	}

	log := readTodayLog(t, dir)
	if !strings.Contains(log, "token budget exhausted") {
		t.Errorf("拒绝分支必须落文件日志（M8-2 A3）；got: %s", log)
	}
	if !strings.Contains(log, `"status":"error"`) {
		t.Errorf("拒绝记录状态应为 error；got: %s", log)
	}
}

// TestGateway_StreamRejectWhenExhausted_WritesFileLogAndRecord 覆盖流式拒绝：
// 此前该分支**既不落库也不落文件**（只在 channel 里 emit 一个 error chunk），
// 比非流式分支更糟。修后须与非流式对齐（DB + 文件各一条）。
func TestGateway_StreamRejectWhenExhausted_WritesFileLogAndRecord(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeLLM{completeContent: "nope"}
	store := newFakeStore()
	rec := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)
	cfg := GatewayConfig{
		Enabled:              true,
		TokenBudget:          true,
		RejectWhenExhausted:  true,
		BudgetPerSession:     1000,
		CompressionThreshold: 0.7,
		ThrottlingThreshold:  0.8,
		FileLogger:           true,
		LogDir:               dir,
	}.Normalize()
	gw := NewWithConfig(inner, rec, "deepseek-v4-flash", cfg, nil)
	closeFileLogger(t, gw)

	ctx := WithTrace(context.Background(), Trace{SessionUUID: "a3-s1", AgentType: "prosecutor", TaskType: "speak"})
	gw.budget.AddUsage(ctx, "a3-s1", BudgetUsage{InputTokens: 500, OutputTokens: 600})

	var lastErr error
	for chunk := range gw.StreamComplete(ctx, "sys", []llm.Message{{Role: "user", Content: "x"}}, llm.CompletionOptions{}) {
		if chunk.Err != nil {
			lastErr = chunk.Err
		}
	}
	if lastErr == nil {
		t.Fatal("expected stream reject error chunk")
	}

	if log := readTodayLog(t, dir); !strings.Contains(log, "token budget exhausted") {
		t.Errorf("流式拒绝必须落文件日志；got: %s", log)
	}
	store.mu.Lock()
	rows := len(store.rows)
	store.mu.Unlock()
	if rows == 0 {
		t.Errorf("流式拒绝必须落 llm_calls 表（此前完全无记录）")
	}
}

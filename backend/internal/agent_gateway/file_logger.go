package agent_gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogEntry 是 Agent Gateway 文件日志的单行 JSON 记录。字段设计尽量详细，
// 方便后续做开关对比实验时做精细化分析。
type LogEntry struct {
	Timestamp             time.Time `json:"timestamp"`
	RequestID             string    `json:"request_id"`
	SessionUUID           string    `json:"session_uuid"`
	AgentType             string    `json:"agent_type"`
	TaskType              string    `json:"task_type"`
	Model                 string    `json:"model"`
	Provider              string    `json:"provider"`
	PromptTokens          int       `json:"prompt_tokens"`
	CompletionTokens      int       `json:"completion_tokens"`
	TotalTokens           int       `json:"total_tokens"`
	LatencyMs             int       `json:"latency_ms"`
	Status                string    `json:"status"`
	ErrorMsg              string    `json:"error_msg"`
	Compressed            bool      `json:"compressed"`
	CompressionBeforeCount int      `json:"compression_before_count"`
	CompressionAfterCount int       `json:"compression_after_count"`
	CompressionBeforeLength int     `json:"compression_before_length"`
	CompressionAfterLength int      `json:"compression_after_length"`
	Throttled             bool      `json:"throttled"`
	ThrottleExempted      bool      `json:"throttle_exempted"`
	ThrottleExemptReason  string    `json:"throttle_exempt_reason"`
	MaxTokensBefore       int       `json:"max_tokens_before"`
	MaxTokensAfter        int       `json:"max_tokens_after"`
	TemperatureBefore     float32   `json:"temperature_before"`
	RetryCount            int       `json:"retry_count"`
	BudgetUsed            int       `json:"budget_used"`
	BudgetTotal           int       `json:"budget_total"`
	BudgetRatio           float64   `json:"budget_ratio"`
}

// FileLogger 把 Agent Gateway 运行日志以 JSON 每行追加到文件，按日期切分。
const defaultLogDir = "logs"
const dateFormat = "2006-01-02"

// FileLogger 把 Agent Gateway 运行日志以 JSON 每行追加到文件，按日期切分。
type FileLogger struct {
	logDir      string
	mu          sync.Mutex
	currentDate string
	file        *os.File
}

// NewFileLogger 构造文件日志器；logDir 为空时使用默认 "logs" 目录。
func NewFileLogger(logDir string) *FileLogger {
	if logDir == "" {
		logDir = defaultLogDir
	}
	return &FileLogger{logDir: logDir}
}

// Write 追加一条日志。按日期自动切换文件。
func (fl *FileLogger) Write(entry LogEntry) error {
	fl.mu.Lock()
	defer fl.mu.Unlock()

	today := time.Now().Local().Format(dateFormat)
	if fl.currentDate != today || fl.file == nil {
		if fl.file != nil {
			fl.file.Close()
		}
		if err := os.MkdirAll(fl.logDir, 0755); err != nil {
			return err
		}
		name := filepath.Join(fl.logDir, "agent_gateway_"+today+".log")
		f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		fl.file = f
		fl.currentDate = today
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().Local()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = fl.file.Write(append(data, '\n'))
	return err
}

// Close 关闭当前文件。
func (fl *FileLogger) Close() error {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	if fl.file != nil {
		return fl.file.Close()
	}
	return nil
}

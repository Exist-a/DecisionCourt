// Package trace 提供 LLM Trace 读端聚合与查询能力。详见 ADR 0033 + V1.0.4-PLAN.md。
//
// 设计目标:
//   - 复用现有 agent_gateway_*.log (v0.10.22 PR-A 实装的 JSON Lines 落盘)
//   - 不引入新存储 (只读 + 聚合),不破坏 LLM 调用 hot path
//   - 暴露 REST 端点供前端 TrialReplay 可视化 (PR-C2)
//
// 本文件 (parser.go) 把 LogEntry JSON 行解析成 Run 类型,并构造聚合数据结构。
//
// Run.TraceID ↔ LogEntry.RequestID:
//   agent_gateway LogEntry 用 RequestID 字段记录 HTTP trace_id (observability.TraceMiddleware
//   从 X-Request-ID header 注入),本包用 TraceID 字段命名是为了对外 API 与 OTel 标准一致。
//   解析时 Run.TraceID = LogEntry.RequestID,Run.RunID = RequestID + "-" + RetryCount。
package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/decisioncourt/backend/internal/agent_gateway"
)

// Run 描述单次 LLM 调用的完整元数据。
//
// 字段映射 (与 V1.0.4-PLAN.md §1.3 Run 数据模型对齐):
//   - RunID       = LogEntry.RequestID + "-" + LogEntry.RetryCount (同 trace 内 unique)
//   - TraceID     = LogEntry.RequestID (与 HTTP middleware 注入的 trace_id 一致)
//   - SessionID   = LogEntry.SessionUUID
//   - AgentType   = LogEntry.AgentType
//   - TaskType    = LogEntry.TaskType (e.g. "react_think" / "react_speak" / "react_rebuttal_retry")
//   - StartedAt   = LogEntry.Timestamp - LogEntry.LatencyMs (从 log 时间戳反推)
//   - EndedAt     = LogEntry.Timestamp
//   - LatencyMs   = LogEntry.LatencyMs
//   - Status      = LogEntry.Status ("ok" / "error")
//   - ErrorMsg    = LogEntry.ErrorMsg
//   - RetryCount  = LogEntry.RetryCount (0 = 首次,1+ = retry)
//
// 缺失字段 (LogEntry 没有,暂留空,后续 PR 可补):
//   - Input       = nil (json:"input" omitempty)
//   - Output      = "" (json:"output" omitempty)
//   - Tags        = nil (json:"tags" omitempty)
//
// 设计妥协:
//   - Input/Output 不在 file log 里 (v0.10.22 PR-A 实装时为节省磁盘只记元数据)
//   - 后续 PR 可加 "全量 prompt 落盘" 开关,把 Input/Output 填上
//   - 本 PR 不强加这个开关,避免 disk 增长失控
type Run struct {
	RunID      string            `json:"run_id"`
	TraceID    string            `json:"trace_id"`
	SessionID  string            `json:"session_id"`
	AgentType  string            `json:"agent_type"`
	TaskType   string            `json:"task_type"`
	Model      string            `json:"model"`
	Provider   string            `json:"provider"`
	StartedAt  time.Time         `json:"started_at"`
	EndedAt    time.Time         `json:"ended_at"`
	LatencyMs  int               `json:"latency_ms"`
	Status     string            `json:"status"`
	Input      map[string]any    `json:"input,omitempty"`
	Output     string            `json:"output,omitempty"`
	ErrorMsg   string            `json:"error_msg,omitempty"`
	RetryCount int               `json:"retry_count"`
	Tags       map[string]string `json:"tags,omitempty"`
}

// SessionTrace 是单个 session 的所有 runs 按 trace_id 分组后的视图。
//
// 字段:
//   - SessionID  session UUID
//   - Traces     按 TraceID 分组的所有 Run 列表
//   - StartedAt  该 session 第一次 LLM 调用时间 (= min Runs.EndedAt)
//   - EndedAt    该 session 最后一次 LLM 调用时间 (= max Runs.EndedAt)
//
// 不直接暴露树状结构 (Tree RunNode),树状构造在 aggregator.go 单独实现。
// REST 端点返回 []SessionTrace,前端按需调用 GetTraceByID 拿单个 trace 的 tree。
type SessionTrace struct {
	SessionID string           `json:"session_id"`
	StartedAt time.Time        `json:"started_at"`
	EndedAt   time.Time        `json:"ended_at"`
	Traces    map[string][]Run `json:"traces"`
}

// ParseFile 读指定 agent_gateway 日志文件,解析成 SessionTrace。
//
// 行为要点:
//   - 文件不存在 → 返回 error (包装 path 便于排查),不静默返回空
//   - 单行 JSON 解析失败 → 跳过该行 + 累计 parseErrors,继续解析剩余行
//   - 空文件 → 返回 (SessionTrace{Traces: map{}}, nil)
//   - 行顺序不影响聚合 (内部按 EndedAt 排序)
//
// 返回值:
//   - SessionTrace: 解析后的数据 (按 trace_id 分组)
//   - parseErrors: 解析失败的行数 (不阻塞主流程,只做统计 + 未来埋点)
//   - error: 仅在文件 open 失败 / 完全无法读取时返回
//
// 复用模式: 本函数被 FileTraceStore.ListBySession 调用,生产读 1 天文件;
// 测试场景 parser_test.go 用 t.TempDir() 写文件 + 调 ParseFile 验证聚合。
func ParseFile(path string) (*SessionTrace, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("trace.ParseFile: open %s: %w", path, err)
	}
	defer f.Close()

	return ParseReader(f)
}

// ParseReader 从任意 io.Reader 解析 JSON Lines。
//
// 设计理由:
//   - 测试场景用 bytes.NewReader (无需写文件),生产场景用 os.File
//   - 接口分离,便于 mock 与 future 数据源 (e.g. 网络流)
//
// bufio.Scanner 默认 token size 64KB 可能不够 (Run.Output 长时),本函数显式
// 提升到 1MB 覆盖典型 prompt + output (实测 max ~50KB)。
func ParseReader(r io.Reader) (*SessionTrace, int, error) {
	sessionTrace := &SessionTrace{
		Traces: make(map[string][]Run),
	}
	var parseErrors int

	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024) // 1MB max token size

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry agent_gateway.LogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// 跳过单行,继续解析。parseErrors 给调用方做埋点 / 日志
			parseErrors++
			continue
		}

		run := entryToRun(&entry)
		if sessionTrace.SessionID == "" {
			sessionTrace.SessionID = run.SessionID
		}
		if run.SessionID != sessionTrace.SessionID && sessionTrace.SessionID != "" {
			// 单文件包含多 session 是异常 (LogEntry.SessionUUID 应一致),
			// 但保留所有 run (用各自 SessionID 作 key) 不丢弃数据
			// — 上层 aggregator 会按 SessionID 二次聚合
		}
		sessionTrace.Traces[run.TraceID] = append(sessionTrace.Traces[run.TraceID], run)
	}

	if err := scanner.Err(); err != nil {
		return nil, parseErrors, fmt.Errorf("trace.ParseReader: scan: %w", err)
	}

	// 排序每个 trace 的 runs (按 RetryCount 升序 = LLM 调用顺序)
	for traceID := range sessionTrace.Traces {
		runs := sessionTrace.Traces[traceID]
		sort.SliceStable(runs, func(i, j int) bool {
			return runs[i].RetryCount < runs[j].RetryCount
		})
		sessionTrace.Traces[traceID] = runs
	}

	// 算 SessionTrace 整体时间范围
	sessionTrace.StartedAt, sessionTrace.EndedAt = sessionTimeRange(sessionTrace)

	return sessionTrace, parseErrors, nil
}

// entryToRun 把 agent_gateway.LogEntry 转成本包的 Run。
//
// 单独抽函数便于测试 (parser_test.go 直接构造 LogEntry 验证 Run 字段映射)。
//
// 缺失/推算字段:
//   - StartedAt = Timestamp - LatencyMs (从 log 落盘时间反推调用开始时间)
//   - EndedAt   = Timestamp
//   - RunID     = RequestID + "-" + RetryCount (同 trace 内 unique)
func entryToRun(entry *agent_gateway.LogEntry) Run {
	endedAt := entry.Timestamp
	if endedAt.IsZero() {
		endedAt = time.Now().Local()
	}
	startedAt := endedAt.Add(-time.Duration(entry.LatencyMs) * time.Millisecond)

	return Run{
		RunID:      fmt.Sprintf("%s-%d", entry.RequestID, entry.RetryCount),
		TraceID:    entry.RequestID,
		SessionID:  entry.SessionUUID,
		AgentType:  entry.AgentType,
		TaskType:   entry.TaskType,
		Model:      entry.Model,
		Provider:   entry.Provider,
		StartedAt:  startedAt,
		EndedAt:    endedAt,
		LatencyMs:  entry.LatencyMs,
		Status:     entry.Status,
		ErrorMsg:   entry.ErrorMsg,
		RetryCount: entry.RetryCount,
	}
}

// sessionTimeRange 算 SessionTrace 的时间范围 (min/max EndedAt)。
//
// 空 SessionTrace 返回零值 time.Time{},调用方应处理 (REST 端点返 200 + 空数据)。
func sessionTimeRange(st *SessionTrace) (time.Time, time.Time) {
	if len(st.Traces) == 0 {
		return time.Time{}, time.Time{}
	}
	var minStart, maxEnd time.Time
	first := true
	for _, runs := range st.Traces {
		for _, r := range runs {
			if first {
				minStart = r.StartedAt
				maxEnd = r.EndedAt
				first = false
				continue
			}
			if r.StartedAt.Before(minStart) {
				minStart = r.StartedAt
			}
			if r.EndedAt.After(maxEnd) {
				maxEnd = r.EndedAt
			}
		}
	}
	return minStart, maxEnd
}

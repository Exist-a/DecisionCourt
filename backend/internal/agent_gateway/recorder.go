package agent_gateway

import (
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Recorder 是 Agent Gateway 的审计写入器。它把每次 LLM 调用封装成
// 一行 llm_calls 记录，写入由 Store 接口提供的后端。
//
// 设计要点：
//   - Enabled=false 时 Record() 是 noop，用于测试与关闭审计开关。
//   - 写库失败仅记 log，不抛出 — 网关不应因为审计失败而中断主流程。
//   - ErrMessage 截断到 MaxErrorMsgLen，避免单条记录被超长异常撑爆。
type Recorder struct {
	cfg   RecorderConfig
	store Store
	// promptVersion 是 promptlab 版本归因键的提供者（R13）。nil 表示未接线，
	// 此时 Record.PromptVersion 留空 —— 宁可为空，也不要伪造一个版本号。
	// 用函数而不是值：promptlab 支持 YAML 热加载，版本会随文件内容变化，
	// 写时取快照才能归因到"那一刻生效的 prompt"。
	promptVersion func() string
}

// RecorderConfig 控制 Recorder 的开关与 provider 标记。
type RecorderConfig struct {
	Enabled  bool
	Provider string // 写死到 Record.Provider；MVP 写 deepseek
}

// CallInput 是 Gateway 在调用结束埋点时拼装的单次调用快照。
type CallInput struct {
	Trace   Trace
	Model   string
	Usage   Usage
	Latency time.Duration
	Status  string // StatusSuccess / StatusError
	Err     error
}

// Usage 是 LLM 返回的 token 计数（与 llm.Usage 解耦，避免在网关层反向
// 依赖 llm 包；上层在埋点时手工 copy）。
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Record 是写入存储的最小可观测单元。
type Record struct {
	ID          string
	SessionUUID string
	AgentType   string
	TaskType    string
	RequestID   string
	Model       string
	Provider    string
	// PromptVersion 见 model.LLMCall.PromptVersion（R13）。
	PromptVersion string
	// Sessionless 见 Trace.Sessionless（R14）：显式无会话的调用照常落库
	// （session_id 为 NULL），而不是被当成"漏填 session 的 bug"拦掉。
	Sessionless      bool
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	LatencyMs        int
	Status           string
	ErrorMsg         string
	CreatedAt        time.Time
}

// Store 是 Recorder 的存储后端抽象。生产环境由 GORMStore 实现；测试
// 可注入内存 store 替身。设计为单方法以保持 Recorder 无依赖、易测试。
type Store interface {
	Insert(r Record) error
}

// 状态常量。供 Gateway 装饰器引用。
const (
	StatusSuccess = "success"
	StatusError   = "error"
)

// MaxErrorMsgLen 限制单条 error_message 的字符数，避免超长异常爆库。
const MaxErrorMsgLen = 500

// MaxPromptVersionLen 与 model.LLMCall.PromptVersion 的 varchar(120) 对齐。
const MaxPromptVersionLen = 120

// NewRecorder 构造一个 Recorder；store 为 nil 时 Record() 仅打 log（不写库）。
func NewRecorder(cfg RecorderConfig, store Store) *Recorder {
	return &Recorder{cfg: cfg, store: store}
}

// SetPromptVersionProvider 接线"当前生效的 prompt 版本"取值函数（R13）。
//
// 必须在 server 开始处理请求前调用一次（装配期）。之后每次 Record 都会重新
// 求值 —— promptlab 支持 YAML 热加载，写成值快照会归因到启动时那一版。
func (r *Recorder) SetPromptVersionProvider(fn func() string) {
	if r == nil {
		return
	}
	r.promptVersion = fn
}

// PromptVersion 返回当前 prompt 版本归因键；未接线时返回空字符串。
// Gateway 的文件日志复用它，保证 DB 行与 JSON Lines 的归因值一致。
func (r *Recorder) PromptVersion() string {
	if r == nil || r.promptVersion == nil {
		return ""
	}
	return truncatePromptVersion(r.promptVersion())
}

// truncatePromptVersion 限制归因键长度，避免超长版本串撑爆 varchar(120)。
func truncatePromptVersion(v string) string {
	if len(v) > MaxPromptVersionLen {
		return v[:MaxPromptVersionLen]
	}
	return v
}

// Record 把一次 LLM 调用的快照写库。失败仅 log，不 panic。
func (r *Recorder) Record(in CallInput) {
	if r == nil || !r.cfg.Enabled {
		return
	}
	rec := r.buildRecord(in)
	if r.store == nil {
		// v0.8 白盒化：slog 结构化日志，含 trace_id / session_uuid 字段。
		slog.Info("agent_gateway noop record",
			"request_id", rec.RequestID,
			"session_uuid", rec.SessionUUID,
			"model", rec.Model,
			"total_tokens", rec.TotalTokens,
			"status", rec.Status,
		)
		return
	}
	if err := r.store.Insert(rec); err != nil {
		slog.Warn("agent_gateway recorder insert failed",
			"request_id", rec.RequestID,
			"session_uuid", rec.SessionUUID,
			"model", rec.Model,
			"error", err,
		)
	}
}

// buildRecord 公开出来供测试断言字段映射；运行期由 Record 内部调用。
func (r *Recorder) buildRecord(in CallInput) Record {
	provider := r.cfg.Provider
	if provider == "" {
		provider = "unknown"
	}
	errMsg := ""
	if in.Err != nil {
		errMsg = in.Err.Error()
		if len(errMsg) > MaxErrorMsgLen {
			errMsg = errMsg[:MaxErrorMsgLen]
		}
	}
	return Record{
		ID:               uuid.NewString(),
		SessionUUID:      in.Trace.SessionUUID,
		AgentType:        in.Trace.AgentType,
		TaskType:         in.Trace.TaskType,
		RequestID:        in.Trace.RequestID,
		Model:            in.Model,
		Provider:         provider,
		PromptVersion:    r.PromptVersion(),
		Sessionless:      in.Trace.Sessionless,
		PromptTokens:     in.Usage.PromptTokens,
		CompletionTokens: in.Usage.CompletionTokens,
		TotalTokens:      in.Usage.TotalTokens,
		LatencyMs:        int(in.Latency / time.Millisecond),
		Status:           in.Status,
		ErrorMsg:         errMsg,
		CreatedAt:        time.Now().UTC(),
	}
}

package agent_gateway

import (
	"log"
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
	cfg    RecorderConfig
	store  Store
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
	ID               string
	SessionUUID      string
	AgentType        string
	TaskType         string
	RequestID        string
	Model            string
	Provider         string
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

// NewRecorder 构造一个 Recorder；store 为 nil 时 Record() 仅打 log（不写库）。
func NewRecorder(cfg RecorderConfig, store Store) *Recorder {
	return &Recorder{cfg: cfg, store: store}
}

// Record 把一次 LLM 调用的快照写库。失败仅 log，不 panic。
func (r *Recorder) Record(in CallInput) {
	if r == nil || !r.cfg.Enabled {
		return
	}
	rec := r.buildRecord(in)
	if r.store == nil {
		log.Printf("[agent_gateway] noop record: req=%s model=%s tokens=%d status=%s",
			rec.RequestID, rec.Model, rec.TotalTokens, rec.Status)
		return
	}
	if err := r.store.Insert(rec); err != nil {
		log.Printf("[agent_gateway] recorder insert failed (req=%s): %v", rec.RequestID, err)
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
		PromptTokens:     in.Usage.PromptTokens,
		CompletionTokens: in.Usage.CompletionTokens,
		TotalTokens:      in.Usage.TotalTokens,
		LatencyMs:        int(in.Latency / time.Millisecond),
		Status:           in.Status,
		ErrorMsg:         errMsg,
		CreatedAt:        time.Now().UTC(),
	}
}

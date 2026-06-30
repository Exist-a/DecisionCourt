package agent_gateway

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStore 是 Recorder 依赖的最小存储接口替身，避免在单测里接 gorm。
type fakeStore struct {
	mu   sync.Mutex
	rows []Record
}

func (f *fakeStore) Insert(r Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, r)
	return nil
}

func newFakeStore() *fakeStore { return &fakeStore{} }

// errStore 模拟 DB 写失败。
type errStore struct{}

func (e *errStore) Insert(_ Record) error { return errors.New("db down") }

// 验证 Recorder.Record 在 DB 失败时不 panic，并保留调用数据供调用方排查。
func TestRecorder_Noop_Disabled(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	r := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, store)
	r.Record(CallInput{
		Trace:    Trace{SessionUUID: "s1", AgentType: "prosecutor", TaskType: "speak", RequestID: "r1"},
		Model:    "deepseek-chat",
		Usage:    Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		Latency:  123 * time.Millisecond,
		Status:   StatusSuccess,
	})
	// 关停后不会调 store.Insert；如调用了，store.Insert 也是 noop；都 OK。
	if got := len(store.rows); got != 0 {
		t.Errorf("expected 0 rows when disabled, got %d", got)
	}
}

func TestRecorder_BuildRecord_Fields(t *testing.T) {
	t.Parallel()
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, nil)
	in := CallInput{
		Trace:   Trace{SessionUUID: "sess", AgentType: "prosecutor", TaskType: "speak", RequestID: "req"},
		Model:   "deepseek-chat",
		Usage:   Usage{PromptTokens: 11, CompletionTokens: 22, TotalTokens: 33},
		Latency: 250 * time.Millisecond,
		Status:  StatusSuccess,
	}
	rec := r.buildRecord(in)
	if rec.SessionUUID != "sess" {
		t.Errorf("SessionUUID: want sess got %q", rec.SessionUUID)
	}
	if rec.AgentType != "prosecutor" {
		t.Errorf("AgentType: want prosecutor got %q", rec.AgentType)
	}
	if rec.TaskType != "speak" {
		t.Errorf("TaskType: want speak got %q", rec.TaskType)
	}
	if rec.RequestID != "req" {
		t.Errorf("RequestID: want req got %q", rec.RequestID)
	}
	if rec.Model != "deepseek-chat" {
		t.Errorf("Model: want deepseek-chat got %q", rec.Model)
	}
	if rec.Provider != "deepseek" {
		t.Errorf("Provider: want deepseek got %q", rec.Provider)
	}
	if rec.PromptTokens != 11 || rec.CompletionTokens != 22 || rec.TotalTokens != 33 {
		t.Errorf("tokens mismatch: p=%d c=%d t=%d", rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens)
	}
	if rec.LatencyMs != 250 {
		t.Errorf("LatencyMs: want 250 got %d", rec.LatencyMs)
	}
	if rec.Status != StatusSuccess {
		t.Errorf("Status: want success got %q", rec.Status)
	}
	if rec.ErrorMsg != "" {
		t.Errorf("ErrorMsg: want empty got %q", rec.ErrorMsg)
	}
	if rec.ID == "" {
		t.Errorf("ID should be auto-generated")
	}
}

func TestRecorder_BuildRecord_TruncatesError(t *testing.T) {
	t.Parallel()
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, nil)
	longMsg := strings.Repeat("a", 1000)
	rec := r.buildRecord(CallInput{
		Trace:  Trace{RequestID: "r"},
		Status: StatusError,
		Err:    errors.New(longMsg),
	})
	if len(rec.ErrorMsg) != MaxErrorMsgLen {
		t.Errorf("ErrorMsg should be truncated to %d, got %d", MaxErrorMsgLen, len(rec.ErrorMsg))
	}
	if rec.Status != StatusError {
		t.Errorf("Status: want error got %q", rec.Status)
	}
}

func TestRecorder_BuildRecord_DefaultsProvider(t *testing.T) {
	t.Parallel()
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: ""}, nil)
	rec := r.buildRecord(CallInput{Status: StatusSuccess})
	if rec.Provider != "unknown" {
		t.Errorf("default provider should be 'unknown', got %q", rec.Provider)
	}
}

func TestRecorder_Record_WritesToStore(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)
	r.Record(CallInput{
		Trace:   Trace{SessionUUID: "s1", AgentType: "judge", TaskType: "assess", RequestID: "r1"},
		Model:   "deepseek-reasoner",
		Usage:   Usage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12},
		Latency: 50 * time.Millisecond,
		Status:  StatusSuccess,
	})
	if got := len(store.rows); got != 1 {
		t.Fatalf("want 1 row, got %d", got)
	}
	got := store.rows[0]
	if got.AgentType != "judge" || got.TaskType != "assess" || got.RequestID != "r1" {
		t.Errorf("trace fields wrong: %+v", got)
	}
	if got.Status != StatusSuccess {
		t.Errorf("status wrong: %q", got.Status)
	}
}

func TestRecorder_Record_SwallowsDBError(t *testing.T) {
	t.Parallel()
	// 用一个会返回 error 的 store，验证 Record 不 panic、不抛 err。
	store := &errStore{}
	r := NewRecorder(RecorderConfig{Enabled: true, Provider: "deepseek"}, store)
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("Record must not panic on db error, got %v", rec)
		}
	}()
	r.Record(CallInput{Status: StatusError, Err: errors.New("boom")})
}

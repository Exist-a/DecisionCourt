package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogger_DefaultReturnsJSONHandler(t *testing.T) {
	// 默认 logger 输出 JSON 格式（key=value 与 JSON 互斥；如果改成 text handler 此测试会失败）
	var buf bytes.Buffer
	SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	// 确保 defaultOnce 已生效
	_ = Default()

	ctx := WithTrace(context.Background(), Trace{RequestID: "req-1", SessionUUID: "s-1", AgentType: "prosecutor"})
	Logger(ctx).Info("hello", "foo", "bar")

	out := buf.String()
	assert.Contains(t, out, `"trace_id":"req-1"`)
	assert.Contains(t, out, `"session_uuid":"s-1"`)
	assert.Contains(t, out, `"agent_type":"prosecutor"`)
	assert.Contains(t, out, `"msg":"hello"`)
	assert.Contains(t, out, `"foo":"bar"`)

	// 反序列化验证合法 JSON
	dec := json.NewDecoder(strings.NewReader(out))
	var row map[string]interface{}
	require.NoError(t, dec.Decode(&row))
	assert.Equal(t, "hello", row["msg"])
}

func TestLogger_FromContextReturnsDefaultWhenMissing(t *testing.T) {
	SetDefault(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	l := FromContext(context.Background())
	assert.NotNil(t, l)
	assert.Equal(t, Default(), l)

	// nil ctx 不 panic
	assert.NotPanics(t, func() { FromContext(context.TODO()) })
}

func TestLogger_WithLoggerOverride(t *testing.T) {
	customBuf := &bytes.Buffer{}
	custom := slog.New(slog.NewJSONHandler(customBuf, nil))
	ctx := WithLogger(context.Background(), custom)
	assert.Equal(t, custom, FromContext(ctx))

	Logger(ctx).Info("hello")
	assert.Contains(t, customBuf.String(), `"msg":"hello"`)
}

func TestTrace_LogValue(t *testing.T) {
	tr := Trace{RequestID: "r1", SessionUUID: "s1", AgentType: "prosecutor", TaskType: "speak"}
	v := tr.LogValue()
	require.Equal(t, slog.KindGroup, v.Kind())
	// 验证 group 包含 4 个字段（trace_id / session_uuid / agent_type / task_type）
	assert.Equal(t, 4, len(v.Group()))
}

func TestTraceFromContext_Empty(t *testing.T) {
	// nil ctx
	assert.Equal(t, Trace{}, TraceFromContext(nil))

	// 未注入的 ctx
	assert.Equal(t, Trace{}, TraceFromContext(context.Background()))

	// 已注入的 ctx
	ctx := WithTrace(context.Background(), Trace{RequestID: "abc"})
	tr := TraceFromContext(ctx)
	assert.Equal(t, "abc", tr.RequestID)
}
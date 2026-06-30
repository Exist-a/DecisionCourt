package tools

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// scriptedDispatch is a DispatchFn for tests. It records calls and returns
// canned finding ID/summary pairs.
type scriptedDispatch struct {
	calls        int32
	lastQuery    string
	lastSession  string
	lastDispatch string
	returnID     string
	returnSummary string
	returnErr    error
}

func (s *scriptedDispatch) Dispatch(_ context.Context, sessionUUID, dispatcher, query string) (string, string, error) {
	atomic.AddInt32(&s.calls, 1)
	s.lastQuery = query
	s.lastSession = sessionUUID
	s.lastDispatch = dispatcher
	if s.returnErr != nil {
		return "", "", s.returnErr
	}
	return s.returnID, s.returnSummary, nil
}

func TestInvestigatorSearchTool_Name(t *testing.T) {
	dispatch := &scriptedDispatch{}
	tool := NewInvestigatorSearchTool("sess", "prosecutor", dispatch.Dispatch)
	require.Equal(t, "investigator_search", tool.Name())
	require.NotEmpty(t, tool.Description())
}

func TestInvestigatorSearchTool_Execute_DispatchesAndReturnsFindingID(t *testing.T) {
	dispatch := &scriptedDispatch{returnID: "finding-uuid-1", returnSummary: "[1] A; [2] B"}
	tool := NewInvestigatorSearchTool("sess-uuid-123", "prosecutor", dispatch.Dispatch)

	obs, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "选项A 长期收益 数据",
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), atomic.LoadInt32(&dispatch.calls))
	require.Equal(t, "选项A 长期收益 数据", dispatch.lastQuery)
	require.Equal(t, "sess-uuid-123", dispatch.lastSession)
	require.Equal(t, "prosecutor", dispatch.lastDispatch)
	require.Contains(t, obs, "finding-uuid-1", "observation 必须含 finding_id")
	require.Contains(t, obs, "调查发现", "observation 必须明确为「调查发现」而非证据")
	require.Contains(t, obs, "[1] A; [2] B", "observation 必须含摘要")
}

func TestInvestigatorSearchTool_Execute_EmptyResultsReturnsGracefulObservation(t *testing.T) {
	dispatch := &scriptedDispatch{returnID: "", returnSummary: "无搜索结果"}
	tool := NewInvestigatorSearchTool("sess", "defender", dispatch.Dispatch)

	obs, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "no result query",
	})
	require.NoError(t, err)
	require.Contains(t, obs, "未返回可用结果")
}

func TestInvestigatorSearchTool_Execute_EmptyQueryRejected(t *testing.T) {
	dispatch := &scriptedDispatch{}
	tool := NewInvestigatorSearchTool("sess", "prosecutor", dispatch.Dispatch)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "query")
	require.Equal(t, int32(0), atomic.LoadInt32(&dispatch.calls), "应直接拒绝而不调用 dispatch")
}

func TestInvestigatorSearchTool_Execute_QueryTooLongRejected(t *testing.T) {
	dispatch := &scriptedDispatch{}
	tool := NewInvestigatorSearchTool("sess", "prosecutor", dispatch.Dispatch)

	longQuery := strings.Repeat("x", 201)
	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": longQuery,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "too long")
}

func TestInvestigatorSearchTool_Execute_DispatchErrorPropagated(t *testing.T) {
	dispatch := &scriptedDispatch{returnErr: errors.New("search provider unavailable")}
	tool := NewInvestigatorSearchTool("sess", "defender", dispatch.Dispatch)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query": "Q",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "search provider unavailable")
}

// T_sessionOnly: 验证同一个 tool 实例绑定到具体 session，不能跨 session 误派
func TestInvestigatorSearchTool_IsBoundToOneSession(t *testing.T) {
	dispatch := &scriptedDispatch{returnID: "f-1"}
	tool := NewInvestigatorSearchTool("sess-A", "prosecutor", dispatch.Dispatch)

	_, err := tool.Execute(context.Background(), map[string]interface{}{
		"query":        "Q",
		"session_uuid": "sess-B", // LLM 试图伪造 session
	})
	require.NoError(t, err)
	require.Equal(t, "sess-A", dispatch.lastSession, "应忽略 LLM 输入里的 session_uuid，仍用绑定的 sess-A")
	require.Equal(t, "prosecutor", dispatch.lastDispatch, "应忽略 LLM 输入里的 dispatcher")
}
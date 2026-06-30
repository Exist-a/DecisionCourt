// Package tools provides agent-callable tools that wrap side-effecting
// operations against the courtroom service. Each tool instance is bound to
// a specific (session, dispatcher) pair at construction time so the LLM
// cannot impersonate the opposing side or cross sessions by manipulating
// its own tool_call input.
package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DispatchFn is the contract the courtroom service must satisfy so the
// investigator_search tool can task the investigator. It returns the new
// InvestigationFinding's UUID and a short summary; the tool surfaces both
// in the Observation that the ReAct runner feeds back into the next LLM
// turn.
//
// Per UX refinement §1, findings are NOT user-submitted evidence: they
// live in a separate table and surface in the dedicated Investigator
// panel rather than the evidence list.
type DispatchFn func(ctx context.Context, sessionUUID, dispatcher, query string) (findingID, summary string, err error)

// InvestigatorSearchToolName is the canonical tool name surfaced to the
// LLM in the system prompt and looked up by the runner.
const InvestigatorSearchToolName = "investigator_search"

// InvestigatorSearchTool dispatches a public web search via the courtroom
// Investigator. The sessionUUID and dispatcher fields are bound at
// construction time and intentionally NOT read from the input map — that
// would let a malicious or hallucinating LLM escalate to the opposing
// side or another session.
type InvestigatorSearchTool struct {
	sessionUUID string
	dispatcher  string
	dispatch    DispatchFn
}

// NewInvestigatorSearchTool binds the tool to one (session, dispatcher)
// pair. The DispatchFn is typically a closure over
// courtroom.Service.DispatchInvestigator.
func NewInvestigatorSearchTool(sessionUUID, dispatcher string, dispatch DispatchFn) *InvestigatorSearchTool {
	return &InvestigatorSearchTool{
		sessionUUID: sessionUUID,
		dispatcher:  dispatcher,
		dispatch:    dispatch,
	}
}

func (t *InvestigatorSearchTool) Name() string {
	return InvestigatorSearchToolName
}

func (t *InvestigatorSearchTool) Description() string {
	return "派遣调查员进行网页搜索，返回新调查发现 (finding_id) + 摘要。调查结果对方律师也能看到，单独显示在「调查活动」面板。"
}

// Execute runs the dispatch. Required input: {"query": "<search string>"};
// query must be non-empty and at most 200 chars. Any extra keys in input
// are ignored.
func (t *InvestigatorSearchTool) Execute(ctx context.Context, input map[string]interface{}) (string, error) {
	query, _ := input["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "", errors.New("query is required")
	}
	if len(query) > 200 {
		return "", fmt.Errorf("query too long (%d chars, max 200)", len(query))
	}

	findingID, summary, err := t.dispatch(ctx, t.sessionUUID, t.dispatcher, query)
	if err != nil {
		return "", fmt.Errorf("dispatch failed: %w", err)
	}
	if findingID == "" {
		return fmt.Sprintf("搜索完成，但未返回可用结果。查询=%q 摘要=%q", query, summary), nil
	}
	return fmt.Sprintf(
		"搜索完成：新增调查发现 finding_id=%s。摘要=%s。查询=%q",
		findingID, summary, query,
	), nil
}
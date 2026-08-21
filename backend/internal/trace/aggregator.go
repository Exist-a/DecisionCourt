package trace

import (
	"sort"
	"time"
)

// Trace 是单个 trace_id 下的全部 Run + 父子关系 tree。
//
// 字段:
//   - TraceID   log RequestID (= HTTP middleware X-Request-ID)
//   - SessionID session UUID
//   - Runs      该 trace 下所有 LLM 调用,按 RetryCount 升序 = LLM 调用顺序
//   - Tree      树状 RunNode,根节点 = RetryCount=0 的首次调用
//   - StartedAt trace 开始时间 (= min Runs.StartedAt)
//   - EndedAt   trace 结束时间 (= max Runs.EndedAt)
//
// 设计要点:
//   - Runs 是"扁平"列表,Tree 是"嵌套"视图,二者冗余但前端两类查询都常用
//   - REST /traces/:trace_id 返回本结构,前端 TrialReplay 直接渲染
//   - 多 trace 聚合 (跨 trace_id) 在 store 层做,本结构只承载单 trace
type Trace struct {
	TraceID   string    `json:"trace_id"`
	SessionID string    `json:"session_id"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Runs      []Run     `json:"runs"`
	Tree      RunNode   `json:"tree"`
}

// RunNode 是 Trace Tree 的节点,递归结构。
//
// 字段:
//   - Run      单次 LLM 调用元数据
//   - Children 子 retry 调用列表
//
// 父子关系算法 (V1.0.4-PLAN.md §1.3):
//   - RetryCount=0 是 root
//   - RetryCount=N (N>0) 是 RetryCount=N-1 的 child (前提:同 trace_id,前一 retry 在同 trace 内)
//
// 例: trace_id="abc123" 有 3 次 LLM 调用,RetryCount 分别为 [0, 1, 2]
//   - Run(RC=0) 是 root
//   - Run(RC=1) 是 Run(RC=0) 的 child
//   - Run(RC=2) 是 Run(RC=1) 的 child (链式)
//
// 多分支情况 (罕见,如 2 次 retry 各自有 retry): 当前实现按"RC=N 是 RC=N-1 的 child"
// 单链处理,多分支场景不出现 (retry 实现是 sequential 不是 tree)。后续如需支持,
// 在 BuildTree 处加 timestamp 分桶。
type RunNode struct {
	Run      Run       `json:"run"`
	Children []RunNode `json:"children,omitempty"`
}

// Aggregate 按 trace_id 列表聚合成 Trace 视图。
//
// 输入:
//   - sessionTrace: parser.go 产出的 SessionTrace (按 trace_id 分组的 Runs)
//   - traceIDs:    要聚合成 Trace 的 trace_id 列表 (REST ?trace_id= 传入)
//
// 输出:
//   - []*Trace    每个 trace_id 一个 Trace,按 StartedAt 升序
//   - 找不到的 trace_id 跳过 (不报错,前端会显示空数据)
//
// 设计要点:
//   - 接受 traceIDs 是为了让 REST 端点支持 "查某个 session 的某几个 trace"
//   - 不传 traceIDs 时 (nil/空),聚合所有 trace (用于 ListBySession 全量返回)
func Aggregate(sessionTrace *SessionTrace, traceIDs []string) []*Trace {
	if sessionTrace == nil || len(sessionTrace.Traces) == 0 {
		return []*Trace{}
	}

	// 决定要聚合哪些 trace_id
	want := make(map[string]bool)
	if len(traceIDs) > 0 {
		for _, id := range traceIDs {
			want[id] = true
		}
	}

	traces := make([]*Trace, 0, len(sessionTrace.Traces))
	for traceID, runs := range sessionTrace.Traces {
		if len(want) > 0 && !want[traceID] {
			continue
		}

		// Runs 已由 parser.go 按 RetryCount 升序排好,这里直接拷贝避免外部修改
		runsCopy := make([]Run, len(runs))
		copy(runsCopy, runs)

		t := &Trace{
			TraceID:   traceID,
			SessionID: sessionTrace.SessionID,
			Runs:      runsCopy,
			Tree:      BuildTree(runsCopy),
		}
		t.StartedAt, t.EndedAt = traceTimeRange(runsCopy)
		traces = append(traces, t)
	}

	// 按 StartedAt 升序,前端时间轴渲染需要
	sort.SliceStable(traces, func(i, j int) bool {
		return traces[i].StartedAt.Before(traces[j].StartedAt)
	})

	return traces
}

// BuildTree 把扁平 Runs 列表构造成 RunNode 树。
//
// 算法 (与 V1.0.4-PLAN.md §1.3 一致):
//   1. 找出 RetryCount=0 的 run 作为 root (假设 1 个;多个时取 StartedAt 最早的)
//   2. 剩余 run 按 RetryCount 链式挂在 RC-1 节点下
//
// 边界:
//   - 没有 RetryCount=0 的 run: 返回 root=空 RunNode (Run 字段零值)
//   - 重复 RetryCount (异常 log): 后到的覆盖前的 (parser.go 排序已保证稳定)
//   - 单 run (RetryCount=0 only): 返回 root 节点,Children 空
//
// 输入 runs 必须已按 RetryCount 升序 (parser.go 已保证)。
func BuildTree(runs []Run) RunNode {
	if len(runs) == 0 {
		return RunNode{}
	}

	// 构造节点 map + 父子关系
	// nodes[run.RunID] = &RunNode{Run: run}
	// children[parentRunID] = []string{childRunID}
	nodes := make(map[string]*RunNode, len(runs))
	childrenOf := make(map[string][]string, len(runs))

	var rootID string
	for i := range runs {
		r := runs[i]
		id := r.RunID
		nodes[id] = &RunNode{Run: r}
		if r.RetryCount == 0 {
			if rootID == "" {
				rootID = id
			}
			// 多个 RC=0: 取首个 (parser.go 排序已稳定)
		} else {
			// 找父节点: RC=N 的父 = RC=N-1
			parentID := parentRunID(r.TraceID, r.RetryCount-1)
			childrenOf[parentID] = append(childrenOf[parentID], id)
		}
	}

	// root 缺失时返回空节点 (异常数据)
	if rootID == "" || nodes[rootID] == nil {
		return RunNode{}
	}

	// 递归挂载 children
	attachChildren(nodes[rootID], nodes, childrenOf)

	return *nodes[rootID]
}

// attachChildren 递归把 children 挂到当前节点上。
//
// DFS 而非 BFS,因为 retry 链是 sequential (RC=0 → RC=1 → RC=2),
// 树深度有限 (实测 max 3),递归不会爆栈。
func attachChildren(node *RunNode, nodes map[string]*RunNode, childrenOf map[string][]string) {
	childIDs := childrenOf[node.Run.RunID]
	if len(childIDs) == 0 {
		return
	}
	node.Children = make([]RunNode, 0, len(childIDs))
	for _, cid := range childIDs {
		child, ok := nodes[cid]
		if !ok {
			continue
		}
		attachChildren(child, nodes, childrenOf)
		node.Children = append(node.Children, *child)
	}
}

// parentRunID 算指定 Run 的父 Run ID。
//
// 父 RunID = TraceID + "-" + (RetryCount-1),与 parser.go entryToRun 构造 RunID 对齐。
func parentRunID(traceID string, retryCount int) string {
	return traceID + "-" + itoa(retryCount)
}

// itoa 是 strconv.Itoa 的局部别名,避免导入 strconv 仅这一处用。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// traceTimeRange 算 Trace 整体时间范围 (min StartedAt, max EndedAt)。
//
// 空 Runs 返回零值,调用方应处理。
func traceTimeRange(runs []Run) (time.Time, time.Time) {
	if len(runs) == 0 {
		return time.Time{}, time.Time{}
	}
	minStart := runs[0].StartedAt
	maxEnd := runs[0].EndedAt
	for _, r := range runs[1:] {
		if r.StartedAt.Before(minStart) {
			minStart = r.StartedAt
		}
		if r.EndedAt.After(maxEnd) {
			maxEnd = r.EndedAt
		}
	}
	return minStart, maxEnd
}

package api

// v2.11 (deferred D12): GET /api/v1/courtrooms/:session_uuid/events
//
// 背景：`decision_events` 一直只有写路径（POST /events，ADR 0020 前端埋点）。
// 简历/文档里说的"跨域查询能力"此前只以 SQL 形式存在于
// `decisioncourt-db-design.md` §9.6，需要人工连库执行 —— 写链路闭环了，
// 读链路没有，能力停在说法上。
//
// 本端点把"读"这一半补上，且刻意只做最小一步：
//   - 只读列表，按 created_at 升序（与 db-design §9.4 查询示例口径一致）
//   - owner-only（复用 checkSessionAccess，跨 session 越权直接 403/404）
//   - 分页 limit/offset，limit 上限 500（防一次拉全表）
//   - event_type_prefix 前缀过滤 —— `fe.` 取前端埋点、`span.` 取后端 span、
//     `state_` 取状态机迁移，与 db-design §9.6 的命名空间约定对齐
//
// 不做（deferred D12 明确边界）：
//   - 不引入 BI / Metabase、不做实时看板
//   - 不加跨 session 的全量分析端点（越权风险 + 非当前需求）
//   - 不在此项里做漏斗聚合（那是可选的第二步）

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/gin-gonic/gin"
)

const (
	// defaultEventPageSize 未传 limit 时返回的条数。
	defaultEventPageSize = 100
	// maxEventPageSize limit 上限，防单次请求拉全表。
	maxEventPageSize = 500
	// maxEventTypePrefixLen 与 model.DecisionEvent.EventType 的 varchar(50) 一致。
	maxEventTypePrefixLen = 50
)

// DecisionEventQuery 是列表查询条件。
//
// Limit 是"最多返回多少行"（handler 传 limit+1 用于判断 has_more）；
// Offset 为跳过行数。事件按 created_at ASC, id ASC 稳定排序。
type DecisionEventQuery struct {
	EventTypePrefix string
	Limit           int
	Offset          int
}

// DecisionEventLister 是 GET /events 的数据源契约。
//
// 生产用 GORM 查 decision_events；测试注入内存 fake，这样 handler 测试
// 不需要 Postgres（与 sessionLookup / MemoryLister 的注入模式一致）。
type DecisionEventLister interface {
	ListDecisionEvents(ctx context.Context, sessionUUID string, q DecisionEventQuery) ([]model.DecisionEvent, error)
}

// WithDecisionEventLister 注入列表数据源。测试用；生产走默认 GORM 实现。
func (h *Handler) WithDecisionEventLister(l DecisionEventLister) {
	h.eventLister = l
}

// gormDecisionEventLister 用全局 model.DB 查询。
//
// 在调用时读 model.DB（而不是构造时捕获），与 defaultSessionLookup 一致：
// 装配顺序（model.Connect 在 NewHandler 之前）不再是隐式约束。
type gormDecisionEventLister struct{}

func (gormDecisionEventLister) ListDecisionEvents(ctx context.Context, sessionUUID string, q DecisionEventQuery) ([]model.DecisionEvent, error) {
	if model.DB == nil {
		return nil, nil
	}
	tx := model.DB.WithContext(ctx).
		Model(&model.DecisionEvent{}).
		Where("session_uuid = ?", sessionUUID)
	if q.EventTypePrefix != "" {
		// 参数化查询（无注入风险）；ESCAPE 只防前缀里的 % / _ 被当通配符用。
		tx = tx.Where(`event_type LIKE ? ESCAPE '\'`, escapeLikePrefix(q.EventTypePrefix)+"%")
	}
	var rows []model.DecisionEvent
	if err := tx.Order("created_at ASC, id ASC").
		Limit(q.Limit).
		Offset(q.Offset).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// escapeLikePrefix 转义 LIKE 前缀里的通配符，让 "fe.%" 只匹配字面 "fe."。
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ListDecisionEvents 处理 GET /api/v1/courtrooms/:session_uuid/events
//
// Query params:
//   - limit (可选): 1-500，默认 100
//   - offset (可选): >= 0，默认 0
//   - event_type_prefix (可选): 事件类型前缀，最多 50 字符。
//     常用值 "fe."(前端埋点) / "span."(后端业务 span) / "state_"(状态机迁移)
//
// 响应 200：
//
//	{"code":0,"data":{"events":[...],"count":N,"limit":100,"offset":0,"has_more":false}}
//
// 错误：
//   - 400 参数非法（limit/offset 非整数或越界、prefix 过长）
//   - 403 不是 session owner
//   - 404 session 不存在
//   - 500 查询失败
func (h *Handler) ListDecisionEvents(c *gin.Context) {
	sessionUUID := c.Param("session_uuid")
	if _, ok := h.checkSessionAccess(c, sessionUUID); !ok {
		return
	}

	limit := defaultEventPageSize
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxEventPageSize {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1001,
				"message": fmt.Sprintf("limit 必须是 1-%d 之间的整数", maxEventPageSize),
			})
			return
		}
		limit = n
	}

	offset := 0
	if v := c.Query("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    1001,
				"message": "offset 必须是非负整数",
			})
			return
		}
		offset = n
	}

	prefix := c.Query("event_type_prefix")
	if len(prefix) > maxEventTypePrefixLen {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    1001,
			"message": fmt.Sprintf("event_type_prefix 最长 %d 字符", maxEventTypePrefixLen),
		})
		return
	}

	// lister 未注入（老部署 / 精简测试）→ 空列表，不报错。
	if h.eventLister == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
			"events":   []gin.H{},
			"count":    0,
			"limit":    limit,
			"offset":   offset,
			"has_more": false,
		}})
		return
	}

	// 多取 1 行判断 has_more，避免额外一次 COUNT（前缀 + 分页下 COUNT 语义也模糊）。
	rows, err := h.eventLister.ListDecisionEvents(c.Request.Context(), sessionUUID, DecisionEventQuery{
		EventTypePrefix: prefix,
		Limit:           limit + 1,
		Offset:          offset,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1500, "message": "查询事件失败"})
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]gin.H, 0, len(rows))
	for _, e := range rows {
		items = append(items, decisionEventResponse(e))
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
		"events":   items,
		"count":    len(items),
		"limit":    limit,
		"offset":   offset,
		"has_more": hasMore,
	}})
}

// decisionEventResponse 把一行 decision_events 转成 API JSON。
//
// payload 是 JSONB 原文，这里解析成对象返回；坏数据（非 JSON）降级为空对象，
// 不为一条脏行让整个列表 500。
func decisionEventResponse(e model.DecisionEvent) gin.H {
	var payload interface{} = gin.H{}
	if e.Payload != "" {
		var p map[string]interface{}
		if err := json.Unmarshal([]byte(e.Payload), &p); err == nil && p != nil {
			payload = p
		}
	}
	return gin.H{
		"id":           e.ID,
		"session_uuid": e.SessionUUID,
		"request_id":   e.RequestID,
		"event_type":   e.EventType,
		"agent_type":   e.AgentType,
		"payload":      payload,
		"duration_ms":  e.DurationMs,
		"status":       e.Status,
		"error_msg":    e.ErrorMsg,
		"created_at":   e.CreatedAt,
	}
}

// 编译期断言：默认实现满足接口
var _ DecisionEventLister = gormDecisionEventLister{}

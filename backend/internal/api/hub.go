package api

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/decisioncourt/backend/internal/observability"
	"github.com/gorilla/websocket"
)

// minChunkSpacing 强制 Broadcast 之间至少间隔这么多时间。Gorilla/websocket
// 的 conn.WriteMessage 写入 OS TCP buffer 后 Nagle 算法会把多个小 frame 合并
// 成一个 TCP send,导致浏览器一次性收到 ~100 个 frame(LLM 流式 chunks 在
// ~50ms 内全部到达)。SetNoDelay 仅控制 server→client 单向 Nagle,但无法阻止
// browser 端 socket 接收缓冲 batching。强制 sleep 是最稳健的方案:
// - 每个 WebSocket frame 在 OS TCP buffer 里停留 < 30ms
// - browser WebSocket onmessage 间隔 ≈ 30ms,前端 React 能渲染出打字机效果
// - 不影响普通广播(普通广播按 event 间隔本来就 > 30ms)
const minChunkSpacing = 30 * time.Millisecond

// client wraps a single WebSocket connection with a write-lock so
// concurrent Broadcast callers (e.g. ReAct streaming + evidence-triggered
// belief.diff / belief.updated events) serialize their WriteMessage calls.
// gorilla/websocket's *Conn is NOT safe for concurrent writes — without this
// the runtime panics with "concurrent write to websocket connection".
type client struct {
	conn *websocket.Conn
	wmu  sync.Mutex
}

// Hub manages WebSocket connections grouped by session_uuid.
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[*client]bool
	// v0.8 白盒化：可选注入 metrics，统计 A2A 事件吞吐量。
	// nil 时 noop（保持向后兼容，便于不依赖 observability 的测试）。
	metrics observability.Metrics
}

func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string]map[*client]bool),
	}
}

// WithMetrics 注入 metrics，供 Broadcast 统计 A2A 事件吞吐量。
func (h *Hub) WithMetrics(m observability.Metrics) {
	h.metrics = m
}

func (h *Hub) Join(sessionUUID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[sessionUUID] == nil {
		h.rooms[sessionUUID] = make(map[*client]bool)
	}
	h.rooms[sessionUUID][&client{conn: conn}] = true
}

func (h *Hub) Leave(sessionUUID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	clients, ok := h.rooms[sessionUUID]
	if !ok {
		return
	}
	for c := range clients {
		if c.conn == conn {
			delete(clients, c)
			break
		}
	}
	if len(clients) == 0 {
		delete(h.rooms, sessionUUID)
	}
}

func (h *Hub) Broadcast(sessionUUID string, event courtroom.Event) {
	// v0.8.1 白盒化修复（2026-07-02 demo 发现）：A2A 事件吞吐量统计必须在
	// 订阅检查之前执行 —— 否则没人订阅 WS 时 IncCounter 永远不会执行，
	// 导致"业务上 A2A 真的发了"但"metrics 显示 0"，失去可观测性价值。
	if h.metrics != nil {
		h.metrics.IncCounter(observability.MetricA2AThroughputTotal, map[string]string{
			"event_type": event.Type,
		})
	}

	h.mu.RLock()
	clients, ok := h.rooms[sessionUUID]
	h.mu.RUnlock()

	if !ok {
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		slog.Warn("hub: marshal event failed", "session_uuid", sessionUUID, "event_type", event.Type, "error", err)
		return
	}

	for c := range clients {
		// 串行化每个 conn 的写操作,避免 "concurrent write to websocket"。
		c.wmu.Lock()
		writeErr := c.conn.WriteMessage(websocket.TextMessage, data)
		c.wmu.Unlock()

		if writeErr != nil {
			// Remove broken connection.
			h.Leave(sessionUUID, c.conn)
			c.conn.Close()
			continue
		}
		// 流式场景下强制 sleep,让 OS TCP buffer 每 ~30ms flush 一次。
		// 这样浏览器 WebSocket onmessage 间隔 ≈ 30ms,前端能渲染出
		// 真正的"打字机"效果。如果 event 不需要流式(普通广播),sleep
		// 也会带来一点延迟(可忽略,因为普通广播 event 之间间隔本来就 > 30ms)。
		//
		// 如果生产环境需要更精细控制,可以改为:仅在 event 类型为
		// agent.speak_chunk 时 sleep,其他 event 不 sleep。
		time.Sleep(minChunkSpacing)
	}
}

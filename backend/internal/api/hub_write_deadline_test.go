package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/gorilla/websocket"
)

// TestHub_Broadcast_SlowClientDoesNotBlockForever 是「庭审静默停滞」的回归钉
// （docs/STAGE-LANDING-2026-10-12.md §4）。
//
// 根因：Broadcast 此前对每个客户端 `conn.WriteMessage` **从不设置写 deadline** ——
// 客户端半死（关页残留 / 网络中断 / 停止读取）时，TCP 发送缓冲填满后写会
// **无限阻塞**；而 Broadcast 同步发生在持 session 锁的 runCrossExamRound /
// 流式发言路径上 → 整场庭审静默卡死（后端零日志、前端一直"进行中"）。
//
// 修复：每次写前 `SetWriteDeadline(5s)`，超时/报错 → WARN + 踢掉该客户端。
// 本测试让一个客户端**连接后不读**、并灌入远超 socket 缓冲的数据量
// （旧实现必然卡死在此），断言 Broadcast 在有限时间内返回。
func TestHub_Broadcast_SlowClientDoesNotBlockForever(t *testing.T) {
	h := NewHub()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		h.Join("slow-session", conn)
		// 服务端也**不读**客户端数据（不影响本测试方向）。
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/"
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientConn.Close()
	// 故意**不启动读 goroutine** —— 让服务端发送缓冲填满，逼出"写阻塞"。

	time.Sleep(300 * time.Millisecond)

	// 48 × 1MB ≈ 48MB，远超任何环回 socket 缓冲 → 旧实现必在某个 WriteMessage 上无限阻塞。
	payload := strings.Repeat("x", 1024*1024)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 48; i++ {
			h.Broadcast("slow-session", courtroom.Event{
				Type: "agent.speak_chunk",
				Payload: map[string]interface{}{
					"i":    i,
					"bulk": payload,
				},
			})
		}
	}()

	select {
	case <-done:
		// Broadcast 在有限时间内返回 —— 核心属性。
	case <-time.After(30 * time.Second):
		t.Fatal("Broadcast 超过 30s 未返回 —— 慢客户端把广播（进而持锁的整轮 goroutine）无限阻塞了")
	}

	// 缓冲耗尽后该客户端应已被踢掉（房间清空），后续广播不再受它影响。
	h.mu.RLock()
	remaining := len(h.rooms["slow-session"])
	h.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("写超时的客户端应被移出房间，仍剩 %d 个", remaining)
	}
}

// TestHub_Broadcast_HealthyClientStillReceivesWhileSlowClientDropped：
// 隔离性 —— 一个慢客户端被踢掉时，同房间的正常客户端必须继续收到事件。
func TestHub_Broadcast_HealthyClientStillReceivesWhileSlowClientDropped(t *testing.T) {
	h := NewHub()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		h.Join("iso-session", conn)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/"

	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return c
	}
	healthy := dial()
	defer healthy.Close()
	// 故意让第二个客户端连上后立刻进入"不读"状态（缓冲很快填满）。
	slow := dial()
	defer slow.Close()

	time.Sleep(300 * time.Millisecond)

	var mu sync.Mutex
	got := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if _, _, err := healthy.ReadMessage(); err != nil {
				return
			}
			mu.Lock()
			got++
			mu.Unlock()
			if got >= 3 {
				return
			}
		}
	}()

	for i := 0; i < 3; i++ {
		h.Broadcast("iso-session", courtroom.Event{
			Type:    "test.event",
			Payload: map[string]interface{}{"i": i, "bulk": strings.Repeat("y", 1024*1024)},
		})
	}

	wgCh := make(chan struct{})
	go func() { wg.Wait(); close(wgCh) }()
	select {
	case <-wgCh:
	case <-time.After(30 * time.Second):
		t.Fatalf("健康客户端 %ds 内只收到 %d 条事件 —— 慢客户端拖死了同房间广播", 30, got)
	}
}

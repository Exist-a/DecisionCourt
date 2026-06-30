package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/courtroom"
	"github.com/gorilla/websocket"
)

// TestHub_Broadcast_StreamTimingGap 验证真实 WebSocket 客户端从
// Hub.Broadcast 收到 N 个 agent.speak_chunk 事件时,事件之间的时间间隔
// 是否 ≥ Broadcast 内部的 sleep(30ms)。
//
// 这是一个端到端时序测试,模拟 LLM 流式场景:服务端在 ~50ms 内连续
// 发出 175 个 chunks,客户端应该收到 ~175 个独立 WebSocket 帧而不是
// 被 Nagle / TCP buffer 合并成几个 batch。
//
// 如果 avg gap < 20ms,说明 chunks 仍然被 batching,客户端看到的是
// "闪一下"而非"逐字"。
func TestHub_Broadcast_StreamTimingGap(t *testing.T) {
	h := NewHub()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade err: %v", err)
			return
		}
		defer conn.Close()
		h.Join("test-session", conn)
		defer h.Leave("test-session", conn)
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

	// 给 server-side Join 一点时间
	time.Sleep(100 * time.Millisecond)

	const numEvents = 50 // 用 50 而不是 175 让测试快速
	clientStart := time.Now()
	var (
		mu         sync.Mutex
		recvTimes  []time.Time
		recvEvents []string
	)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			_, data, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			var ev struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(data, &ev); err != nil {
				continue
			}
			if ev.Type == "agent.speak_chunk" {
				mu.Lock()
				recvTimes = append(recvTimes, time.Now())
				recvEvents = append(recvEvents, ev.Type)
				mu.Unlock()
				if len(recvTimes) >= numEvents {
					return
				}
			}
		}
	}()

	// 服务端模拟"LLM 流式"场景:连续 50 个 chunk 几乎同时到达 Broadcast
	broadcastStart := time.Now()
	for i := 0; i < numEvents; i++ {
		h.Broadcast("test-session", courtroom.Event{
			Type: "agent.speak_chunk",
			Payload: map[string]interface{}{
				"i":           i,
				"accumulated": "chunk-" + string(rune('a'+i%26)),
			},
		})
	}
	broadcastDuration := time.Since(broadcastStart)
	t.Logf("Broadcast 用了 %v 平均每事件 %v (含 Broadcast 内部 sleep 30ms × %d)",
		broadcastDuration, broadcastDuration/time.Duration(numEvents), numEvents)

	wg.Wait()
	totalClientDuration := time.Since(clientStart)

	if len(recvTimes) < numEvents {
		t.Fatalf("期望收到 %d 个事件,实际收到 %d", numEvents, len(recvTimes))
	}

	// 计算客户端相邻事件间隔分布
	var gaps []time.Duration
	for i := 1; i < len(recvTimes); i++ {
		gaps = append(gaps, recvTimes[i].Sub(recvTimes[i-1]))
	}
	var sumGaps time.Duration
	var minGap, maxGap time.Duration = gaps[0], gaps[0]
	for _, g := range gaps {
		sumGaps += g
		if g < minGap {
			minGap = g
		}
		if g > maxGap {
			maxGap = g
		}
	}
	avgGap := sumGaps / time.Duration(len(gaps))
	t.Logf("客户端共收到 %d 个 agent.speak_chunk 事件,耗时 %v", len(recvTimes), totalClientDuration)
	t.Logf("客户端相邻事件间隔: avg=%v min=%v max=%v", avgGap, minGap, maxGap)

	// 判定:如果 avg gap < 20ms,说明 chunks 在传输链路某处被 batching
	// (Nagle / TCP buffer / ws frame merge),客户端仍然只能看到"闪一下"。
	if avgGap < 20*time.Millisecond {
		t.Errorf("❌ 流式失败:相邻事件 avg gap = %v < 20ms,说明 chunks 被 batching,客户端看不到逐字效果", avgGap)
	} else {
		t.Logf("✅ 流式 OK:avg gap = %v ≥ 20ms,客户端能看到逐字打字", avgGap)
	}
}
package courtroom

import (
	"sync"
	"testing"
	"time"
)

func newLockWatchdogService() *Service {
	return &Service{sessionLocks: make(map[string]*sync.Mutex)}
}

// TestLockSession_FastPathReturnsSameMutex：空闲锁直接拿到，且与 getSessionLock
// 返回同一把（保证调用方的 Unlock 解的是同一把锁）。
func TestLockSession_FastPathReturnsSameMutex(t *testing.T) {
	s := newLockWatchdogService()
	got := s.lockSession("s-fast", "test")
	want := s.getSessionLock("s-fast")
	if got != want {
		t.Fatalf("lockSession 应返回与 getSessionLock 相同的 mutex")
	}
	got.Unlock()
}

// TestLockSession_WatchdogWaitsThenAcquires：锁被占时必须**阻塞等待**（语义与
// lock.Lock() 相同），持有方释放后拿到同一把锁。
func TestLockSession_WatchdogWaitsThenAcquires(t *testing.T) {
	s := newLockWatchdogService()
	holder := s.getSessionLock("s-wait")
	holder.Lock()

	go func() {
		time.Sleep(600 * time.Millisecond)
		holder.Unlock()
	}()

	start := time.Now()
	got := s.lockSession("s-wait", "test")
	waited := time.Since(start)

	if got != holder {
		t.Fatalf("应拿到同一把锁")
	}
	if waited < 500*time.Millisecond {
		t.Errorf("应等待持有方释放（≥500ms），实际 %v", waited)
	}
	if waited > 3*time.Second {
		t.Errorf("等待不应异常拉长，实际 %v", waited)
	}
	got.Unlock()
}

// TestLockSession_ConcurrentCallersAllAcquireEventually：多个等待者最终都能
// 进入临界区（不丢、不饿死到测试超时）。
func TestLockSession_ConcurrentCallersAllAcquireEventually(t *testing.T) {
	s := newLockWatchdogService()
	const n = 8
	var entered sync.WaitGroup
	entered.Add(n)
	var inside int
	var mu sync.Mutex

	for i := 0; i < n; i++ {
		go func() {
			defer entered.Done()
			lock := s.lockSession("s-conc", "test")
			mu.Lock()
			inside++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			inside--
			mu.Unlock()
			lock.Unlock()
		}()
	}
	done := make(chan struct{})
	go func() { entered.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("10s 内未全部拿到锁")
	}
}

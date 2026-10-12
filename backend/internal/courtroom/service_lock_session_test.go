package courtroom

import (
	"sync"
	"testing"
	"time"

	"github.com/decisioncourt/backend/internal/model"
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

// TestResumeCrossExam_NoSelfDeadlock 是 2026-10-12 soak 复测发现的启动恢复
// 自死锁的回归钉:resumeCrossExam 此前持锁调用 runCrossExamRound(内部再取
// 同一把不可重入的 sync.Mutex)→ 自己锁死自己,recovery goroutine 永不返回、
// 占满恢复并发闸(默认 5)、"recovery: all done" 永不打印。
//
// 修复后契约:resumeCrossExam 在调 runCrossExamRound 前必须释放锁
// (runCrossExamRound 自己加锁,与 ProcessUserAction.continue_cross_exam
// 的注释同一条纪律)。nopLLM 下 runCrossExamRound 会因发言失败快速返回
// error —— error 本身不算失败,**必须返回**才是断言重点。
func TestResumeCrossExam_NoSelfDeadlock(t *testing.T) {
	svc, db, _, _ := newLockTestService(t)
	session := seedSessionForLock(t, db, model.PhaseCrossExam, 1)

	done := make(chan error, 1)
	go func() { done <- svc.resumeCrossExam(session) }()
	select {
	case err := <-done:
		t.Logf("resumeCrossExam 返回(允许 error): %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("resumeCrossExam 10s 未返回 = 持锁自调 runCrossExamRound 的重入死锁回归")
	}

	// 死锁的旁证:若上一 goroutine 卡死,session 锁仍被持有 → TryLock 应失败;
	// 修复后锁已释放,TryLock 必须成功。
	lock := svc.getSessionLock(session.SessionUUID)
	if !lock.TryLock() {
		t.Fatal("resumeCrossExam 返回后 session 锁仍被持有(锁泄漏)")
	}
	lock.Unlock()
}

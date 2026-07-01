package agent_gateway

import (
	"context"
	"testing"
	"time"
)

// TestMemStore_MultiDimAddAndCheck: 多维计数 input / output / total。
func TestMemStore_MultiDimAddAndCheck(t *testing.T) {
	store := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	if err := store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 200, OutputTokens: 100}); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	snap, err := store.Check(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if snap.InputTokens != 200 {
		t.Errorf("InputTokens: want 200 got %d", snap.InputTokens)
	}
	if snap.OutputTokens != 100 {
		t.Errorf("OutputTokens: want 100 got %d", snap.OutputTokens)
	}
	if snap.TotalTokens != 300 {
		t.Errorf("TotalTokens: want 300 got %d", snap.TotalTokens)
	}
}

// TestMemStore_WorstOfTotalAndSliding: sliding 5min 触发更高档；总档次以二者中较严者为准。
func TestMemStore_WorstOfTotalAndSliding(t *testing.T) {
	store := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	// 添加 850 tokens（throttle 档阈值）
	_ = store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 850})
	snap, _ := store.Check(context.Background(), "s1")
	if snap.Status != StatusThrottle {
		t.Errorf("Status: want %s got %s", StatusThrottle, snap.Status)
	}
}

// TestMemStore_WarningLevel_FireOnUpgrade: 仅在等级"升级"时才返回非空 WarningLevel。
func TestMemStore_WarningLevel_FireOnUpgrade(t *testing.T) {
	store := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	_ = store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 730})
	snap, _ := store.Check(context.Background(), "s1")
	// 730/1000 = 0.73 → compress → warning_70
	if snap.WarningLevel != WarningLevel70 {
		t.Errorf("730/1000=0.73 compress: want %s got %q", WarningLevel70, snap.WarningLevel)
	}
	// 第二次 check 同样状态 → WarningLevel 应为空（避免重复触发）
	snap2, _ := store.Check(context.Background(), "s1")
	if snap2.WarningLevel != "" {
		t.Errorf("second check should not re-emit warning: got %q", snap2.WarningLevel)
	}
	// 升级到 850 → 0.85 → throttle → warning_80
	_ = store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 120})
	snap3, _ := store.Check(context.Background(), "s1")
	if snap3.WarningLevel != WarningLevel80 {
		t.Errorf("upgrade to throttle: want %s got %q", WarningLevel80, snap3.WarningLevel)
	}
	// 升级到 1100 → 1.1 → exhausted
	_ = store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 250})
	snap4, _ := store.Check(context.Background(), "s1")
	if snap4.WarningLevel != WarningLevelExhausted {
		t.Errorf("upgrade to exhausted: want %s got %q", WarningLevelExhausted, snap4.WarningLevel)
	}
}

// TestMemStore_Reset: Reset 清空 + WarningLevel 复位（下次 Check 重新升级）。
func TestMemStore_Reset(t *testing.T) {
	store := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	_ = store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 730})
	_, _ = store.Check(context.Background(), "s1")
	if err := store.Reset(context.Background(), "s1"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	snap, _ := store.Check(context.Background(), "s1")
	if snap.TotalTokens != 0 {
		t.Errorf("after reset TotalTokens: want 0 got %d", snap.TotalTokens)
	}
	if snap.WarningLevel != "" {
		t.Errorf("after reset WarningLevel should be empty: %q", snap.WarningLevel)
	}
}

// TestMemStore_SlidingWindowExcludesOld: 旧条目会随时间被剔除。
func TestMemStore_SlidingWindowExcludesOld(t *testing.T) {
	store := NewMemStore(1000, 0, 0.7, 0.8, 100*time.Millisecond)
	_ = store.AddUsage(context.Background(), "s1", BudgetUsage{InputTokens: 500})
	time.Sleep(120 * time.Millisecond)
	// 100ms 之后 sliding 窗口过期
	snap, _ := store.Check(context.Background(), "s1")
	if snap.SlidingTokens != 0 {
		t.Errorf("sliding should be 0 after window: got %d", snap.SlidingTokens)
	}
	// total 没变（不清数据）
	if snap.TotalTokens != 500 {
		t.Errorf("TotalTokens should stay 500: got %d", snap.TotalTokens)
	}
	// 但因 sliding 已小于 compressRatio，状态回到 normal
	if snap.Status != StatusNormal {
		t.Errorf("Status after sliding expiry: want %s got %s", StatusNormal, snap.Status)
	}
}

// TestMemStore_EmptySession: 无记录时返回 normal 零值。
func TestMemStore_EmptySession(t *testing.T) {
	store := NewMemStore(1000, 0, 0.7, 0.8, 5*time.Minute)
	snap, _ := store.Check(context.Background(), "new-session")
	if snap.Status != StatusNormal {
		t.Errorf("empty session status: want %s got %s", StatusNormal, snap.Status)
	}
	if snap.TotalTokens != 0 {
		t.Errorf("TotalTokens: want 0 got %d", snap.TotalTokens)
	}
}

// TestMemStore_Defaults: 不合规入参使用默认。
func TestMemStore_Defaults(t *testing.T) {
	store := NewMemStore(0, -1, 0, 0, 0)
	if store.limitTotal != 20000 {
		t.Errorf("limitTotal default: want 20000 got %d", store.limitTotal)
	}
	if store.compressR != 0.7 {
		t.Errorf("compressR default: want 0.7 got %f", store.compressR)
	}
	if store.throttleR != 0.8 {
		t.Errorf("throttleR default: want 0.8 got %f", store.throttleR)
	}
	if store.slidingWin != 5*time.Minute {
		t.Errorf("sliding default: want 5m got %v", store.slidingWin)
	}
}

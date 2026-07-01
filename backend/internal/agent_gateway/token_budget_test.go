package agent_gateway

import (
	"context"
	"testing"
)

func TestTokenBudget_Normal(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	tb.RecordUsage(context.Background(), "s1", 100)
	bs := tb.Check(context.Background(), "s1")
	if bs.Status != StatusNormal {
		t.Errorf("status: want %s got %s", StatusNormal, bs.Status)
	}
	if bs.Ratio != 0.1 {
		t.Errorf("ratio: want 0.1 got %.3f", bs.Ratio)
	}
}

func TestTokenBudget_Compress(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	tb.RecordUsage(context.Background(), "s1", 720)
	bs := tb.Check(context.Background(), "s1")
	if bs.Status != StatusCompress {
		t.Errorf("status: want %s got %s", StatusCompress, bs.Status)
	}
	if bs.Used != 720 || bs.Total != 1000 {
		t.Errorf("used/total: want 720/1000 got %d/%d", bs.Used, bs.Total)
	}
}

func TestTokenBudget_Throttle(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	tb.RecordUsage(context.Background(), "s1", 850)
	bs := tb.Check(context.Background(), "s1")
	if bs.Status != StatusThrottle {
		t.Errorf("status: want %s got %s", StatusThrottle, bs.Status)
	}
}

func TestTokenBudget_Exhausted(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	tb.RecordUsage(context.Background(), "s1", 1000)
	bs := tb.Check(context.Background(), "s1")
	if bs.Status != StatusExhausted {
		t.Errorf("status: want %s got %s", StatusExhausted, bs.Status)
	}
}

func TestTokenBudget_OverUsage(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	tb.RecordUsage(context.Background(), "s1", 1500)
	bs := tb.Check(context.Background(), "s1")
	if bs.Status != StatusExhausted {
		t.Errorf("status: want %s got %s", StatusExhausted, bs.Status)
	}
	if bs.Ratio != 1.5 {
		t.Errorf("ratio: want 1.5 got %.3f", bs.Ratio)
	}
}

func TestTokenBudget_IsolatedBySession(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	tb.RecordUsage(context.Background(), "s1", 900)
	tb.RecordUsage(context.Background(), "s2", 100)
	if tb.Check(context.Background(), "s1").Status != StatusThrottle {
		t.Errorf("s1 should throttle")
	}
	if tb.Check(context.Background(), "s2").Status != StatusNormal {
		t.Errorf("s2 should normal")
	}
}

func TestTokenBudget_EmptySession(t *testing.T) {
	tb := NewTokenBudget(1000, 0.7, 0.8)
	bs := tb.Check(context.Background(), "new")
	if bs.Status != StatusNormal {
		t.Errorf("new session status: want %s got %s", StatusNormal, bs.Status)
	}
	if bs.Used != 0 {
		t.Errorf("used: want 0 got %d", bs.Used)
	}
}

func TestTokenBudget_DefaultThresholds(t *testing.T) {
	tb := NewTokenBudget(0, 0, 0)
	if tb == nil || tb.store == nil {
		t.Fatalf("tb.store must not be nil after construction")
	}
	mem, ok := tb.store.(*MemStore)
	if !ok {
		t.Fatalf("default store should be *MemStore, got %T", tb.store)
	}
	if mem.limitTotal != 20000 {
		t.Errorf("default limit: want 20000 got %d", mem.limitTotal)
	}
	if mem.compressR != 0.7 {
		t.Errorf("default compress ratio: want 0.7 got %.2f", mem.compressR)
	}
	if mem.throttleR != 0.8 {
		t.Errorf("default throttle ratio: want 0.8 got %.2f", mem.throttleR)
	}
}

package agent_gateway

import "testing"

// TestGreedyPack_ScoreDesc: 高分组优先（用 Compress 状态保留更多组）。
// 3 个等长 100B 的组，target=210B（0.7*300），strict 算法恰好能装下 2 组（200B ≤ 210）。
func TestGreedyPack_ScoreDesc(t *testing.T) {
	groups := []AtomicGroup{
		{ID: "low", GroupScore: 1.0, GroupLength: 100, Indices: []int{0}},
		{ID: "high", GroupScore: 3.0, GroupLength: 100, Indices: []int{1}},
		{ID: "mid", GroupScore: 2.0, GroupLength: 100, Indices: []int{2}},
	}
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusCompress})
	if keptCount != 2 {
		t.Fatalf("compress should fit 2 groups strictly (target 210B, 3rd would exceed), got %d", keptCount)
	}
	if len(keep) != 2 {
		t.Fatalf("len(keep) mismatch: want 2 got %d", len(keep))
	}
	// 高分先：高 (idx 1) → 中 (idx 2)；低分(idx 0) 被抛
	if keep[0] != 1 {
		t.Errorf("first kept should be idx=1 (high score), got %d", keep[0])
	}
	if keep[1] != 2 {
		t.Errorf("second kept should be idx=2 (mid), got %d", keep[1])
	}
}

// TestGreedyPack_ExhaustedAggressive: exhausted 状态只保留 20% 字节。
func TestGreedyPack_ExhaustedAggressive(t *testing.T) {
	groups := []AtomicGroup{
		{GroupScore: 1.0, GroupLength: 100, Indices: []int{0}},
		{GroupScore: 1.0, GroupLength: 100, Indices: []int{1}},
		{GroupScore: 1.0, GroupLength: 100, Indices: []int{2}},
		{GroupScore: 1.0, GroupLength: 100, Indices: []int{3}},
		{GroupScore: 1.0, GroupLength: 100, Indices: []int{4}},
	}
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusExhausted})
	// Total = 500. 20% = 100. 至少保留 1 个组。
	if keptCount != 1 {
		t.Errorf("exhausted should keep only 1 group, got %d (keep=%v)", keptCount, keep)
	}
}

// TestGreedyPack_CompressSoft: compress 保留 70%。
func TestGreedyPack_CompressSoft(t *testing.T) {
	groups := make([]AtomicGroup, 5)
	for i := range groups {
		groups[i] = AtomicGroup{GroupScore: 1.0, GroupLength: 100, Indices: []int{i}}
	}
	_, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusCompress})
	// Total = 500. 70% = 350. 累计到 350 能装 3 个组。
	if keptCount != 3 {
		t.Errorf("compress should keep ~3 groups, got %d", keptCount)
	}
}

// TestGreedyPack_KeepAtLeastOne: empty content → 全保留；按比例切但至少 1。
func TestGreedyPack_KeepAtLeastOne(t *testing.T) {
	groups := []AtomicGroup{
		{GroupScore: 1.0, GroupLength: 0, Indices: []int{0}},
	}
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusExhausted})
	if keptCount != 1 || len(keep) != 1 {
		t.Errorf("empty content should keep 1 group anyway, got keptCount=%d keep=%v", keptCount, keep)
	}
}

// TestKeepRatioForStatus: 映射表正确。
func TestKeepRatioForStatus(t *testing.T) {
	cases := map[string]float64{
		StatusNormal:    1.0,
		StatusCompress:  0.7,
		StatusThrottle:  0.4,
		StatusExhausted: 0.2,
	}
	for status, want := range cases {
		if got := keepRatioForStatus(status); got != want {
			t.Errorf("%s: want %f got %f", status, want, got)
		}
	}
}

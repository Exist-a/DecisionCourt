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
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusCompress}, 0)
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
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusExhausted}, 0)
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
	_, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusCompress}, 0)
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
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusExhausted}, 0)
	if keptCount != 1 || len(keep) != 1 {
		t.Errorf("empty content should keep 1 group anyway, got keptCount=%d keep=%v", keptCount, keep)
	}
}

// v2.10 ADR 0044 #2: ScoreThreshold 过滤——低于阈值的组被排除。
// 用 StatusNormal（keepRatio 1.0）隔离 ratio 影响，只考察阈值行为。
func TestGreedyPack_ScoreThreshold(t *testing.T) {
	groups := []AtomicGroup{
		{ID: "high", GroupScore: 2.0, GroupLength: 100, Indices: []int{0}},
		{ID: "low", GroupScore: 0.1, GroupLength: 100, Indices: []int{1}},
		{ID: "mid", GroupScore: 1.0, GroupLength: 100, Indices: []int{2}},
	}
	// 阈值 0.5 → low(0.1) 被排除，只剩 high + mid（Normal 不强加 ratio 限制）
	keep, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusNormal}, 0.5)
	if keptCount != 2 {
		t.Fatalf("threshold 0.5 should exclude low-score group, want 2 groups got %d", keptCount)
	}
	for _, idx := range keep {
		if idx == 1 {
			t.Error("low-score group (idx 1) should be filtered out by threshold")
		}
	}
}

// v2.10 ADR 0044 #2: 同组数据在阈值为 0 时全部保留，对照组。
func TestGreedyPack_ScoreThresholdUnfilteredBaseline(t *testing.T) {
	groups := []AtomicGroup{
		{ID: "high", GroupScore: 2.0, GroupLength: 100, Indices: []int{0}},
		{ID: "low", GroupScore: 0.1, GroupLength: 100, Indices: []int{1}},
		{ID: "mid", GroupScore: 1.0, GroupLength: 100, Indices: []int{2}},
	}
	_, keptCount, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusNormal}, 0)
	if keptCount != 3 {
		t.Errorf("without threshold all 3 groups should fit under Normal, got %d", keptCount)
	}
}

// v2.10 ADR 0044 #2: 阈值为 0 时不过滤（向后兼容）。
func TestGreedyPack_ScoreThresholdZeroNoFilter(t *testing.T) {
	groups := []AtomicGroup{
		{ID: "high", GroupScore: 2.0, GroupLength: 100, Indices: []int{0}},
		{ID: "low", GroupScore: 0.1, GroupLength: 100, Indices: []int{1}},
	}
	keep, _, _ := GreedyPack(groups, BudgetSnapshot{Status: StatusCompress}, 0)
	// 阈值 0 → 不过滤；compress 70% of 200 tokens(=100) → target 70
	// 高分组 50 tokens 装入；低分组 50+50=100 > 70 → 停止
	if len(keep) != 1 {
		t.Errorf("threshold 0 should not filter, want ratio-limited result got %d", len(keep))
	}
}

// v2.10 ADR 0044 #5: 内容感知 token 估算 —— CJK 与 ASCII 权重不同。
func TestEstimateTokens_ContentAware(t *testing.T) {
	// 纯 ASCII: 8 chars → 8/4 = 2
	if got := EstimateTokens("abcdefgh"); got != 2 {
		t.Errorf("ascii 8 chars: want 2 got %d", got)
	}
	// 纯中文: 4 chars → 4*3/2 = 6
	if got := EstimateTokens("证据反驳"); got != 6 {
		t.Errorf("cjk 4 chars: want 6 got %d", got)
	}
	// 空串
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("empty: want 0 got %d", got)
	}
}

// v2.10 ADR 0044 #5: 同等字符数下，中文文本的 token 估算必须高于 ASCII 文本。
// 这正是本条问题要修的偏差——旧实现只看字符数，两者被当成等价。
func TestEstimateTokens_CJKCostsMorePerChar(t *testing.T) {
	cjkText := "被告已知情并接受该条款" // 11 CJK chars
	asciiText := "abcdefghijk"    // 11 ASCII chars
	if len([]rune(cjkText)) != len([]rune(asciiText)) {
		t.Fatalf("test setup: char counts should match")
	}
	cjkTokens := EstimateTokens(cjkText)
	asciiTokens := EstimateTokens(asciiText)
	if cjkTokens <= asciiTokens {
		t.Errorf("CJK text should estimate higher tokens than same-length ASCII: cjk=%d ascii=%d", cjkTokens, asciiTokens)
	}
}

// v2.10 ADR 0044 #5: tokens() 回退语义 —— EstimatedTokens 未填时用 GroupLength。
func TestAtomicGroup_TokensFallback(t *testing.T) {
	g := AtomicGroup{GroupLength: 100}
	if got := g.tokens(); got != 100 {
		t.Errorf("fallback should return GroupLength, want 100 got %d", got)
	}
	g2 := AtomicGroup{GroupLength: 100, EstimatedTokens: 250}
	if got := g2.tokens(); got != 250 {
		t.Errorf("should prefer EstimatedTokens, want 250 got %d", got)
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

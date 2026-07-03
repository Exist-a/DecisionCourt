package agent

import (
	"math"
	"testing"
)

// TestClampProbability_Boundaries 验证 v0.8.4 硬编码 clamp 在所有边界
// 都把单值收窄到 [0, 1]。这是 v0.8.3 实装时发现的 DeepSeek 抽风问题
// （返回 0-100 范围整数 35.0 / 65.0）必须被无条件兜底的最后一道门。
func TestClampProbability_Boundaries(t *testing.T) {
	cases := []struct {
		name     string
		input    float64
		expected float64
	}{
		// 正常路径
		{"zero", 0.0, 0.0},
		{"one", 1.0, 1.0},
		{"mid", 0.5, 0.5},
		{"low-mid", 0.18, 0.18},
		{"high-mid", 0.82, 0.82},

		// 上界 clamp —— v0.8.3 实际抽风值
		{"deepseek-flap-35", 35.0, 1.0},
		{"deepseek-flap-65", 65.0, 1.0},
		{"deepseek-flap-100", 100.0, 1.0},
		{"just-above-1", 1.0001, 1.0},
		{"huge", 1e10, 1.0},

		// 下界 clamp
		{"negative", -0.5, 0.0},
		{"just-below-0", -0.0001, 0.0},
		{"huge-negative", -1e10, 0.0},

		// NaN / Inf —— Clamp01 实现用 v<lo/v>hi 比较，NaN 比较结果
		// 总是 false，所以会被 hi/lo 兜住（不能传 NaN 到 DB 引发 PG 报错）
		{"NaN", math.NaN(), 0.0},
		{"+Inf", math.Inf(1), 1.0},
		{"-Inf", math.Inf(-1), 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ClampProbability(c.input)
			// NaN != NaN，不能用等号比较；用"非 NaN 且等于期望"
			if math.IsNaN(c.expected) {
				if !math.IsNaN(got) {
					t.Errorf("ClampProbability(%v) = %v, want NaN", c.input, got)
				}
				return
			}
			if got != c.expected {
				t.Errorf("ClampProbability(%v) = %v, want %v", c.input, got, c.expected)
			}
		})
	}
}

// TestClampProbability_Idempotent 验证 clamp 是幂等：已经合法的值
// 再次 clamp 不会改变（避免 belief_diffs 多次累加时被反复边界化）。
func TestClampProbability_Idempotent(t *testing.T) {
	for _, v := range []float64{0.0, 0.18, 0.5, 0.82, 1.0} {
		once := ClampProbability(v)
		twice := ClampProbability(once)
		if once != twice {
			t.Errorf("ClampProbability(%v) not idempotent: once=%v, twice=%v", v, once, twice)
		}
	}
}

// TestClampProbabilityPair_LLM_Flap 验证 v0.8.4 智能归一化核心场景：
// DeepSeek 抽风返回 35.0 / 65.0（0-100 范围整数）→ 0.35 / 0.65，
// 保留 LLM 原本的"35% A, 65% B"偏向，**而不是** 简单 clamp 到
// 1.0/1.0（丢语义）。
//
// 这是 v0.8.4 选 B 方案（智能归一化）的行为契约。回归任何"老方案"
// 都会让 verdict 显示 100/100 而非 35/65 —— 跟 v0.8.3 的 3500/6500
// 同样反直觉。
func TestClampProbabilityPair_LLM_Flap(t *testing.T) {
	cases := []struct {
		name           string
		inA, inB       float64
		wantA, wantB   float64
		tolerance      float64
	}{
		// v0.8.3 实际抽风值 —— pair 归一化
		{
			name: "deepseek-35-65",
			inA: 35.0, inB: 65.0,
			wantA: 0.35, wantB: 0.65,
			tolerance: 1e-9,
		},
		{
			name: "deepseek-100-100",
			inA: 100.0, inB: 100.0,
			wantA: 0.5, wantB: 0.5, // a+b=200, /100=2,2, 归一化 = 0.5/0.5
			tolerance: 1e-9,
		},
		{
			name: "deepseek-70-30",
			inA: 70.0, inB: 30.0,
			wantA: 0.7, wantB: 0.3,
			tolerance: 1e-9,
		},
		// 合法 0-1 范围 —— 不应触发归一化
		{
			name: "valid-0.35-0.65",
			inA: 0.35, inB: 0.65,
			wantA: 0.35, wantB: 0.65,
			tolerance: 1e-9,
		},
		{
			name: "valid-0.5-0.5",
			inA: 0.5, inB: 0.5,
			wantA: 0.5, wantB: 0.5,
			tolerance: 1e-9,
		},
		// 边界 case：刚好在阈值附近
		{
			name: "threshold-0.9-0.8", // a+b = 1.7 > 1.5 → 视为 0-100
			inA: 0.9, inB: 0.8,
			wantA: 0.9 / 1.7, wantB: 0.8 / 1.7,
			tolerance: 1e-9,
		},
		{
			name: "below-threshold-0.7-0.7", // a+b = 1.4 < 1.5 → 不归一化
			inA: 0.7, inB: 0.7,
			wantA: 0.5, wantB: 0.5, // 不归一化时 sum=1.4, 归一化后 = 0.5/0.5
			tolerance: 1e-9,
		},
		// 负数 + 抽风：负数先 clamp 到 0，0 + 65 = 65 → 0-100 归一化
		{
			name: "neg-with-flap",
			inA: -5.0, inB: 65.0,
			wantA: 0.0, wantB: 1.0, // clamp 后 0/65, 0+65=65>1.5 → /100=0/0.65, 归一化=0/1
			tolerance: 1e-9,
		},
		// 全 0 兜底
		{
			name: "all-zero",
			inA: 0.0, inB: 0.0,
			wantA: 0.5, wantB: 0.5, // 全 0 → 0.5/0.5 中立
			tolerance: 1e-9,
		},
		// NaN 守卫 + 抽风
		{
			name: "nan-with-flap",
			inA: math.NaN(), inB: 65.0,
			wantA: 0.0, wantB: 1.0, // NaN→0, 0+65=65>1.5 → /100=0/0.65, 归一化=0/1
			tolerance: 1e-9,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotA, gotB := ClampProbabilityPair(c.inA, c.inB)
			if math.Abs(gotA-c.wantA) > c.tolerance {
				t.Errorf("ClampProbabilityPair(%v, %v) A = %v, want %v",
					c.inA, c.inB, gotA, c.wantA)
			}
			if math.Abs(gotB-c.wantB) > c.tolerance {
				t.Errorf("ClampProbabilityPair(%v, %v) B = %v, want %v",
					c.inA, c.inB, gotB, c.wantB)
			}
		})
	}
}

// TestClampProbabilityPair_SumsToOne 验证归一化后 a+b 严格等于 1
// （v0.6 业务约束：belief_a + belief_b = 1）。
func TestClampProbabilityPair_SumsToOne(t *testing.T) {
	cases := [][2]float64{
		{35.0, 65.0},
		{0.4, 0.6},
		{0.0, 0.0},
		{100.0, 100.0},
		{math.NaN(), 0.5},
		{-1.0, 200.0},
	}
	for _, c := range cases {
		a, b := ClampProbabilityPair(c[0], c[1])
		sum := a + b
		// 允许 1e-9 浮点误差
		if math.Abs(sum-1.0) > 1e-9 {
			t.Errorf("ClampProbabilityPair(%v, %v) = (%v, %v), sum = %v, want 1.0",
				c[0], c[1], a, b, sum)
		}
	}
}

// TestClampProbabilityPair_AlwaysInRange 验证归一化后两个值都 [0, 1]。
func TestClampProbabilityPair_AlwaysInRange(t *testing.T) {
	cases := [][2]float64{
		{35.0, 65.0},
		{-100.0, 100.0},
		{1e10, 1e10},
		{0.4, 0.6},
		{math.NaN(), math.Inf(1)},
	}
	for _, c := range cases {
		a, b := ClampProbabilityPair(c[0], c[1])
		if a < 0 || a > 1 {
			t.Errorf("ClampProbabilityPair(%v, %v) A=%v out of [0,1]", c[0], c[1], a)
		}
		if b < 0 || b > 1 {
			t.Errorf("ClampProbabilityPair(%v, %v) B=%v out of [0,1]", c[0], c[1], b)
		}
	}
}

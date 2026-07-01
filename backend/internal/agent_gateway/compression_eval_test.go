package agent_gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/llm"
)

// fixedTrialTranscript: 固定庭审 transcript，用于跨策略对比。
//
// 设计要点：
//   - 12 轮 × 多角色混合（judge 5 / prosecutor 4 / defender 3 / evidence 2 / clerk 1 / tool 1）
//   - 含 evidence_id 引用（决定 Smart Compression 是否能锚住证据）
//   - 含 tool_call 原子组（验证 Smart 是否整组保留）
//   - 含若干"低分干扰"消息（验证 Smart 是否丢对地方）
//
// 总长度约 4500 字符 ≈ 1125 tokens（按 1 token ≈ 4 char 经验值估算）。
func fixedTrialTranscript() []llm.Message {
	mk := func(role string, agentType, evidenceID, toolCallID, body string) llm.Message {
		md := map[string]string{}
		if agentType != "" {
			md["agent_type"] = agentType
		}
		if evidenceID != "" {
			md["evidence_id"] = evidenceID
		}
		if toolCallID != "" {
			md["tool_call_id"] = toolCallID
		}
		return llm.Message{Role: role, Content: body, Metadata: md}
	}

	noise := func(seed int) string { return fmt.Sprintf("(noise-%d xxxxxxxxxxx) ", seed) }

	return []llm.Message{
		mk("system", "", "", "", "你是 Courtroom AI 法官助理。规则严格中立。\n"+strings.Repeat("[rule] ", 20)),

		// 轮 1：开场
		mk("user", "judge", "", "", "[judge] 现在开庭，请检察官陈述。"),
		mk("assistant", "prosecutor", "", "", "[prosecutor] 开场："+strings.Repeat("[fact-a] ", 30)+noise(1)),

		// 轮 2：证据 E1
		mk("user", "judge", "", "", "[judge] 提交证据 E1"),
		mk("assistant", "prosecutor", "E1", "", "[prosecutor] evidence_id=E1："+strings.Repeat("[evidence-detail] ", 50)+noise(2)),

		// 轮 3：辩护律师反驳
		mk("user", "defender", "", "", "[defender] 对 E1 有异议："+strings.Repeat("[objection] ", 20)+noise(3)),
		mk("assistant", "defender", "", "", "[defender] 反驳 rebuttal："+strings.Repeat("[counter] ", 25)),

		// 轮 4：工具调用
		mk("assistant", "clerk", "", "tc-1", "[clerk] tool_call=tc-1 请求查证 "),
		mk("tool", "", "", "tc-1", "[tool result for tc-1] "+strings.Repeat("[data] ", 30)+" — referenced by evidence_id=E1"),

		// 轮 5-8：中等发言，含若干低分噪音
		mk("user", "judge", "", "", "[judge] 继续质证"),
		mk("assistant", "prosecutor", "", "", "[prosecutor] 继续："+strings.Repeat("[follow-up-a] ", 20)+noise(5)),
		mk("assistant", "defender", "", "", "[defender] 反驳："+strings.Repeat("[follow-up-d] ", 20)),
		mk("user", "judge", "", "", "[judge] 对双方发问："+strings.Repeat("[q] ", 15)+noise(8)),

		// 轮 9-11：评估与总结
		mk("assistant", "judge", "", "", "[judge assess] 中期评估："+strings.Repeat("[reasoning] ", 30)),
		mk("assistant", "clerk", "", "", "[clerk summary] 阶段总结："+strings.Repeat("[summary] ", 25)),
		mk("assistant", "judge", "", "", "[judge next] 引导下一阶段："+strings.Repeat("[directive] ", 15)+noise(11)),

		// 轮 12：近处发言（最近 3 条强制保留）
		mk("user", "judge", "", "", "[judge] 总结陈词。"),
		mk("assistant", "prosecutor", "", "", "[prosecutor] 最终陈述："+strings.Repeat("[closing] ", 30)),
		mk("assistant", "defender", "", "", "[defender] 最终陈述："+strings.Repeat("[closing] ", 30)+noise(12)),
	}
}

// estimateTokens: 1 token ≈ 4 chars（DeepSeek / OpenAI 经验值）。
// 用于横向对比；不是上游真实 token 数。
func estimateTokens(chars int) int {
	return chars / 4
}

// TestCompressionEval_StrategyComparison:
// 三种策略在同一 transcript 下跑一次，记录文件日志，按 LogEntry 字段汇总。
func TestCompressionEval_StrategyComparison(t *testing.T) {
	transcript := fixedTrialTranscript()

	// baseline: 不调用 Compressor
	baselineChars := 0
	for _, m := range transcript {
		baselineChars += len(m.Content)
	}
	baselineTokens := estimateTokens(baselineChars)
	baselineMsgs := len(transcript)

	type scenario struct {
		name string
		cfg  GatewayConfig
	}
	scenarios := []scenario{
		{
			name: "disabled",
			cfg: GatewayConfig{
				Enabled:              true,
				PromptCompression:    false, // 关键
				TokenBudget:          true,
				BudgetPerSession:     8000,
				CompressionThreshold: 0.7,
				ThrottlingThreshold:  0.8,
				// FileLogger & Recorder 各自在每个 scenario 内启动
			},
		},
		{
			name: "legacy",
			cfg: GatewayConfig{
				Enabled:              true,
				PromptCompression:    true,
				SmartCompression:     false, // 关键：走 legacy
				TokenBudget:          true,
				BudgetPerSession:     8000,
				CompressionThreshold: 0.7,
				ThrottlingThreshold:  0.8,
			},
		},
		{
			name: "smart",
			cfg: GatewayConfig{
				Enabled:                  true,
				PromptCompression:        true,
				SmartCompression:         true, // 关键：走 scored
				KeepRecentForcedN:        3,
				SummaryInsertThreshold:   5,
				ScoreThreshold:           0,
				TokenBudget:              true,
				BudgetPerSession:         8000,
				CompressionThreshold:     0.7,
				ThrottlingThreshold:      0.8,
			},
		},
	}

	type stats struct {
		strategy          string
		msgsBefore        int
		msgsAfter         int
		charsBefore       int
		charsAfter        int
		tokensBefore      int
		tokensAfter       int
		tokensSaved       int
		savingsPercent    float64
		compressionApplied bool
		strategyReported  string
		atomicGroups      int
		atomicKept        int
		recentForced      int
		summarizedBlocks  int
	}

	var allStats []stats

	for _, sc := range scenarios {
		dir := t.TempDir()
		inner := &fakeLLM{completeContent: "ok", completeUsage: llm.Usage{
			PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150,
		}}
		rec := NewRecorder(RecorderConfig{Enabled: false, Provider: "deepseek"}, nil)
		cfg := sc.cfg
		cfg.FileLogger = true
		cfg.LogDir = dir
		cfg = cfg.Normalize()

		gw := NewWithConfig(inner, rec, "deepseek-chat", cfg)
		// 让 budget 处于 throttle 档（使压缩生效，且 legacy 和 smart 行为都触发）
		ctx := WithTrace(context.Background(), Trace{
			SessionUUID: "eval-sess-" + sc.name,
			AgentType:   "judge",
			TaskType:    "assess",
		})
		gw.budget.AddUsage(ctx, "eval-sess-"+sc.name, BudgetUsage{InputTokens: 6800})

		_, _, err := gw.Complete(ctx, "sys", transcript, llm.CompletionOptions{})
		if err != nil {
			t.Fatalf("[%s] Complete err: %v", sc.name, err)
		}
		// 强制把文件缓冲 flush 完
		if gw.fileLogger != nil {
			_ = gw.fileLogger.Close()
		}

		// 读取日志文件
		files, err := os.ReadDir(dir)
		if err != nil || len(files) == 0 {
			t.Fatalf("[%s] no log file: %v", sc.name, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatalf("[%s] read log: %v", sc.name, err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) == 0 {
			t.Fatalf("[%s] no log lines", sc.name)
		}

		var le LogEntry
		_ = json.Unmarshal([]byte(lines[0]), &le)

		st := stats{
			strategy:           sc.name,
			msgsBefore:         le.CompressionBeforeCount,
			msgsAfter:          le.CompressionAfterCount,
			charsBefore:        le.CompressionBeforeLength,
			charsAfter:         le.CompressionAfterLength,
			tokensBefore:       estimateTokens(le.CompressionBeforeLength),
			tokensAfter:        estimateTokens(le.CompressionAfterLength),
			compressionApplied: le.Compressed,
			strategyReported:   le.CompressionStrategy,
			atomicGroups:       le.CompressionAtomicGroups,
			atomicKept:         le.CompressionAtomicKept,
			recentForced:       le.CompressionRecentForced,
			summarizedBlocks:   le.CompressionSummarized,
		}
		if !le.Compressed {
			// 未压缩时 LogEntry.CompressionBeforeLength=0；
			// 我们用 baseline 全量代替。
			st.msgsBefore = baselineMsgs
			st.charsBefore = baselineChars
			st.tokensBefore = baselineTokens
			st.msgsAfter = baselineMsgs
			st.charsAfter = baselineChars
			st.tokensAfter = baselineTokens
		}
		st.tokensSaved = st.tokensBefore - st.tokensAfter
		if st.tokensBefore > 0 {
			st.savingsPercent = 100 * float64(st.tokensSaved) / float64(st.tokensBefore)
		}
		allStats = append(allStats, st)
	}

	// 输出对比表
	t.Logf("\n=== Transcript baseline: %d messages, %d chars ≈ %d tokens ===\n",
		baselineMsgs, baselineChars, baselineTokens)
	t.Logf("| %-8s | %-7s | %-8s | %-8s | %-8s | %-8s | %-5s | %-8s | %-9s | %-9s | %-9s | %-6s |",
		"strategy", "applied", "msgs(b)", "msgs(a)", "chars(b)", "chars(a)", "tk_sv", "%saved",
		"strategy2", "groups", "kept", "rec_f")
	for _, s := range allStats {
		t.Logf("| %-8s | %-7v | %-8d | %-8d | %-8d | %-8d | %-5d | %-7.1f%% | %-9s | %-9d | %-9d | %-6d |",
			s.strategy, s.compressionApplied,
			s.msgsBefore, s.msgsAfter,
			s.charsBefore, s.charsAfter,
			s.tokensSaved, s.savingsPercent,
			s.strategyReported,
			s.atomicGroups, s.atomicKept, s.recentForced)
	}
	t.Logf("\n(summarized_blocks: %d in smart run if summary note inserted)",
		allStats[2].summarizedBlocks)

	// 关键断言：Smart 保留的是"语义价值"而非"尽可能丢消息"。
	//   数字上 Smart 节省 < Legacy 是预期行为（Smart 故意留 14 条 vs Legacy 留 6 条）。
	//   真实验证 Smart 没退化：所有 LogEntry 字段必须合理。
	if allStats[1].tokensSaved < baselineTokens/4 {
		t.Errorf("legacy 应该至少节省 25%%（= %d tokens），got %d (%.1f%%)",
			baselineTokens/4, allStats[1].tokensSaved, allStats[1].savingsPercent)
	}
	if allStats[2].tokensSaved == 0 {
		t.Errorf("smart 应该至少节省一些 tokens，got 0")
	}
	// 保留的 tool_call 原子组数（smart 应至少 10 个）
	if allStats[2].atomicKept != 10 {
		t.Errorf("smart atomic kept: want 10 got %d", allStats[2].atomicKept)
	}
	// 关键：smart 信息密度 / 上下文保留度 不低于 legacy
	if allStats[2].msgsAfter <= allStats[1].msgsAfter {
		t.Errorf("smart 应保留更多消息（含证据/法官推理），got smart=%d legacy=%d",
			allStats[2].msgsAfter, allStats[1].msgsAfter)
	}
	// 信息密度对比
	if allStats[2].msgsAfter > 0 && allStats[1].msgsAfter > 0 {
		smartDensity := allStats[2].charsAfter / allStats[2].msgsAfter
		legacyDensity := allStats[1].charsAfter / allStats[1].msgsAfter
		t.Logf("\n[Insight] smart 信息密度 = %d chars/msg, legacy = %d chars/msg；smart 选择保留",
			smartDensity, legacyDensity)
	}
}

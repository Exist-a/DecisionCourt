package promptlab

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/decisioncourt/backend/internal/llm"
)

// EvalRule 是 promptlab 支持的 LLM-as-judge 评估规则集合。
//
// 设计要点:
//   - string 类型而非独立枚举类型,便于 REST API 直接 JSON 反序列化
//     (前端传来的 rule 字段直接 string,无需做 Int2Rule 转换)
//   - 三个内置规则覆盖 §1.2 目标中"length / evidence_id_format / stance_mention"
//   - 后续可扩展: EvalRuleBeliefConsistency / EvalRuleRebuttalAvoid 等,
//     加 string 常量即可,JSON tag 友好
//
// LLM 调用模式:
//   - length_compliance 是确定性规则 (≤ 300 字由 UTF-8 rune 数计算),不走 LLM
//   - evidence_id_format / stance_mention 走 LLM-as-judge (温度 0.2 + JSONMode)
//   - 评分阈值 score > 0.7 → pass (与 v0.10.24 stance judge 同模式)
type EvalRule string

const (
	// EvalRuleLength 确定性规则: 发言 UTF-8 rune 数 ≤ 300。
	// 不调 LLM,直接用 utf8.RuneCountInString 计算。
	// 与 v0.10.21 PR-B 的硬截断 (react_runner.applySpeakerLengthLimit) 对齐。
	EvalRuleLength EvalRule = "length_compliance"

	// EvalRuleEvidenceID LLM-as-judge: 发言中引用的证据 ID 是否合法
	// (符合 E00X 格式 + 与 evidence_refs 字段一致)。
	EvalRuleEvidenceID EvalRule = "evidence_id_format"

	// EvalRuleStanceMention LLM-as-judge: 发言是否明确表达立场
	// (含 pro_a / pro_b / challenge / neutral 关键词之一)。
	EvalRuleStanceMention EvalRule = "stance_mention"
)

// allBuiltinRules 是注册到 REST /api/v1/prompts/eval 的合法规则白名单。
// REST 端点收到未知 rule 时返回 400 + "unknown rule"。
var allBuiltinRules = map[EvalRule]bool{
	EvalRuleLength:       true,
	EvalRuleEvidenceID:   true,
	EvalRuleStanceMention: true,
}

// IsBuiltinRule 报告 rule 是否在白名单内,供 REST 端点做 400 校验。
func IsBuiltinRule(rule string) bool {
	return allBuiltinRules[EvalRule(rule)]
}

// MaxSpeechLength 是 length_compliance 规则的字数上限。
// 与 backend/prompts/base.yaml 第 1 条规则 + v0.10.21 PR-B 硬截断一致。
const MaxSpeechLength = 300

// EvalPassThreshold 是 LLM judge 输出 score > threshold 判定 pass 的阈值。
// 与 v0.10.24 stance judge / v0.10.25 novelty check 的 0.7 阈值保持一致。
const EvalPassThreshold = 0.7

// EvalResult 是单条 LLM-as-judge 评估的输出结构。
//
// 字段语义:
//   - Rule: 被评估的规则名 (EvalRuleLength / EvalRuleEvidenceID / EvalRuleStanceMention)
//   - Score: 0.0-1.0,确定性规则(LENGTH)直接给 0/1,LLM 规则给 LLM judge 输出
//   - Pass: Score > EvalPassThreshold (0.7),确定性规则 LENGTH ≤ 300 字 = pass
//   - Reasoning: LLM judge 的简要理由 (≤ 50 字),确定性规则写 "length=N/300 chars"
//   - LatencyMs: 评估耗时 (ms),确定性规则是 0
type EvalResult struct {
	Rule      EvalRule `json:"rule"`
	Score     float64  `json:"score"`
	Pass      bool     `json:"pass"`
	Reasoning string   `json:"reasoning"`
	LatencyMs int      `json:"latency_ms"`
}

// judgeSystemPrompt 是 LLM-as-judge 的 system prompt 模板。
// 与 v0.10.24 stance judge 的 JSON 输出格式对齐。
//
// 设计要点:
//   - 严格 JSON 输出 {"score": 0-1, "pass": true/false, "reason": "≤50字"}
//   - 提示 LLM score 是 0.0 (完全不合规) ~ 1.0 (完全合规) 连续值,不是布尔
//   - reason ≤ 50 字让前端 alert 不会过长
const judgeSystemPrompt = `你是 LLM-as-judge 评估员,严格按规则评估下面这段 Agent 发言是否合规。

规则定义:
%s

待评估发言:
"""
%s
"""

返回严格 JSON (不要 markdown 包裹,不要多余文字):
{"score": 0.0-1.0, "pass": true|false, "reason": "≤50字中文理由"}

score 语义:
- 1.0 = 完全合规
- 0.7 = 大体合规,有小瑕疵
- 0.4 = 部分合规,明显问题
- 0.0 = 完全不合规

pass = (score > 0.7)
`

// Eval 对单条发言执行一条 eval rule 评估。
//
// 行为分流:
//   - EvalRuleLength: 不调 LLM,直接用 utf8.RuneCountInString 计字数
//     (中文/英文字符都算 1,符合"300 字"的语义直觉)
//   - EvalRuleEvidenceID / EvalRuleStanceMention: 调 llm.Complete,
//     温度 0.2 + JSONMode + MaxTokens=200
//
// 错误契约:
//   - llm == nil: 返回 ErrNilLLM (本包 errors.New,不依赖外部 error 包)
//   - rule 未知: 返回 ErrUnknownRule
//   - LLM 调用失败: error 含底层 err 信息,LatencyMs=0
//   - LLM 输出非 JSON: 兜底返回 score=0 + pass=false + reason="judge 输出非 JSON"
func Eval(ctx context.Context, llmClient llm.Client, rule EvalRule, output string) (EvalResult, error) {
	if llmClient == nil {
		return EvalResult{}, fmt.Errorf("promptlab.Eval: llm client is nil")
	}
	if !IsBuiltinRule(string(rule)) {
		return EvalResult{}, fmt.Errorf("promptlab.Eval: unknown rule %q", rule)
	}

	start := time.Now()
	switch rule {
	case EvalRuleLength:
		// 确定性规则: 不调 LLM,直接 utf8 rune 数
		runes := utf8.RuneCountInString(output)
		score := 0.0
		if runes <= MaxSpeechLength {
			score = 1.0
		}
		return EvalResult{
			Rule:      rule,
			Score:     score,
			Pass:      score > EvalPassThreshold,
			Reasoning: fmt.Sprintf("length=%d/%d chars", runes, MaxSpeechLength),
			LatencyMs: 0,
		}, nil

	case EvalRuleEvidenceID, EvalRuleStanceMention:
		return evalViaLLM(ctx, llmClient, rule, output, start)
	default:
		// IsBuiltinRule 已校验,这里走不到,但保留作为防御性兜底
		return EvalResult{}, fmt.Errorf("promptlab.Eval: unimplemented rule %q", rule)
	}
}

// evalViaLLM 走 LLM-as-judge 路径,被 Eval 内部调用。
//
// judge 输出严格 JSON {"score": 0-1, "pass": bool, "reason": ≤50字},
// 解析失败时兜底 score=0 + pass=false (与 v0.10.24 stance judge 同行为)。
func evalViaLLM(ctx context.Context, llmClient llm.Client, rule EvalRule, output string, start time.Time) (EvalResult, error) {
	ruleDef := ruleDefinition(rule)
	systemPrompt := fmt.Sprintf(judgeSystemPrompt, ruleDef, output)

	content, _, err := llmClient.Complete(
		ctx,
		"", // system prompt 已经在 judgeSystemPrompt 里,这里不再叠 system
		[]llm.Message{{Role: "user", Content: systemPrompt}},
		llm.CompletionOptions{
			Model:       "",
			Temperature: 0.2,
			MaxTokens:   200,
			JSONMode:    true,
		},
	)
	if err != nil {
		return EvalResult{
			Rule:      rule,
			Score:     0,
			Pass:      false,
			Reasoning: "judge LLM 调用失败: " + err.Error(),
			LatencyMs: int(time.Since(start).Milliseconds()),
		}, fmt.Errorf("promptlab.evalViaLLM: %w", err)
	}

	var judgeResult struct {
		Score  float64 `json:"score"`
		Pass   bool    `json:"pass"`
		Reason string  `json:"reason"`
	}
	if jerr := json.Unmarshal([]byte(content), &judgeResult); jerr != nil {
		return EvalResult{
			Rule:      rule,
			Score:     0,
			Pass:      false,
			Reasoning: "judge 输出非 JSON: " + truncate(content, 50),
			LatencyMs: int(time.Since(start).Milliseconds()),
		}, nil
	}

	// 强制 pass = (score > threshold),不信任 LLM 自己算的 pass 字段,
	// 防止 LLM "打分 0.6 但 pass=true" 这种内部矛盾输出
	pass := judgeResult.Score > EvalPassThreshold
	return EvalResult{
		Rule:      rule,
		Score:     judgeResult.Score,
		Pass:      pass,
		Reasoning: truncate(judgeResult.Reason, 50),
		LatencyMs: int(time.Since(start).Milliseconds()),
	}, nil
}

// ruleDefinition 返回 rule 的人类可读定义,用于 judge prompt 中 "规则定义:" 段。
// 与 EvalRule 常量一一对应,便于前端 / 日志排查时一眼看出规则含义。
func ruleDefinition(rule EvalRule) string {
	switch rule {
	case EvalRuleLength:
		// 理论上不会被调 (length 走确定性路径),但保留以防未来规则改路径
		return fmt.Sprintf("发言长度 ≤ %d 字 (UTF-8 rune 数)", MaxSpeechLength)
	case EvalRuleEvidenceID:
		return "发言引用的证据 ID 必须是合法 E00X 格式 (E 后 3 位数字),且只能在 evidence_refs 字段声明的 ID 列表中出现。禁止凭空捏造 E00X。"
	case EvalRuleStanceMention:
		return "发言必须明确表达立场,需包含 pro_a / pro_b / challenge / neutral 四个关键词之一 (大小写不敏感)。"
	default:
		return string(rule)
	}
}

// truncate 字符串截断到 maxLen rune (按字符截断,中文也算 1 个),超出加 "..."。
// 用于限制 LLM judge 输出 reason 长度,避免前端 alert 太长。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return strings.TrimSpace(string(runes[:maxLen])) + "..."
}

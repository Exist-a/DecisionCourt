package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/promptlab"
)

// TestHardcodedBaseRules_MatchesYAML 是 R10 遗留缺口的护栏（F2）。
//
// `HardcodedBaseRules()` 的注释声明"内容与 backend/prompts/base.yaml 的 base_rules
// 字段保持一致（PR-B1 启动时人工同步验证）"，但此前**没有任何测试断言** ——
// 两边一旦漂移，YAML 路径（线上生效）与 hardcoded fallback（YAML 加载失败时生效）
// 会给出不同规则，且只在 fallback 时才暴露。
//
// 断言两者**逐字节相等**（含 {{TOOLS_BLOCK}} 占位符）。若将来有意让 YAML 先行演进，
// 请显式更新 hardcoded 常量并在此说明，而不是让断言静默失效。
func TestHardcodedBaseRules_MatchesYAML(t *testing.T) {
	yamlPath := filepath.Join("..", "..", "prompts", "base.yaml")

	store := promptlab.NewStore(yamlPath)
	if err := store.Load(); err != nil {
		t.Fatalf("加载 %s 失败: %v", yamlPath, err)
	}
	// 传入占位符本身 → Replace(placeholder, placeholder) 无变化 → 拿到原始 base_rules。
	rawYAML := store.GetBaseRules(promptlab.ToolsBlockPlaceholder)

	// 唯一允许的差异：**末尾换行**。Go 原始字符串字面量以换行结尾（源码排版使然），
	// 而 base.yaml 的块标量文件末尾没有换行 —— 实测 yaml=7604B / hardcoded=7605B，
	// 首个差异就在最后一字节。除此之外必须逐字节一致。
	const note = "（末尾换行差异已忽略）"
	if strings.TrimRight(rawYAML, "\n") != strings.TrimRight(HardcodedBaseRules(), "\n") {
		t.Errorf("hardcoded baseRules 与 base.yaml 漂移（R10/F2）%s\n"+
			"yaml len=%d hardcoded len=%d\n第一个差异位置=%d\n%s",
			note, len(rawYAML), len(HardcodedBaseRules()),
			firstDiff(rawYAML, HardcodedBaseRules()),
			diffContext(rawYAML, HardcodedBaseRules()))
	}
}

func firstDiff(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func diffContext(a, b string) string {
	i := firstDiff(a, b)
	lo := i - 40
	if lo < 0 {
		lo = 0
	}
	hi := i + 40
	clip := func(s string) string {
		if hi > len(s) {
			hi = len(s)
		}
		if lo > len(s) {
			return ""
		}
		return s[lo:hi]
	}
	return "yaml:      …" + strings.ReplaceAll(clip(a), "\n", "\\n") + "…\n" +
		"hardcoded: …" + strings.ReplaceAll(clip(b), "\n", "\\n") + "…"
}

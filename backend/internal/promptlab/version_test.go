package promptlab

// R13: prompt 版本归因键（semver@git_sha#内容哈希）的回归护栏。
//
// 为什么需要内容哈希（而不只是 semver + git_sha）：Prompt Lab 的招牌能力是
// 「改 YAML → 5 秒热加载」，而 semver 要人工改 YAML 才变、git_sha 只有重新构建
// 才变 —— 热加载场景下两者都不动，归因失效。内容哈希直接对 base_rules 正文
// 取哈希，改一个字就变。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContentHashOf_ChangesWithContent 同内容同哈希、改一字就变。
func TestContentHashOf_ChangesWithContent(t *testing.T) {
	a := ContentHashOf("规则内容 A")
	b := ContentHashOf("规则内容 A")
	c := ContentHashOf("规则内容 B")

	assert.Equal(t, a, b, "同内容必须得到同一个哈希（否则归因无法聚合）")
	assert.NotEqual(t, a, c, "内容变了哈希必须变（否则热加载换了 prompt 也归因不到）")
	assert.Len(t, a, ContentHashLen, "哈希长度应为 ContentHashLen")
}

// TestVersionString_IncludesContentHash 归因键三段式。
func TestVersionString_IncludesContentHash(t *testing.T) {
	v := Version{Semver: "1.0.3-pr1", GitSHA: "3fc2ae83a758", ContentHash: "ab12cd34"}
	assert.Equal(t, "1.0.3-pr1@3fc2ae8#ab12cd34", v.String(),
		"git sha 取前 7 位；内容哈希跟在 '#' 之后")
}

// TestVersionString_OmitsHashWhenAbsent 没有哈希时省略 "#" 段。
// 保证手工构造的 Version（REST handler 的测试 fixture）仍能正常输出。
func TestVersionString_OmitsHashWhenAbsent(t *testing.T) {
	v := Version{Semver: "1.0.3-pr1"}
	assert.Equal(t, "1.0.3-pr1@dev", v.String())
}

// TestResolveGitSHA_YAMLWinsThenBuildFallback YAML 显式声明优先于构建期注入。
func TestResolveGitSHA_YAMLWinsThenBuildFallback(t *testing.T) {
	saved := buildGitSHA
	t.Cleanup(func() { buildGitSHA = saved })

	buildGitSHA = "from-build"
	assert.Equal(t, "declared-in-yaml", resolveGitSHA("declared-in-yaml"),
		"YAML 显式声明 git_sha 时应以 YAML 为准")
	assert.Equal(t, "from-build", resolveGitSHA(""),
		"YAML 未声明时应回落到构建期注入的 commit（R13 之前这里恒为空）")

	buildGitSHA = ""
	assert.Equal(t, "", resolveGitSHA(""), "都没提供时为空（本地 dev build）")
}

// TestLoad_FillsContentHashAndGitSHA 加载真实 YAML 后两个新字段都被填充。
func TestLoad_FillsContentHashAndGitSHA(t *testing.T) {
	saved := buildGitSHA
	buildGitSHA = "cafebabe1234"
	t.Cleanup(func() { buildGitSHA = saved })

	dir := t.TempDir()
	path := filepath.Join(dir, "base.yaml")
	require.NoError(t, os.WriteFile(path, []byte(
		"version: 9.9.9-pr1\nbase_rules: |\n  规则正文 {{TOOLS_BLOCK}}\n"), 0o644))

	s := NewStore(path)
	require.NoError(t, s.Load())

	v := s.Version()
	assert.Equal(t, "9.9.9-pr1", v.Semver)
	assert.Equal(t, "cafebabe1234", v.GitSHA, "YAML 未声明时用构建期注入值")
	assert.Equal(t, ContentHashOf("规则正文 {{TOOLS_BLOCK}}\n"), v.ContentHash)
	assert.Equal(t, "9.9.9-pr1@cafebab#"+v.ContentHash, v.String(),
		"归因键 = semver@sha7#hash")
}

// TestApplyFallback_FillsContentHash fallback 也有归因键（可区分 hardcoded
// 规则内容有没有随版本变化）。
func TestApplyFallback_FillsContentHash(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "missing.yaml"))
	s.ApplyFallback("hardcoded 规则")

	v := s.Version()
	assert.Equal(t, "fallback", v.Semver)
	assert.Equal(t, ContentHashOf("hardcoded 规则"), v.ContentHash)
}

package promptlab

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 写一个最小 YAML 文件用于测试。
// 内容刻意只包含 version + base_rules, 其他字段省略, 验证 yaml 解析的字段容忍性。
const validYAML = `version: "1.0.3-pr1"
git_sha: "abc1234"
created_at: "2026-08-20"
author: "Exist"
base_rules: |
  基本规则:
  1. 测试规则 A
  2. 测试规则 B

  {{TOOLS_BLOCK}}

  ## 输出格式
  json schema
`

// T1: Load 成功 — YAML 文件存在且字段合法, Store 应填充 rules + version。
func TestStore_Load_Success(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "base.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(validYAML), 0644))

	store := NewStore(yamlPath)
	require.NoError(t, store.Load(), "valid YAML 应该 Load 成功")

	require.False(t, store.IsFallback(), "Load 成功后 IsFallback 应为 false")

	v := store.Version()
	require.Equal(t, "1.0.3-pr1", v.Semver)
	require.Equal(t, "abc1234", v.GitSHA)
	require.Equal(t, yamlPath, v.SourcePath)
	require.False(t, v.LoadedAt.IsZero(), "LoadedAt 应被自动填充")

	// LastMtime 应反映 YAML 文件的 mtime
	require.False(t, store.LastMtime().IsZero(), "LastMtime 应被填充")
}

// T2: YAML 文件不存在 — Load 返回 error, 之后调用 ApplyFallback 可恢复。
func TestStore_Load_FileNotExist_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	missingPath := filepath.Join(tmpDir, "nonexistent.yaml")

	store := NewStore(missingPath)
	err := store.Load()
	require.Error(t, err, "不存在的文件应返回 error")
	require.Contains(t, err.Error(), missingPath, "error 应包含文件路径便于排查")

	// 调用方 (cmd/server/main.go) 在 Load 失败后调 ApplyFallback
	store.ApplyFallback("hardcoded rules fallback")
	require.True(t, store.IsFallback())
	require.Equal(t, "fallback", store.Version().Semver)
	require.Equal(t, "", store.Version().GitSHA)
	require.Equal(t, "hardcoded rules fallback", store.GetBaseRules(""))
}

// T3: YAML 字段缺失 — Load 返回 error (校验 version/base_rules 必填)。
func TestStore_Load_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "missing version",
			content: "base_rules: |\n  test",
			wantErr: "missing 'version' field",
		},
		{
			name:    "missing base_rules",
			content: `version: "1.0.0"`,
			wantErr: "missing or empty 'base_rules' field",
		},
		{
			name: "empty base_rules",
			content: `version: "1.0.0"
base_rules: ""`,
			wantErr: "missing or empty 'base_rules' field",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			yamlPath := filepath.Join(tmpDir, tc.name+".yaml")
			require.NoError(t, os.WriteFile(yamlPath, []byte(tc.content), 0644))

			store := NewStore(yamlPath)
			err := store.Load()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// T4: mtime 变化触发 IsYAMLReloadNeeded → true (热加载检测)。
// 模拟用户编辑 YAML 文件后, mtime 应该推进, IsYAMLReloadNeeded 应返回 true。
func TestStore_IsYAMLReloadNeeded_DetectsChange(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "base.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(validYAML), 0644))

	store := NewStore(yamlPath)
	require.NoError(t, store.Load())
	initialMtime := store.LastMtime()
	require.False(t, initialMtime.IsZero())

	// 模拟用户编辑: 等文件系统 mtime 精度 (通常 1s) 后重写文件。
	// 部分 Windows 文件系统 mtime 精度低, 用 Sleep 1.1s 跨越边界。
	time.Sleep(1100 * time.Millisecond)
	updatedYAML := strings.Replace(validYAML, "1.0.3-pr1", "1.0.3-pr1-modified", 1)
	require.NoError(t, os.WriteFile(yamlPath, []byte(updatedYAML), 0644))

	// mtime 应该推进, IsYAMLReloadNeeded 应返回 true
	require.True(t, IsYAMLReloadNeeded(yamlPath, initialMtime),
		"YAML 文件修改后, IsYAMLReloadNeeded 应返回 true")

	// Reload 后 version 应更新
	require.NoError(t, store.Load())
	v := store.Version()
	require.Equal(t, "1.0.3-pr1-modified", v.Semver,
		"Reload 后 version 应反映 YAML 新内容")
	require.True(t, store.LastMtime().After(initialMtime) || store.LastMtime().Equal(initialMtime),
		"LastMtime 应推进或保持 (不同文件系统 mtime 精度不同)")
}

// T5: 并发安全 — 多个 goroutine 同时读 GetBaseRules / Version, 加上
// 偶发的 Load 写, 不应触发 race detector。
func TestStore_ConcurrentReadWrite_NoRace(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "base.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(validYAML), 0644))

	store := NewStore(yamlPath)
	require.NoError(t, store.Load())

	const readerCount = 16
	const writeCount = 4
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(readerCount + writeCount)

	// reader: 高频读 GetBaseRules + Version
	for i := 0; i < readerCount; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = store.GetBaseRules("test tools block")
				_ = store.Version()
				_ = store.IsFallback()
				_ = store.LastMtime()
			}
		}()
	}

	// writer: 偶发 Load (热加载模拟)
	for i := 0; i < writeCount; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				_ = store.Load()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// 最终状态: Load 应已成功 (race-free)
	v := store.Version()
	require.Equal(t, "1.0.3-pr1", v.Semver, "Load 在 race 下也应最终一致")
}

// T6 (bonus): 占位符替换 — GetBaseRules 用 toolsBlock 替换 {{TOOLS_BLOCK}}。
// 验证与原 baseRules(toolsBlock) 行为一致。
func TestStore_GetBaseRules_PlaceholderReplacement(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "base.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(validYAML), 0644))

	store := NewStore(yamlPath)
	require.NoError(t, store.Load())

	t.Run("non-empty toolsBlock 替换占位符", func(t *testing.T) {
		got := store.GetBaseRules("## 工具调用协议\n- investigator_search\n")
		require.Contains(t, got, "## 工具调用协议")
		require.Contains(t, got, "- investigator_search")
		require.NotContains(t, got, "{{TOOLS_BLOCK}}", "占位符应被替换掉")
		require.Contains(t, got, "## 输出格式", "输出格式段应在 toolsBlock 之后")
	})

	t.Run("empty toolsBlock 替换为空串", func(t *testing.T) {
		got := store.GetBaseRules("")
		require.NotContains(t, got, "{{TOOLS_BLOCK}}")
		require.Contains(t, got, "## 输出格式")
	})
}

// T7 (bonus): fallback 路径也走占位符替换 — ApplyFallback 的输入字符串
// 必须含 {{TOOLS_BLOCK}}, GetBaseRules 同样替换。
func TestStore_ApplyFallback_PlaceholderReplacement(t *testing.T) {
	store := NewStore("/nonexistent.yaml")
	store.ApplyFallback("fallback rules\n\n{{TOOLS_BLOCK}}\n\n## 输出格式")
	got := store.GetBaseRules("TOOLS_CONTENT_HERE")
	require.Contains(t, got, "fallback rules")
	require.Contains(t, got, "TOOLS_CONTENT_HERE")
	require.Contains(t, got, "## 输出格式")
	require.NotContains(t, got, "{{TOOLS_BLOCK}}")
}
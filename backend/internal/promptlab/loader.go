package promptlab

import (
	"fmt"
	"os"
	"time"

	yamlv3 "go.yaml.in/yaml/v3"
)

// baseRulesYAML 是 YAML 文件的结构化表示。
//
// 设计理由:
//   - 用结构体而非 map[string]interface{}, 编译期校验字段名拼写
//   - 字段全部对齐 YAML schema (见 prompts/base.yaml), 文档化在 struct tag
//   - 保留 baseRules 字段是核心 (其他字段为元数据)
//
// YAML 字段映射:
//   version     → BaseRulesYAML.Version  (semver-pr 字符串)
//   git_sha     → BaseRulesYAML.GitSHA   (留空 / ldflags 注入)
//   created_at  → BaseRulesYAML.CreatedAt (ISO 日期字符串)
//   author      → BaseRulesYAML.Author   (可选)
//   base_rules  → BaseRulesYAML.BaseRules (核心 prompt 内容)
//
// 后续 PR-B2 可能新增的字段 (A/B Test 多版本):
//   versions    → map[string]VersionSpec (PR-B2 引入, PR-B1 不读)
type baseRulesYAML struct {
	Version   string `yaml:"version"`
	GitSHA    string `yaml:"git_sha"`
	CreatedAt string `yaml:"created_at"`
	Author    string `yaml:"author"`
	BaseRules string `yaml:"base_rules"`
}

// loadFromFile 是 Store.Load() 的实际实现。包外不可见 (lowercase),
// 由 Store.Load() 持有 Lock 后调用。
//
// 行为:
//   1. os.ReadFile 读 YAML (不存在 → 返回 fs.ErrNotExist 包装)
//   2. yamlv3.Unmarshal 解析 (字段缺失容忍, 不会因 author/git_sha 缺字段报错)
//   3. 校验 version + base_rules 非空 (空 → 返回 ErrInvalidYAML)
//   4. 返回 Version + rules, 由 Store 填充
//
// 错误契约: 任何错误都通过 fmt.Errorf("...: %w", err) 包装, 调用方
// 可用 errors.Is 判 os.IsNotFound 等。
func loadFromFile(yamlPath string) (Version, string, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return Version{}, "", fmt.Errorf("read %s: %w", yamlPath, err)
	}

	var doc baseRulesYAML
	if err := yamlv3.Unmarshal(data, &doc); err != nil {
		return Version{}, "", fmt.Errorf("parse %s: %w", yamlPath, err)
	}

	if doc.Version == "" {
		return Version{}, "", fmt.Errorf("invalid %s: missing 'version' field", yamlPath)
	}
	if doc.BaseRules == "" {
		return Version{}, "", fmt.Errorf("invalid %s: missing or empty 'base_rules' field", yamlPath)
	}

	v := Version{
		Semver:     doc.Version,
		GitSHA:     doc.GitSHA,
		LoadedAt:   time.Now(),
		SourcePath: yamlPath,
	}
	return v, doc.BaseRules, nil
}

// fileMtime 返回 YAML 文件的 mtime。失败时 (stat 错误) 返回零值 +
// false, 调用方应忽略。
//
// 设计理由: 把 stat 抽成独立函数, 便于测试时注入 fake mtime。
func fileMtime(yamlPath string) (time.Time, bool) {
	info, err := os.Stat(yamlPath)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// IsYAMLReloadNeeded 比较 current mtime 和 store.lastMtime, true 表示
// 需要重新 Load()。
//
// 注意: 文件 mtime 精度在部分文件系统上是 1 秒 (FAT32 / NFS), 在
// 同一秒内多次修改可能漏检。cmd/server/main.go 的 tick interval 设为
// 5s 足以覆盖此场景。
func IsYAMLReloadNeeded(yamlPath string, lastMtime time.Time) bool {
	current, ok := fileMtime(yamlPath)
	if !ok {
		return false
	}
	return current.After(lastMtime)
}
// Package promptlab 提供 LLM prompt 的版本管理、热加载、LLM-as-judge 评分
// 与 A/B Test 能力。详见 ADR 0031 + docs/V1.0.3-PLAN.md。
//
// 设计目标：
//   - baseRules 从 Go 代码字符串字面量 (internal/agent/prompts.go) 提取到 YAML
//   - 文件 mtime 检测实现秒级热加载 (避免 commit → docker build → docker compose up 链路)
//   - LLM-as-judge 自动化评分 (length / evidence_id_format / stance_mention)
//   - A/B Test 同时跑 2 个版本, judge 选 winner
//
// 本文件定义 Version 类型 (semver + git_sha + created_at)。
package promptlab

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// buildGitSHA 由构建期 ldflags 注入（-X .../promptlab.buildGitSHA=$VERSION），
// 默认空字符串表示本地 dev build。YAML 里显式写了 git_sha 时以 YAML 为准。
//
// R13: 没有它，`/api/v1/prompts/version` 的 git_sha 恒为空，线上跑的到底是
// 哪个 commit 的 prompt 只能靠猜。
var buildGitSHA string

// ContentHashLen 是内容哈希保留的十六进制字符数。
// 8 个字符（32 bit）足以在人手改 prompt 的场景下区分版本，且短到能进日志。
const ContentHashLen = 8

// Version 描述 YAML 文件加载后的元数据, 用于:
//   - REST /api/v1/prompts/version 返回给前端
//   - audit log / 排查时确认加载的版本
//   - A/B Test 标识 v1.0.3-pr1 vs v1.0.4-pr1
//   - llm_calls.prompt_version: 归因"这条调用用的是哪版 prompt"（R13）
//
// 字段映射:
//   Semver      → version 字段 (YAML: "version: 1.0.3-pr1")
//   GitSHA      → git_sha 字段 (YAML 优先, 否则构建期 ldflags 注入的 commit)
//   ContentHash → 不来自 YAML: 由 base_rules 正文算出的 sha256 前 8 位
//   LoadedAt    → 加载时间 (time.Time), 由 Store.Load() 自动填充 (不在 YAML)
//   SourcePath  → YAML 文件绝对路径, 仅作 debug 用
//
// 为什么需要 ContentHash（不只是 semver + git_sha）: semver 要人工改 YAML 才变，
// git_sha 只在重新构建时才变 —— 而 Prompt Lab 的招牌能力是**改 YAML 后 5 秒热加载**。
// 那两种标识在热加载场景下都不动，归因会失效（这正是 R13 记录的问题）。
// ContentHash 直接对加载到的 base_rules 正文取哈希，改一个字就变。
//
// 注意: GitSHA 为空字符串表示 build 时未注入 (本地 dev build 不传 ldflags),
// 不视为错误 — 仅在 REST /version 返回中标注 "(dev)"。
type Version struct {
	Semver      string    `json:"semver"`
	GitSHA      string    `json:"git_sha"`
	ContentHash string    `json:"content_hash"`
	LoadedAt    time.Time `json:"loaded_at"`
	SourcePath  string    `json:"source_path"`
}

// String 返回归因键, 用于日志与 llm_calls.prompt_version。
//
//	例: "1.0.3-pr1@abc1234#a1b2c3d4" / "1.0.3-pr1@dev#a1b2c3d4"
//
// 三段都可缺省：GitSHA 空 → "dev"；ContentHash 空 → 省略 "#" 段（兼容
// 手工构造的 Version，例如 REST handler 的测试 fixture）。
func (v Version) String() string {
	sha := v.GitSHA
	if sha == "" {
		sha = "dev"
	}
	// 仅取 SHA 前 7 位 (与 git log 习惯一致)
	if len(sha) > 7 {
		sha = sha[:7]
	}
	out := v.Semver + "@" + sha
	if v.ContentHash != "" {
		out += "#" + v.ContentHash
	}
	return out
}

// ContentHashOf 计算 base_rules 正文的内容哈希（sha256 前 ContentHashLen 位）。
//
// 纯函数：同内容必得同哈希，改一个字必变 —— 这是热加载场景下唯一可靠的
// 版本标识（semver / git_sha 都不会随 YAML 改动而变）。
func ContentHashOf(rules string) string {
	sum := sha256.Sum256([]byte(rules))
	return hex.EncodeToString(sum[:])[:ContentHashLen]
}

// resolveGitSHA 决定生效的 commit 标识：YAML 显式声明优先，其次构建期注入。
func resolveGitSHA(yamlSHA string) string {
	if yamlSHA != "" {
		return yamlSHA
	}
	return buildGitSHA
}
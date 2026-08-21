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
	"time"
)

// Version 描述 YAML 文件加载后的元数据, 用于:
//   - REST /api/v1/prompts/version 返回给前端
//   - audit log / 排查时确认加载的版本
//   - A/B Test 标识 v1.0.3-pr1 vs v1.0.4-pr1
//
// 字段映射:
//   Semver     → version 字段 (YAML: "version: 1.0.3-pr1")
//   GitSHA     → git_sha 字段 (YAML + ldflags 注入, PR-B1 留空字符串)
//   LoadedAt   → 加载时间 (time.Time), 由 Store.Load() 自动填充 (不在 YAML)
//   SourcePath → YAML 文件绝对路径, 仅作 debug 用
//
// 注意: GitSHA 为空字符串表示 build 时未注入 (本地 dev build 不传 ldflags),
// 不视为错误 — 仅在 REST /version 返回中标注 "(dev)"。
type Version struct {
	Semver     string    `json:"semver"`
	GitSHA     string    `json:"git_sha"`
	LoadedAt   time.Time `json:"loaded_at"`
	SourcePath string    `json:"source_path"`
}

// String 返回 "semver@gitsha" 格式, 用于日志输出。
//   例: "1.0.3-pr1@abc1234" 或 "1.0.3-pr1@dev"
func (v Version) String() string {
	sha := v.GitSHA
	if sha == "" {
		sha = "dev"
	}
	// 仅取 SHA 前 7 位 (与 git log 习惯一致)
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return v.Semver + "@" + sha
}
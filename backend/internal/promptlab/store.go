package promptlab

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ToolsBlockPlaceholder 是 YAML base_rules 中的工具块占位符。
//
// 协议:
//   - YAML 的 base_rules 字段必须包含字符串 "{{TOOLS_BLOCK}}" 作为注入位
//   - Store.GetBaseRules(toolsBlock) 用 toolsBlock 字符串替换该占位符
//   - 空 toolsBlock 时占位符替换为空字符串, 渲染为 "基本规则\n\n\n## 输出格式"
//     (与原 baseRules(toolsBlock="") 行为完全一致)
//
// 与 internal/agent/prompts.go 的 hardcodedBaseRules 共享同一占位符约定,
// 两条路径输出语义对齐。
const ToolsBlockPlaceholder = "{{TOOLS_BLOCK}}"

// Store 是 baseRules 的 thread-safe holder, 详见 ADR 0031 §1.4。
//
// 并发模型:
//   - GetBaseRules / Version 是高频读路径 (每次 LLM call 调一次), 用 RWMutex.RLock
//   - Load / fallback 是低频写路径 (启动 1 次 + 每 N 秒 mtime 检测 1 次), 用 Lock
//
// Fallback 行为:
//   - YAML 加载失败时, Store 保留一份 hardcoded fallback 字符串 (来自
//     internal/agent/prompts.go 的 hardcodedBaseRules), 庭审不中断
//   - fallback 状态下 GetBaseRules 仍返回合法字符串, 只是带 warn 日志
//   - 上层可通过 Version().Semver == "fallback" 区分 YAML-loaded vs fallback
type Store struct {
	mu sync.RWMutex

	version Version
	rules   string // 当前生效的 baseRules 主体 (含 {{TOOLS_BLOCK}} 占位符, 待替换)

	yamlPath    string
	lastLoadAt  time.Time
	lastMtime   time.Time // YAML 文件上次加载时的 mtime, 用于 hot-reload 检测
	fallbackSet bool      // true 表示当前 rules 来自 hardcoded fallback (YAML 加载失败)
}

// NewStore 创建 Store 并立即尝试加载 YAML。失败不返回 error,
// 而是设置 fallbackSet=true, 用 fallback rules (空字符串)。调用方
// (cmd/server/main.go) 必须在 main() 里显式调用 Load() 拿真 YAML 内容。
//
// 设计理由: NewStore 不应该因为一个配置文件缺失就阻止 server 启动 —
// 庭审服务可用性优先于 prompt 可调优性。
func NewStore(yamlPath string) *Store {
	s := &Store{
		yamlPath: yamlPath,
		version: Version{
			Semver:     "fallback",
			GitSHA:     "",
			LoadedAt:   time.Now(),
			SourcePath: "",
		},
		fallbackSet: true,
		rules:       "", // Load() 会填充; fallback path 在 Load 失败时回退到 fallbackBaseRules()
	}
	return s
}

// Load 重新读取 YAML 文件并更新 rules + version。失败时 Store 保持
// 旧状态 (rules / version 不变) 并返回 error — 调用方决定是否降级到
// fallback。fallback 切换由调用方在 Load 失败后调用 applyFallback() 完成。
//
// mtime 语义: 调用方应在 Load 前先 stat 文件, 若 mtime <= lastMtime 则跳过
// (返回 nil + isReload=false)。Store 自身不内置 ticker — 由 cmd/server/main.go
// 起 goroutine 每 5s 检测一次。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 实际加载逻辑在 loader.go (LoadFromFile + parseYAML)
	version, rules, err := loadFromFile(s.yamlPath)
	if err != nil {
		return fmt.Errorf("promptlab load %s: %w", s.yamlPath, err)
	}

	s.version = version
	s.rules = rules
	s.fallbackSet = false
	s.lastLoadAt = time.Now()
	if mtime, ok := fileMtime(s.yamlPath); ok {
		s.lastMtime = mtime
	}
	return nil
}

// ApplyFallback 把 Store 切到 hardcoded fallback 状态。fallbackRules
// 参数由 internal/agent/prompts.go 提供 (HardcodedBaseRules() —
// 与原 baseRules() 完全一致, 来自 v1.0.2 hardcoded 字符串, 确保
// 庭审在 YAML 加载失败时仍能用旧版 prompt)。
//
// 注意: fallbackRules 必须已经包含 {{TOOLS_BLOCK}} 占位符,
// ApplyFallback 不做插入; GetBaseRules 统一做占位符替换。
//
// 调用场景:
//   - cmd/server/main.go 启动时 Load 失败
//   - 后台 ticker 检测到 YAML 文件被删除/损坏时 (v1.0.3 PR-B1 不自动
//     fallback, 仅 warn; 自动 fallback 是 v1.0.x 后续讨论项)
func (s *Store) ApplyFallback(fallbackRules string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.version = Version{
		Semver:     "fallback",
		GitSHA:     "",
		LoadedAt:   time.Now(),
		SourcePath: "",
	}
	s.rules = fallbackRules
	s.fallbackSet = true
	s.lastLoadAt = time.Now()
	s.lastMtime = time.Time{}
}

// GetBaseRules 返回 baseRules 主体 + toolsBlock 拼接结果。
// 这是 hot path, 用 RLock 保护并发读。
//
// 行为:
//   - yamlPath 已加载: 返回 YAML rules, 用 toolsBlock 替换 {{TOOLS_BLOCK}} 占位符
//   - yamlPath fallback: 返回 fallback rules, 同样用 toolsBlock 替换占位符
//   - yamlPath 加载成功但 baseRules 字段为空: 返回 toolsBlock, 调用方应处理空 prompt
//
// 占位符替换语义: toolsBlock 为空字符串时, 渲染为 "基本规则\n\n\n## 输出格式..."
// (与原 baseRules("") 行为一致 — 三换行来自 "## 调查活动结束\n\n{{TOOLS_BLOCK}}\n\n## 输出格式"
// 路径的 "\n\n" + 空 + "\n\n")。
func (s *Store) GetBaseRules(toolsBlock string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.rules == "" {
		// 极端情况: YAML 解析成功但 baseRules 字段缺失, 或 fallback 路径
		// 未注入 (测试场景)。返回 toolsBlock, 调用方应处理空 prompt。
		return toolsBlock
	}
	return strings.Replace(s.rules, ToolsBlockPlaceholder, toolsBlock, 1)
}

// Version 返回当前生效的 version (读快照, 调用方不应持有修改)。
func (s *Store) Version() Version {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

// IsFallback 报告 Store 当前是否处于 fallback 状态 (YAML 加载失败)。
// 用于:
//   - 启动日志 ("loaded promptlab version 1.0.3-pr1" vs "promptlab fallback")
//   - REST /prompts/version 返回 (前端可显示 "⚠ fallback" badge)
//   - 测试断言
func (s *Store) IsFallback() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fallbackSet
}

// LastMtime 返回 YAML 文件上次成功加载的 mtime, 用于 caller tick 检测
// "mtime > lastMtime → reload"。fallback 状态返回零值 time.Time{}。
func (s *Store) LastMtime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastMtime
}
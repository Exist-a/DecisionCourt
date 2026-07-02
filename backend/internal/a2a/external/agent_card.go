package external

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// AgentCard 是 Google A2A 协议标准的 agent 描述。
//
// 参考：https://a2a-protocol.org/latest/specification/#agent-card
// 我们做了简化（v0.9 阶段）：
//   - authentication.schemes 仅支持 "none"
//   - 暂不支持 pushNotifications 真实推送
//   - 暂不支持 AgentSkill 详细分类（仅 name/description）
type AgentCard struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`
	URL         string   `json:"url"`
	Version     string   `json:"version"`
	Provider    Provider `json:"provider"`
	Capabilities Capabilities `json:"capabilities"`
	Skills      []string  `json:"defaultInputModes,omitempty"` // 简化：仅标记
	InputModes  []string  `json:"inputModes,omitempty"`
	OutputModes []string  `json:"outputModes,omitempty"`
}

// Provider 描述 Agent 提供方。
type Provider struct {
	Organization string `json:"organization"`
	URL          string `json:"url,omitempty"`
}

// Capabilities 描述 Agent 能力。
type Capabilities struct {
	Streaming   bool `json:"streaming"`
	PushNotif   bool `json:"pushNotifications"`
	StateTrail  bool `json:"stateTransitionHistory"`
}

// DiscoveryDocument 是 /.well-known/agent-card.json 端点返回的文档。
//
// 它聚合了所有 Agent 的 card，加上系统级元信息。
type DiscoveryDocument struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Provider    Provider       `json:"provider"`
	Agents      []AgentSummary `json:"agents"`
}

// AgentSummary 是 Discovery 中的 Agent 摘要。
type AgentSummary struct {
	Type string `json:"type"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// loadAgentCard 从 JSON 文件加载 agent-card。
func loadAgentCard(path string) (*AgentCard, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseAgentCardJSON(data)
}

// parseAgentCardJSON 解析 JSON 字节流为 AgentCard，验证必填字段。
func parseAgentCardJSON(data []byte) (*AgentCard, error) {
	var card AgentCard
	if err := json.Unmarshal(data, &card); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	// 验证必填字段（"required fields" 检查）
	if card.Name == "" {
		return nil, fmt.Errorf("required field: name")
	}
	if card.Type == "" {
		return nil, fmt.Errorf("required field: type")
	}
	if card.URL == "" {
		return nil, fmt.Errorf("required field: url")
	}
	return &card, nil
}

// assembleDiscoveryDocument 把多个 AgentCard 组装成 DiscoveryDocument。
//
// agents 按 type 字典序排序，保证输出稳定（便于测试 + 缓存）。
func assembleDiscoveryDocument(cards map[string]AgentCard, version string) DiscoveryDocument {
	doc := DiscoveryDocument{
		Name:        "DecisionCourt Multi-Agent Courtroom",
		Description: "5 个 AI Agent 协作的庭审模拟系统（基于 Google A2A 协议 2025 设计）",
		Version:     version,
		Provider: Provider{
			Organization: "DecisionCourt",
		},
		Agents: make([]AgentSummary, 0, len(cards)),
	}
	types := make([]string, 0, len(cards))
	for t := range cards {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		c := cards[t]
		doc.Agents = append(doc.Agents, AgentSummary{
			Type: c.Type,
			Name: c.Name,
			URL:  c.URL,
		})
	}
	return doc
}

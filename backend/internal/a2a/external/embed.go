package external

import (
	_ "embed"
	"fmt"
	"sort"
)

//go:embed agent_cards/prosecutor.json
var prosecutorCardJSON []byte

//go:embed agent_cards/defender.json
var defenderCardJSON []byte

//go:embed agent_cards/judge.json
var judgeCardJSON []byte

//go:embed agent_cards/investigator.json
var investigatorCardJSON []byte

//go:embed agent_cards/clerk.json
var clerkCardJSON []byte

// LoadEmbeddedCards 加载编译时嵌入的 5 个 agent-card JSON。
//
// 使用 //go:embed 编译时嵌入，避免运行时文件系统依赖。
// 返回 map[type]AgentCard，按 type 排序。
func LoadEmbeddedCards() (map[string]AgentCard, error) {
	raws := map[string][]byte{
		"prosecutor":   prosecutorCardJSON,
		"defender":     defenderCardJSON,
		"judge":        judgeCardJSON,
		"investigator": investigatorCardJSON,
		"clerk":        clerkCardJSON,
	}
	cards := make(map[string]AgentCard, len(raws))
	for agentType, raw := range raws {
		card, err := parseAgentCardJSON(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", agentType, err)
		}
		cards[agentType] = *card
	}
	return cards, nil
}

// agentCardTypes 返回 agent-card 的所有 type（按字典序）。
// 用于 main.go 装配时记录日志。
func agentCardTypes(cards map[string]AgentCard) []string {
	types := make([]string, 0, len(cards))
	for t := range cards {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

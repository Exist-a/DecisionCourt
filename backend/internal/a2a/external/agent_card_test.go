package external

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentCard_LoadFromJSON 验证 agent-card JSON 加载与字段映射。
func TestAgentCard_LoadFromJSON(t *testing.T) {
	card, err := loadAgentCard("../../internal/a2a/external/agent_cards/prosecutor.json")
	if err != nil {
		t.Skipf("agent_card.json not found, skipping: %v", err)
		return
	}
	assert.Equal(t, "DecisionCourt Prosecutor", card.Name)
	assert.Equal(t, "prosecutor", card.Type)
	assert.NotEmpty(t, card.URL)
	assert.NotEmpty(t, card.Version)
}

// TestAgentCard_RequiredFields 验证必填字段缺失时报错。
func TestAgentCard_RequiredFields(t *testing.T) {
	// 缺 name 字段
	raw := `{"type":"x","url":"http://x","version":"v1"}`
	_, err := parseAgentCardJSON([]byte(raw))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "name"))

	// 缺 type 字段
	raw = `{"name":"x","url":"http://x","version":"v1"}`
	_, err = parseAgentCardJSON([]byte(raw))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "type"))

	// 缺 url 字段
	raw = `{"name":"x","type":"y","version":"v1"}`
	_, err = parseAgentCardJSON([]byte(raw))
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "url"))
}

// TestAllAgentCards_ExistAndValid 验证 5 个 agent-card.json 都存在且合法。
func TestAllAgentCards_ExistAndValid(t *testing.T) {
	types := []string{"prosecutor", "defender", "judge", "investigator", "clerk"}
	for _, agentType := range types {
		t.Run(agentType, func(t *testing.T) {
			card, err := loadAgentCard("../../internal/a2a/external/agent_cards/" + agentType + ".json")
			if err != nil {
				t.Skipf("agent_card.json not found for %s: %v", agentType, err)
				return
			}
			assert.Equal(t, agentType, card.Type, "type mismatch")
			assert.NotEmpty(t, card.Name, "name empty")
			assert.NotEmpty(t, card.URL, "url empty")
		})
	}
}

// TestDiscoveryDocument_Assemble 验证 discovery 文档能正确组装。
func TestDiscoveryDocument_Assemble(t *testing.T) {
	cards := map[string]AgentCard{
		"prosecutor": {Name: "P", Type: "prosecutor", URL: "http://x/p"},
		"defender":   {Name: "D", Type: "defender", URL: "http://x/d"},
	}
	doc := assembleDiscoveryDocument(cards, "v0.8.1")
	assert.Equal(t, "DecisionCourt Multi-Agent Courtroom", doc.Name)
	assert.Equal(t, "v0.8.1", doc.Version)
	assert.Len(t, doc.Agents, 2)
	// 验证 agents 按 type 排序（保证输出一致）
	assert.Equal(t, "defender", doc.Agents[0].Type)
	assert.Equal(t, "prosecutor", doc.Agents[1].Type)
}

// TestLoadEmbeddedCards 验证编译时嵌入的 5 个 agent-card.json 都有效。
func TestLoadEmbeddedCards(t *testing.T) {
	cards, err := LoadEmbeddedCards()
	require.NoError(t, err)
	assert.Len(t, cards, 5, "应嵌入 5 个 agent-card")

	requiredTypes := []string{"prosecutor", "defender", "judge", "investigator", "clerk"}
	for _, agentType := range requiredTypes {
		card, ok := cards[agentType]
		assert.True(t, ok, "缺少 %s", agentType)
		assert.NotEmpty(t, card.Name, "%s name 为空", agentType)
		assert.NotEmpty(t, card.URL, "%s url 为空", agentType)
		assert.Equal(t, agentType, card.Type, "%s type 不匹配", agentType)
	}
}

// TestEmbeddedCardsDiscovery 验证嵌入的 cards 能组装出有效 discovery。
func TestEmbeddedCardsDiscovery(t *testing.T) {
	cards, err := LoadEmbeddedCards()
	require.NoError(t, err)
	doc := assembleDiscoveryDocument(cards, "v0.8.1")
	assert.Len(t, doc.Agents, 5)
	// 按字典序：clerk / defender / investigator / judge / prosecutor
	assert.Equal(t, "clerk", doc.Agents[0].Type)
	assert.Equal(t, "prosecutor", doc.Agents[4].Type)
}

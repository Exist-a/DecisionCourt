package a2a

// v2.11 (deferred D13-B) 护栏：payload 投影白名单的完整性。
//
// 白名单化的收益是"漏登记只会少给对方看，而不是多泄漏"。但收益成立的前提是
// **每个 MessageType 都被有意识地决定过**。这组测试钉住三件事：
//   1. 有生产者的公共类型必须在白名单里（否则对方看不到本该看到的内容）
//   2. 私有记忆类型绝不能出现在白名单里（它们永远不该被投影给对方）
//   3. 枚举全部已声明类型 —— 新增类型必须显式分类，否则测试失败

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// publicProducedTypes 是"真的有公开生产者"的类型（见 orchestrator.go /
// investigation/service.go）。这些必须在白名单里有条目。
func TestPublicPayloadWhitelist_PublicProducersRegistered(t *testing.T) {
	for _, mt := range []MessageType{MessageTypeSpeech, MessageTypeDispatch, MessageTypeReport} {
		if _, ok := publicPayloadWhitelist[mt]; !ok {
			t.Errorf("%s 有公开生产者，必须在 publicPayloadWhitelist 里登记 —— "+
				"否则投影给对方时会被当成未知类型，只放行 content", mt)
		}
	}
}

// 私有记忆类型绝不能进白名单。
func TestPublicPayloadWhitelist_PrivateTypesNeverWhitelisted(t *testing.T) {
	for _, mt := range PrivateMemoryMessageTypes() {
		if _, ok := publicPayloadWhitelist[mt]; ok {
			t.Errorf("%s 是私有记忆类型，绝不能出现在对方视图的白名单里", mt)
		}
	}
}

// 枚举全部已声明类型：每个都必须被显式分类。
func TestPublicPayloadWhitelist_CoversAllTypes(t *testing.T) {
	// 允许"不在白名单"的类型 = 私有记忆（永不投影）+ 已声明但当前无生产者
	// （evidence / challenge / inquiry / verdict_task）。后者走 unknown 兜底
	// （只放行 content），属于"安全降级"。
	allowedUnregistered := map[MessageType]bool{
		MessageTypeEvidence:    true,
		MessageTypeChallenge:   true,
		MessageTypeInquiry:     true,
		MessageTypeVerdictTask: true,
	}
	for _, mt := range PrivateMemoryMessageTypes() {
		allowedUnregistered[mt] = true
	}

	for _, mt := range AllMessageTypes() {
		_, registered := publicPayloadWhitelist[mt]
		if !registered && !allowedUnregistered[mt] {
			t.Errorf("MessageType %q 既不在白名单、也没被登记为「私有/无生产者」—— "+
				"新增类型必须显式决定它投影哪些字段（D13-B）", mt)
		}
	}
}

// 未知类型兜底只放行 content。
func TestUnknownTypeFallback_OnlyContent(t *testing.T) {
	require.Equal(t, []string{"content"}, unknownTypeFallbackKeys)
}

// 投影不改入参（返回新 map），且丢弃白名单外的键。
func TestProjectPayloadForOpponent_DoesNotMutateInput(t *testing.T) {
	in := map[string]interface{}{
		"content":   "c",
		"reasoning": "r",
	}
	out := ProjectPayloadForOpponent(MessageTypeSpeech, in)
	require.Equal(t, map[string]interface{}{"content": "c"}, out)
	// 入参必须原样（调用方可能还要用它写库/广播）
	require.Equal(t, map[string]interface{}{"content": "c", "reasoning": "r"}, in)
}

// 白名单内的键在 payload 里缺失时不应被塞成 nil（保持"没有就是没有"）。
func TestProjectPayloadForOpponent_MissingKeysOmitted(t *testing.T) {
	out := ProjectPayloadForOpponent(MessageTypeSpeech, map[string]interface{}{"content": "c"})
	require.Equal(t, map[string]interface{}{"content": "c"}, out)
	_, hasStance := out["stance"]
	require.False(t, hasStance, "payload 里没有的键不应以 nil 形式出现")
}

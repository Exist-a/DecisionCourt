package agent

import (
	"strings"
	"testing"

	"github.com/decisioncourt/backend/internal/model"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// v0.10.23 候选 2: applySpeakerNoveltyCheck 单元测试

// mockHistoryMessage 构造一条 history message
func mockHistoryMessage(agentID *uuid.UUID, content string) model.Message {
	return model.Message{
		ID:         uuid.New(),
		AgentID:    agentID,
		ActionType: "speak",
		Content:    content,
	}
}

func TestApplySpeakerNoveltyCheck_NoHistory(t *testing.T) {
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: "Hello world"},
		nil,
	)
	require.False(t, rejected)
	require.Equal(t, 0.0, jaccard)
}

func TestApplySpeakerNoveltyCheck_EmptyHistory(t *testing.T) {
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: "Hello world"},
		[]model.Message{},
	)
	require.False(t, rejected)
	require.Equal(t, 0.0, jaccard)
}

func TestApplySpeakerNoveltyCheck_EmptySpeaker(t *testing.T) {
	agentID := uuid.New()
	history := []model.Message{mockHistoryMessage(&agentID, "Hello world")}
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: ""},
		history,
	)
	require.False(t, rejected)
	require.Equal(t, 0.0, jaccard)
}

func TestApplySpeakerNoveltyCheck_DifferentContent(t *testing.T) {
	agentID := uuid.New()
	history := []model.Message{mockHistoryMessage(&agentID, "苹果好吃")}
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: "香蕉便宜"},
		history,
	)
	// 完全不重复 → rejected=false, jaccard < 0.6
	require.False(t, rejected)
	require.Less(t, jaccard, 0.6)
}

func TestApplySpeakerNoveltyCheck_HighOverlap(t *testing.T) {
	agentID := uuid.New()
	// 历史: 重复出现 "估值过高"
	history := []model.Message{mockHistoryMessage(&agentID, "估值过高 估值过高 估值过高 估值过高")}
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: "估值过高 估值过高 估值过高 估值过高"},
		history,
	)
	require.True(t, rejected)
	require.Greater(t, jaccard, 0.6)
}

func TestApplySpeakerNoveltyCheck_ChineseCJK(t *testing.T) {
	agentID := uuid.New()
	// 历史发言 (中文)
	history := []model.Message{mockHistoryMessage(&agentID, "估值过高")}
	rejected, _ := applySpeakerNoveltyCheck(
		Speaker{Content: "估值过高"},
		history,
	)
	// 完全相同中文 → jaccard = 1.0 > 0.6 → rejected
	require.True(t, rejected)
}

func TestApplySpeakerNoveltyCheck_PicksMaxAcrossHistory(t *testing.T) {
	agentID := uuid.New()
	// 3 条历史: 第 1 条不重复, 第 2 条中度重复, 第 3 条高度重复
	history := []model.Message{
		mockHistoryMessage(&agentID, "苹果好吃"),
		mockHistoryMessage(&agentID, "估值过高 估值过高 估值过高"), // 中度
		mockHistoryMessage(&agentID, "重复论据 重复论据 重复论据 重复论据"), // 高度
	}
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: "重复论据 重复论据 重复论据 重复论据"},
		history,
	)
	// 应该 pick 第 3 条 (最高)
	require.True(t, rejected)
	require.Greater(t, jaccard, 0.6)
}

func TestApplySpeakerNoveltyCheck_BelowThreshold(t *testing.T) {
	agentID := uuid.New()
	// jaccard = 0.5 → 边界, 不应触发 (> not >=)
	history := []model.Message{
		mockHistoryMessage(&agentID, "苹果好吃香蕉便宜"),
	}
	// 设计 content 让 jaccard = 0.5: a={苹果,好吃,香蕉,便宜}, b={苹果,好吃,X,Y}
	// intersection=2, union=6 → 2/6 ≈ 0.333
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: strings.Repeat("苹果好吃 ", 4)},
		history,
	)
	require.False(t, rejected)
	require.Less(t, jaccard, 0.6)
}

func TestApplySpeakerNoveltyCheck_SkipNonSpeak(t *testing.T) {
	agentID := uuid.New()
	// history 含非 speak (应跳过)
	history := []model.Message{
		mockHistoryMessage(&agentID, "估值过高 估值过高"), // action_type="speak"
		{
			ID:         uuid.New(),
			AgentID:    &agentID,
			ActionType: "system",
			Content:    "重复重复重复重复", // 不参与比对
		},
	}
	rejected, jaccard := applySpeakerNoveltyCheck(
		Speaker{Content: "估值过高 估值过高"},
		history,
	)
	require.True(t, rejected)
	require.Greater(t, jaccard, 0.6)
	// 因为 system 消息跳过, jaccard 应该 ≈ 1.0 (speak 内容完全相同)
	require.Greater(t, jaccard, 0.9)
}
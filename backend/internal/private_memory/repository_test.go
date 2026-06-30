package private_memory

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEntry_Validate(t *testing.T) {
	sessionID := uuid.New()
	agentID := uuid.New()

	cases := []struct {
		name    string
		entry   Entry
		wantErr bool
	}{
		{
			name: "valid strategy_note",
			entry: Entry{
				SessionID: sessionID,
				AgentID:   agentID,
				Type:      TypeStrategyNote,
				Content:   "对方对 E002 的反驳存在漏洞",
			},
		},
		{
			name: "missing session id",
			entry: Entry{
				AgentID: agentID,
				Type:    TypeStrategyNote,
				Content: "x",
			},
			wantErr: true,
		},
		{
			name: "missing agent id",
			entry: Entry{
				SessionID: sessionID,
				Type:      TypeStrategyNote,
				Content:   "x",
			},
			wantErr: true,
		},
		{
			name: "missing content",
			entry: Entry{
				SessionID: sessionID,
				AgentID:   agentID,
				Type:      TypeStrategyNote,
			},
			wantErr: true,
		},
		{
			name: "missing type",
			entry: Entry{
				SessionID: sessionID,
				AgentID:   agentID,
				Content:   "x",
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			entry: Entry{
				SessionID: sessionID,
				AgentID:   agentID,
				Type:      "diary",
				Content:   "x",
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.entry.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInMemoryRepository_Append_AssignsUUIDAndTimestamp(t *testing.T) {
	repo := NewInMemoryRepository(func() time.Time {
		return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	})
	ctx := context.Background()
	sessionID := uuid.New()
	agentID := uuid.New()

	e, err := repo.Append(ctx, Entry{
		SessionID: sessionID,
		AgentID:   agentID,
		Type:      TypeStrategyNote,
		Content:   "下一轮攻击重点：E002 数据来源",
		Round:     2,
	})
	require.NoError(t, err)
	require.NotEmpty(t, e.MemoryUUID)
	require.Equal(t, sessionID, e.SessionID)
	require.Equal(t, agentID, e.AgentID)
	require.Equal(t, 2, e.Round)
	require.NotEqual(t, uuid.Nil, e.ID)
}

func TestInMemoryRepository_Append_RejectsInvalid(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	_, err := repo.Append(context.Background(), Entry{Content: "x"})
	require.Error(t, err)
}

func TestInMemoryRepository_List_OwnerCanReadOwn(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	sessionID := uuid.New()
	agentID := uuid.New()

	_, err := repo.Append(ctx, Entry{
		SessionID: sessionID,
		AgentID:   agentID,
		Type:      TypeStrategyNote,
		Content:   "我的私有策略笔记",
	})
	require.NoError(t, err)

	rows, err := repo.List(ctx, sessionID, agentID, "agent:"+agentID.String())
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "我的私有策略笔记", rows[0].Content)
}

func TestInMemoryRepository_List_OtherAgentForbidden(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	sessionID := uuid.New()
	owner := uuid.New()
	other := uuid.New()

	_, err := repo.Append(ctx, Entry{
		SessionID: sessionID,
		AgentID:   owner,
		Type:      TypeStrategyNote,
		Content:   "私有",
	})
	require.NoError(t, err)

	_, err = repo.List(ctx, sessionID, owner, "agent:"+other.String())
	require.ErrorIs(t, err, ErrNotOwned)
}

func TestInMemoryRepository_List_OrchestratorSeesAnyPool(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	sessionID := uuid.New()
	owner := uuid.New()

	_, err := repo.Append(ctx, Entry{
		SessionID: sessionID,
		AgentID:   owner,
		Type:      TypeOpponentWeakness,
		Content:   "辩方发言过于依赖情绪",
	})
	require.NoError(t, err)

	rows, err := repo.List(ctx, sessionID, owner, "orchestrator")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, TypeOpponentWeakness, Type(rows[0].Type))
}

func TestInMemoryRepository_List_InvalidViewerRole(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	_, err := repo.List(ctx, uuid.New(), uuid.New(), "")
	require.ErrorIs(t, err, ErrNotOwned)

	_, err = repo.List(ctx, uuid.New(), uuid.New(), "agent:not-a-uuid")
	require.ErrorIs(t, err, ErrNotOwned)
}

func TestInMemoryRepository_List_IsolatesByAgent(t *testing.T) {
	repo := NewInMemoryRepository(nil)
	ctx := context.Background()
	sessionID := uuid.New()
	prosecutor := uuid.New()
	defender := uuid.New()

	_, err := repo.Append(ctx, Entry{
		SessionID: sessionID,
		AgentID:   prosecutor,
		Type:      TypeStrategyNote,
		Content:   "控方笔记",
	})
	require.NoError(t, err)
	_, err = repo.Append(ctx, Entry{
		SessionID: sessionID,
		AgentID:   defender,
		Type:      TypeStrategyNote,
		Content:   "辩方笔记",
	})
	require.NoError(t, err)

	proRows, err := repo.List(ctx, sessionID, prosecutor, "orchestrator")
	require.NoError(t, err)
	require.Len(t, proRows, 1)
	require.Equal(t, "控方笔记", proRows[0].Content)

	defRows, err := repo.List(ctx, sessionID, defender, "orchestrator")
	require.NoError(t, err)
	require.Len(t, defRows, 1)
	require.Equal(t, "辩方笔记", defRows[0].Content)
}

func TestNewEntry_HelperSetsFields(t *testing.T) {
	sessionID := uuid.New()
	agentID := uuid.New()
	e := NewEntry(sessionID, agentID, 3, TypeEvidenceEval, "E001 来自权威机构")

	require.Equal(t, sessionID, e.SessionID)
	require.Equal(t, agentID, e.AgentID)
	require.Equal(t, 3, e.Round)
	require.Equal(t, TypeEvidenceEval, e.Type)
	require.Equal(t, "E001 来自权威机构", e.Content)
	require.NoError(t, e.Validate())
}
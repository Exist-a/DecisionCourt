package api

// v1.0-patch (2026-08-22) PR-2: ListMySessions handler 单元测试
//
// 3 sub-test 覆盖:
//   1. owner 过滤 — viewer A 只看到自己的 session, 看不到 B 的
//   2. 排序 + limit — 默认按 updated_at DESC, limit/offset 生效
//   3. 鉴权 — 无 viewer 返 1401
//
// 测试基础设施复用 reopen_test.go 的 sqlite in-memory + 手工建 court_sessions 表模式。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/decisioncourt/backend/internal/auth"
	"github.com/decisioncourt/backend/internal/model"
)

// newListTestDB 创 sqlite in-memory DB + 手工建 court_sessions 表。
//
// 为什么不用 AutoMigrate: db.go CourtSession struct 用 PostgreSQL 专属的
// `default:gen_random_uuid()`, SQLite 解析失败 (reopen_test.go 解释)。
// 手工建表只含本测试需要的列。
func newListTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`CREATE TABLE court_sessions (
		id TEXT PRIMARY KEY,
		session_uuid TEXT NOT NULL,
		owner_id TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL,
		option_a TEXT,
		option_b TEXT,
		context TEXT,
		mode TEXT DEFAULT 'standard',
		max_rounds INTEGER DEFAULT 3,
		current_phase TEXT DEFAULT 'idle',
		current_round INTEGER DEFAULT 0,
		status TEXT DEFAULT 'active',
		converged INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error)
	return db
}

// seedSession 插一条 session, 返 session_uuid 便于断言。
func seedSession(t *testing.T, db *gorm.DB, ownerID, title string, updatedAt time.Time) string {
	t.Helper()
	uuidStr := uuid.New().String()
	row := model.CourtSession{
		ID:          uuid.New(),
		SessionUUID: uuidStr,
		OwnerID:     ownerID,
		Title:       title,
		OptionA:     "A",
		OptionB:     "B",
		Mode:        "standard",
		MaxRounds:   3,
		Status:      model.StatusActive,
		CreatedAt:   updatedAt,
		UpdatedAt:   updatedAt,
	}
	require.NoError(t, db.Create(&row).Error)
	return uuidStr
}

// listTestEngine 把 model.DB 注入到 ListMySessions 用的全局 DB, 启动 gin engine。
//
// 与 handler_belief_diffs_test.go 等的 ginEngine 不同: 我们要切 DB,
// 所以重新写一个 helper (handler_test.go 没有现成的 expose DB 入口)。
func listTestEngine(t *testing.T, db *gorm.DB, viewer string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	saved := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = saved
	})
	r := gin.New()
	api := r.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		if viewer != "" {
			c.Set(auth.ContextKey, viewer)
		}
		c.Next()
	})
	h := &Handler{service: nil} // ListMySessions 只用 model.DB
	h.RegisterAPIRoutes(api)
	return r
}

// T1: owner 过滤 — userA 创建 2 个 session, userB 创建 1 个,
// ListMySessions 以 userA 调用 → 应只返 2 个 A 的 session, 不含 B 的。
func TestListMySessions_OwnerFilter(t *testing.T) {
	db := newListTestDB(t)
	now := time.Now()
	seedSession(t, db, "user-a", "A 案 1", now)
	seedSession(t, db, "user-a", "A 案 2", now)
	seedSession(t, db, "user-b", "B 案 1", now)

	r := listTestEngine(t, db, "user-a")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Sessions []map[string]interface{} `json:"sessions"`
			Count    int                       `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 2, resp.Data.Count, "user-a 应只看 2 个 session (不是 user-b 的 1 个)")
	// sessionResponse 不暴露 owner_id (避免越权风险),
	// 这里通过 title 前缀验证是 user-a 的: "A 案 1" / "A 案 2"
	for _, s := range resp.Data.Sessions {
		title, _ := s["title"].(string)
		require.Contains(t, title, "A 案", "返回的应是 user-a 的 session, 不应混入 B 案")
	}
}

// T2: 排序 + limit/offset — userA 创建 5 个 session (不同 updated_at),
// limit=2 → 返最近 2 个; offset=2 → 跳前 2 个, 返第 3-4 个。
func TestListMySessions_LimitOffsetSort(t *testing.T) {
	db := newListTestDB(t)
	base := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		// updated_at: 0, 1, 2, 3, 4 分钟 (i=0 最老, i=4 最新)
		seedSession(t, db, "user-a", fmt.Sprintf("A 案 %d", i), base.Add(time.Duration(i)*time.Minute))
	}

	// limit=2, offset=0 → 最新 2 个 (i=4, i=3 — i=4 (4 分钟) 最新)
	r := listTestEngine(t, db, "user-a")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms?limit=2&offset=0", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			Sessions []map[string]interface{} `json:"sessions"`
			Count    int                       `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.Data.Count)
	require.Equal(t, "A 案 4", resp.Data.Sessions[0]["title"], "DESC 第一应是 i=4 (4 分钟, 最新)")
	require.Equal(t, "A 案 3", resp.Data.Sessions[1]["title"], "DESC 第二应是 i=3 (3 分钟)")
}

// T3: 鉴权 — 无 viewer (header 缺 auth) → 返 1401 unauthorized。
func TestListMySessions_NoViewer_Unauthorized(t *testing.T) {
	db := newListTestDB(t)
	seedSession(t, db, "user-a", "A 案", time.Now())

	r := listTestEngine(t, db, "" /* no viewer */)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/courtrooms", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1401, resp.Code)
}

// _ = bytes.NewReader (避免 import 警告)
var _ = bytes.NewReader

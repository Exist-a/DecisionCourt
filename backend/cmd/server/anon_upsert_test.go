package main

// v2.13 (deferred D21) 匿名用户 upsert 回归测试。
//
// 背景: /auth/anon 原来用 GORM FirstOrCreate(SELECT-then-INSERT)。前端一次
// 立案会并发打两次该端点,两个请求都 SELECT 不到 → 都 INSERT → 后者撞
// users_pkey(SQLSTATE 23505 duplicate key),打 WARN + ERROR 噪音。
//
// 覆盖:
//  1. SQL 形状: 生成的语句必须是单条 ON CONFLICT(而不是 SELECT+INSERT),
//     这是防"手滑改回 FirstOrCreate"的主要回归护栏。
//  2. 幂等语义: 重复 upsert 只有 1 行,FirstSeen 保留,LastSeen/IP/UA 更新。
//  3. 并发冒烟: 16 goroutine 打同一个 user_id,无错误且只有 1 行。

import (
	"bytes"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/decisioncourt/backend/internal/model"
)

func newAnonUpsertDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// sqlite :memory: 每连接一个库 → 钉成单连接,让并发写入序列化后可确定性断言。
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	require.NoError(t, db.AutoMigrate(&model.User{}))
	return db
}

// TestUpsertAnonUser_GeneratesOnConflictSQL 断言 upsert 走单条 ON CONFLICT,
// 而不是 FirstOrCreate 的 SELECT+INSERT 两步(AutoMigrate 之前那种竞态写法)。
func TestUpsertAnonUser_GeneratesOnConflictSQL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DryRun: true, // 只生成 SQL,不连库
		Logger: logger.New(log.New(&buf, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	require.NoError(t, err)

	err = upsertAnonUser(db, model.User{
		UserID:    "anon_dryrun",
		FirstSeen: time.Now().UTC(),
		LastSeen:  time.Now().UTC(),
		LastIP:    "10.0.0.1",
		LastUA:    "ua",
	})
	require.NoError(t, err)

	sql := buf.String()
	require.Contains(t, sql, "ON CONFLICT", "upsert 必须是 ON CONFLICT 写法, got: %s", sql)
	require.Contains(t, sql, "user_id", "冲突目标列必须是 user_id, got: %s", sql)
	require.Contains(t, sql, "last_seen", "冲突时应更新 last_seen, got: %s", sql)
}

// TestUpsertAnonUser_Idempotent 断言重复 upsert 只有一行,且 FirstSeen 不被覆盖。
func TestUpsertAnonUser_Idempotent(t *testing.T) {
	t.Parallel()

	db := newAnonUpsertDB(t)

	first := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, upsertAnonUser(db, model.User{
		UserID:    "anon_same",
		FirstSeen: first,
		LastSeen:  first,
		LastIP:    "1.1.1.1",
		LastUA:    "ua-1",
	}))

	second := first.Add(time.Hour)
	require.NoError(t, upsertAnonUser(db, model.User{
		UserID:    "anon_same",
		FirstSeen: second, // 应当被忽略(冲突时不更新 first_seen)
		LastSeen:  second,
		LastIP:    "2.2.2.2",
		LastUA:    "ua-2",
	}))

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("user_id = ?", "anon_same").Count(&count).Error)
	require.EqualValues(t, 1, count, "upsert 后应始终只有 1 行")

	var got model.User
	require.NoError(t, db.Where("user_id = ?", "anon_same").First(&got).Error)
	require.True(t, got.FirstSeen.Equal(first), "FirstSeen 应保留首次值 %v, got %v", first, got.FirstSeen)
	require.True(t, got.LastSeen.Equal(second), "LastSeen 应被更新为 %v, got %v", second, got.LastSeen)
	require.Equal(t, "2.2.2.2", got.LastIP)
	require.Equal(t, "ua-2", got.LastUA)
}

// TestUpsertAnonUser_ConcurrentSameUser 并发打同一个 user_id,不应有错误,且只有 1 行。
func TestUpsertAnonUser_ConcurrentSameUser(t *testing.T) {
	t.Parallel()

	db := newAnonUpsertDB(t)

	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			now := time.Now().UTC()
			errs[idx] = upsertAnonUser(db, model.User{
				UserID:    "anon_race",
				FirstSeen: now,
				LastSeen:  now,
				LastIP:    "3.3.3.3",
				LastUA:    "ua-race",
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "goroutine %d upsert 失败", i)
	}

	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("user_id = ?", "anon_race").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

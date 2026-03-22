package model

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupPDEPUsageBucketDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	require.NoError(t, db.AutoMigrate(&PDEPTokenUsageBucket{}, &PDEPTokenUsageFlushRecord{}))

	t.Cleanup(func() {
		DB = previousDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
	})
	return db
}

func TestPDEPUsageBucketStart_RoundsDownToTenMinutes(t *testing.T) {
	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.EqualValues(t, time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix(), pdepUsageBucketStart(ts))
}

func TestPDEPUsageBucketUpsert_IncrementsExistingRow(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()

	require.NoError(t, upsertPDEPUsageBucket(PDEPTokenUsageBucket{
		OwnerID:      1,
		TokenID:      2,
		BucketStart:  bucketStart,
		TokenUsed:    100,
		QuotaUsed:    30,
		RequestCount: 1,
	}))
	require.NoError(t, upsertPDEPUsageBucket(PDEPTokenUsageBucket{
		OwnerID:      1,
		TokenID:      2,
		BucketStart:  bucketStart,
		TokenUsed:    60,
		QuotaUsed:    20,
		RequestCount: 1,
	}))

	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 1, 2, bucketStart).First(&row).Error)
	require.EqualValues(t, 160, row.TokenUsed)
	require.EqualValues(t, 50, row.QuotaUsed)
	require.EqualValues(t, 2, row.RequestCount)
}

func TestPDEPUsageBucketUpsert_NormalizesBucketStart(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	rawStart := time.Date(2026, 3, 22, 10, 12, 34, 0, time.UTC).Unix()
	expectedStart := pdepUsageBucketStart(rawStart)

	require.NoError(t, upsertPDEPUsageBucket(PDEPTokenUsageBucket{
		OwnerID:      3,
		TokenID:      4,
		BucketStart:  rawStart,
		TokenUsed:    50,
		QuotaUsed:    10,
		RequestCount: 1,
	}))

	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ?", 3, 4).First(&row).Error)
	require.EqualValues(t, expectedStart, row.BucketStart)
}

func TestPDEPUsageBucketSchemaConstraints(t *testing.T) {
	db := setupPDEPUsageBucketDB(t)
	columnTypes, err := db.Migrator().ColumnTypes(&PDEPTokenUsageBucket{})
	require.NoError(t, err)
	required := map[string]bool{
		"owner_id":     true,
		"token_id":     true,
		"bucket_start": true,
	}
	found := map[string]bool{}
	for _, ct := range columnTypes {
		if _, ok := required[ct.Name()]; ok {
			nullable, ok := ct.Nullable()
			require.True(t, ok, "Nullable info unavailable for column %s", ct.Name())
			require.False(t, nullable, "column %s should be NOT NULL", ct.Name())
			found[ct.Name()] = true
		}
	}
	require.Equal(t, len(required), len(found))

	// Cleanup runs by bucket_start, so we must have a standalone bucket_start index.
	require.True(t, db.Migrator().HasIndex(&PDEPTokenUsageBucket{}, "idx_pdep_usage_bucket_start"))
	require.True(t, db.Migrator().HasIndex(&PDEPTokenUsageFlushRecord{}, "idx_pdep_usage_flush_bucket_start"))
}

func TestPDEPUsageBucketConflictColumns(t *testing.T) {
	columns := pdepUsageBucketConflictColumns()
	names := map[string]struct{}{}
	for _, col := range columns {
		names[col.Name] = struct{}{}
	}
	require.Contains(t, names, "owner_id")
	require.Contains(t, names, "token_id")
	require.Contains(t, names, "bucket_start")
	require.Len(t, names, 3)
}

func TestPDEPUsageBucketDialectSQLs(t *testing.T) {
	delta := PDEPTokenUsageBucket{
		OwnerID:      7,
		TokenID:      8,
		BucketStart:  time.Now().Unix(),
		TokenUsed:    15,
		QuotaUsed:    6,
		RequestCount: 2,
	}

	mysqlDB, cleanupMySQL := openMySQLMockDB(t)
	defer cleanupMySQL()
	mysqlSQL, _, err := buildPDEPUsageBucketUpsertSQL(mysqlDB, delta)
	require.NoError(t, err)
	require.Contains(t, mysqlSQL, "ON DUPLICATE KEY UPDATE")
	require.Contains(t, mysqlSQL, "token_used")
	require.Contains(t, mysqlSQL, "quota_used")
	require.Contains(t, mysqlSQL, "request_count")
	require.Contains(t, mysqlSQL, "updated_at")

	pgDB, cleanupPG := openPostgresMockDB(t)
	defer cleanupPG()
	pgSQL, _, err := buildPDEPUsageBucketUpsertSQL(pgDB, delta)
	require.NoError(t, err)
	require.Contains(t, strings.ToUpper(pgSQL), `ON CONFLICT ("OWNER_ID","TOKEN_ID","BUCKET_START")`)
	require.Contains(t, pgSQL, `"owner_id","token_id","bucket_start"`)
	require.Contains(t, pgSQL, `"quota_used"=quota_used +`)
	require.Contains(t, pgSQL, `"request_count"=request_count +`)
	require.Contains(t, pgSQL, `"token_used"=token_used +`)
	require.Contains(t, pgSQL, `"updated_at"`)
}

func TestPDEPUsageBucketAccumulate_WritesRedisHashAndPendingSet(t *testing.T) {
	mr := miniredis.RunT(t)
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousTTL := common.PDEPUsageBucketRedisTTLSeconds

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketRedisTTLSeconds = 600
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketRedisTTLSeconds = previousTTL
	})

	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.NoError(t, AccumulatePDEPUsageBucket(7, 9, ts, 123, 45))

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	key := pdepUsageHotKey(bucketStart, 7, 9)
	values, err := common.RDB.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	require.Equal(t, "123", values["token_used"])
	require.Equal(t, "45", values["quota_used"])
	require.Equal(t, "1", values["request_count"])

	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), key).Result()
	require.NoError(t, err)
	require.True(t, isPending)

	expiration := time.Duration(common.PDEPUsageBucketRedisTTLSeconds) * time.Second
	hashTTL, err := common.RDB.TTL(context.Background(), key).Result()
	require.NoError(t, err)
	require.Equal(t, expiration, hashTTL)
	pendingTTL, err := common.RDB.TTL(context.Background(), pdepUsagePendingSetKey()).Result()
	require.NoError(t, err)
	require.Equal(t, expiration, pendingTTL)
}

func TestAccumulatePDEPUsageBucket_NoopsWhenRedisUnavailableOrInvalid(t *testing.T) {
	mr := miniredis.RunT(t)
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB

	common.RedisEnabled = false
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	// Still no-op for invalid owner/token.
	require.NoError(t, AccumulatePDEPUsageBucket(0, 1, time.Now().Unix(), 1, 1))
	require.Empty(t, mr.Keys())
	require.NoError(t, AccumulatePDEPUsageBucket(1, 0, time.Now().Unix(), 1, 1))
	require.Empty(t, mr.Keys())
}

func TestAccumulatePDEPUsageBucket_FallsBackToDBWhenRedisDisabled(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	common.RedisEnabled = false
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.NoError(t, AccumulatePDEPUsageBucket(7, 9, ts, 123, 45))
	require.Empty(t, mr.Keys())

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 7, 9, bucketStart).First(&row).Error)
	require.EqualValues(t, 123, row.TokenUsed)
	require.EqualValues(t, 45, row.QuotaUsed)
	require.EqualValues(t, 1, row.RequestCount)
}

func TestAccumulatePDEPUsageBucket_FallsBackToDBWhenRedisClientMissing(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	common.RedisEnabled = true
	common.RDB = nil

	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.NoError(t, AccumulatePDEPUsageBucket(7, 9, ts, 123, 45))

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 7, 9, bucketStart).First(&row).Error)
	require.EqualValues(t, 123, row.TokenUsed)
	require.EqualValues(t, 45, row.QuotaUsed)
	require.EqualValues(t, 1, row.RequestCount)
}

func TestFlushPDEPUsageBuckets_PersistsRedisDeltaToDB(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousTTL := common.PDEPUsageBucketRedisTTLSeconds
	previousFlushInterval := common.PDEPUsageBucketFlushIntervalSeconds
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketRedisTTLSeconds = previousTTL
		common.PDEPUsageBucketFlushIntervalSeconds = previousFlushInterval
	})

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketRedisTTLSeconds = 600
	common.PDEPUsageBucketFlushIntervalSeconds = 1

	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.NoError(t, AccumulatePDEPUsageBucket(7, 9, ts, 123, 45))

	flushed, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, flushed)

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 7, 9, bucketStart).First(&row).Error)
	require.EqualValues(t, 123, row.TokenUsed)
	require.EqualValues(t, 45, row.QuotaUsed)
	require.EqualValues(t, 1, row.RequestCount)

	hotKey := pdepUsageHotKey(bucketStart, 7, 9)
	processingKey := pdepUsageProcessingKey(bucketStart, 7, 9)
	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.False(t, isPending)
	require.False(t, mr.Exists(processingKey))
}

func TestFlushPDEPUsageBuckets_DoesNotDoubleApplyReplayedProcessing(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousTTL := common.PDEPUsageBucketRedisTTLSeconds
	previousFlushInterval := common.PDEPUsageBucketFlushIntervalSeconds
	previousFinalize := pdepUsageFinalizeProcessingFunc
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketRedisTTLSeconds = previousTTL
		common.PDEPUsageBucketFlushIntervalSeconds = previousFlushInterval
		pdepUsageFinalizeProcessingFunc = previousFinalize
	})

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketRedisTTLSeconds = 600
	common.PDEPUsageBucketFlushIntervalSeconds = 1

	// Fail finalize once: simulate crash window "DB committed but processing not finalized".
	var finalizeCalls atomic.Int32
	pdepUsageFinalizeProcessingFunc = func(ctx context.Context, pendingKey, hotKey, processingKey string) error {
		if finalizeCalls.Add(1) == 1 {
			return fmt.Errorf("finalize failed")
		}
		return previousFinalize(ctx, pendingKey, hotKey, processingKey)
	}

	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.NoError(t, AccumulatePDEPUsageBucket(7, 9, ts, 123, 45))
	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	hotKey := pdepUsageHotKey(bucketStart, 7, 9)
	processingKey := pdepUsageProcessingKey(bucketStart, 7, 9)

	_, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.Error(t, err)

	// First run should have already applied to DB.
	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 7, 9, bucketStart).First(&row).Error)
	require.EqualValues(t, 123, row.TokenUsed)
	require.EqualValues(t, 45, row.QuotaUsed)
	require.EqualValues(t, 1, row.RequestCount)

	// And processing should still exist due to finalize failure.
	require.True(t, mr.Exists(processingKey))
	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.True(t, isPending)

	// Replay flush must not double-apply.
	_, err = FlushPDEPUsageBucketsOnce(context.Background())
	require.NoError(t, err)

	var row2 PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 7, 9, bucketStart).First(&row2).Error)
	require.EqualValues(t, 123, row2.TokenUsed)
	require.EqualValues(t, 45, row2.QuotaUsed)
	require.EqualValues(t, 1, row2.RequestCount)
	require.False(t, mr.Exists(processingKey))

	isPending, err = common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.False(t, isPending)
}

func TestFlushPDEPUsageBuckets_RemovesPendingWhenHotKeyMissing(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousFlushInterval := common.PDEPUsageBucketFlushIntervalSeconds
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketFlushIntervalSeconds = previousFlushInterval
	})

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketFlushIntervalSeconds = 1

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	hotKey := pdepUsageHotKey(bucketStart, 11, 22)
	require.NoError(t, common.RDB.SAdd(context.Background(), pdepUsagePendingSetKey(), hotKey).Err())
	require.False(t, mr.Exists(hotKey))

	flushed, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 0, flushed)

	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.False(t, isPending)
}

func TestFlushPDEPUsageBuckets_PreservesPendingWhenNewHotExistsDuringProcessing(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousFlushInterval := common.PDEPUsageBucketFlushIntervalSeconds
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketFlushIntervalSeconds = previousFlushInterval
	})

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketFlushIntervalSeconds = 1

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	ownerID := 7
	tokenID := 9
	hotKey := pdepUsageHotKey(bucketStart, ownerID, tokenID)
	processingKey := pdepUsageProcessingKey(bucketStart, ownerID, tokenID)

	// Simulate: flush is processing an existing processing key while new traffic writes a new hot key.
	require.NoError(t, common.RDB.HSet(context.Background(), processingKey,
		"token_used", 5,
		"quota_used", 2,
		"request_count", 1,
	).Err())
	require.NoError(t, common.RDB.HSet(context.Background(), hotKey,
		"token_used", 7,
		"quota_used", 3,
		"request_count", 1,
	).Err())
	require.NoError(t, common.RDB.SAdd(context.Background(), pdepUsagePendingSetKey(), hotKey).Err())

	flushed, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, flushed)
	require.False(t, mr.Exists(processingKey))

	// New hot key still exists, so pending must be preserved for the next round.
	require.True(t, mr.Exists(hotKey))
	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.True(t, isPending)

	// Next round should pick up the hot key and persist it too.
	flushed2, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, flushed2)

	var row PDEPTokenUsageBucket
	require.NoError(t, DB.Where("owner_id = ? AND token_id = ? AND bucket_start = ?", ownerID, tokenID, bucketStart).First(&row).Error)
	require.EqualValues(t, 12, row.TokenUsed)
	require.EqualValues(t, 5, row.QuotaUsed)
	require.EqualValues(t, 2, row.RequestCount)

	isPending, err = common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.False(t, isPending)
}

func TestFlushPDEPUsageBuckets_CleansInvalidPendingMembers(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousFlushInterval := common.PDEPUsageBucketFlushIntervalSeconds
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketFlushIntervalSeconds = previousFlushInterval
	})

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketFlushIntervalSeconds = 1

	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	hotKey := pdepUsageHotKey(bucketStart, 1, 2)
	processingMember := pdepUsageProcessingKey(bucketStart, 1, 2)
	garbageMember := "pdep:usage:garbage"

	require.NoError(t, common.RDB.HSet(context.Background(), hotKey,
		"token_used", 10,
		"quota_used", 4,
		"request_count", 1,
	).Err())
	require.NoError(t, common.RDB.SAdd(context.Background(), pdepUsagePendingSetKey(), hotKey, processingMember, garbageMember).Err())

	flushed, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, flushed)

	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), processingMember).Result()
	require.NoError(t, err)
	require.False(t, isPending)
	isPending, err = common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), garbageMember).Result()
	require.NoError(t, err)
	require.False(t, isPending)
}

func TestFlushPDEPUsageBuckets_AbortsWhenLockRefreshFails(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	mr := miniredis.RunT(t)

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	previousFlushInterval := common.PDEPUsageBucketFlushIntervalSeconds
	previousRefresh := pdepUsageRefreshLockFunc
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
		common.PDEPUsageBucketFlushIntervalSeconds = previousFlushInterval
		pdepUsageRefreshLockFunc = previousRefresh
	})

	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	common.PDEPUsageBucketFlushIntervalSeconds = 1

	var calls atomic.Int32
	pdepUsageRefreshLockFunc = func(ctx context.Context, lockKey, lockVal string, ttl time.Duration) (bool, error) {
		n := calls.Add(1)
		if n <= 1 {
			return true, nil
		}
		// Fail on the second check (after HGetAll), forcing abort mid-key.
		return false, nil
	}

	ts := time.Date(2026, 3, 22, 10, 19, 59, 0, time.UTC).Unix()
	require.NoError(t, AccumulatePDEPUsageBucket(7, 9, ts, 123, 45))
	bucketStart := time.Date(2026, 3, 22, 10, 10, 0, 0, time.UTC).Unix()
	hotKey := pdepUsageHotKey(bucketStart, 7, 9)
	processingKey := pdepUsageProcessingKey(bucketStart, 7, 9)

	_, err := FlushPDEPUsageBucketsOnce(context.Background())
	require.Error(t, err)

	// Should not persist to DB and should not finalize processing key.
	var count int64
	require.NoError(t, DB.Model(&PDEPTokenUsageBucket{}).Where("owner_id = ? AND token_id = ? AND bucket_start = ?", 7, 9, bucketStart).Count(&count).Error)
	require.EqualValues(t, 0, count)
	require.True(t, mr.Exists(processingKey))

	isPending, err := common.RDB.SIsMember(context.Background(), pdepUsagePendingSetKey(), hotKey).Result()
	require.NoError(t, err)
	require.True(t, isPending)
}

func TestDeleteExpiredPDEPUsageBuckets_RemovesRowsOlderThanRetention(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	now := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC).Unix()
	retentionDays := 10
	cutoff := now - int64(retentionDays)*24*3600

	require.NoError(t, DB.Create(&PDEPTokenUsageBucket{
		OwnerID:      1,
		TokenID:      1,
		BucketStart:  cutoff - 600,
		TokenUsed:    1,
		QuotaUsed:    1,
		RequestCount: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error)
	require.NoError(t, DB.Create(&PDEPTokenUsageBucket{
		OwnerID:      1,
		TokenID:      1,
		BucketStart:  cutoff, // should be retained (bucket_start < cutoff only)
		TokenUsed:    2,
		QuotaUsed:    2,
		RequestCount: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error)
	require.NoError(t, DB.Create(&PDEPTokenUsageBucket{
		OwnerID:      2,
		TokenID:      2,
		BucketStart:  cutoff + 600,
		TokenUsed:    3,
		QuotaUsed:    3,
		RequestCount: 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error)

	deleted, err := DeleteExpiredPDEPUsageBuckets(now, retentionDays)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var count int64
	require.NoError(t, DB.Model(&PDEPTokenUsageBucket{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

func TestDeleteExpiredPDEPUsageBuckets_RemovesExpiredFlushRecords(t *testing.T) {
	_ = setupPDEPUsageBucketDB(t)
	now := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC).Unix()
	retentionDays := 10
	cutoff := now - int64(retentionDays)*24*3600

	require.NoError(t, DB.Create(&PDEPTokenUsageFlushRecord{
		FlushToken:  "old-flush",
		OwnerID:     1,
		TokenID:     1,
		BucketStart: cutoff - 600,
		CreatedAt:   now,
	}).Error)
	require.NoError(t, DB.Create(&PDEPTokenUsageFlushRecord{
		FlushToken:  "new-flush",
		OwnerID:     1,
		TokenID:     1,
		BucketStart: cutoff,
		CreatedAt:   now,
	}).Error)

	_, err := DeleteExpiredPDEPUsageBuckets(now, retentionDays)
	require.NoError(t, err)

	var records []PDEPTokenUsageFlushRecord
	require.NoError(t, DB.Order("flush_token asc").Find(&records).Error)
	require.Len(t, records, 1)
	require.Equal(t, "new-flush", records[0].FlushToken)
}

func TestPDEPRetentionBatchQueriesUseBucketStartOrdering(t *testing.T) {
	db := setupPDEPUsageBucketDB(t)
	cutoff := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC).Unix()

	bucketSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var ids []int
		return buildPDEPBucketCleanupBatchQuery(tx, cutoff, 100).Pluck("id", &ids)
	})
	require.Contains(t, strings.ToLower(bucketSQL), "where bucket_start <")
	require.Contains(t, strings.ToLower(bucketSQL), "order by bucket_start asc")
	require.Contains(t, strings.ToLower(bucketSQL), "id asc")

	flushSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var flushTokens []string
		return buildPDEPFlushRecordCleanupBatchQuery(tx, cutoff, 100).Pluck("flush_token", &flushTokens)
	})
	require.Contains(t, strings.ToLower(flushSQL), "where bucket_start <")
	require.Contains(t, strings.ToLower(flushSQL), "order by bucket_start asc")
	require.Contains(t, strings.ToLower(flushSQL), "flush_token asc")
}

func openMySQLMockDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	sqlDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return gormDB, func() {
		sqlDB.Close()
	}
}

func openPostgresMockDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()
	sqlDB, _, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return gormDB, func() {
		sqlDB.Close()
	}
}

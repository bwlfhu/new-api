package model

import (
	"context"
	"strings"
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
	require.NoError(t, db.AutoMigrate(&PDEPTokenUsageBucket{}))

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

	require.NoError(t, AccumulatePDEPUsageBucket(1, 1, time.Now().Unix(), 1, 1))
	require.Empty(t, mr.Keys())

	common.RedisEnabled = true
	common.RDB = nil
	require.NoError(t, AccumulatePDEPUsageBucket(1, 1, time.Now().Unix(), 1, 1))
	require.Empty(t, mr.Keys())

	common.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	require.NoError(t, AccumulatePDEPUsageBucket(0, 1, time.Now().Unix(), 1, 1))
	require.Empty(t, mr.Keys())
	require.NoError(t, AccumulatePDEPUsageBucket(1, 0, time.Now().Unix(), 1, 1))
	require.Empty(t, mr.Keys())
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

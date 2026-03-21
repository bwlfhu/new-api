package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

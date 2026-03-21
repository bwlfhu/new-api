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

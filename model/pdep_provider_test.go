package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupPDEPProviderModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := DB
	previousLogDB := LOG_DB
	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	DB = db
	LOG_DB = db

	if err := db.AutoMigrate(&User{}, &Token{}, &Log{}, &PDEPTokenUsageBucket{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedPDEPProviderOwnerUser(t *testing.T, db *gorm.DB, id int) {
	t.Helper()

	user := &User{
		Id:          id,
		Username:    fmt.Sprintf("owner_%d", id),
		Password:    "password123",
		Role:        common.RoleAdminUser,
		Status:      common.UserStatusEnabled,
		DisplayName: fmt.Sprintf("Owner %d", id),
		Email:       fmt.Sprintf("owner_%d@example.com", id),
		Group:       "default",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to create owner user: %v", err)
	}
}

func seedPDEPProviderToken(t *testing.T, db *gorm.DB, userID int, name string, key string) *Token {
	t.Helper()

	token := &Token{
		UserId:       userID,
		Name:         name,
		Key:          key,
		Status:       common.TokenStatusEnabled,
		CreatedTime:  1710742800,
		AccessedTime: 1710742800,
		ExpiredTime:  -1,
	}
	if err := db.Create(token).Error; err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return token
}

func TestPDEPProvider_ListTokens_OnlyOwnerTokens(t *testing.T) {
	db := setupPDEPProviderModelTestDB(t)
	ownerToken := seedPDEPProviderToken(t, db, 1001, "owner-token", "abcd1234owner00000000000000000000000000000000")
	seedPDEPProviderToken(t, db, 1002, "other-token", "wxyz1234other00000000000000000000000000000000")

	items, err := ListPDEPTokens(1001)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only owner token, got %d", len(items))
	}
	if items[0].ID != fmt.Sprintf("%d", ownerToken.Id) {
		t.Fatalf("expected id %d, got %s", ownerToken.Id, items[0].ID)
	}
	if items[0].DisplayID != fmt.Sprintf("token-%d", ownerToken.Id) {
		t.Fatalf("expected displayId token-%d, got %s", ownerToken.Id, items[0].DisplayID)
	}
	if items[0].KeyPrefix != "sk-abcd" {
		t.Fatalf("expected keyPrefix sk-abcd, got %s", items[0].KeyPrefix)
	}
}

func TestPDEPProvider_CreateToken_NameConflictReturnsError(t *testing.T) {
	db := setupPDEPProviderModelTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 2001)
	seedPDEPProviderToken(t, db, 2001, "same-name", "abcd1234owner20010000000000000000000000000000")
	seedPDEPProviderToken(t, db, 2002, "same-name", "abcd1234owner20020000000000000000000000000000")

	_, err := CreatePDEPToken(2001, "same-name")
	if !errors.Is(err, ErrPDEPTokenNameConflict) {
		t.Fatalf("expected ErrPDEPTokenNameConflict, got %v", err)
	}
}

func TestPDEPProvider_CreateToken_ConcurrentSameOwnerName(t *testing.T) {
	db := setupPDEPProviderModelTestDB(t)
	seedPDEPProviderOwnerUser(t, db, 2601)

	var successCount int32
	var conflictCount int32
	var otherErrCount int32

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	createFn := func() {
		defer wg.Done()
		<-start
		_, err := CreatePDEPToken(2601, "parallel-name")
		if err == nil {
			atomic.AddInt32(&successCount, 1)
			return
		}
		if errors.Is(err, ErrPDEPTokenNameConflict) {
			atomic.AddInt32(&conflictCount, 1)
			return
		}
		atomic.AddInt32(&otherErrCount, 1)
	}

	go createFn()
	go createFn()
	close(start)
	wg.Wait()

	if successCount != 1 || conflictCount != 1 || otherErrCount != 0 {
		t.Fatalf("expected 1 success + 1 conflict + 0 other err, got success=%d conflict=%d other=%d", successCount, conflictCount, otherErrCount)
	}
}

func TestPDEPProvider_DeleteToken_IdempotentWhenMissing(t *testing.T) {
	_ = setupPDEPProviderModelTestDB(t)

	err := DeletePDEPToken(3001, 999999)
	if err != nil {
		t.Fatalf("expected nil error for missing token delete, got %v", err)
	}
}

func TestPDEPProvider_DeleteToken_RejectsNonOwnerToken(t *testing.T) {
	db := setupPDEPProviderModelTestDB(t)
	otherOwnerToken := seedPDEPProviderToken(t, db, 4002, "other-owner-token", "abcd1234owner40020000000000000000000000000000")

	err := DeletePDEPToken(4001, otherOwnerToken.Id)
	if !errors.Is(err, ErrPDEPForbiddenToken) {
		t.Fatalf("expected ErrPDEPForbiddenToken, got %v", err)
	}
}

func TestPDEPProvider_GetAggregated_BucketsByTenMinutes(t *testing.T) {
	db := setupPDEPProviderModelTestDB(t)
	ownerToken := seedPDEPProviderToken(t, db, 5101, "owner-token", "abcd1234owner51010000000000000000000000000000")

	start := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 19, 0, 30, 0, 0, time.UTC)

	requireTs := start.Unix()
	// 插入乱序数据，验证返回按 bucket_start asc 排序。
	if err := db.Create(&PDEPTokenUsageBucket{
		OwnerID:      5101,
		TokenID:      ownerToken.Id,
		BucketStart:  start.Add(10 * time.Minute).Unix(),
		TokenUsed:    180,
		QuotaUsed:    80,
		RequestCount: 1,
		CreatedAt:    requireTs,
		UpdatedAt:    requireTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket 2 (out-of-order insert): %v", err)
	}
	if err := db.Create(&PDEPTokenUsageBucket{
		OwnerID:      5101,
		TokenID:      ownerToken.Id,
		BucketStart:  start.Unix(),
		TokenUsed:    300,
		QuotaUsed:    120,
		RequestCount: 2,
		CreatedAt:    requireTs,
		UpdatedAt:    requireTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket 1: %v", err)
	}
	// 边界桶：bucket_start < start 以及 bucket_start == end 必须被排除（查询范围 [start, end)）。
	if err := db.Create(&PDEPTokenUsageBucket{
		OwnerID:      5101,
		TokenID:      ownerToken.Id,
		BucketStart:  start.Add(-1 * time.Second).Unix(),
		TokenUsed:    9999,
		QuotaUsed:    9999,
		RequestCount: 9999,
		CreatedAt:    requireTs,
		UpdatedAt:    requireTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket start-1s: %v", err)
	}
	if err := db.Create(&PDEPTokenUsageBucket{
		OwnerID:      5101,
		TokenID:      ownerToken.Id,
		BucketStart:  end.Unix(),
		TokenUsed:    8888,
		QuotaUsed:    8888,
		RequestCount: 8888,
		CreatedAt:    requireTs,
		UpdatedAt:    requireTs,
	}).Error; err != nil {
		t.Fatalf("failed to seed usage bucket end: %v", err)
	}
	// 种入与桶表矛盾的 logs，证明聚合不再扫描 logs（否则旧逻辑会受 quota 影响）。
	if err := db.Create(&Log{
		UserId:    5101,
		TokenId:   ownerToken.Id,
		Type:      LogTypeConsume,
		Quota:     777777,
		CreatedAt: start.Add(60 * time.Second).Unix(),
	}).Error; err != nil {
		t.Fatalf("failed to seed conflicting consume log: %v", err)
	}

	buckets, err := GetPDEPTokenAggregated(5101, fmt.Sprintf("token-%d", ownerToken.Id), start, end)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(buckets))
	}

	if buckets[0].Timestamp != "2026-03-19T00:00:00Z" ||
		buckets[0].Usage != 120 ||
		buckets[0].Refill != 0 ||
		buckets[0].Net != 120 ||
		buckets[0].TokenUsed != 300 ||
		buckets[0].QuotaUsed != 120 ||
		buckets[0].RequestCount != 2 {
		t.Fatalf("unexpected first bucket: %+v", buckets[0])
	}
	if buckets[1].Timestamp != "2026-03-19T00:10:00Z" ||
		buckets[1].Usage != 80 ||
		buckets[1].Refill != 0 ||
		buckets[1].Net != 80 ||
		buckets[1].TokenUsed != 180 ||
		buckets[1].QuotaUsed != 80 ||
		buckets[1].RequestCount != 1 {
		t.Fatalf("unexpected second bucket: %+v", buckets[1])
	}
}

func TestPDEPProvider_GetAggregated_RejectsInvalidSourceID(t *testing.T) {
	_ = setupPDEPProviderModelTestDB(t)
	start := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	_, err := GetPDEPTokenAggregated(5201, "invalid-source-id", start, end)
	if !errors.Is(err, ErrPDEPInvalidSourceID) {
		t.Fatalf("expected ErrPDEPInvalidSourceID, got %v", err)
	}
}

func TestPDEPProvider_GetAggregated_RejectsTokenOutsideOwner(t *testing.T) {
	db := setupPDEPProviderModelTestDB(t)
	otherOwnerToken := seedPDEPProviderToken(t, db, 5302, "other-owner-token", "abcd1234owner53020000000000000000000000000000")
	start := time.Date(2026, 3, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	_, err := GetPDEPTokenAggregated(5301, fmt.Sprintf("token-%d", otherOwnerToken.Id), start, end)
	if !errors.Is(err, ErrPDEPForbiddenToken) {
		t.Fatalf("expected ErrPDEPForbiddenToken, got %v", err)
	}
}

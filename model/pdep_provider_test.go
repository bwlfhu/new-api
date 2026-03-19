package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupPDEPProviderModelTestDB(t *testing.T) *gorm.DB {
	t.Helper()

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

	if err := db.AutoMigrate(&Token{}, &Log{}); err != nil {
		t.Fatalf("failed to migrate tables: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
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
	seedPDEPProviderToken(t, db, 2001, "same-name", "abcd1234owner20010000000000000000000000000000")
	seedPDEPProviderToken(t, db, 2002, "same-name", "abcd1234owner20020000000000000000000000000000")

	_, err := CreatePDEPToken(2001, "same-name")
	if !errors.Is(err, ErrPDEPTokenNameConflict) {
		t.Fatalf("expected ErrPDEPTokenNameConflict, got %v", err)
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

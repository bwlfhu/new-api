package model

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrPDEPTokenNameConflict = errors.New("pdep token name conflict")
var ErrPDEPForbiddenToken = errors.New("pdep forbidden token")
var ErrPDEPInvalidSourceID = errors.New("pdep invalid source id")

type PDEPTokenItem struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	DisplayID string  `json:"displayId"`
	KeyPrefix string  `json:"keyPrefix"`
	LastUsed  *string `json:"lastUsed,omitempty"`
	CreatedAt string  `json:"createdAt"`
	IsActive  bool    `json:"isActive"`
}

type PDEPTokenCreateResult struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	DisplayID string `json:"displayId"`
	KeyPrefix string `json:"keyPrefix"`
	CreatedAt string `json:"createdAt"`
	IsActive  bool   `json:"isActive"`
	Key       string `json:"key"`
}

type PDEPAggregatedBucket struct {
	Timestamp string `json:"timestamp"`
	Usage     int    `json:"usage"`
	Refill    int    `json:"refill"`
	Net       int    `json:"net"`
}

func buildPDEPKeyPrefix(key string) string {
	rawKey := strings.TrimPrefix(strings.TrimSpace(key), "sk-")
	if rawKey == "" {
		return "sk-"
	}
	prefixLength := 4
	if len(rawKey) < prefixLength {
		prefixLength = len(rawKey)
	}
	return "sk-" + rawKey[:prefixLength]
}

func buildPDEPKey(key string) string {
	rawKey := strings.TrimPrefix(strings.TrimSpace(key), "sk-")
	if rawKey == "" {
		return "sk-"
	}
	return "sk-" + rawKey
}

func toRFC3339UTC(ts int64) string {
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}

func isPDEPTokenActive(token *Token, now int64) bool {
	if token.Status != common.TokenStatusEnabled {
		return false
	}
	return token.ExpiredTime == -1 || token.ExpiredTime > now
}

func ListPDEPTokens(ownerID int) ([]PDEPTokenItem, error) {
	var tokens []Token
	if err := DB.Where("user_id = ?", ownerID).Order("id desc").Find(&tokens).Error; err != nil {
		return nil, err
	}

	now := common.GetTimestamp()
	items := make([]PDEPTokenItem, 0, len(tokens))
	for i := range tokens {
		token := tokens[i]
		item := PDEPTokenItem{
			ID:        strconv.Itoa(token.Id),
			Name:      token.Name,
			DisplayID: "token-" + strconv.Itoa(token.Id),
			KeyPrefix: buildPDEPKeyPrefix(token.Key),
			CreatedAt: toRFC3339UTC(token.CreatedTime),
			IsActive:  isPDEPTokenActive(&token, now),
		}

		var latestConsumeLog Log
		err := LOG_DB.Where("type = ? AND token_id = ?", LogTypeConsume, token.Id).
			Order("created_at desc").
			Limit(1).
			Take(&latestConsumeLog).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			lastUsed := toRFC3339UTC(latestConsumeLog.CreatedAt)
			item.LastUsed = &lastUsed
		}

		items = append(items, item)
	}
	return items, nil
}

func CreatePDEPToken(ownerID int, name string) (*PDEPTokenCreateResult, error) {
	name = strings.TrimSpace(name)

	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := createPDEPTokenTx(ownerID, name)
		if err == nil {
			return result, nil
		}
		if !isPDEPLockConflictError(err) {
			return nil, err
		}
		// SQLite 并发写可能直接返回 table is locked/deadlocked，重试后通常可读到冲突状态。
		time.Sleep(10 * time.Millisecond)
		if pdepTokenNameExists(ownerID, name) {
			return nil, ErrPDEPTokenNameConflict
		}
	}

	if pdepTokenNameExists(ownerID, name) {
		return nil, ErrPDEPTokenNameConflict
	}
	return nil, errors.New("pdep token create failed due to lock conflict")
}

func createPDEPTokenTx(ownerID int, name string) (*PDEPTokenCreateResult, error) {
	var result *PDEPTokenCreateResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var owner User
		lockErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("id = ?", ownerID).
			Take(&owner).Error
		if lockErr != nil && !errors.Is(lockErr, gorm.ErrRecordNotFound) {
			return lockErr
		}

		var count int64
		if err := tx.Model(&Token{}).Where("user_id = ? AND name = ?", ownerID, name).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrPDEPTokenNameConflict
		}

		rawKey, err := common.GenerateKey()
		if err != nil {
			return err
		}

		now := common.GetTimestamp()
		token := &Token{
			UserId:       ownerID,
			Name:         name,
			Key:          rawKey,
			Status:       common.TokenStatusEnabled,
			CreatedTime:  now,
			AccessedTime: now,
			ExpiredTime:  -1,
		}
		if err := tx.Create(token).Error; err != nil {
			return err
		}

		result = &PDEPTokenCreateResult{
			ID:        strconv.Itoa(token.Id),
			Name:      token.Name,
			DisplayID: "token-" + strconv.Itoa(token.Id),
			KeyPrefix: buildPDEPKeyPrefix(token.Key),
			CreatedAt: toRFC3339UTC(token.CreatedTime),
			IsActive:  isPDEPTokenActive(token, now),
			Key:       buildPDEPKey(token.Key),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func pdepTokenNameExists(ownerID int, name string) bool {
	var count int64
	err := DB.Model(&Token{}).Where("user_id = ? AND name = ?", ownerID, name).Count(&count).Error
	return err == nil && count > 0
}

func isPDEPLockConflictError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "database table is locked") ||
		strings.Contains(errMsg, "database is deadlocked") ||
		strings.Contains(errMsg, "database is locked")
}

func DeletePDEPToken(ownerID int, tokenID int) error {
	var token Token
	err := DB.Where("id = ?", tokenID).First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if token.UserId != ownerID {
		return ErrPDEPForbiddenToken
	}
	return token.Delete()
}

func parsePDEPSourceID(sourceID string) (int, error) {
	if !strings.HasPrefix(sourceID, "token-") {
		return 0, ErrPDEPInvalidSourceID
	}
	rawID := strings.TrimPrefix(sourceID, "token-")
	tokenID, err := strconv.Atoi(rawID)
	if err != nil || tokenID <= 0 {
		return 0, ErrPDEPInvalidSourceID
	}
	return tokenID, nil
}

func GetPDEPTokenAggregated(ownerID int, sourceID string, startUTC time.Time, endUTC time.Time) ([]PDEPAggregatedBucket, error) {
	tokenID, err := parsePDEPSourceID(sourceID)
	if err != nil {
		return nil, err
	}

	var token Token
	err = DB.Select("id,user_id").Where("id = ?", tokenID).Take(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPDEPForbiddenToken
	}
	if err != nil {
		return nil, err
	}
	if token.UserId != ownerID {
		return nil, ErrPDEPForbiddenToken
	}

	startTs := startUTC.UTC().Unix()
	endTs := endUTC.UTC().Unix()

	type aggregatedRow struct {
		BucketTS int64 `gorm:"column:bucket_ts"`
		Usage    int   `gorm:"column:usage"`
	}
	var rows []aggregatedRow
	err = LOG_DB.Model(&Log{}).
		Select("((created_at / 600) * 600) AS bucket_ts, COALESCE(SUM(quota), 0) AS usage").
		Where("type = ? AND token_id = ? AND created_at >= ? AND created_at < ?", LogTypeConsume, tokenID, startTs, endTs).
		Group("((created_at / 600) * 600)").
		Order("bucket_ts asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	buckets := make([]PDEPAggregatedBucket, 0, len(rows))
	for i := range rows {
		buckets = append(buckets, PDEPAggregatedBucket{
			Timestamp: toRFC3339UTC(rows[i].BucketTS),
			Usage:     rows[i].Usage,
			Refill:    0,
			Net:       rows[i].Usage,
		})
	}
	return buckets, nil
}

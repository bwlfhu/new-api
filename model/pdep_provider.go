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

package model

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PDEPTokenUsageBucket struct {
	ID           int   `json:"id"`
	OwnerID      int   `json:"owner_id" gorm:"index:idx_pdep_usage_owner_bucket,priority:1;index:idx_pdep_usage_owner_token_bucket,priority:1"`
	TokenID      int   `json:"token_id" gorm:"index:idx_pdep_usage_token_bucket,priority:1;index:idx_pdep_usage_owner_token_bucket,priority:2"`
	BucketStart  int64 `json:"bucket_start" gorm:"bigint;index:idx_pdep_usage_token_bucket,priority:2;index:idx_pdep_usage_owner_bucket,priority:2;uniqueIndex:idx_pdep_usage_owner_token_bucket,priority:3"`
	TokenUsed    int64 `json:"token_used" gorm:"not null;default:0"`
	QuotaUsed    int64 `json:"quota_used" gorm:"not null;default:0"`
	RequestCount int64 `json:"request_count" gorm:"not null;default:0"`
	CreatedAt    int64 `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt    int64 `json:"updated_at" gorm:"bigint;not null"`
}

func (PDEPTokenUsageBucket) TableName() string {
	return "pdep_token_usage_bucket"
}

func pdepUsageBucketStart(ts int64) int64 {
	return ts - (ts % 600)
}

func upsertPDEPUsageBucket(delta PDEPTokenUsageBucket) error {
	now := time.Now().Unix()
	if delta.CreatedAt == 0 {
		delta.CreatedAt = now
	}
	delta.UpdatedAt = now
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner_id"}, {Name: "token_id"}, {Name: "bucket_start"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"token_used":    gorm.Expr("token_used + ?", delta.TokenUsed),
			"quota_used":    gorm.Expr("quota_used + ?", delta.QuotaUsed),
			"request_count": gorm.Expr("request_count + ?", delta.RequestCount),
			"updated_at":    now,
		}),
	}).Create(&delta).Error
}

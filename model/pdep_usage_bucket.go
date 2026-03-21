package model

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PDEPTokenUsageBucket struct {
	ID           int   `json:"id"`
	OwnerID      int   `json:"owner_id" gorm:"not null;index:idx_pdep_usage_owner_bucket,priority:1;index:idx_pdep_usage_owner_token_bucket,priority:1"`
	TokenID      int   `json:"token_id" gorm:"not null;index:idx_pdep_usage_token_bucket,priority:1;index:idx_pdep_usage_owner_token_bucket,priority:2"`
	BucketStart  int64 `json:"bucket_start" gorm:"not null;bigint;index:idx_pdep_usage_token_bucket,priority:2;index:idx_pdep_usage_owner_bucket,priority:2;uniqueIndex:idx_pdep_usage_owner_token_bucket,priority:3"`
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
	preparePDEPUsageBucketDelta(&delta)
	return DB.Clauses(clause.OnConflict{
		Columns: pdepUsageBucketConflictColumns(),
		DoUpdates: clause.Assignments(map[string]interface{}{
			"token_used":    gorm.Expr("token_used + ?", delta.TokenUsed),
			"quota_used":    gorm.Expr("quota_used + ?", delta.QuotaUsed),
			"request_count": gorm.Expr("request_count + ?", delta.RequestCount),
			"updated_at":    delta.UpdatedAt,
		}),
	}).Create(&delta).Error
}

func pdepUsageBucketConflictColumns() []clause.Column {
	return []clause.Column{
		{Name: "owner_id"},
		{Name: "token_id"},
		{Name: "bucket_start"},
	}
}

func preparePDEPUsageBucketDelta(delta *PDEPTokenUsageBucket) {
	now := time.Now().Unix()
	if delta.BucketStart == 0 {
		delta.BucketStart = pdepUsageBucketStart(now)
	} else {
		delta.BucketStart = pdepUsageBucketStart(delta.BucketStart)
	}
	if delta.CreatedAt == 0 {
		delta.CreatedAt = now
	}
	delta.UpdatedAt = now
}

func buildPDEPUsageBucketUpsertSQL(db *gorm.DB, delta PDEPTokenUsageBucket) (string, []interface{}, error) {
	preparePDEPUsageBucketDelta(&delta)
	dry := db.Session(&gorm.Session{DryRun: true, SkipDefaultTransaction: true})
	const callbackName = "pdep_usage_bucket_capture_sql"
	var sql string
	var vars []interface{}
	dry.Callback().Create().After("gorm:after_create").Register(callbackName, func(tx *gorm.DB) {
		if sql == "" {
			sql = tx.Statement.SQL.String()
			vars = append([]interface{}{}, tx.Statement.Vars...)
		}
	})
	err := dry.Clauses(clause.OnConflict{
		Columns: pdepUsageBucketConflictColumns(),
		DoUpdates: clause.Assignments(map[string]interface{}{
			"token_used":    gorm.Expr("token_used + ?", delta.TokenUsed),
			"quota_used":    gorm.Expr("quota_used + ?", delta.QuotaUsed),
			"request_count": gorm.Expr("request_count + ?", delta.RequestCount),
			"updated_at":    delta.UpdatedAt,
		}),
	}).Create(&delta).Error
	dry.Callback().Create().Remove(callbackName)
	if sql == "" {
		dry.Statement.Build("INSERT")
		sql = dry.Statement.SQL.String()
		vars = append([]interface{}{}, dry.Statement.Vars...)
	}
	return sql, vars, err
}

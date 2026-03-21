package model

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
)

func pdepUsagePendingSetKey() string {
	return "pdep:usage:pending"
}

func pdepUsageHotKey(bucketStart int64, ownerID int, tokenID int) string {
	return fmt.Sprintf("pdep:usage:%d:%d:%d:hot", bucketStart, ownerID, tokenID)
}

// AccumulatePDEPUsageBucket writes per-owner/token usage deltas to Redis for the 10-minute bucket
// and registers the hot key in the pending set for later background processing.
func AccumulatePDEPUsageBucket(ownerID, tokenID int, createdAt int64, tokenUsed int, quotaUsed int) error {
	if ownerID <= 0 || tokenID <= 0 {
		return nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	if createdAt <= 0 {
		createdAt = common.GetTimestamp()
	}
	bucketStart := pdepUsageBucketStart(createdAt)
	key := pdepUsageHotKey(bucketStart, ownerID, tokenID)
	ctx := context.Background()
	pipe := common.RDB.TxPipeline()
	pipe.HIncrBy(ctx, key, "token_used", int64(tokenUsed))
	pipe.HIncrBy(ctx, key, "quota_used", int64(quotaUsed))
	pipe.HIncrBy(ctx, key, "request_count", 1)
	pipe.SAdd(ctx, pdepUsagePendingSetKey(), key)
	if ttl := common.PDEPUsageBucketRedisTTLSeconds; ttl > 0 {
		expiration := time.Duration(ttl) * time.Second
		pipe.Expire(ctx, key, expiration)
		pipe.Expire(ctx, pdepUsagePendingSetKey(), expiration)
	}
	_, err := pipe.Exec(ctx)
	return err
}

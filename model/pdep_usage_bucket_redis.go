package model

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

func pdepUsagePendingSetKey() string {
	return "pdep:usage:pending"
}

func pdepUsageHotKey(bucketStart int64, ownerID int, tokenID int) string {
	return fmt.Sprintf("pdep:usage:%d:%d:%d:hot", bucketStart, ownerID, tokenID)
}

func pdepUsageProcessingKey(bucketStart int64, ownerID int, tokenID int) string {
	return fmt.Sprintf("pdep:usage:%d:%d:%d:processing", bucketStart, ownerID, tokenID)
}

func pdepUsageFlushLockKey() string {
	return "pdep:usage:flush:lock"
}

func parsePDEPUsageHotLikeKey(key string) (bucketStart int64, ownerID int, tokenID int, suffix string, err error) {
	parts := strings.Split(key, ":")
	// pdep:usage:{bucketStart}:{ownerID}:{tokenID}:{hot|processing}
	if len(parts) != 6 || parts[0] != "pdep" || parts[1] != "usage" {
		return 0, 0, 0, "", fmt.Errorf("invalid pdep usage key: %q", key)
	}
	bucketStart, err = strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("invalid bucketStart in key %q: %w", key, err)
	}
	ownerID64, err := strconv.ParseInt(parts[3], 10, 0)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("invalid ownerID in key %q: %w", key, err)
	}
	tokenID64, err := strconv.ParseInt(parts[4], 10, 0)
	if err != nil {
		return 0, 0, 0, "", fmt.Errorf("invalid tokenID in key %q: %w", key, err)
	}
	suffix = parts[5]
	if suffix != "hot" && suffix != "processing" {
		return 0, 0, 0, "", fmt.Errorf("invalid suffix in key %q: %q", key, suffix)
	}
	return bucketStart, int(ownerID64), int(tokenID64), suffix, nil
}

func isRedisNoSuchKeyErr(err error) bool {
	if err == nil {
		return false
	}
	// go-redis generally returns a regular error for RENAME* when src doesn't exist.
	if err == redis.Nil {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "no such key")
}

func releaseRedisLock(ctx context.Context, lockKey, lockVal string) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	// Safe unlock: delete only if value matches.
	const lua = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`
	_, _ = common.RDB.Eval(ctx, lua, []string{lockKey}, lockVal).Result()
}

func refreshRedisLock(ctx context.Context, lockKey, lockVal string, ttl time.Duration) (bool, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return false, nil
	}
	if ttl <= 0 {
		return false, fmt.Errorf("invalid lock ttl")
	}
	const lua = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`
	ms := ttl.Milliseconds()
	if ms <= 0 {
		ms = 1
	}
	res, err := common.RDB.Eval(ctx, lua, []string{lockKey}, lockVal, ms).Int64()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

var pdepUsageRefreshLockFunc = refreshRedisLock

func ensurePDEPFlushLock(ctx context.Context, lockKey, lockVal string, ttl time.Duration, lockLost *atomic.Bool) error {
	if lockLost != nil && lockLost.Load() {
		return fmt.Errorf("pdep usage flush lock lost")
	}
	ok, err := pdepUsageRefreshLockFunc(ctx, lockKey, lockVal, ttl)
	if err != nil {
		if lockLost != nil {
			lockLost.Store(true)
		}
		return err
	}
	if !ok {
		if lockLost != nil {
			lockLost.Store(true)
		}
		return fmt.Errorf("pdep usage flush lock lost")
	}
	return nil
}

func finalizePDEPUsageProcessing(ctx context.Context, pendingKey, hotKey, processingKey string) error {
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	// DEL processing; if hot exists => keep pending (SADD); else remove pending (SREM).
	const lua = `
redis.call("DEL", KEYS[1])
if redis.call("EXISTS", KEYS[2]) == 1 then
  redis.call("SADD", KEYS[3], KEYS[2])
else
  redis.call("SREM", KEYS[3], KEYS[2])
end
return 1
`
	_, err := common.RDB.Eval(ctx, lua, []string{processingKey, hotKey, pendingKey}).Result()
	return err
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

// FlushPDEPUsageBucketsOnce drains pending Redis usage bucket hashes into DB rows.
// It is safe to call from multiple instances due to a global Redis lock.
func FlushPDEPUsageBucketsOnce(ctx context.Context) (int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	intervalSec := common.PDEPUsageBucketFlushIntervalSeconds
	if intervalSec <= 0 {
		intervalSec = 60
	}
	lockTTL := time.Duration(intervalSec*3) * time.Second
	if lockTTL < 30*time.Second {
		lockTTL = 30 * time.Second
	}
	if lockTTL > 10*time.Minute {
		lockTTL = 10 * time.Minute
	}

	lockKey := pdepUsageFlushLockKey()
	lockVal := strconv.FormatInt(time.Now().UnixNano(), 10)
	acquired, err := common.RDB.SetNX(ctx, lockKey, lockVal, lockTTL).Result()
	if err != nil || !acquired {
		return 0, err
	}
	defer releaseRedisLock(ctx, lockKey, lockVal)

	var lockLost atomic.Bool
	{
		refreshEvery := lockTTL / 3
		if refreshEvery < 5*time.Second {
			refreshEvery = 5 * time.Second
		}
		if refreshEvery > 30*time.Second {
			refreshEvery = 30 * time.Second
		}
		done := make(chan struct{})
		defer close(done)
		ticker := time.NewTicker(refreshEvery)
		defer ticker.Stop()
		go func() {
			for {
				select {
				case <-ticker.C:
					ok, err := pdepUsageRefreshLockFunc(ctx, lockKey, lockVal, lockTTL)
					if err != nil || !ok {
						lockLost.Store(true)
						return
					}
				case <-done:
					return
				}
			}
		}()
	}

	var flushed int64
	pendingKey := pdepUsagePendingSetKey()
	var cursor uint64
	for {
		keys, next, err := common.RDB.SScan(ctx, pendingKey, cursor, "*", 256).Result()
		if err != nil {
			return flushed, err
		}
		for _, hotKey := range keys {
			if err := ensurePDEPFlushLock(ctx, lockKey, lockVal, lockTTL, &lockLost); err != nil {
				return flushed, err
			}

			bucketStart, ownerID, tokenID, suffix, err := parsePDEPUsageHotLikeKey(hotKey)
			if err != nil {
				// Bad key format: drop it to avoid infinite retry.
				_, _ = common.RDB.SRem(ctx, pendingKey, hotKey).Result()
				continue
			}
			if suffix != "hot" {
				// Pending set only accepts :hot members; clean up anything else.
				_, _ = common.RDB.SRem(ctx, pendingKey, hotKey).Result()
				continue
			}
			processingKey := pdepUsageProcessingKey(bucketStart, ownerID, tokenID)

			// Atomically switch :hot -> :processing.
			renamed, err := common.RDB.RenameNX(ctx, hotKey, processingKey).Result()
			if err != nil {
				if isRedisNoSuchKeyErr(err) {
					// If hot is missing but processing exists (previous partial flush), process processing.
					exists, exErr := common.RDB.Exists(ctx, processingKey).Result()
					if exErr != nil {
						return flushed, exErr
					}
					if exists == 0 {
						_, _ = common.RDB.SRem(ctx, pendingKey, hotKey).Result()
						continue
					}
				} else {
					return flushed, err
				}
			}
			if err == nil && !renamed {
				// processing key already exists (e.g. previous crash). We can still try to flush it.
				exists, exErr := common.RDB.Exists(ctx, processingKey).Result()
				if exErr != nil {
					return flushed, exErr
				}
				if exists == 0 {
					// Nothing to do; keep pending for later hot increments.
					continue
				}
			}

			values, err := common.RDB.HGetAll(ctx, processingKey).Result()
			if err != nil {
				return flushed, err
			}
			if err := ensurePDEPFlushLock(ctx, lockKey, lockVal, lockTTL, &lockLost); err != nil {
				return flushed, err
			}
			if len(values) == 0 {
				// processing missing or empty: clean up pending only when both hot/processing are gone.
				processingExists, exErr := common.RDB.Exists(ctx, processingKey).Result()
				if exErr != nil {
					return flushed, exErr
				}
				if processingExists == 0 {
					hotExists, exErr := common.RDB.Exists(ctx, hotKey).Result()
					if exErr != nil {
						return flushed, exErr
					}
					if hotExists == 0 {
						_, _ = common.RDB.SRem(ctx, pendingKey, hotKey).Result()
					}
					continue
				}
				// Empty hash: drop it.
				if err := ensurePDEPFlushLock(ctx, lockKey, lockVal, lockTTL, &lockLost); err != nil {
					return flushed, err
				}
				if err := finalizePDEPUsageProcessing(ctx, pendingKey, hotKey, processingKey); err != nil {
					return flushed, err
				}
				continue
			}

			tokenUsed, parseErr := strconv.ParseInt(values["token_used"], 10, 64)
			if parseErr != nil && values["token_used"] != "" {
				// Data corruption: keep processing key for manual inspection/retry; don't drop pending.
				return flushed, parseErr
			}
			quotaUsed, parseErr := strconv.ParseInt(values["quota_used"], 10, 64)
			if parseErr != nil && values["quota_used"] != "" {
				return flushed, parseErr
			}
			requestCount, parseErr := strconv.ParseInt(values["request_count"], 10, 64)
			if parseErr != nil && values["request_count"] != "" {
				return flushed, parseErr
			}

			if tokenUsed == 0 && quotaUsed == 0 && requestCount == 0 {
				if err := ensurePDEPFlushLock(ctx, lockKey, lockVal, lockTTL, &lockLost); err != nil {
					return flushed, err
				}
				if err := finalizePDEPUsageProcessing(ctx, pendingKey, hotKey, processingKey); err != nil {
					return flushed, err
				}
				continue
			}

			if err := ensurePDEPFlushLock(ctx, lockKey, lockVal, lockTTL, &lockLost); err != nil {
				return flushed, err
			}
			err = upsertPDEPUsageBucket(PDEPTokenUsageBucket{
				OwnerID:      ownerID,
				TokenID:      tokenID,
				BucketStart:  bucketStart,
				TokenUsed:    tokenUsed,
				QuotaUsed:    quotaUsed,
				RequestCount: requestCount,
			})
			if err != nil {
				// DB error must not be swallowed; leave processing+pending for retry.
				return flushed, err
			}
			if err := ensurePDEPFlushLock(ctx, lockKey, lockVal, lockTTL, &lockLost); err != nil {
				return flushed, err
			}
			if err := finalizePDEPUsageProcessing(ctx, pendingKey, hotKey, processingKey); err != nil {
				return flushed, err
			}
			flushed++
		}

		cursor = next
		if cursor == 0 {
			break
		}
	}

	return flushed, nil
}

// DeleteExpiredPDEPUsageBuckets removes rows with bucket_start older than retention.
func DeleteExpiredPDEPUsageBuckets(nowUnix int64, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	if nowUnix <= 0 {
		nowUnix = time.Now().Unix()
	}
	cutoff := nowUnix - int64(retentionDays)*24*3600
	res := DB.Where("bucket_start < ?", cutoff).Delete(&PDEPTokenUsageBucket{})
	return res.RowsAffected, res.Error
}

func StartPDEPUsageBucketFlushTask() {
	intervalSec := common.PDEPUsageBucketFlushIntervalSeconds
	if intervalSec <= 0 {
		intervalSec = 60
	}
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	go func() {
		defer ticker.Stop()
		ctx := context.Background()
		for {
			_, err := FlushPDEPUsageBucketsOnce(ctx)
			if err != nil {
				common.SysError(fmt.Sprintf("FlushPDEPUsageBucketsOnce failed: %v", err))
			}
			<-ticker.C
		}
	}()
}

func StartPDEPUsageBucketCleanupTask() {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		defer ticker.Stop()
		for {
			_, err := DeleteExpiredPDEPUsageBuckets(time.Now().Unix(), common.PDEPUsageBucketRetentionDays)
			if err != nil {
				common.SysError(fmt.Sprintf("DeleteExpiredPDEPUsageBuckets failed: %v", err))
			}
			<-ticker.C
		}
	}()
}

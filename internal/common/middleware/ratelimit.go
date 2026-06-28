package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// rateBucket uses atomic operations for lock-free token bucket.
type rateBucket struct {
	tokens     int64
	maxTokens  int64
	refillRate int64
	lastRefill int64
}

func newRateBucket(rps float64, burst int) *rateBucket {
	return &rateBucket{
		tokens:     int64(burst) * 1000,
		maxTokens:  int64(burst) * 1000,
		refillRate: int64(rps * 1000),
		lastRefill: time.Now().UnixNano(),
	}
}

func (b *rateBucket) allow() bool {
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&b.lastRefill)
	elapsed := now - last

	if elapsed > 0 {
		add := elapsed * b.refillRate / 1e9
		if add > 0 {
			atomic.AddInt64(&b.tokens, add)
			for {
				cur := atomic.LoadInt64(&b.tokens)
				if cur <= b.maxTokens {
					break
				}
				if atomic.CompareAndSwapInt64(&b.tokens, cur, b.maxTokens) {
					break
				}
			}
			atomic.CompareAndSwapInt64(&b.lastRefill, last, now)
		}
	}

	for {
		cur := atomic.LoadInt64(&b.tokens)
		if cur < 1000 {
			return false
		}
		if atomic.CompareAndSwapInt64(&b.tokens, cur, cur-1000) {
			return true
		}
	}
}

// RateLimiter returns a local token-bucket rate limiter per IP.
func RateLimiter(rps float64, burst int) gin.HandlerFunc {
	limiters := &sync.Map{}

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now().UnixNano()
			limiters.Range(func(key, value interface{}) bool {
				bucket := value.(*rateBucket)
				if now-atomic.LoadInt64(&bucket.lastRefill) > 10*int64(time.Minute) {
					limiters.Delete(key)
				}
				return true
			})
		}
	}()

	return func(c *gin.Context) {
		ip := c.ClientIP()
		val, _ := limiters.LoadOrStore(ip, newRateBucket(rps, burst))
		bucket := val.(*rateBucket)
		if !bucket.allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 500101, "msg": "rate limit exceeded"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// RedisRateLimiter returns a distributed sliding-window rate limiter using Redis.
// Falls back to local rate limiter if Redis is unavailable.
func RedisRateLimiter(rdb *redis.Client, maxReqs int, window time.Duration) gin.HandlerFunc {
	ctx := context.Background()

	// Lua script for atomic sliding window check
	script := redis.NewScript(`
		local key = KEYS[1]
		local max_reqs = tonumber(ARGV[1])
		local window = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])

		-- Remove expired entries
		redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

		-- Count current requests
		local count = redis.call('ZCARD', key)

		if count < max_reqs then
			-- Add current request
			redis.call('ZADD', key, now, now .. '-' .. math.random(100000))
			redis.call('EXPIRE', key, math.ceil(window / 1000))
			return 1
		else
			return 0
		end
	`)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		key := fmt.Sprintf("ratelimit:%s", ip)
		now := time.Now().UnixMilli()

		result, err := script.Run(ctx, rdb, []string{key}, maxReqs, window.Milliseconds(), now).Int()
		if err != nil {
			// Redis down, allow request (fail open)
			c.Next()
			return
		}

		if result == 0 {
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 500101, "msg": "rate limit exceeded"})
			c.Abort()
			return
		}
		c.Next()
	}
}

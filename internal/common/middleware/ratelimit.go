package middleware

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// rateBucket is a mutex-protected token bucket for per-IP rate limiting.
// Previously used lock-free atomic operations, but the refill+consume logic
// was not atomic across goroutines — multiple goroutines each added tokens
// based on the same lastRefill timestamp, effectively multiplying the refill
// rate by the concurrency level and making the limiter ineffective.
type rateBucket struct {
	mu         sync.Mutex
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
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now().UnixNano()
	elapsed := now - b.lastRefill
	if elapsed > 0 {
		add := elapsed * b.refillRate / 1e9
		b.tokens += add
		if b.tokens > b.maxTokens {
			b.tokens = b.maxTokens
		}
		b.lastRefill = now
	}

	if b.tokens < 1000 {
		return false
	}
	b.tokens -= 1000
	return true
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
				bucket.mu.Lock()
				last := bucket.lastRefill
				bucket.mu.Unlock()
				if now-last > 10*int64(time.Minute) {
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

package service

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/aqi/qlink-server/internal/common/constant"
)

// TestInvalidateRedirectCache verifies the MQ consumer removes the negative
// placeholder written while its async DB insert was in flight. Without this,
// a newly created link stays 403/404 until the cache TTL expires.
func TestInvalidateRedirectCache(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	svc := &ShortLinkService{rdb: rdb}
	ctx := t.Context()
	code := "0abcdef0"
	key := constant.FormatShortLinkCacheKey(code)

	if err := rdb.HSet(ctx, key, "nf", "1").Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	if n := rdb.Exists(ctx, key).Val(); n != 1 {
		t.Fatalf("cache key missing before invalidate, exists=%d", n)
	}

	svc.invalidateRedirectCache(code)

	if n := rdb.Exists(ctx, key).Val(); n != 0 {
		t.Errorf("cache key still exists after invalidate, exists=%d", n)
	}
}

// TestInvalidateRedirectCache_NilRedis guards the deployment case where Redis
// is disabled: the MQ write must still succeed without panicking.
func TestInvalidateRedirectCache_NilRedis(t *testing.T) {
	svc := &ShortLinkService{rdb: nil}
	svc.invalidateRedirectCache("0abcdef0") // must not panic
}

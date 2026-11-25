package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/link/sharding"
)

// dispatchFixture builds a Dispatch harness with 3 in-memory SQLite shards
// (mirroring ds0/ds1/dsa) and miniredis. Kafka is nil so sendVisitLog is a
// no-op; no network or external service is touched.
type dispatchFixture struct {
	ctrl *LinkApiController
	rdb  *redis.Client
	mr   *miniredis.Miniredis
	dbs  []*gorm.DB
}

func newDispatchFixture(t *testing.T) *dispatchFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	dbs := make([]*gorm.DB, 0, len(sharding.DBPrefixList))
	for range sharding.DBPrefixList {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("raw db: %v", err)
		}
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sqlDB.Close() })

		for _, suffix := range sharding.TableSuffixList {
			table := sharding.GetTableName("short_link", suffix)
			if err := db.Exec(`CREATE TABLE ` + table + ` (
				id INTEGER PRIMARY KEY,
				code TEXT NOT NULL,
				original_url TEXT,
				password TEXT,
				state TEXT,
				del INTEGER DEFAULT 0,
				account_no INTEGER
			)`).Error; err != nil {
				t.Fatalf("create %s: %v", table, err)
			}
		}
		dbs = append(dbs, db)
	}

	return &dispatchFixture{
		ctrl: NewLinkApiController(dbs, nil, rdb),
		rdb:  rdb,
		mr:   mr,
		dbs:  dbs,
	}
}

func (f *dispatchFixture) request(code string) *httptest.ResponseRecorder {
	r := gin.New()
	r.GET("/:shortLinkCode", f.ctrl.Dispatch)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/"+code, nil)
	r.ServeHTTP(w, req)
	return w
}

// insertLink writes a row into the shard that RouteShortLink resolves to.
func (f *dispatchFixture) insertLink(t *testing.T, code, url string) {
	t.Helper()
	prefix, suffix := sharding.RouteShortLink(code)
	idx := sharding.GetDBIndexByPrefix(prefix)
	table := sharding.GetTableName("short_link", suffix)
	if err := f.dbs[idx].Exec(`INSERT INTO `+table+` (id, code, original_url, state, del, account_no) VALUES (?, ?, ?, 'ACTIVE', 0, 1)`,
		time.Now().UnixNano(), code, url).Error; err != nil {
		t.Fatalf("insert %s.%s: %v", prefix, table, err)
	}
}

// TestDispatch_InvalidShardCodeReturns404 verifies a crawler-style code that
// would route to a non-existent table is rejected before any DB query and
// never leaks a 500.
func TestDispatch_InvalidShardCodeReturns404(t *testing.T) {
	f := newDispatchFixture(t)
	for _, code := range []string{"NotExistCode123", "Abcdefb", "z3z3JYk0", "0a"} {
		t.Run(code, func(t *testing.T) {
			w := f.request(code)
			if w.Code != http.StatusNotFound {
				t.Errorf("GET /%s = %d, want 404 (must not be 500)", code, w.Code)
			}
		})
	}
}

// TestDispatch_NegativeCacheUsesDedicatedField verifies the "not found"
// placeholder no longer reuses del=1 (which made freshly created links answer
// 403), and that its TTL is short.
func TestDispatch_NegativeCacheUsesDedicatedField(t *testing.T) {
	f := newDispatchFixture(t)
	code := "0abcdef0" // valid shard: db 0, table 0

	w := f.request(code)
	if w.Code != http.StatusNotFound {
		t.Fatalf("first lookup = %d, want 404", w.Code)
	}

	key := constant.FormatShortLinkCacheKey(code)
	if got := f.rdb.HGet(t.Context(), key, negativeCacheField).Val(); got != "1" {
		t.Errorf("cache nf = %q, want 1", got)
	}
	if got := f.rdb.HGet(t.Context(), key, "del").Val(); got == "1" {
		t.Error("negative cache must NOT set del=1 (that means deleted/locked)")
	}
	ttl := f.rdb.TTL(t.Context(), key).Val()
	if ttl <= 0 || ttl > negativeCacheTTL {
		t.Errorf("negative cache TTL = %v, want (0, %v]", ttl, negativeCacheTTL)
	}

	// A cached miss must answer 404, not 403.
	if w2 := f.request(code); w2.Code != http.StatusNotFound {
		t.Errorf("cached miss = %d, want 404", w2.Code)
	}
}

// TestDispatch_MQWriteInvalidatesNegativeCache is the regression for
// "create then share immediately -> 403 for a minute": a miss is cached, then
// the async writer lands the row and invalidates the placeholder, and the very
// next request must resolve the link.
func TestDispatch_MQWriteInvalidatesNegativeCache(t *testing.T) {
	f := newDispatchFixture(t)
	code := "0abcdefa" // valid shard: db 0, table a

	// 1. Visitor arrives before the MQ write lands -> negative placeholder.
	if w := f.request(code); w.Code != http.StatusNotFound {
		t.Fatalf("pre-write lookup = %d, want 404", w.Code)
	}
	key := constant.FormatShortLinkCacheKey(code)
	if f.rdb.HGet(t.Context(), key, negativeCacheField).Val() != "1" {
		t.Fatal("expected negative cache entry")
	}

	// 2. MQ consumer lands the row and drops the placeholder (mirrors
	//    ShortLinkService.invalidateRedirectCache).
	f.insertLink(t, code, "https://example.com")
	if err := f.rdb.Del(t.Context(), key).Err(); err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	// 3. Next request must now redirect instead of 403/404.
	w := f.request(code)
	if w.Code != http.StatusFound {
		t.Fatalf("post-write lookup = %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://example.com" {
		t.Errorf("Location = %q, want https://example.com", loc)
	}
}

// TestDispatch_RealDeleteStillForbidden guards against over-correcting: a
// genuinely deleted/locked link must still answer 403.
func TestDispatch_RealDeleteStillForbidden(t *testing.T) {
	f := newDispatchFixture(t)
	code := "0abcde00"
	key := constant.FormatShortLinkCacheKey(code)
	f.rdb.HSet(t.Context(), key, "del", "1", "state", "LOCK", "url", "https://example.com")
	if w := f.request(code); w.Code != http.StatusForbidden {
		t.Errorf("deleted link = %d, want 403", w.Code)
	}
}

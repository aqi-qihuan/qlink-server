package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	accountmodel "github.com/aqi/qlink-server/internal/account/model"
)

// Test infrastructure: in-memory SQLite (pure Go, no CGO) + miniredis.
// RabbitMQ is always nil in tests (production code guards with `if rmq != nil`).

// newTestDB opens a fresh in-memory SQLite database. A single pooled
// connection keeps the private :memory: schema visible to every GORM call.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("raw db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

// newTrafficDBs builds the 2-datasource sharded layout (traffic_0/traffic_1
// + traffic_task), mirroring the production account_no%2 split.
func newTrafficDBs(t *testing.T) []*gorm.DB {
	t.Helper()
	dbs := make([]*gorm.DB, 2)
	for i := 0; i < 2; i++ {
		db := newTestDB(t)
		if err := db.Table(timeTableName(i)).AutoMigrate(&accountmodel.TrafficDO{}); err != nil {
			t.Fatalf("migrate traffic_%d: %v", i, err)
		}
		if err := db.AutoMigrate(&accountmodel.TrafficTaskDO{}); err != nil {
			t.Fatalf("migrate traffic_task: %v", err)
		}
		dbs[i] = db
	}
	return dbs
}

func timeTableName(idx int) string {
	if idx == 0 {
		return "traffic_0"
	}
	return "traffic_1"
}

// newAccountDB builds the aqicloud_account schema (account table).
func newAccountDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&accountmodel.AccountDO{}); err != nil {
		t.Fatalf("migrate account: %v", err)
	}
	return db
}

// newTestRedis starts miniredis and returns a connected client.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// newShopServer mocks the shop service GET /api/product/v1/detail/:id.
func newShopServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedPack inserts a traffic pack and force-sets gmt_modified afterwards
// (GORM autoUpdateTime would otherwise overwrite it with "now", which the
// day_used-reset branch depends on).
func seedPack(t *testing.T, db *gorm.DB, tableName string, p accountmodel.TrafficDO) accountmodel.TrafficDO {
	t.Helper()
	if err := db.Table(tableName).Create(&p).Error; err != nil {
		t.Fatalf("seed pack: %v", err)
	}
	if !p.GmtModified.IsZero() {
		if err := db.Exec("UPDATE "+tableName+" SET gmt_modified = ? WHERE id = ?", p.GmtModified, p.ID).Error; err != nil {
			t.Fatalf("force gmt_modified: %v", err)
		}
	}
	return p
}

func packOf(accountNo int64, dayLimit, dayUsed int, expired time.Time) accountmodel.TrafficDO {
	return accountmodel.TrafficDO{
		DayLimit:    dayLimit,
		DayUsed:     dayUsed,
		AccountNo:   accountNo,
		OutTradeNo:  "test-order",
		Level:       "FIRST",
		ExpiredDate: &expired,
		PluginType:  "short_link",
		ProductID:   1,
	}
}

func tomorrow() time.Time {
	return time.Now().AddDate(0, 0, 1)
}

func yesterday() time.Time {
	return time.Now().AddDate(0, 0, -1)
}

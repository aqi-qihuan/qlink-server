package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aqi/qlink-server/internal/data/vo"
)

// fakeCHConn implements driver.Conn by embedding the interface (unexpected
// methods panic) and routing Query/QueryRow to test-supplied handlers that
// dispatch on the SQL text. This keeps the service-level tests focused on
// orchestration, parameter passing and result assembly — the SQL itself is
// validated by the PV full-chain regression against real ClickHouse.
type fakeCHConn struct {
	driver.Conn
	queryRow func(query string, args ...any) driver.Row
	query    func(query string, args ...any) (driver.Rows, error)
}

func (c *fakeCHConn) QueryRow(_ context.Context, q string, args ...any) driver.Row {
	return c.queryRow(q, args...)
}

func (c *fakeCHConn) Query(_ context.Context, q string, args ...any) (driver.Rows, error) {
	return c.query(q, args...)
}

// fakeRows serves a fixed set of rows with per-column type coercion.
// Implements clickhouse-go v2.48 lib/driver.Rows (note: Next() bool).
type fakeRows struct {
	cols []string
	rows [][]any
	idx  int
}

func newFakeRows(cols []string, rows ...[]any) *fakeRows {
	return &fakeRows{cols: cols, rows: rows}
}

func (r *fakeRows) Columns() []string             { return r.cols }
func (r *fakeRows) ColumnTypes() []driver.ColumnType { return nil }
func (r *fakeRows) Close() error                  { return nil }
func (r *fakeRows) Err() error                    { return nil }
func (r *fakeRows) Totals(_ ...any) error         { return nil }
func (r *fakeRows) HasData() bool                 { return true }
func (r *fakeRows) Next() bool                    { r.idx++; return r.idx <= len(r.rows) }
func (r *fakeRows) Scan(dest ...any) error {
	if r.idx < 1 || r.idx > len(r.rows) {
		return fmt.Errorf("fakeRows: scan beyond data")
	}
	return assignRow(r.rows[r.idx-1], dest)
}

func (r *fakeRows) ScanStruct(_ any) error { return fmt.Errorf("fakeRows: ScanStruct not supported") }

// fakeRow implements driver.Row (single row) for QueryRow.
type fakeRow struct {
	row []any
}

func (r *fakeRow) Err() error             { return nil }
func (r *fakeRow) Scan(dest ...any) error { return assignRow(r.row, dest) }
func (r *fakeRow) ScanStruct(_ any) error { return fmt.Errorf("fakeRow: ScanStruct not supported") }

func assignRow(row []any, dest []any) error {
	for i, d := range dest {
		src := row[i]
		switch ptr := d.(type) {
		case *string:
			if s, ok := src.(string); ok {
				*ptr = s
			} else {
				*ptr = fmt.Sprintf("%v", src)
			}
		case *int64:
			*ptr = toI64(src)
		case *int:
			*ptr = int(toI64(src))
		case *uint64:
			*ptr = uint64(toI64(src))
		case *time.Time:
			if t, ok := src.(time.Time); ok {
				*ptr = t
			}
		default:
			return fmt.Errorf("fakeRows: unsupported scan target %T", d)
		}
	}
	return nil
}

func toI64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint64:
		return int64(n)
	case uint32:
		return int64(n)
	default:
		return 0
	}
}

func newDataRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// emptyRow answers a single all-zero row (CH aggregate semantics).
var emptyRow = func() driver.Row {
	return &fakeRow{row: []any{0}}
}

// TestPageRecord_QueryLimit: the page*size guard rejects oversized scans
// before any ClickHouse call is made.
func TestPageRecord_QueryLimit(t *testing.T) {
	conn := &fakeCHConn{
		queryRow: func(_ string, _ ...any) driver.Row {
			t.Fatal("QueryRow must not be called when the limit guard rejects")
			return nil
		},
		query: func(_ string, _ ...any) (driver.Rows, error) {
			t.Fatal("Query must not be called when the limit guard rejects")
			return nil, nil
		},
	}
	svc := NewVisitStatsService(conn, nil)

	_, err := svc.PageRecord(1, "a3z3JYk0", 101, 10) // 101*10 > 1000
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query limit exceeded")
}

// TestPageRecord_Pagination verifies pagination math and that the LIMIT
// offset/size parameters are forwarded to ClickHouse.
func TestPageRecord_Pagination(t *testing.T) {
	var gotFrom, gotSize int64
	conn := &fakeCHConn{
		queryRow: func(_ string, _ ...any) driver.Row {
			return &fakeRow{row: []any{25}} // total
		},
		query: func(q string, args ...interface{}) (driver.Rows, error) {
			// args: accountNo, code, from, size
			gotFrom, gotSize = toI64(args[2]), toI64(args[3])
			return newFakeRows(
				[]string{"code", "referer", "ts", "is_new", "account_no", "province", "city", "ip", "browser_name", "os", "device_type", "start_time"},
				[]interface{}{"a3z3JYk0", "", int64(1790610519943), 1, int64(100), "GD", "SZ", "1.2.3.4", "Chrome", "Windows", "COMPUTER", time.Now()},
			), nil
		},
	}
	svc := NewVisitStatsService(conn, nil)

	page, err := svc.PageRecord(100, "a3z3JYk0", 3, 10)
	require.NoError(t, err)

	assert.Equal(t, int64(25), page.Total)
	assert.Equal(t, 3, page.TotalPage, "25 records / size 10 → 3 pages")
	assert.Equal(t, 3, page.CurrentPage)
	require.Len(t, page.Data, 1)
	assert.Equal(t, "a3z3JYk0", page.Data[0].Code)
	assert.Equal(t, "Chrome", page.Data[0].BrowserName)
	assert.NotEmpty(t, page.Data[0].StartTime, "start_time must be formatted to string")
	assert.Equal(t, int64(20), gotFrom, "LIMIT offset = (page-1)*size")
	assert.Equal(t, int64(10), gotSize)
}

// TestTrend_UnsupportedType: unknown granularity is rejected up front.
func TestTrend_UnsupportedType(t *testing.T) {
	conn := &fakeCHConn{query: func(string, ...interface{}) (driver.Rows, error) {
		t.Fatal("no query expected")
		return nil, nil
	}}
	svc := NewVisitStatsService(conn, nil)

	_, err := svc.Trend(1, "c1", "SECOND", "20260901", "20260930")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported trend type")
}

// TestDeviceInfo_UnsupportedField: unknown field is rejected up front.
func TestDeviceInfo_UnsupportedField(t *testing.T) {
	conn := &fakeCHConn{query: func(string, ...interface{}) (driver.Rows, error) {
		t.Fatal("no query expected")
		return nil, nil
	}}
	svc := NewVisitStatsService(conn, nil)

	_, err := svc.DeviceInfo(1, "c1", "gpu")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported device field")
}

// TestDeviceInfo_Combined: empty field returns the {device, os} combined
// response, mapping the two group-by queries into their VO shapes.
func TestDeviceInfo_Combined(t *testing.T) {
	conn := &fakeCHConn{
		query: func(q string, args ...interface{}) (driver.Rows, error) {
			switch {
			case contains(q, "device_type"):
				return newFakeRows([]string{"d", "pv"},
					[]interface{}{"COMPUTER", int64(7)},
					[]interface{}{"MOBILE", int64(3)}), nil
			case contains(q, "GROUP BY os"):
				return newFakeRows([]string{"o", "pv"},
					[]interface{}{"Windows", int64(9)},
					[]interface{}{"iOS", int64(1)}), nil
			default:
				return nil, fmt.Errorf("unexpected query: %s", q)
			}
		},
	}
	svc := NewVisitStatsService(conn, nil)

	res, err := svc.DeviceInfo(1, "c1", "")
	require.NoError(t, err)

	combined, ok := res.(*vo.DeviceInfoResponseVO)
	require.True(t, ok, "empty field must return combined response")
	require.Len(t, combined.Device, 2)
	assert.Equal(t, "COMPUTER", combined.Device[0].DeviceType)
	assert.Equal(t, int64(7), combined.Device[0].PVCount)
	require.Len(t, combined.OS, 2)
	assert.Equal(t, "Windows", combined.OS[0].OS)
}

// TestGetDashboard_CacheHit: a cached dashboard payload is returned without
// touching ClickHouse.
func TestGetDashboard_CacheHit(t *testing.T) {
	conn := &fakeCHConn{
		queryRow: func(_ string, _ ...any) driver.Row {
			t.Fatal("ClickHouse must not be queried on cache hit")
			return nil
		},
		query: func(_ string, _ ...any) (driver.Rows, error) {
			t.Fatal("ClickHouse must not be queried on cache hit")
			return nil, nil
		},
	}
	rdb := newDataRedis(t)
	svc := NewVisitStatsService(conn, rdb)

	cached := `{"totalPv":100,"totalUv":42,"totalIpCount":9,"todayPv":7,"todayUv":3,"todayNewUv":1,"topLinks":[{"code":"a","pvCount":5}]}`
	require.NoError(t, rdb.Set(context.Background(), "data:dashboard:55:20260901:20260930", cached, 0).Err())

	d, err := svc.GetDashboard(55, "20260901", "20260930")
	require.NoError(t, err)
	assert.Equal(t, int64(100), d.TotalPV)
	assert.Equal(t, int64(42), d.TotalUV)
	require.Len(t, d.TopLinks, 1)
	assert.Equal(t, "a", d.TopLinks[0].Code)
}

// TestGetDashboard_DefaultRangeAndAssembly: empty time range defaults to
// the last 30 days; the four CH calls are assembled into one DashboardVO
// and the result lands in the Redis cache.
func TestGetDashboard_DefaultRangeAndAssembly(t *testing.T) {
	var rangeArgs []any
	conn := &fakeCHConn{
		queryRow: func(q string, args ...any) driver.Row {
			switch {
			case contains(q, "count(DISTINCT ip)"):
				rangeArgs = args
				return &fakeRow{row: []any{int64(500), int64(200), int64(33)}}
			case contains(q, "is_new=1"):
				return &fakeRow{row: []any{int64(50), int64(20), int64(5)}}
			default:
				return emptyRow()
			}
		},
		query: func(q string, args ...interface{}) (driver.Rows, error) {
			switch {
			case contains(q, "GROUP BY code"):
				return newFakeRows([]string{"code", "pv"},
					[]interface{}{"top1", int64(80)},
					[]interface{}{"top2", int64(20)}), nil
			case contains(q, "GROUP BY dt"):
				return newFakeRows([]string{"dt", "pv", "uv"},
					[]interface{}{"20260929", int64(10), int64(4)}), nil
			default:
				return nil, fmt.Errorf("unexpected query: %s", q)
			}
		},
	}
	rdb := newDataRedis(t)
	svc := NewVisitStatsService(conn, rdb)

	d, err := svc.GetDashboard(55, "", "")
	require.NoError(t, err)

	// Default range applied: start = 30 days ago, end = today
	require.Len(t, rangeArgs, 3)
	assert.Equal(t, int64(55), toI64(rangeArgs[0]))
	wantStart := time.Now().AddDate(0, 0, -30).Format("20060102")
	wantEnd := time.Now().Format("20060102")
	assert.Equal(t, wantStart, rangeArgs[1])
	assert.Equal(t, wantEnd, rangeArgs[2])

	// Assembled from the four fake queries
	assert.Equal(t, int64(500), d.TotalPV)
	assert.Equal(t, int64(33), d.TotalIPCount)
	assert.Equal(t, int64(50), d.TodayPV)
	assert.Equal(t, int64(5), d.TodayNewUV)
	require.Len(t, d.TopLinks, 2)
	assert.Equal(t, "top1", d.TopLinks[0].Code)
	require.Len(t, d.DailyTrend, 1)
	assert.Equal(t, "20260929", d.DailyTrend[0].Date)

	// Result cached under the default-range key
	cached, err := rdb.Get(context.Background(),
		fmt.Sprintf("data:dashboard:55:%s:%s", wantStart, wantEnd)).Result()
	require.NoError(t, err)
	assert.Contains(t, cached, `"totalPv":500`)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

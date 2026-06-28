package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/aqi/qlink-server/internal/data/vo"
	"github.com/redis/go-redis/v9"
)

type VisitStatsService struct {
	conn driver.Conn
	rdb  *redis.Client
}

func NewVisitStatsService(conn driver.Conn, rdb *redis.Client) *VisitStatsService {
	return &VisitStatsService{conn: conn, rdb: rdb}
}

// PageRecord returns paginated visit records.
func (s *VisitStatsService) PageRecord(accountNo int64, code string, page, size int) (*vo.VisitRecordPageVO, error) {
	if page*size > 1000 {
		return nil, fmt.Errorf("query limit exceeded")
	}

	ctx := context.Background()
	var total uint64
	err := s.conn.QueryRow(ctx, "SELECT count(1) FROM visit_stats WHERE account_no = $1 AND code = $2", accountNo, code).Scan(&total)
	if err != nil {
		return nil, err
	}

	from := (page - 1) * size
	rows, err := s.conn.Query(ctx,
		`SELECT code, referer, ts, is_new, account_no, province, city, ip, browser_name, os, device_type, start_time
		 FROM visit_stats WHERE account_no = $1 AND code = $2
		 ORDER BY ts DESC LIMIT $3, $4`,
		accountNo, code, from, size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []vo.VisitRecordVO
	for rows.Next() {
		var r vo.VisitRecordVO
		var startTime time.Time
		if err := rows.Scan(&r.Code, &r.Referer, &r.VisitTime, &r.IsNew, &r.AccountNo,
			&r.Province, &r.City, &r.IP, &r.BrowserName, &r.OS, &r.DeviceType, &startTime); err != nil {
			return nil, err
		}
		r.StartTime = startTime.Format("2006-01-02 15:04:05")
		list = append(list, r)
	}

	totalPage := int(math.Ceil(float64(total) / float64(size)))
	return &vo.VisitRecordPageVO{
		Total:       int64(total),
		CurrentPage: page,
		TotalPage:   totalPage,
		Data:        list,
	}, nil
}

// RegionDay returns region distribution for a date range.
func (s *VisitStatsService) RegionDay(accountNo int64, code, startTime, endTime string) ([]vo.RegionDayVO, error) {
	ctx := context.Background()
	rows, err := s.conn.Query(ctx,
		`SELECT province, city, sum(pv) AS pv_count, sum(uv) AS uv_count, count(DISTINCT ip) AS ip_count
		 FROM visit_stats WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN toYYYYMMDD(parseDateTimeBestEffort($3)) AND toYYYYMMDD(parseDateTimeBestEffort($4))
		 GROUP BY province, city ORDER BY pv_count DESC`,
		accountNo, code, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []vo.RegionDayVO
	for rows.Next() {
		var r vo.RegionDayVO
		if err := rows.Scan(&r.Province, &r.City, &r.PVCount, &r.UVCount, &r.IPCount); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, nil
}

// Trend returns visit trend data with DAY/HOUR/MINUTE granularity.
func (s *VisitStatsService) Trend(accountNo int64, code, trendType, startTime, endTime string) ([]vo.VisitTrendVO, error) {
	var query string
	var args []interface{}
	ctx := context.Background()

	trendType = strings.ToUpper(trendType)
	switch trendType {
	case "DAY":
		query = `SELECT toYYYYMMDD(start_time) AS dt,
				 sum(if(is_new = 1, uv, 0)) AS new_uv, sum(uv) AS uv_count,
				 sum(pv) AS pv_count, count(DISTINCT ip) AS ip_count
				 FROM visit_stats WHERE account_no = $1 AND code = $2
				 AND toYYYYMMDD(start_time) BETWEEN toYYYYMMDD(parseDateTimeBestEffort($3)) AND toYYYYMMDD(parseDateTimeBestEffort($4))
				 GROUP BY dt ORDER BY dt ASC`
		args = []interface{}{accountNo, code, startTime, endTime}
	case "HOUR":
		query = `SELECT toString(toHour(start_time)) AS dt,
				 sum(if(is_new = 1, uv, 0)) AS new_uv, sum(uv) AS uv_count,
				 sum(pv) AS pv_count, count(DISTINCT ip) AS ip_count
				 FROM visit_stats WHERE account_no = $1 AND code = $2
				 AND toYYYYMMDD(start_time) = toYYYYMMDD(parseDateTimeBestEffort($3))
				 GROUP BY dt ORDER BY dt ASC`
		args = []interface{}{accountNo, code, startTime}
	case "MINUTE":
		query = `SELECT toString(toMinute(start_time)) AS dt,
				 sum(if(is_new = 1, uv, 0)) AS new_uv, sum(uv) AS uv_count,
				 sum(pv) AS pv_count, count(DISTINCT ip) AS ip_count
				 FROM visit_stats WHERE account_no = $1 AND code = $2
				 AND toYYYYMMDDhhmmss(start_time) BETWEEN $3 AND $4
				 GROUP BY dt ORDER BY dt ASC`
		args = []interface{}{accountNo, code, startTime, endTime}
	case "WEEK":
		query = `SELECT concat(toString(toYear(start_time)), toString(toISOWeek(start_time))) AS dt,
				 sum(if(is_new = 1, uv, 0)) AS new_uv, sum(uv) AS uv_count,
				 sum(pv) AS pv_count, count(DISTINCT ip) AS ip_count
				 FROM visit_stats WHERE account_no = $1 AND code = $2
				 AND toYYYYMMDD(start_time) BETWEEN toYYYYMMDD(parseDateTimeBestEffort($3)) AND toYYYYMMDD(parseDateTimeBestEffort($4))
				 GROUP BY dt ORDER BY dt ASC`
		args = []interface{}{accountNo, code, startTime, endTime}
	default:
		return nil, fmt.Errorf("unsupported trend type: %s", trendType)
	}

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []vo.VisitTrendVO
	for rows.Next() {
		var r vo.VisitTrendVO
		if err := rows.Scan(&r.DateTimeStr, &r.NewUVCount, &r.UVCount, &r.PVCount, &r.IPCount); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, nil
}

// FrequentIP returns top IPs by visit count.
func (s *VisitStatsService) FrequentIP(accountNo int64, code string) ([]vo.FrequentItemVO, error) {
	return s.frequentQuery(accountNo, code, "ip")
}

// FrequentReferer returns top referrers by visit count.
func (s *VisitStatsService) FrequentReferer(accountNo int64, code string) ([]vo.RefererItemVO, error) {
	query := `SELECT referer, sum(pv) AS pv_count FROM visit_stats
			 WHERE account_no = $1 AND code = $2
			 GROUP BY referer ORDER BY pv_count DESC LIMIT 10`

	ctx := context.Background()
	rows, err := s.conn.Query(ctx, query, accountNo, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []vo.RefererItemVO
	for rows.Next() {
		var r vo.RefererItemVO
		if err := rows.Scan(&r.Referer, &r.PVCount); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, nil
}

func (s *VisitStatsService) frequentQuery(accountNo int64, code, field string) ([]vo.FrequentItemVO, error) {
	query := fmt.Sprintf(
		`SELECT %s, sum(pv) AS pv_count FROM visit_stats
		 WHERE account_no = $1 AND code = $2
		 GROUP BY %s ORDER BY pv_count DESC LIMIT 10`, field, field)

	ctx := context.Background()
	rows, err := s.conn.Query(ctx, query, accountNo, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []vo.FrequentItemVO
	for rows.Next() {
		var r vo.FrequentItemVO
		if err := rows.Scan(&r.Name, &r.PVCount); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, nil
}

// DeviceInfo returns device distribution. When field is empty, returns combined
// {device:[], os:[]} response; otherwise returns a flat array for the given field.
func (s *VisitStatsService) DeviceInfo(accountNo int64, code, field string) (interface{}, error) {
	ctx := context.Background()

	if field == "" {
		// Combined response: frontend expects {device: [{pvCount, deviceType}], os: [{pvCount, os}]}
		deviceList, err := s.queryDeviceField(ctx, accountNo, code, "device_type")
		if err != nil {
			return nil, err
		}
		osList, err := s.queryDeviceField(ctx, accountNo, code, "os")
		if err != nil {
			return nil, err
		}

		devices := make([]vo.DeviceItemVO, len(deviceList))
		for i, item := range deviceList {
			devices[i] = vo.DeviceItemVO{Name: item.Name, PVCount: item.PVCount, DeviceType: item.Name}
		}
		osItems := make([]vo.DeviceItemVO, len(osList))
		for i, item := range osList {
			osItems[i] = vo.DeviceItemVO{Name: item.Name, PVCount: item.PVCount, OS: item.Name}
		}

		return &vo.DeviceInfoResponseVO{Device: devices, OS: osItems}, nil
	}

	// Single field query (legacy support)
	var groupCol string
	switch field {
	case "os":
		groupCol = "os"
	case "browser", "browser_name":
		groupCol = "browser_name"
	case "device", "device_type":
		groupCol = "device_type"
	default:
		return nil, fmt.Errorf("unsupported device field: %s", field)
	}

	return s.queryDeviceField(ctx, accountNo, code, groupCol)
}

func (s *VisitStatsService) queryDeviceField(ctx context.Context, accountNo int64, code, column string) ([]vo.DeviceInfoVO, error) {
	query := fmt.Sprintf(
		`SELECT %s, sum(pv) AS pv_count FROM visit_stats
		 WHERE account_no = $1 AND code = $2
		 GROUP BY %s ORDER BY pv_count DESC`, column, column)

	rows, err := s.conn.Query(ctx, query, accountNo, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []vo.DeviceInfoVO
	for rows.Next() {
		var r vo.DeviceInfoVO
		if err := rows.Scan(&r.Name, &r.PVCount); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, nil
}

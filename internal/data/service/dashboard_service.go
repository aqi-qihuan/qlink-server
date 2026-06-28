package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aqi/qlink-server/internal/data/vo"
)

const dashboardCacheTTL = 60 * time.Second

// GetDashboard returns aggregated dashboard stats for a user account.
// Results are cached in Redis for 60 seconds to reduce ClickHouse load.
func (s *VisitStatsService) GetDashboard(accountNo int64, startTime, endTime string) (*vo.DashboardVO, error) {
	ctx := context.Background()

	// Check Redis cache first
	cacheKey := fmt.Sprintf("data:dashboard:%d:%s:%s", accountNo, startTime, endTime)
	if s.rdb != nil {
		cached, err := s.rdb.Get(ctx, cacheKey).Result()
		if err == nil && cached != "" {
			var d vo.DashboardVO
			if json.Unmarshal([]byte(cached), &d) == nil {
				return &d, nil
			}
		}
	}

	// Default range: last 30 days
	if startTime == "" {
		startTime = time.Now().AddDate(0, 0, -30).Format("20060102")
	}
	if endTime == "" {
		endTime = time.Now().Format("20060102")
	}
	today := time.Now().Format("20060102")

	d := &vo.DashboardVO{}

	// Total PV/UV/IP in range
	err := s.conn.QueryRow(ctx,
		`SELECT toInt64(sum(pv)), toInt64(sum(uv)), toInt64(count(DISTINCT ip))
		 FROM visit_stats WHERE account_no = $1
		 AND toYYYYMMDD(start_time) BETWEEN $2 AND $3`,
		accountNo, startTime, endTime).Scan(&d.TotalPV, &d.TotalUV, &d.TotalIPCount)
	if err != nil {
		return nil, fmt.Errorf("dashboard total stats: %w", err)
	}

	// Today's stats
	err = s.conn.QueryRow(ctx,
		`SELECT toInt64(sum(pv)), toInt64(sum(uv)), toInt64(sum(if(is_new=1, uv, 0)))
		 FROM visit_stats WHERE account_no = $1 AND toYYYYMMDD(start_time) = $2`,
		accountNo, today).Scan(&d.TodayPV, &d.TodayUV, &d.TodayNewUV)
	if err != nil {
		return nil, fmt.Errorf("dashboard today stats: %w", err)
	}

	// Top 10 links by PV
	rows, err := s.conn.Query(ctx,
		`SELECT code, toInt64(sum(pv)) AS pv_count FROM visit_stats
		 WHERE account_no = $1
		 AND toYYYYMMDD(start_time) BETWEEN $2 AND $3
		 GROUP BY code ORDER BY pv_count DESC LIMIT 10`,
		accountNo, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("dashboard top links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var t vo.TopLinkVO
		if err := rows.Scan(&t.Code, &t.PVCount); err != nil {
			return nil, err
		}
		d.TopLinks = append(d.TopLinks, t)
	}

	// Daily trend (last 7 days within range for chart)
	trendStart := time.Now().AddDate(0, 0, -7).Format("20060102")
	trendEnd := endTime
	trendRows, err := s.conn.Query(ctx,
		`SELECT toYYYYMMDD(start_time) AS dt, toInt64(sum(pv)) AS pv_count, toInt64(sum(uv)) AS uv_count
		 FROM visit_stats WHERE account_no = $1
		 AND toYYYYMMDD(start_time) BETWEEN $2 AND $3
		 GROUP BY dt ORDER BY dt ASC`,
		accountNo, trendStart, trendEnd)
	if err != nil {
		// Non-fatal: trend is bonus data
		return d, nil
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var t vo.DashboardTrendVO
		if err := trendRows.Scan(&t.Date, &t.PVCount, &t.UVCount); err != nil {
			return nil, err
		}
		d.DailyTrend = append(d.DailyTrend, t)
	}

	// Cache the result in Redis
	if s.rdb != nil {
		if data, err := json.Marshal(d); err == nil {
			s.rdb.Set(ctx, cacheKey, string(data), dashboardCacheTTL)
		}
	}

	return d, nil
}

// GetAnalysis returns aggregated analysis for a single short link.
func (s *VisitStatsService) GetAnalysis(accountNo int64, code, startTime, endTime string) (*vo.AnalysisVO, error) {
	ctx := context.Background()

	if startTime == "" {
		startTime = time.Now().AddDate(0, 0, -30).Format("20060102")
	}
	if endTime == "" {
		endTime = time.Now().Format("20060102")
	}

	a := &vo.AnalysisVO{Code: code}

	// Total PV/UV/NewUV
	err := s.conn.QueryRow(ctx,
		`SELECT toInt64(sum(pv)), toInt64(sum(uv)), toInt64(sum(if(is_new=1, uv, 0)))
		 FROM visit_stats WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4`,
		accountNo, code, startTime, endTime).Scan(&a.TotalPV, &a.TotalUV, &a.NewUV)
	if err != nil {
		return nil, fmt.Errorf("analysis total: %w", err)
	}

	// Top referrers
	refRows, err := s.conn.Query(ctx,
		`SELECT referer, toInt64(sum(pv)) AS pv_count FROM visit_stats
		 WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 GROUP BY referer ORDER BY pv_count DESC LIMIT 10`,
		accountNo, code, startTime, endTime)
	if err == nil {
		defer refRows.Close()
		for refRows.Next() {
			var r vo.RefererItemVO
			if err := refRows.Scan(&r.Referer, &r.PVCount); err != nil {
				return nil, err
			}
			a.TopReferers = append(a.TopReferers, r)
		}
	}

	// Regions
	regRows, err := s.conn.Query(ctx,
		`SELECT province, city, toInt64(sum(pv)) AS pv_count, toInt64(sum(uv)) AS uv_count, toInt64(count(DISTINCT ip)) AS ip_count
		 FROM visit_stats WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 GROUP BY province, city ORDER BY pv_count DESC`,
		accountNo, code, startTime, endTime)
	if err == nil {
		defer regRows.Close()
		for regRows.Next() {
			var r vo.RegionDayVO
			if err := regRows.Scan(&r.Province, &r.City, &r.PVCount, &r.UVCount, &r.IPCount); err != nil {
				return nil, err
			}
			a.Regions = append(a.Regions, r)
		}
	}

	// Device distribution
	devRows, err := s.conn.Query(ctx,
		`SELECT device_type, toInt64(sum(pv)) AS pv_count FROM visit_stats
		 WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 GROUP BY device_type ORDER BY pv_count DESC`,
		accountNo, code, startTime, endTime)
	if err == nil {
		defer devRows.Close()
		for devRows.Next() {
			var d vo.DeviceItemVO
			if err := devRows.Scan(&d.Name, &d.PVCount); err != nil {
				return nil, err
			}
			d.DeviceType = d.Name
			a.Devices = append(a.Devices, d)
		}
	}

	// OS
	osRows, err := s.conn.Query(ctx,
		`SELECT os, toInt64(sum(pv)) AS pv_count FROM visit_stats
		 WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 GROUP BY os ORDER BY pv_count DESC`,
		accountNo, code, startTime, endTime)
	if err == nil {
		defer osRows.Close()
		for osRows.Next() {
			var o vo.DeviceItemVO
			if err := osRows.Scan(&o.Name, &o.PVCount); err != nil {
				return nil, err
			}
			o.OS = o.Name
			a.OSList = append(a.OSList, o)
		}
	}

	// Browsers
	brRows, err := s.conn.Query(ctx,
		`SELECT browser_name, toInt64(sum(pv)) AS pv_count FROM visit_stats
		 WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 GROUP BY browser_name ORDER BY pv_count DESC`,
		accountNo, code, startTime, endTime)
	if err == nil {
		defer brRows.Close()
		for brRows.Next() {
			var b vo.DeviceItemVO
			if err := brRows.Scan(&b.Name, &b.PVCount); err != nil {
				return nil, err
			}
			b.Browser = b.Name
			a.Browsers = append(a.Browsers, b)
		}
	}

	// Daily trend
	trendRows, err := s.conn.Query(ctx,
		`SELECT toYYYYMMDD(start_time) AS dt,
		 toInt64(sum(if(is_new=1, uv, 0))) AS new_uv, toInt64(sum(uv)) AS uv_count,
		 toInt64(sum(pv)) AS pv_count, toInt64(count(DISTINCT ip)) AS ip_count
		 FROM visit_stats WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 GROUP BY dt ORDER BY dt ASC`,
		accountNo, code, startTime, endTime)
	if err == nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var t vo.VisitTrendVO
			if err := trendRows.Scan(&t.DateTimeStr, &t.NewUVCount, &t.UVCount, &t.PVCount, &t.IPCount); err != nil {
				return nil, err
			}
			a.DailyTrend = append(a.DailyTrend, t)
		}
	}

	return a, nil
}

// ExportCSV returns all visit records as CSV content for a code.
func (s *VisitStatsService) ExportCSV(accountNo int64, code, startTime, endTime string) ([]string, [][]string, error) {
	ctx := context.Background()

	header := []string{"code", "referer", "visit_time", "is_new", "ip", "province", "city",
		"browser", "os", "device", "pv", "uv"}

	rows, err := s.conn.Query(ctx,
		`SELECT code, referer, toString(start_time) AS visit_time, is_new, ip, province, city,
		 browser_name, os, device_type, pv, uv
		 FROM visit_stats WHERE account_no = $1 AND code = $2
		 AND toYYYYMMDD(start_time) BETWEEN $3 AND $4
		 ORDER BY start_time DESC LIMIT 10000`,
		accountNo, code, startTime, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("export query: %w", err)
	}
	defer rows.Close()

	var data [][]string
	for rows.Next() {
		var code, referer, visitTime, ip, province, city, browser, oss, device string
		var isNew, pv, uv int32
		if err := rows.Scan(&code, &referer, &visitTime, &isNew, &ip, &province, &city,
			&browser, &oss, &device, &pv, &uv); err != nil {
			return nil, nil, err
		}
		data = append(data, []string{
			code, referer, visitTime, fmt.Sprintf("%d", isNew), ip, province, city,
			browser, oss, device, fmt.Sprintf("%d", pv), fmt.Sprintf("%d", uv),
		})
	}

	return header, data, nil
}

// GetGeo returns map-ready geo data (province → pv count).
func (s *VisitStatsService) GetGeo(accountNo int64, code, startTime, endTime string) ([]vo.GeoVO, error) {
	ctx := context.Background()

	if startTime == "" {
		startTime = time.Now().AddDate(0, 0, -30).Format("20060102")
	}
	if endTime == "" {
		endTime = time.Now().Format("20060102")
	}

	query := `SELECT province, toInt64(sum(pv)) AS pv_count FROM visit_stats
		 WHERE account_no = $1
		 AND toYYYYMMDD(start_time) BETWEEN $2 AND $3`
	args := []interface{}{accountNo, startTime, endTime}

	if code != "" {
		query += ` AND code = $4 GROUP BY province ORDER BY pv_count DESC`
		args = append(args, code)
	} else {
		query += ` GROUP BY province ORDER BY pv_count DESC`
	}

	rows, err := s.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("geo query: %w", err)
	}
	defer rows.Close()

	var list []vo.GeoVO
	for rows.Next() {
		var g vo.GeoVO
		if err := rows.Scan(&g.Name, &g.Value); err != nil {
			return nil, err
		}
		list = append(list, g)
	}
	return list, nil
}

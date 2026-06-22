package streamer

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/redis/go-redis/v9"
)

// Config holds streamer pipeline configuration.
type Config struct {
	KafkaBrokers string
	AmapAPIKey   string
}

// Pipeline orchestrates all 4 streaming stages.
type Pipeline struct {
	cfg    *Config
	rdb    *redis.Client
	chConn clickhouse.Conn
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewPipeline(cfg *Config, rdb *redis.Client, chConn clickhouse.Conn) *Pipeline {
	return &Pipeline{cfg: cfg, rdb: rdb, chConn: chConn}
}

func (p *Pipeline) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	jobs := []struct {
		name string
		run  func(ctx context.Context)
		stop func()
	}{
		{"DWD", nil, nil},
		{"DWM-Wide", nil, nil},
		{"DWM-UV", nil, nil},
		{"DWS", nil, nil},
	}

	// DWD
	dwd := NewDWDJob(p.cfg.KafkaBrokers, p.rdb)
	jobs[0].run = dwd.Run
	jobs[0].stop = dwd.Stop

	// DWM-Wide
	dwmWide := NewDWMWideJob(p.cfg.KafkaBrokers, p.cfg.AmapAPIKey)
	jobs[1].run = dwmWide.Run
	jobs[1].stop = dwmWide.Stop

	// DWM-UV
	dwmUV := NewDWMUVJob(p.cfg.KafkaBrokers, p.rdb)
	jobs[2].run = dwmUV.Run
	jobs[2].stop = dwmUV.Stop

	// DWS
	dws := NewDWSJob(p.cfg.KafkaBrokers, p.chConn)
	jobs[3].run = dws.Run
	jobs[3].stop = dws.Stop

	for _, job := range jobs {
		p.wg.Add(1)
		go func(name string, run func(ctx context.Context)) {
			defer p.wg.Done()
			run(ctx)
		}(job.name, job.run)
	}

	log.Printf("[Pipeline] %d stages started", len(jobs))
}

func (p *Pipeline) Stop() {
	log.Println("[Pipeline] Stopping all stages...")
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	log.Println("[Pipeline] All stages stopped")
}

// timezone for date formatting
var tz = time.UTC

func SetTimezone(loc *time.Location) {
	tz = loc
}

// FormatDate formats epoch milliseconds to "yyyy-MM-dd".
func FormatDate(tsMillis int64) string {
	t := time.UnixMilli(tsMillis).In(tz)
	return t.Format("2006-01-02")
}

// FormatDateTime formats time.Time to "yyyy-MM-dd HH:mm:ss".
func FormatDateTime(t time.Time) string {
	return t.In(tz).Format("2006-01-02 15:04:05")
}

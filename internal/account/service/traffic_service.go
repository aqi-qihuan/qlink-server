package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	accountmodel "github.com/aqi/qlink-server/internal/account/model"
	"github.com/aqi/qlink-server/internal/account/request"
	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Traffic sharding: account_no % 2 -> traffic_0 or traffic_1
func getTrafficDBIndex(accountNo int64) int {
	return int(accountNo%2) & 0x7FFFFFFF
}

func getTrafficTableName(accountNo int64) string {
	return fmt.Sprintf("traffic_%d", getTrafficDBIndex(accountNo))
}

type TrafficService struct {
	dbs            []*gorm.DB // 2 datasources: traffic_0, traffic_1
	rdb            *redis.Client
	rmq            *mq.RabbitMQ
	shopServiceURL string // e.g. "http://localhost:8005"
	accountDB      *gorm.DB // aqicloud_account DB for auth upgrade
}

func NewTrafficService(dbs []*gorm.DB, rdb *redis.Client, rmq *mq.RabbitMQ, shopServiceURL string, accountDB *gorm.DB) *TrafficService {
	return &TrafficService{dbs: dbs, rdb: rdb, rmq: rmq, shopServiceURL: shopServiceURL, accountDB: accountDB}
}

// Reduce handles the traffic deduction RPC call from the link service.
func (s *TrafficService) Reduce(req *request.UseTrafficRequest) error {
	accountNo := req.AccountNo
	dbIdx := getTrafficDBIndex(accountNo)
	tableName := getTrafficTableName(accountNo)
	db := s.dbs[dbIdx]
	ctx := context.Background()

	// 1. Get all active traffic packs for this account
	var packs []accountmodel.TrafficDO
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	if err := db.Table(tableName).
		Where("account_no = ? AND expired_date >= ?", accountNo, today).
		Find(&packs).Error; err != nil {
		return fmt.Errorf("query traffic packs failed: %w", err)
	}

	if len(packs) == 0 {
		return fmt.Errorf("no available traffic pack")
	}

	// 2. Separate un-updated (day_used not reset today) and already-updated packs
	var unupdated []accountmodel.TrafficDO
	var updated []accountmodel.TrafficDO
	for _, p := range packs {
		if p.GmtModified.Before(today) {
			unupdated = append(unupdated, p)
		} else {
			updated = append(updated, p)
		}
	}

	// 3. Batch reset day_used for un-updated packs
	if len(unupdated) > 0 {
		ids := make([]int64, len(unupdated))
		for i, p := range unupdated {
			ids[i] = p.ID
		}
		db.Table(tableName).Where("id IN ?", ids).Update("day_used", 0)
	}

	// 4. Calculate total remaining across all packs
	dayTotalLeft := 0
	for _, p := range packs {
		remaining := p.DayLimit - p.DayUsed
		if remaining > 0 {
			dayTotalLeft += remaining
		}
	}
	if dayTotalLeft <= 0 {
		return fmt.Errorf("traffic exhausted")
	}

	// 5. Select a pack with remaining capacity and increment day_used
	var selectedPack *accountmodel.TrafficDO
	for i := range packs {
		remaining := packs[i].DayLimit - packs[i].DayUsed
		if remaining > 0 {
			selectedPack = &packs[i]
			break
		}
	}
	if selectedPack == nil {
		return fmt.Errorf("no pack with remaining capacity")
	}

	result := db.Table(tableName).
		Where("id = ? AND account_no = ? AND (day_limit - day_used) >= 1", selectedPack.ID, accountNo).
		Update("day_used", gorm.Expr("day_used + 1"))
	if result.Error != nil {
		return fmt.Errorf("increment day_used failed: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("concurrent update failed, retry")
	}

	// 6. Insert traffic task record
	task := accountmodel.TrafficTaskDO{
		AccountNo: accountNo,
		TrafficID: selectedPack.ID,
		UseTimes:  1,
		LockState: string(enums.TASK_LOCK),
		BizID:     req.BizID,
	}
	if err := db.Create(&task).Error; err != nil {
		return fmt.Errorf("create traffic task failed: %w", err)
	}

	// 7. Update Redis cache
	remainKey := constant.FormatDayTotalTrafficKey(accountNo)
	remaining := dayTotalLeft - 1
	secondsToday := util.GetRemainSecondsToday()
	s.rdb.Set(ctx, remainKey, remaining, time.Duration(secondsToday)*time.Second)

	// 8. Send delayed message for rollback verification (60s)
	if s.rmq != nil {
		eventMsg := model.EventMessage{
			MessageId:        util.GenerateUUID(),
			EventMessageType: string(enums.TRAFFIC_USED),
			BizId:            fmt.Sprintf("%d", task.ID),
			AccountNo:        accountNo,
		}
		if err := s.rmq.PublishJSON("traffic.event.exchange", "traffic.release.delay.routing.key", eventMsg); err != nil {
			log.Printf("[MQ] publish TRAFFIC_USED delay error: %v", err)
		}
	}

	return nil
}

// Page returns paginated traffic packs.
func (s *TrafficService) Page(accountNo int64, page, size int) ([]accountmodel.TrafficDO, int64, error) {
	dbIdx := getTrafficDBIndex(accountNo)
	tableName := getTrafficTableName(accountNo)
	db := s.dbs[dbIdx]

	var total int64
	db.Table(tableName).Where("account_no = ?", accountNo).Count(&total)

	var packs []accountmodel.TrafficDO
	offset := (page - 1) * size
	if err := db.Table(tableName).
		Where("account_no = ?", accountNo).
		Order("gmt_create DESC").
		Offset(offset).Limit(size).
		Find(&packs).Error; err != nil {
		return nil, 0, err
	}

	return packs, total, nil
}

// Detail returns a single traffic pack.
func (s *TrafficService) Detail(accountNo, trafficId int64) (*accountmodel.TrafficDO, error) {
	dbIdx := getTrafficDBIndex(accountNo)
	tableName := getTrafficTableName(accountNo)
	db := s.dbs[dbIdx]

	var pack accountmodel.TrafficDO
	if err := db.Table(tableName).
		Where("id = ? AND account_no = ?", trafficId, accountNo).
		First(&pack).Error; err != nil {
		return nil, err
	}
	return &pack, nil
}

// DeleteExpireTraffic removes expired traffic packs from both tables.
// This is the Go equivalent of Java's @XxlJob("trafficExpiredHandler").
func (s *TrafficService) DeleteExpireTraffic() {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	for i, db := range s.dbs {
		tableName := fmt.Sprintf("traffic_%d", i)
		result := db.Table(tableName).
			Where("expired_date < ?", today).
			Delete(nil)
		if result.Error != nil {
			log.Printf("[Scheduler] delete expired traffic from %s error: %v", tableName, result.Error)
		} else if result.RowsAffected > 0 {
			log.Printf("[Scheduler] deleted %d expired traffic packs from %s", result.RowsAffected, tableName)
		}
	}
}

// HandleTrafficMessage processes traffic-related MQ events.
func (s *TrafficService) HandleTrafficMessage(eventMsg *model.EventMessage) {
	switch eventMsg.EventMessageType {
	case string(enums.PRODUCT_ORDER_PAY):
		s.handleOrderPay(eventMsg)
	case string(enums.TRAFFIC_FREE_INIT):
		s.handleFreeInit(eventMsg)
	case string(enums.TRAFFIC_USED):
		s.handleTrafficUsed(eventMsg)
	default:
		log.Printf("[MQ] unknown traffic event type: %s", eventMsg.EventMessageType)
	}
}

// handleOrderPay processes PRODUCT_ORDER_PAY: creates a traffic pack from a paid order.
func (s *TrafficService) handleOrderPay(eventMsg *model.EventMessage) {
	type OrderPayContent struct {
		OutTradeNo string `json:"outTradeNo"`
		BuyNum     int    `json:"buyNum"`
		Product    struct {
			DayTimes int    `json:"dayTimes"`
			ValidDay int    `json:"validDay"`
			Level    string `json:"level"`
			ID       int64  `json:"id"`
		} `json:"product"`
	}

	var content OrderPayContent
	if err := json.Unmarshal([]byte(eventMsg.Content), &content); err != nil {
		log.Printf("[MQ] unmarshal order pay content error: %v", err)
		return
	}

	accountNo := eventMsg.AccountNo
	expiry := time.Now().AddDate(0, 0, content.Product.ValidDay)

	traffic := accountmodel.TrafficDO{
		DayLimit:    content.Product.DayTimes * content.BuyNum,
		DayUsed:     0,
		TotalLimit:  0,
		AccountNo:   accountNo,
		OutTradeNo:  content.OutTradeNo,
		Level:       content.Product.Level,
		ExpiredDate: &expiry,
		PluginType:  "short_link",
		ProductID:   content.Product.ID,
	}

	dbIdx := getTrafficDBIndex(accountNo)
	tableName := getTrafficTableName(accountNo)
	if err := s.dbs[dbIdx].Table(tableName).Create(&traffic).Error; err != nil {
		log.Printf("[MQ] insert traffic error: %v", err)
		return
	}

	// Delete Redis cache to force recalculation
	s.rdb.Del(context.Background(), constant.FormatDayTotalTrafficKey(accountNo))
	log.Printf("[MQ] created traffic pack for account %d, outTradeNo=%s", accountNo, content.OutTradeNo)

	// 根据商品等级同步更新用户会员身份 (matching Java TrafficServiceImpl)
	var authLevel string
	switch content.Product.Level {
	case "SECOND":
		authLevel = string(enums.AUTH_GOLD)
	case "THIRD":
		authLevel = string(enums.AUTH_BLACK)
	}
	if authLevel != "" && s.accountDB != nil {
		result := s.accountDB.Table("account").
			Where("account_no = ?", accountNo).
			Update("auth", authLevel)
		if result.Error != nil {
			log.Printf("[MQ] update auth error: %v", result.Error)
		} else {
			log.Printf("[MQ] updated user auth: accountNo=%d, authLevel=%s, rows=%d", accountNo, authLevel, result.RowsAffected)
		}
	}
}

// handleFreeInit processes TRAFFIC_FREE_INIT: creates a free traffic pack on registration.
func (s *TrafficService) handleFreeInit(eventMsg *model.EventMessage) {
	accountNo := eventMsg.AccountNo
	today := time.Now()

	// Free product: 10 links/day, expires today
	traffic := accountmodel.TrafficDO{
		DayLimit:    10,
		DayUsed:     0,
		TotalLimit:  0,
		AccountNo:   accountNo,
		OutTradeNo:  "free_init",
		Level:       string(enums.TRAFFIC_FIRST),
		ExpiredDate: &today,
		PluginType:  "short_link",
		ProductID:   1,
	}

	dbIdx := getTrafficDBIndex(accountNo)
	tableName := getTrafficTableName(accountNo)
	if err := s.dbs[dbIdx].Table(tableName).Create(&traffic).Error; err != nil {
		log.Printf("[MQ] insert free traffic error: %v", err)
		return
	}
	log.Printf("[MQ] created free traffic pack for account %d", accountNo)
}

// handleTrafficUsed processes TRAFFIC_USED: rollback if short link was not created.
// The BizId is the traffic_task ID. If the task is still in LOCK state, it means
// the short link creation failed and we need to rollback the day_used deduction.
func (s *TrafficService) handleTrafficUsed(eventMsg *model.EventMessage) {
	accountNo := eventMsg.AccountNo
	dbIdx := getTrafficDBIndex(accountNo)

	// Find the traffic task
	var task accountmodel.TrafficTaskDO
	if err := s.dbs[dbIdx].Where("id = ? AND account_no = ?", eventMsg.BizId, accountNo).First(&task).Error; err != nil {
		log.Printf("[MQ] traffic task not found: %s, account %d", eventMsg.BizId, accountNo)
		return
	}

	// If task is still LOCK, the short link was not created successfully
	if task.LockState != string(enums.TASK_LOCK) {
		log.Printf("[MQ] traffic task %s already in state %s, skip rollback", eventMsg.BizId, task.LockState)
		return
	}

	// Rollback: decrement day_used on the traffic pack
	tableName := getTrafficTableName(accountNo)
	result := s.dbs[dbIdx].Table(tableName).
		Where("id = ? AND account_no = ? AND day_used > 0", task.TrafficID, accountNo).
		Update("day_used", gorm.Expr("day_used - 1"))
	if result.Error != nil {
		log.Printf("[MQ] rollback day_used failed: %v", result.Error)
		return
	}

	// Update task state to CANCEL
	db := s.dbs[dbIdx]
	db.Table("traffic_task").
		Where("id = ?", task.ID).
		Update("lock_state", string(enums.TASK_CANCEL))

	// Update Redis cache
	remainKey := constant.FormatDayTotalTrafficKey(accountNo)
	s.rdb.Incr(context.Background(), remainKey)

	log.Printf("[MQ] rolled back traffic for task %s, account %d, rows=%d", eventMsg.BizId, accountNo, result.RowsAffected)
}

// ClaimFree lets a logged-in user claim the daily free traffic pack (1 per day).
func (s *TrafficService) ClaimFree(accountNo int64) error {
	ctx := context.Background()
	todayStr := time.Now().Format("2006-01-02")
	claimKey := fmt.Sprintf("traffic:free:claim:%d:%s", accountNo, todayStr)

	// Idempotency: one claim per day per user
	secondsToday := util.GetRemainSecondsToday()
	ok, err := s.rdb.SetNX(ctx, claimKey, "1", time.Duration(secondsToday)*time.Second).Result()
	if err != nil {
		return fmt.Errorf("redis error: %w", err)
	}
	if !ok {
		return fmt.Errorf("今日已领取过免费流量包")
	}

	// Fetch free product (id=1) from shop service
	product, err := s.fetchProduct(1)
	if err != nil {
		s.rdb.Del(ctx, claimKey) // rollback idempotency lock
		return fmt.Errorf("免费流量包商品不存在")
	}

	expiry := time.Now().AddDate(0, 0, product.ValidDay)

	traffic := accountmodel.TrafficDO{
		DayLimit:    product.DayTimes,
		DayUsed:     0,
		TotalLimit:  product.TotalTimes,
		AccountNo:   accountNo,
		OutTradeNo:  "free_init",
		Level:       product.Level,
		ExpiredDate: &expiry,
		PluginType:  product.PluginType,
		ProductID:   product.ID,
	}

	dbIdx := getTrafficDBIndex(accountNo)
	tableName := getTrafficTableName(accountNo)
	if err := s.dbs[dbIdx].Table(tableName).Create(&traffic).Error; err != nil {
		s.rdb.Del(ctx, claimKey) // rollback
		return fmt.Errorf("create free traffic failed: %w", err)
	}

	// Clear daily traffic cache
	s.rdb.Del(ctx, constant.FormatDayTotalTrafficKey(accountNo))

	log.Printf("[ClaimFree] account=%d, product=%d, dayLimit=%d", accountNo, product.ID, product.DayTimes)
	return nil
}

// productInfo mirrors the ProductVO JSON returned by shop service GET /api/product/v1/detail/:id.
type productInfo struct {
	ID         int64  `json:"id"`
	Level      string `json:"level"`
	DayTimes   int    `json:"dayTimes"`
	TotalTimes int    `json:"totalTimes"`
	ValidDay   int    `json:"validDay"`
	PluginType string `json:"pluginType"`
}

// fetchProduct calls the shop service to get product details.
func (s *TrafficService) fetchProduct(productID int64) (*productInfo, error) {
	if s.shopServiceURL == "" {
		return nil, fmt.Errorf("shop service URL not configured")
	}

	url := fmt.Sprintf("%s/api/product/v1/detail/%d", s.shopServiceURL, productID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Code int          `json:"code"`
		Data *productInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if result.Code != 0 || result.Data == nil {
		return nil, fmt.Errorf("product %d not found (code=%d)", productID, result.Code)
	}
	return result.Data, nil
}

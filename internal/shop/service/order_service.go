package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/enums"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/mq"
	"github.com/aqi/qlink-server/internal/common/util"
	shopmodel "github.com/aqi/qlink-server/internal/shop/component"
	shopdb "github.com/aqi/qlink-server/internal/shop/model"
	"github.com/aqi/qlink-server/internal/shop/request"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const orderPayTimeoutMills = 1800000 // 30 minutes

type OrderService struct {
	db        *gorm.DB
	rdb       *redis.Client
	rmq       *mq.RabbitMQ
	payFactory *shopmodel.PayFactory
}

func NewOrderService(db *gorm.DB, rdb *redis.Client, rmq *mq.RabbitMQ, payFactory *shopmodel.PayFactory) *OrderService {
	return &OrderService{db: db, rdb: rdb, rmq: rmq, payFactory: payFactory}
}

// orderTableName returns the sharded table name: product_order_0 or product_order_1
func orderTableName(accountNo int64) string {
	return fmt.Sprintf("product_order_%d", accountNo%2)
}

// GetOrderToken generates an anti-resubmit token.
func (s *OrderService) GetOrderToken(accountNo int64) (string, error) {
	token := util.GetStringNumRandom(32)
	key := constant.FormatSubmitOrderTokenKey(accountNo, token)
	ctx := context.Background()
	if err := s.rdb.Set(ctx, key, "1", 30*time.Minute).Err(); err != nil {
		return "", err
	}
	return token, nil
}

// Confirm creates a new order and initiates payment.
func (s *OrderService) Confirm(accountNo int64, nickname string, req *request.ConfirmOrderRequest) (map[string]interface{}, error) {
	ctx := context.Background()

	// Price validation
	var product shopdb.ProductDO
	if err := s.db.Where("id = ?", req.ProductID).First(&product).Error; err != nil {
		return nil, fmt.Errorf("product not found")
	}
	expectedTotal := product.Amount * float64(req.BuyNum)
	if req.TotalAmount != expectedTotal || req.PayAmount != expectedTotal {
		return nil, fmt.Errorf("price mismatch")
	}

	// Anti-resubmit: TOKEN mode
	key := constant.FormatSubmitOrderTokenKey(accountNo, req.Token)
	deleted, err := s.rdb.Del(ctx, key).Result()
	if err != nil || deleted == 0 {
		return nil, fmt.Errorf("duplicate submission")
	}

	// Create order
	outTradeNo := util.GetStringNumRandom(32)
	snapshot, _ := json.Marshal(product)

	order := shopdb.ProductOrderDO{
		ID:              int64(util.GenerateSnowflakeID()),
		ProductID:       product.ID,
		ProductTitle:    product.Title,
		ProductAmount:   product.Amount,
		ProductSnapshot: string(snapshot),
		BuyNum:          req.BuyNum,
		OutTradeNo:      outTradeNo,
		State:           string(enums.ORDER_NEW),
		CreateTime:      time.Now(),
		TotalAmount:     req.TotalAmount,
		PayAmount:       req.PayAmount,
		PayType:         req.PayType,
		Nickname:        nickname,
		AccountNo:       accountNo,
		Del:             0,
		BillType:        req.BillType,
		BillHeader:      req.BillHeader,
		BillContent:     req.BillContent,
		BillReceiverPhone: req.BillReceiverPhone,
		BillReceiverEmail: req.BillReceiverEmail,
	}

	if err := s.db.Table(orderTableName(accountNo)).Create(&order).Error; err != nil {
		return nil, fmt.Errorf("create order failed: %w", err)
	}

	// Send delay message for order close
	if s.rmq != nil {
		eventMsg := model.EventMessage{
			MessageId:        util.GenerateUUID(),
			EventMessageType: string(enums.PRODUCT_ORDER_NEW),
			BizId:            outTradeNo,
			AccountNo:        accountNo,
		}
		s.rmq.PublishJSON("order.event.exchange", "order.close.delay.routing.key", eventMsg)
	}

	// Initiate payment
	strategy, ok := s.payFactory.GetStrategy(req.PayType)
	if !ok {
		return nil, fmt.Errorf("unsupported pay type: %s", req.PayType)
	}

	payInfo := &shopmodel.PayInfoVO{
		OutTradeNo:        outTradeNo,
		PayFee:            req.PayAmount,
		PayType:           req.PayType,
		ClientType:        req.ClientType,
		Title:             product.Title,
		Description:       product.Detail,
		OrderPayTimeoutMills: orderPayTimeoutMills,
		AccountNo:         accountNo,
	}

	payResult, err := strategy.UnifiedOrder(payInfo)
	if err != nil {
		return nil, fmt.Errorf("create payment failed: %w", err)
	}

	return map[string]interface{}{
		"code_url":    payResult,
		"out_trade_no": outTradeNo,
	}, nil
}

// Page returns paginated orders.
func (s *OrderService) Page(accountNo int64, req *request.ProductOrderPageRequest) (map[string]interface{}, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 {
		req.Size = 10
	}

	tbl := orderTableName(accountNo)
	query := s.db.Table(tbl).Where("account_no = ? AND del = 0", accountNo)
	if req.State != "" {
		query = query.Where("state = ?", req.State)
	}

	var total int64
	query.Count(&total)

	var orders []shopdb.ProductOrderDO
	offset := (req.Page - 1) * req.Size
	query.Order("gmt_create DESC").Offset(offset).Limit(req.Size).Find(&orders)

	totalPage := int(math.Ceil(float64(total) / float64(req.Size)))

	return map[string]interface{}{
		"total_record": total,
		"total_page":   totalPage,
		"current_data": orders,
	}, nil
}

// QueryState returns the order state.
func (s *OrderService) QueryState(accountNo int64, outTradeNo string) (string, error) {
	var order shopdb.ProductOrderDO
	if err := s.db.Table(orderTableName(accountNo)).Where("out_trade_no = ? AND account_no = ?", outTradeNo, accountNo).First(&order).Error; err != nil {
		return "", fmt.Errorf("order not found")
	}
	return order.State, nil
}

// HandleProductOrderMessage processes order MQ events.
func (s *OrderService) HandleProductOrderMessage(eventMsg *model.EventMessage) error {
	switch eventMsg.EventMessageType {
	case string(enums.PRODUCT_ORDER_NEW):
		return s.closeProductOrder(eventMsg)
	case string(enums.PRODUCT_ORDER_PAY):
		return s.updateOrderToPaid(eventMsg)
	default:
		return fmt.Errorf("unknown order event type: %s", eventMsg.EventMessageType)
	}
}

// closeProductOrder handles the delayed order close event.
// Before cancelling, it queries the third-party payment status to avoid cancelling paid orders.
func (s *OrderService) closeProductOrder(eventMsg *model.EventMessage) error {
	tbl := orderTableName(eventMsg.AccountNo)
	var order shopdb.ProductOrderDO
	if err := s.db.Table(tbl).Where("out_trade_no = ? AND state = ?", eventMsg.BizId, string(enums.ORDER_NEW)).First(&order).Error; err != nil {
		log.Printf("[MQ] order not found or already processed: %s", eventMsg.BizId)
		return nil
	}

	// Query payment status from third-party provider
	if s.payFactory != nil {
		if strategy, ok := s.payFactory.GetStrategy(order.PayType); ok {
			payInfo := &shopmodel.PayInfoVO{
				OutTradeNo: order.OutTradeNo,
				PayFee:     order.PayAmount,
				PayType:    order.PayType,
			}
			status, err := strategy.QueryPayStatus(payInfo)
			if err == nil {
				isPaySuccess := false
				if order.PayType == string(enums.PAY_ALI) {
					isPaySuccess = status == "TRADE_SUCCESS" || status == "TRADE_FINISHED"
				} else if order.PayType == string(enums.PAY_WECHAT) {
					isPaySuccess = status != "" && status != "NOTPAY" && status != "CLOSED" && status != "NOT_EXIST"
				}
				if isPaySuccess {
					// Payment was actually completed ??mark as paid instead of cancelled
					s.db.Table(tbl).Where("out_trade_no = ? AND state = ?", order.OutTradeNo, string(enums.ORDER_NEW)).Update("state", string(enums.ORDER_PAY))
					log.Printf("[MQ] order already paid (status=%s), marked as PAY: %s", status, eventMsg.BizId)

					// Send to traffic queue via topic exchange (fans out to both update and traffic queues)
					if s.rmq != nil {
						trafficMsg := model.EventMessage{
							MessageId:        util.GenerateUUID(),
							EventMessageType: string(enums.PRODUCT_ORDER_PAY),
							BizId:            eventMsg.BizId,
							AccountNo:        order.AccountNo,
						}
						s.rmq.PublishJSON("order.event.exchange", "order.update.traffic.routing.key", trafficMsg)
					}
					return nil
				}
			}
		}
	}

	s.db.Table(tbl).Where("out_trade_no = ? AND state = ?", eventMsg.BizId, string(enums.ORDER_NEW)).Update("state", string(enums.ORDER_CANCEL))
	log.Printf("[MQ] cancelled order: %s", eventMsg.BizId)
	return nil
}

// updateOrderToPaid updates order state from NEW to PAY.
func (s *OrderService) updateOrderToPaid(eventMsg *model.EventMessage) error {
	tables := []string{orderTableName(eventMsg.AccountNo)}
	if eventMsg.AccountNo == 0 {
		// AccountNo not provided (e.g. from payment callback), try both tables
		tables = []string{"product_order_0", "product_order_1"}
	}
	for _, tbl := range tables {
		result := s.db.Table(tbl).
			Where("out_trade_no = ? AND state = ?", eventMsg.BizId, string(enums.ORDER_NEW)).
			Update("state", string(enums.ORDER_PAY))
		if result.Error != nil {
			log.Printf("[MQ] update order to paid error on %s: %v", tbl, result.Error)
			continue
		}
		if result.RowsAffected > 0 {
			log.Printf("[MQ] order marked as paid: %s on %s, rows=%d", eventMsg.BizId, tbl, result.RowsAffected)
			return nil
		}
	}
	log.Printf("[MQ] order not found or already processed: %s", eventMsg.BizId)
	return nil
}

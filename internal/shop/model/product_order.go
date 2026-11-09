package model

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// BillType 兼容 Java 侧字符串枚举与 DB int 列。
// JSON/API 层保持字符串（NO_BILL / PAPER_BILL / ELE_BILL），
// 落库时映射为 int（0 / 1 / 2），读取时反向还原。
type BillType string

const (
	BillTypeNone  BillType = "NO_BILL"
	BillTypePaper BillType = "PAPER_BILL"
	BillTypeEle   BillType = "ELE_BILL"
)

// Value 实现 driver.Valuer，落库为 int64。
func (b BillType) Value() (driver.Value, error) {
	switch b {
	case BillTypeNone, "":
		return int64(0), nil
	case BillTypeEle, "1":
		return int64(1), nil
	case BillTypePaper, "2":
		return int64(2), nil
	default:
		return nil, fmt.Errorf("invalid bill type: %s", string(b))
	}
}

// Scan 实现 sql.Scanner，从 int 还原为字符串枚举。
func (b *BillType) Scan(src interface{}) error {
	switch v := src.(type) {
	case int64:
		switch v {
		case 0:
			*b = BillTypeNone
		case 1:
			*b = BillTypeEle
		case 2:
			*b = BillTypePaper
		default:
			return fmt.Errorf("unknown bill type: %d", v)
		}
	case []byte:
		switch string(v) {
		case "0":
			*b = BillTypeNone
		case "1":
			*b = BillTypeEle
		case "2":
			*b = BillTypePaper
		default:
			return fmt.Errorf("unknown bill type: %s", string(v))
		}
	case nil:
		*b = ""
	default:
		return fmt.Errorf("unsupported bill type source: %T", src)
	}
	return nil
}

// ProductOrderDO maps to the product_order table.
type ProductOrderDO struct {
	ID                int64     `gorm:"column:id;primaryKey" json:"id"`
	ProductID         int64     `gorm:"column:product_id" json:"productId"`
	ProductTitle      string    `gorm:"column:product_title" json:"productTitle"`
	ProductAmount     float64   `gorm:"column:product_amount" json:"productAmount"`
	ProductSnapshot   string    `gorm:"column:product_snapshot" json:"productSnapshot"`
	BuyNum            int       `gorm:"column:buy_num" json:"buyNum"`
	OutTradeNo        string    `gorm:"column:out_trade_no" json:"outTradeNo"`
	State             string    `gorm:"column:state" json:"state"`
	CreateTime        time.Time `gorm:"column:create_time" json:"createTime"`
	TotalAmount       float64   `gorm:"column:total_amount" json:"totalAmount"`
	PayAmount         float64   `gorm:"column:pay_amount" json:"payAmount"`
	PayType           string    `gorm:"column:pay_type" json:"payType"`
	Nickname          string    `gorm:"column:nickname" json:"nickname"`
	AccountNo         int64     `gorm:"column:account_no" json:"accountNo"`
	Del               int       `gorm:"column:del" json:"del"`
	GmtModified       time.Time `gorm:"column:gmt_modified;autoUpdateTime" json:"gmtModified"`
	GmtCreate         time.Time `gorm:"column:gmt_create;autoCreateTime" json:"gmtCreate"`
	BillType          BillType  `gorm:"column:bill_type" json:"billType"`
	BillHeader        string    `gorm:"column:bill_header" json:"billHeader"`
	BillContent       string    `gorm:"column:bill_content" json:"billContent"`
	BillReceiverPhone string    `gorm:"column:bill_receiver_phone" json:"billReceiverPhone"`
	BillReceiverEmail string    `gorm:"column:bill_receiver_email" json:"billReceiverEmail"`
}

func (ProductOrderDO) TableName() string {
	return "product_order"
}

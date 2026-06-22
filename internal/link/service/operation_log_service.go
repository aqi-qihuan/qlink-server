package service

import (
	"encoding/json"
	"time"

	"github.com/aqi/qlink-server/internal/link/model"
	"gorm.io/gorm"
)

type OperationLogService struct {
	db *gorm.DB // ds0 only (logs are not sharded)
}

func NewOperationLogService(db *gorm.DB) *OperationLogService {
	return &OperationLogService{db: db}
}

// Record writes an operation log entry.
func (s *OperationLogService) Record(accountNo int64, action, resource, resourceID, ip, userAgent string, details map[string]interface{}) {
	detailsJSON, _ := json.Marshal(details)
	log := model.OperationLogDO{
		AccountNo:  accountNo,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IP:         ip,
		UserAgent:  userAgent,
		Details:    string(detailsJSON),
		GmtCreate:  time.Now(),
	}
	s.db.Create(&log)
}

// Page returns paginated operation logs.
func (s *OperationLogService) Page(accountNo int64, action, resourceID string, startTime, endTime time.Time, page, size int) ([]model.OperationLogDO, int64, error) {
	q := s.db.Model(&model.OperationLogDO{}).Where("account_no = ?", accountNo)
	if action != "" {
		q = q.Where("action = ?", action)
	}
	if resourceID != "" {
		q = q.Where("resource_id = ?", resourceID)
	}
	if !startTime.IsZero() {
		q = q.Where("gmt_create >= ?", startTime)
	}
	if !endTime.IsZero() {
		q = q.Where("gmt_create <= ?", endTime)
	}

	var total int64
	q.Count(&total)

	var list []model.OperationLogDO
	offset := (page - 1) * size
	q.Order("gmt_create DESC").Offset(offset).Limit(size).Find(&list)
	return list, total, nil
}

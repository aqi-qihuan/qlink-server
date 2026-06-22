package service

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/aqi/qlink-server/internal/account/model"
	"github.com/aqi/qlink-server/internal/account/request"
	"github.com/aqi/qlink-server/internal/account/vo"
	"gorm.io/gorm"
)

type ApiTokenService struct {
	db *gorm.DB
}

func NewApiTokenService(db *gorm.DB) *ApiTokenService {
	return &ApiTokenService{db: db}
}

// Create generates a new API token.
func (s *ApiTokenService) Create(accountNo int64, req *request.ApiTokenCreateRequest) (*vo.ApiTokenVO, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}

	do := model.ApiTokenDO{
		AccountNo: accountNo,
		Name:      req.Name,
		Token:     token,
		Scopes:    req.Scopes,
	}
	if req.ExpiredAt != "" {
		t, err := time.Parse("2006-01-02", req.ExpiredAt)
		if err == nil {
			do.ExpiredAt = &t
		}
	}
	if err := s.db.Create(&do).Error; err != nil {
		return nil, fmt.Errorf("create api token failed: %w", err)
	}

	return &vo.ApiTokenVO{
		ID:        do.ID,
		Name:      do.Name,
		Token:     token,
		Scopes:    do.Scopes,
		ExpiredAt: req.ExpiredAt,
		CreatedAt: do.GmtCreate.Format("2006-01-02 15:04:05"),
	}, nil
}

// List returns all active API tokens for an account (token values masked).
func (s *ApiTokenService) List(accountNo int64) ([]vo.ApiTokenVO, error) {
	var tokens []model.ApiTokenDO
	if err := s.db.Where("account_no = ?", accountNo).Order("gmt_create DESC").Find(&tokens).Error; err != nil {
		return nil, err
	}
	list := make([]vo.ApiTokenVO, len(tokens))
	for i, t := range tokens {
		list[i] = vo.ApiTokenVO{
			ID:        t.ID,
			Name:      t.Name,
			Token:     maskToken(t.Token),
			Scopes:    t.Scopes,
			CreatedAt: t.GmtCreate.Format("2006-01-02 15:04:05"),
		}
		if t.ExpiredAt != nil {
			list[i].ExpiredAt = t.ExpiredAt.Format("2006-01-02 15:04:05")
		}
		if t.LastUsed != nil {
			list[i].LastUsed = t.LastUsed.Format("2006-01-02 15:04:05")
		}
	}
	return list, nil
}

// Delete removes an API token.
func (s *ApiTokenService) Delete(accountNo int64, id int64) error {
	result := s.db.Where("id = ? AND account_no = ?", id, accountNo).Delete(&model.ApiTokenDO{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("api token not found")
	}
	return nil
}

// ValidateToken checks an API token and returns the account_no or 0 if invalid.
func (s *ApiTokenService) ValidateToken(tokenStr string) int64 {
	var do model.ApiTokenDO
	err := s.db.Where("token = ?", tokenStr).First(&do).Error
	if err != nil {
		return 0
	}
	// Check expiry
	if do.ExpiredAt != nil && do.ExpiredAt.Before(time.Now()) {
		return 0
	}
	// Update last_used
	s.db.Model(&do).Update("last_used", time.Now())
	return do.AccountNo
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func maskToken(token string) string {
	if len(token) <= 12 {
		return "****"
	}
	return token[:6] + "****" + token[len(token)-6:]
}

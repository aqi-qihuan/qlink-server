package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	accountmodel "github.com/aqi/qlink-server/internal/account/model"
	"github.com/aqi/qlink-server/internal/common/constant"
	"github.com/aqi/qlink-server/internal/common/model"
	"github.com/aqi/qlink-server/internal/common/util"
	"github.com/redis/go-redis/v9"
	"github.com/sony/sonyflake"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

var sf = sonyflake.NewSonyflake(sonyflake.Settings{})

func nextID() int64 {
	id, err := sf.NextID()
	if err != nil {
		return time.Now().UnixNano()/1e6 + int64(9000000000000000000)
	}
	return int64(id)
}

// OAuthService handles OAuth login for multiple providers.
type OAuthService struct {
	db         *gorm.DB
	rdb        *redis.Client
	httpClient *http.Client
}

// NewOAuthService creates a new OAuthService.
func NewOAuthService(db *gorm.DB, rdb *redis.Client) *OAuthService {
	return &OAuthService{db: db, rdb: rdb, httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// GenerateState creates a random state string for CSRF protection.
func (s *OAuthService) GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// FetchUserInfo fetches user info from the provider's userinfo endpoint.
func (s *OAuthService) FetchUserInfo(tokenSource oauth2.TokenSource, userInfoURL string, subField, emailField, nameField, pictureField string) (*OAuthUserInfo, error) {
	token, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	req, _ := http.NewRequest("GET", userInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	// GitHub needs User-Agent
	req.Header.Set("User-Agent", "qlink-server")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read userinfo: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("parse userinfo: %w", err)
	}

	info := &OAuthUserInfo{
		Sub:     claimString(claims, subField),
		Email:   claimString(claims, emailField),
		Name:    claimString(claims, nameField),
		Picture: claimString(claims, pictureField),
	}

	// GitHub returns id as float64, convert to string
	if subField == "id" {
		if v, ok := claims["id"]; ok {
			info.Sub = fmt.Sprintf("%.0f", v)
		}
	}

	return info, nil
}

// OAuthUserInfo holds the user info extracted from the provider.
type OAuthUserInfo struct {
	Sub      string
	Email    string
	Name     string
	Picture  string
	Provider string
}

// FindOrCreateAccount looks up an account by OAuth sub+provider.
// If not found, creates a new account.
func (s *OAuthService) FindOrCreateAccount(info *OAuthUserInfo) (*accountmodel.AccountDO, error) {
	var account accountmodel.AccountDO
	err := s.db.Where("oidc_sub = ? AND oidc_provider = ?", info.Sub, info.Provider).First(&account).Error

	if err == nil {
		// Existing account: update profile if changed
		updates := map[string]interface{}{}
		if info.Email != "" && account.Mail != info.Email {
			updates["mail"] = info.Email
		}
		if info.Name != "" && account.Username != info.Name {
			updates["username"] = info.Name
		}
		if info.Picture != "" && account.HeadImg != info.Picture {
			updates["head_img"] = info.Picture
		}
		if len(updates) > 0 {
			updates["gmt_modified"] = time.Now()
			s.db.Model(&account).Updates(updates)
		}
		return &account, nil
	}

	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("lookup account: %w", err)
	}

	// Create new account
	now := time.Now()
	account = accountmodel.AccountDO{
		AccountNo:    nextID(),
		Username:     info.Name,
		Mail:         info.Email,
		HeadImg:      info.Picture,
		Phone:        "",
		Pwd:          "",
		Secret:       "OAUTH",
		Auth:         "",
		OidcSub:      info.Sub,
		OidcProvider: info.Provider,
		GmtCreate:    now,
		GmtModified:  now,
	}

	if account.Username == "" {
		if info.Email != "" {
			account.Username = info.Email
		} else {
			account.Username = fmt.Sprintf("user_%d", account.AccountNo%100000)
		}
	}

	if err := s.db.Create(&account).Error; err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}

	fmt.Printf("OAuth: created account %d for sub=%s provider=%s\n", account.AccountNo, info.Sub, info.Provider)
	return &account, nil
}

// GenerateJWT creates a JWT token for the authenticated account.
func (s *OAuthService) GenerateJWT(account *accountmodel.AccountDO) (string, error) {
	loginUser := &model.LoginUser{
		AccountNo: account.AccountNo,
		HeadImg:   account.HeadImg,
		Username:  account.Username,
		Mail:      account.Mail,
		Phone:     account.Phone,
		Auth:      account.Auth,
	}
	return util.GenerateToken(loginUser)
}

// SaveState saves a state token with redirectTo URL in Redis (10-minute TTL).
func (s *OAuthService) SaveState(state, redirectTo string) {
	key := constant.FormatOAuthStateKey(state)
	ctx := context.Background()
	s.rdb.Set(ctx, key, redirectTo, 10*time.Minute)
}

// ValidateState validates and consumes the OAuth state from Redis.
func (s *OAuthService) ValidateState(state string) (redirectTo string, ok bool) {
	key := constant.FormatOAuthStateKey(state)
	ctx := context.Background()
	val, err := s.rdb.GetDel(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func claimString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return fmt.Sprintf("%.0f", val)
		default:
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}

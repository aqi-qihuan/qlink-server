package config

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
	"golang.org/x/oauth2/google"
)

// OAuthProvider holds a single OAuth2 provider config.
type OAuthProvider struct {
	Name         string
	Endpoint     oauth2.Endpoint
	ClientID     string
	ClientSecret string
	Scopes       []string
	UserInfoURL  string
	// Field mapping (JSON path from userinfo response)
	SubField     string // unique user ID field
	EmailField   string
	NameField    string
	PictureField string
}

// OAuthConfig holds all enabled OAuth providers.
type OAuthConfig struct {
	Providers    map[string]*OAuthProvider
	RedirectBase string // e.g. http://localhost:8001/api/account/v1/oauth
	SuccessURL   string // frontend URL to redirect after login
}

// LoadOAuthConfig reads OAuth config from environment variables.
func LoadOAuthConfig() *OAuthConfig {
	redirectBase := envDefault("OAUTH_REDIRECT_BASE", "http://localhost:8001/api/account/v1/oauth")
	successURL := envDefault("OAUTH_SUCCESS_URL", "http://localhost:5173/oauth-callback")

	cfg := &OAuthConfig{
		Providers:    make(map[string]*OAuthProvider),
		RedirectBase: redirectBase,
		SuccessURL:   successURL,
	}

	// Google
	if id := os.Getenv("OAUTH_GOOGLE_CLIENT_ID"); id != "" {
		cfg.Providers["google"] = &OAuthProvider{
			Name:         "Google",
			Endpoint:     google.Endpoint,
			ClientID:     id,
			ClientSecret: os.Getenv("OAUTH_GOOGLE_CLIENT_SECRET"),
			Scopes:       []string{"openid", "email", "profile"},
			UserInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
			SubField:     "sub",
			EmailField:   "email",
			NameField:    "name",
			PictureField: "picture",
		}
	}

	// GitHub
	if id := os.Getenv("OAUTH_GITHUB_CLIENT_ID"); id != "" {
		cfg.Providers["github"] = &OAuthProvider{
			Name:         "GitHub",
			Endpoint:     github.Endpoint,
			ClientID:     id,
			ClientSecret: os.Getenv("OAUTH_GITHUB_CLIENT_SECRET"),
			Scopes:       []string{"read:user", "user:email"},
			UserInfoURL:  "https://api.github.com/user",
			SubField:     "id", // GitHub returns int64 id
			EmailField:   "email",
			NameField:    "name",
			PictureField: "avatar_url",
		}
	}

	// WeChat (微信开放平台)
	if id := os.Getenv("OAUTH_WECHAT_APP_ID"); id != "" {
		cfg.Providers["wechat"] = &OAuthProvider{
			Name: "WeChat",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://open.weixin.qq.com/connect/qrconnect",
				TokenURL: "https://api.weixin.qq.com/sns/oauth2/access_token",
			},
			ClientID:     id,
			ClientSecret: os.Getenv("OAUTH_WECHAT_APP_SECRET"),
			Scopes:       []string{"snsapi_login"},
			UserInfoURL:  "https://api.weixin.qq.com/sns/userinfo",
			SubField:     "openid",
			EmailField:   "", // WeChat doesn't provide email
			NameField:    "nickname",
			PictureField: "headimgurl",
		}
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("OAuth: no providers configured")
	} else {
		names := make([]string, 0, len(cfg.Providers))
		for k := range cfg.Providers {
			names = append(names, k)
		}
		fmt.Printf("OAuth enabled: %s\n", strings.Join(names, ", "))
	}

	return cfg
}

// OAuth2Config returns an oauth2.Config for the given provider.
func (c *OAuthConfig) OAuth2Config(p *OAuthProvider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		Endpoint:     p.Endpoint,
		RedirectURL:  c.RedirectBase + "/" + strings.ToLower(p.Name) + "/callback",
		Scopes:       p.Scopes,
	}
}

// HTTPClient returns an HTTP client (for self-signed certs in dev, customize here).
func HTTPClient() *http.Client {
	return &http.Client{}
}

func envDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// unused but kept for future use
var _ = context.Background()

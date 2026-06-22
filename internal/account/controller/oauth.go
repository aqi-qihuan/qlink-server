package controller

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/aqi/qlink-server/internal/account/config"
	"github.com/aqi/qlink-server/internal/account/service"
	"github.com/aqi/qlink-server/internal/common/response"
	"github.com/gin-gonic/gin"
)

// OAuthController handles the OAuth login flow for multiple providers.
type OAuthController struct {
	svc *service.OAuthService
	cfg *config.OAuthConfig
}

// NewOAuthController creates a new OAuthController.
func NewOAuthController(svc *service.OAuthService, cfg *config.OAuthConfig) *OAuthController {
	return &OAuthController{svc: svc, cfg: cfg}
}

// Login initiates the OAuth login flow.
// GET /api/account/v1/oauth/:provider/login
func (ctrl *OAuthController) Login(c *gin.Context) {
	providerName := c.Param("provider")
	provider, ok := ctrl.cfg.Providers[providerName]
	if !ok {
		response.JSON(c, response.BuildError("unsupported provider: "+providerName))
		return
	}

	state, err := ctrl.svc.GenerateState()
	if err != nil {
		response.JSON(c, response.BuildError("failed to generate state"))
		return
	}

	redirectTo := c.Query("redirect")
	if redirectTo == "" {
		redirectTo = ctrl.cfg.SuccessURL
	}

	ctrl.svc.SaveState(state, redirectTo)

	oauth2Cfg := ctrl.cfg.OAuth2Config(provider)
	authURL := oauth2Cfg.AuthCodeURL(state)

	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "application/json") {
		response.JSON(c, response.BuildSuccessData(map[string]string{"url": authURL}))
		return
	}

	c.Redirect(http.StatusFound, authURL)
}

// Callback handles the OAuth callback from the provider.
// GET /api/account/v1/oauth/:provider/callback
func (ctrl *OAuthController) Callback(c *gin.Context) {
	providerName := c.Param("provider")
	provider, ok := ctrl.cfg.Providers[providerName]
	if !ok {
		response.JSON(c, response.BuildError("unsupported provider: "+providerName))
		return
	}

	// Validate state
	state := c.Query("state")
	redirectTo, ok := ctrl.svc.ValidateState(state)
	if !ok {
		response.JSON(c, response.BuildError("invalid or expired state"))
		return
	}

	// Exchange code for token
	code := c.Query("code")
	oauth2Cfg := ctrl.cfg.OAuth2Config(provider)
	token, err := oauth2Cfg.Exchange(c.Request.Context(), code)
	if err != nil {
		fmt.Printf("OAuth[%s]: token exchange failed: %v\n", providerName, err)
		response.JSON(c, response.BuildError("token exchange failed"))
		return
	}

	// Fetch user info from the provider
	tokenSource := oauth2Cfg.TokenSource(c.Request.Context(), token)
	userInfo, err := ctrl.svc.FetchUserInfo(
		tokenSource,
		provider.UserInfoURL,
		provider.SubField,
		provider.EmailField,
		provider.NameField,
		provider.PictureField,
	)
	if err != nil {
		fmt.Printf("OAuth[%s]: fetch userinfo failed: %v\n", providerName, err)
		response.JSON(c, response.BuildError("failed to get user info"))
		return
	}

	if userInfo.Sub == "" {
		response.JSON(c, response.BuildError("missing user ID from provider"))
		return
	}

	userInfo.Provider = providerName

	// Find or create account
	account, err := ctrl.svc.FindOrCreateAccount(userInfo)
	if err != nil {
		fmt.Printf("OAuth[%s]: find/create account failed: %v\n", providerName, err)
		response.JSON(c, response.BuildError("account lookup failed"))
		return
	}

	// Generate JWT
	jwtToken, err := ctrl.svc.GenerateJWT(account)
	if err != nil {
		fmt.Printf("OAuth[%s]: JWT generation failed: %v\n", providerName, err)
		response.JSON(c, response.BuildError("token generation failed"))
		return
	}

	// API response or browser redirect
	accept := c.GetHeader("Accept")
	if strings.Contains(accept, "application/json") {
		response.JSON(c, response.BuildSuccessData(map[string]interface{}{
			"token":      jwtToken,
			"account_no": account.AccountNo,
			"username":   account.Username,
		}))
		return
	}

	sep := "?"
	if strings.Contains(redirectTo, "?") {
		sep = "&"
	}
	c.Redirect(http.StatusFound, redirectTo+sep+"token="+jwtToken)
}

// Providers returns the list of enabled OAuth providers.
// GET /api/account/v1/oauth/providers
func (ctrl *OAuthController) Providers(c *gin.Context) {
	providers := make([]map[string]string, 0, len(ctrl.cfg.Providers))
	for key, p := range ctrl.cfg.Providers {
		providers = append(providers, map[string]string{
			"id":   key,
			"name": p.Name,
			"url":  ctrl.cfg.RedirectBase + "/" + key + "/login",
		})
	}
	response.JSON(c, response.BuildSuccessData(providers))
}

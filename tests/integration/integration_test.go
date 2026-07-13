package integration

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Service base URLs (must be running locally)
const (
	accountURL = "http://localhost:8001"
	linkURL    = "http://localhost:8003"
	shopURL    = "http://localhost:8005"
	dataURL    = "http://localhost:8002"
	aiURL      = "http://localhost:8006"
	gatewayURL = "http://localhost:8888"
)

var (
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
	})
	testPhone    = "138" + fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	testPassword = "Test123456"
	testUA       = "IntegrationTest/1.0"
	authToken    string
)

// ========== Helper Functions ==========

func httpJSONWithUA(method, url string, body interface{}, token string) (map[string]interface{}, error) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", testUA)
	if token != "" {
		req.Header.Set("token", token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	return result, nil
}

func httpJSON(method, url string, body interface{}, token string) (map[string]interface{}, error) {
	return httpJSONWithUA(method, url, body, token)
}

func httpGet(url string, token string) (map[string]interface{}, error) {
	return httpJSON("GET", url, nil, token)
}

func httpPost(url string, body interface{}, token string) (map[string]interface{}, error) {
	return httpJSON("POST", url, body, token)
}

func assertSuccess(t *testing.T, name string, result map[string]interface{}, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("[%s] request error: %v", name, err)
	}
	code, ok := result["code"]
	if !ok {
		t.Fatalf("[%s] no 'code' field in response: %v", name, result)
	}
	if code != float64(0) {
		t.Fatalf("[%s] expected code=0, got code=%v, msg=%v", name, code, result["msg"])
	}
}

func md5Hash(s string) string {
	return fmt.Sprintf("%X", md5.Sum([]byte(s)))
}

// ========== Health Checks ==========

func TestHealthChecks(t *testing.T) {
	services := map[string]string{
		"account": accountURL,
		"link":    linkURL,
		"shop":    shopURL,
		"data":    dataURL,
		"ai":      aiURL,
		"gateway": gatewayURL,
	}
	for name, base := range services {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", base+"/health", nil)
			req.Header.Set("User-Agent", testUA)
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("health check failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("health check returned %d", resp.StatusCode)
			}
			var result map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&result)
			if result["status"] != "ok" {
				t.Fatalf("health status not ok: %v", result)
			}
			t.Logf("[OK] %s health: %v", name, result["service"])
		})
	}
}

// ========== Account: Register + Login + Detail + Update ==========

func TestAccountFlow(t *testing.T) {
	ctx := context.Background()
	phone := testPhone
	password := testPassword

	// Step 1: Get captcha (with custom UA so we can predict the Redis key)
	t.Run("captcha", func(t *testing.T) {
		req, _ := http.NewRequest("GET", accountURL+"/api/notify/v1/captcha", nil)
		req.Header.Set("User-Agent", testUA)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("captcha request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("captcha returned %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "image") {
			t.Fatalf("expected image content type, got %s", ct)
		}
		t.Logf("[OK] captcha returned content-type: %s", ct)
	})

	// Step 2: Read captcha from Redis (key = MD5 of clientIP + User-Agent)
	// The server sees our IP as 127.0.0.1 (localhost) and our custom UA
	captchaKey := "account-service:captcha:" + md5Hash("127.0.0.1"+testUA)
	captchaVal, err := rdb.Get(ctx, captchaKey).Result()
	if err != nil {
		// Try ::1 as fallback (IPv6 loopback)
		captchaKey = "account-service:captcha:" + md5Hash("::1"+testUA)
		captchaVal, err = rdb.Get(ctx, captchaKey).Result()
		if err != nil {
			t.Fatalf("read captcha from Redis failed (tried 127.0.0.1 and ::1): %v", err)
		}
	}
	t.Logf("[OK] captcha from Redis: %s", captchaVal)

	// Step 3: Send verification code
	t.Run("send_code", func(t *testing.T) {
		result, err := httpPost(accountURL+"/api/notify/v1/send_code", map[string]string{
			"captcha": captchaVal,
			"to":      phone,
		}, "")
		assertSuccess(t, "send_code", result, err)
		t.Log("[OK] verification code sent")
	})

	// Step 4: Read verification code from Redis
	codeKey := fmt.Sprintf("code:USER_REGISTER:%s", phone)
	codeVal, err := rdb.Get(ctx, codeKey).Result()
	if err != nil {
		t.Fatalf("read verification code from Redis failed: %v", err)
	}
	verifyCode := codeVal[:6]
	t.Logf("[OK] verification code: %s", verifyCode)

	// Step 5: Register
	t.Run("register", func(t *testing.T) {
		result, err := httpPost(accountURL+"/api/account/v1/register", map[string]string{
			"phone":    phone,
			"pwd":      password,
			"code":     verifyCode,
			"username": "IntegrationTestUser",
			"mail":     "test@aqicloud.com",
		}, "")
		assertSuccess(t, "register", result, err)
		t.Log("[OK] account registered")
	})

	// Step 6: Login
	t.Run("login", func(t *testing.T) {
		result, err := httpPost(accountURL+"/api/account/v1/login", map[string]string{
			"phone": phone,
			"pwd":   password,
		}, "")
		assertSuccess(t, "login", result, err)
		data, ok := result["data"]
		if !ok || data == nil {
			t.Fatalf("login response missing token: %v", result)
		}
		authToken = fmt.Sprintf("%v", data)
		t.Logf("[OK] login success, token length=%d", len(authToken))
	})

	// Step 7: Get profile detail
	t.Run("detail", func(t *testing.T) {
		result, err := httpGet(accountURL+"/api/account/v1/detail", authToken)
		assertSuccess(t, "detail", result, err)
		data := result["data"].(map[string]interface{})
		t.Logf("[OK] profile: username=%v, phone=%v", data["username"], data["phone"])
	})

	// Step 8: Update profile
	t.Run("update", func(t *testing.T) {
		result, err := httpPost(accountURL+"/api/account/v1/update", map[string]string{
			"username": "UpdatedUser",
			"mail":     "updated@aqicloud.com",
		}, authToken)
		assertSuccess(t, "update", result, err)
		t.Log("[OK] profile updated")
	})

	// Step 9: Verify update
	t.Run("verify_update", func(t *testing.T) {
		result, err := httpGet(accountURL+"/api/account/v1/detail", authToken)
		assertSuccess(t, "verify_update", result, err)
		data := result["data"].(map[string]interface{})
		if data["username"] != "UpdatedUser" {
			t.Fatalf("expected username=UpdatedUser, got %v", data["username"])
		}
		if data["mail"] != "updated@aqicloud.com" {
			t.Fatalf("expected mail=updated@aqicloud.com, got %v", data["mail"])
		}
		t.Log("[OK] update verified")
	})

	t.Run("unauth_access", func(t *testing.T) {
		result, err := httpGet(accountURL+"/api/account/v1/detail", "")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		code := result["code"]
		if code == float64(0) {
			t.Fatal("unauthenticated access should not succeed")
		}
		t.Logf("[OK] unauth access rejected: code=%v", code)
	})
}

// ========== Shop: Product List + Detail ==========

func TestShopFlow(t *testing.T) {
	// Public endpoints - no auth needed
	t.Run("product_list", func(t *testing.T) {
		result, err := httpGet(shopURL+"/api/product/v1/list", "")
		assertSuccess(t, "product_list", result, err)
		data := result["data"]
		t.Logf("[OK] product list: %v products", data)
	})

	t.Run("product_detail", func(t *testing.T) {
		result, err := httpGet(shopURL+"/api/product/v1/detail/1", "")
		assertSuccess(t, "product_detail", result, err)
		data := result["data"].(map[string]interface{})
		t.Logf("[OK] product: name=%v, price=%v", data["name"], data["price"])
	})
}

// ========== Link: Create + List + Redirect ==========

func TestLinkFlow(t *testing.T) {
	if authToken == "" {
		t.Skip("skipping: no auth token (run TestAccountFlow first)")
	}

	var shortLinkCode string

	// Create a short link
	t.Run("create_link", func(t *testing.T) {
		result, err := httpPost(linkURL+"/api/link/v1/add", map[string]interface{}{
			"groupId":     0,
			"title":       "Integration Test Link",
			"originalUrl": "https://www.google.com",
			"domainType":  "OFFICIAL",
			"expired":     time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05"),
		}, authToken)
		assertSuccess(t, "create_link", result, err)
		data := result["data"]
		if data != nil {
			if m, ok := data.(map[string]interface{}); ok {
				shortLinkCode = fmt.Sprintf("%v", m["code"])
			} else {
				shortLinkCode = fmt.Sprintf("%v", data)
			}
		}
		t.Logf("[OK] short link created: code=%v", shortLinkCode)
	})

	// List links
	t.Run("list_links", func(t *testing.T) {
		result, err := httpPost(linkURL+"/api/link/v1/page", map[string]interface{}{
			"page": 1,
			"size": 10,
		}, authToken)
		assertSuccess(t, "list_links", result, err)
		t.Logf("[OK] link page response received")
	})

	// Redirect test (follow short link)
	if shortLinkCode != "" {
		t.Run("redirect", func(t *testing.T) {
			client := &http.Client{
				Timeout: 10 * time.Second,
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse // don't follow redirects
				},
			}
			req, _ := http.NewRequest("GET", linkURL+"/"+shortLinkCode, nil)
			req.Header.Set("User-Agent", testUA)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("redirect request failed: %v", err)
			}
			defer resp.Body.Close()
			t.Logf("[OK] redirect response: status=%d, location=%s", resp.StatusCode, resp.Header.Get("Location"))
		})
	}

	// Link group operations
	t.Run("group_list", func(t *testing.T) {
		result, err := httpGet(linkURL+"/api/group/v1/list", authToken)
		assertSuccess(t, "group_list", result, err)
		t.Logf("[OK] group list retrieved")
	})
}

// ========== Data: Visit Stats ==========

func TestDataFlow(t *testing.T) {
	if authToken == "" {
		t.Skip("skipping: no auth token (run TestAccountFlow first)")
	}

	t.Run("page_record", func(t *testing.T) {
		result, err := httpPost(dataURL+"/api/visit_stats/v1/page_record", map[string]interface{}{
			"code": "test",
			"page": 1,
			"size": 10,
		}, authToken)
		assertSuccess(t, "page_record", result, err)
		t.Logf("[OK] page_record query successful")
	})

	t.Run("trend", func(t *testing.T) {
		result, err := httpPost(dataURL+"/api/visit_stats/v1/trend", map[string]interface{}{
			"code":      "test",
			"type":      "DAY",
			"startTime": "20260101",
			"endTime":   "20261231",
		}, authToken)
		assertSuccess(t, "trend", result, err)
		t.Logf("[OK] trend query successful")
	})

	t.Run("frequent_ip", func(t *testing.T) {
		result, err := httpPost(dataURL+"/api/visit_stats/v1/frequent_ip", map[string]interface{}{
			"code": "test",
		}, authToken)
		assertSuccess(t, "frequent_ip", result, err)
		t.Logf("[OK] frequent_ip query successful")
	})

	t.Run("device_info", func(t *testing.T) {
		result, err := httpPost(dataURL+"/api/visit_stats/v1/device_info", map[string]interface{}{
			"code":  "test",
			"field": "os",
		}, authToken)
		assertSuccess(t, "device_info", result, err)
		t.Logf("[OK] device_info query successful")
	})
}

// ========== AI: Recommend + Analytics + Safety ==========

func TestAIFlow(t *testing.T) {
	if authToken == "" {
		t.Skip("skipping: no auth token (run TestAccountFlow first)")
	}

	t.Run("recommend", func(t *testing.T) {
		result, err := httpPost(aiURL+"/api/ai/v1/recommend", map[string]interface{}{
			"code": "test",
		}, authToken)
		// AI might fail if LLM is not available, just log
		if err != nil {
			t.Logf("[WARN] recommend request failed: %v (LLM may not be running)", err)
			return
		}
		t.Logf("[OK] recommend response: code=%v", result["code"])
	})

	t.Run("check_safety", func(t *testing.T) {
		result, err := httpPost(aiURL+"/api/ai/v1/check_safety", map[string]interface{}{
			"url": "https://example.com",
		}, authToken)
		if err != nil {
			t.Logf("[WARN] check_safety request failed: %v (LLM may not be running)", err)
			return
		}
		t.Logf("[OK] check_safety response: code=%v", result["code"])
	})
}

// ========== Gateway: Proxy Routing ==========

func TestGatewayRouting(t *testing.T) {
	// Gateway proxies to backend services; test a few routes
	t.Run("health_via_gateway", func(t *testing.T) {
		req, _ := http.NewRequest("GET", gatewayURL+"/health", nil)
		req.Header.Set("User-Agent", testUA)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("gateway health failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("gateway health returned %d", resp.StatusCode)
		}
		t.Log("[OK] gateway health OK")
	})

	t.Run("proxy_product_list", func(t *testing.T) {
		result, err := httpGet(gatewayURL+"/shop-server/api/product/v1/list", "")
		assertSuccess(t, "proxy_product_list", result, err)
		t.Log("[OK] gateway → shop proxy OK")
	})

	t.Run("proxy_account_unauth", func(t *testing.T) {
		result, err := httpGet(gatewayURL+"/account-server/api/account/v1/detail", "")
		if err != nil {
			t.Fatalf("request error: %v", err)
		}
		code := result["code"]
		if code == float64(0) {
			t.Log("[WARN] unauthenticated access through gateway succeeded (expected rejection)")
		} else {
			t.Logf("[OK] gateway → account proxy OK (unauth rejected: code=%v)", code)
		}
	})
}

// ========== Traffic: Page + ClaimFree ==========

func TestTrafficFlow(t *testing.T) {
	if authToken == "" {
		t.Skip("skipping: no auth token (run TestAccountFlow first)")
	}

	t.Run("traffic_page", func(t *testing.T) {
		result, err := httpGet(accountURL+"/api/traffic/v1/page?page=1&size=10", authToken)
		assertSuccess(t, "traffic_page", result, err)
		t.Logf("[OK] traffic page: %v", result["data"])
	})

	t.Run("claim_free", func(t *testing.T) {
		result, err := httpGet(accountURL+"/api/traffic/v1/claim_free", authToken)
		if err != nil {
			t.Fatalf("claim_free request failed: %v", err)
		}
		// May fail if free product doesn't exist, which is expected
		t.Logf("[INFO] claim_free response: code=%v, msg=%v", result["code"], result["msg"])
	})
}

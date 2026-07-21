package service

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aqi/qlink-server/internal/link/vo"
)

// Known malicious patterns (basic check)
var maliciousPatterns = []string{
	"phish", "malware", ".exe", ".scr", ".bat", ".cmd", ".pif",
}

// URLSafeChecker performs basic URL safety validation.
type URLSafeChecker struct {
	client *http.Client
}

func NewURLSafeChecker() *URLSafeChecker {
	return &URLSafeChecker{
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// Check validates URL safety.
func (c *URLSafeChecker) Check(rawURL string) *vo.URLSafeCheckVO {
	result := &vo.URLSafeCheckVO{Safe: true, Message: "URL appears safe"}

	// Parse URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		result.Safe = false
		result.Message = "Invalid URL format"
		return result
	}

	// Check protocol
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		result.Safe = false
		result.Message = fmt.Sprintf("Unsupported protocol: %s", parsed.Scheme)
		return result
	}

	// Check for common malicious patterns in URL
	lower := strings.ToLower(rawURL)
	for _, pattern := range maliciousPatterns {
		if strings.Contains(lower, pattern) {
			result.Safe = false
			result.Message = fmt.Sprintf("URL contains suspicious pattern: %s", pattern)
			return result
		}
	}

	// SSL check
	if parsed.Scheme == "https" {
		result.SSL = true
	} else {
		// Try HTTPS HEAD to check if SSL is available
		httpsURL := "https://" + parsed.Host + parsed.Path
		if parsed.RawQuery != "" {
			httpsURL += "?" + parsed.RawQuery
		}
		resp, err := c.doHEAD(httpsURL)
		if err == nil {
			resp.Body.Close()
			result.SSL = true
		}
	}

	// Reachability check (HEAD request)
	if resp, err := c.doHEAD(rawURL); err == nil {
		resp.Body.Close()
		result.Reachable = true
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			result.Reachable = false
			result.Message = fmt.Sprintf("URL returned HTTP %d", resp.StatusCode)
		}
	} else {
		result.Reachable = false
		result.Message = "URL is not reachable: " + err.Error()
	}

	return result
}

func (c *URLSafeChecker) doHEAD(rawURL string) (*http.Response, error) {
	req, err := http.NewRequest("HEAD", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "qlink-safety-checker/1.0")
	return c.client.Do(req)
}

// AbuseReportService handles abuse reports.
type AbuseReportService struct {
	opLog *OperationLogService
}

func NewAbuseReportService(opLog *OperationLogService) *AbuseReportService {
	return &AbuseReportService{opLog: opLog}
}

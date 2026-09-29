package streamer

import (
	"encoding/json"
	"testing"
)

// TestParseUserAgent covers browser/OS/device classification against real
// user agents. OS expectations follow the Java original's group names
// (bitwalker operatingSystem.getGroup().getName()): Windows / iOS /
// Android / Mac OS X — enforced by normalizeOSGroup.
func TestParseUserAgent(t *testing.T) {
	cases := []struct {
		name                     string
		ua                       string
		browser, os, device, mfr string
	}{
		{
			name: "windows chrome",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
			browser: "Chrome", os: "Windows", device: "COMPUTER", mfr: "MICROSOFT",
		},
		{
			name: "iphone safari",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
			browser: "Safari", os: "iOS", device: "MOBILE", mfr: "APPLE",
		},
		{
			// Known limitation: mssola/user_agent (0.6.0) reports iPad as
			// "Mac OS X" ("like Mac OS X" in UA wins); Java's bitwalker would
			// group it as iOS. Accepted gap — locked here so a future UA lib
			// upgrade consciously flips this expectation.
			name: "ipad safari (mssola limitation: grouped as Mac OS X)",
			ua:   "Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/604.1",
			browser: "Safari", os: "Mac OS X", device: "MOBILE", mfr: "APPLE",
		},
		{
			name: "android chrome",
			ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36",
			browser: "Chrome", os: "Android", device: "MOBILE", mfr: "GOOGLE",
		},
		{
			name: "mac safari",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
			browser: "Safari", os: "Mac OS X", device: "COMPUTER", mfr: "APPLE",
		},
		{
			name: "desktop linux firefox",
			ua:   "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0",
			browser: "Firefox", os: "Linux", device: "COMPUTER", mfr: "UNKNOWN",
		},
		{
			name: "curl",
			ua:   "curl/8.5.0",
			browser: "curl", os: "Unknown", device: "COMPUTER", mfr: "UNKNOWN",
		},
		{
			name: "python urllib (production regression anchor)",
			ua:   "Python-urllib/3.12",
			browser: "Python-urllib", os: "Unknown", device: "COMPUTER", mfr: "UNKNOWN",
		},
		{
			name: "empty",
			ua:   "",
			browser: "Unknown", os: "Unknown", device: "COMPUTER", mfr: "Unknown",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			browser, os, osVer, device, mfr := parseUserAgent(c.ua)
			if browser != c.browser {
				t.Errorf("browser = %q, want %q", browser, c.browser)
			}
			if os != c.os {
				t.Errorf("os = %q, want %q", os, c.os)
			}
			if device != c.device {
				t.Errorf("deviceType = %q, want %q", device, c.device)
			}
			if mfr != c.mfr {
				t.Errorf("manufacturer = %q, want %q", mfr, c.mfr)
			}
			_ = osVer // version extraction is best-effort; format checked separately below
		})
	}
}

// TestExtractOSVersion mimics Java's DeviceUtil.getOSVersion(): content
// between the first '(' and ')', split by ';', take index [1] trimmed.
func TestExtractOSVersion(t *testing.T) {
	cases := []struct {
		ua, want string
	}{
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/130", "Win64"},
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8) Chrome/130", "Android 14"},
		{"curl/8.5.0", ""},          // no parens
		{"(only-one-part)", ""},     // split length 1 → ""
	}
	for _, c := range cases {
		if got := extractOSVersion(c.ua); got != c.want {
			t.Errorf("extractOSVersion(%q) = %q, want %q", c.ua, got, c.want)
		}
	}
}

// TestParseAndEnrichDevice verifies the DWD→DWM-Wide field mapping on a
// realistic ODS/DWD message. This is the exact shape produced by
// link.sendVisitLog + DWD.process (udid/referer/is_new injected).
func TestParseAndEnrichDevice(t *testing.T) {
	raw := `{
		"ip": "203.0.113.7",
		"ts": 1790610519943,
		"event": "SHORT_LINK_TYPE",
		"bizId": "a3z3JYk0",
		"udid": "FEF6C023C3E55DFD853FEC5792D23C1E",
		"referer": "t.example.com",
		"is_new": 1,
		"data": {
			"user-agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
			"referer": "https://t.example.com/page",
			"accountNo": 13900139003
		}
	}`
	wide, err := parseAndEnrichDevice([]byte(raw))
	if err != nil {
		t.Fatalf("parseAndEnrichDevice: %v", err)
	}

	if wide.Code != "a3z3JYk0" {
		t.Errorf("Code = %q, want a3z3JYk0 (from bizId)", wide.Code)
	}
	if wide.AccountNo != 13900139003 {
		t.Errorf("AccountNo = %d, want 13900139003 (from data.accountNo)", wide.AccountNo)
	}
	if wide.VisitTime != 1790610519943 {
		t.Errorf("VisitTime = %d, want 1790610519943 (from ts)", wide.VisitTime)
	}
	if wide.IsNew != 1 {
		t.Errorf("IsNew = %d, want 1 (from is_new)", wide.IsNew)
	}
	if wide.UDID != "FEF6C023C3E55DFD853FEC5792D23C1E" {
		t.Errorf("UDID = %q, want passthrough of DWD udid", wide.UDID)
	}
	if wide.Referer != "t.example.com" {
		t.Errorf("Referer = %q, want t.example.com (host extracted by DWD)", wide.Referer)
	}
	if wide.BrowserName != "Chrome" {
		t.Errorf("BrowserName = %q, want Chrome", wide.BrowserName)
	}
	if wide.OS != "Windows" {
		t.Errorf("OS = %q, want Windows", wide.OS)
	}
	if wide.DeviceType != "COMPUTER" {
		t.Errorf("DeviceType = %q, want COMPUTER", wide.DeviceType)
	}
	if wide.DeviceManufacturer != "MICROSOFT" {
		t.Errorf("DeviceManufacturer = %q, want MICROSOFT", wide.DeviceManufacturer)
	}

	// parseAndEnrichDevice must not set geo — that is DWM-Wide lookupGeo's job
	// (and province/city were "-" in production when AMAP key missing).
}

// TestParseAndEnrichDevice_MissingData verifies graceful degradation when
// optional fields are absent (defaults, not errors).
func TestParseAndEnrichDevice_MissingData(t *testing.T) {
	raw := `{"ip":"1.2.3.4","bizId":"x1"}`
	wide, err := parseAndEnrichDevice([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wide.BrowserName != "Unknown" || wide.OS != "Unknown" {
		t.Errorf("missing UA should yield Unknown/Unknown, got %q/%q", wide.BrowserName, wide.OS)
	}
	if wide.Code != "x1" || wide.AccountNo != 0 || wide.IsNew != 0 {
		t.Errorf("field defaults wrong: %+v", wide)
	}
}

// TestShortLinkWideJSONRoundTrip locks the JSON field names of the DWM
// message contract — Kafka topics carry these exact keys (camelCase) and
// both DWM-UV passthrough and DWS parseWide depend on them.
func TestShortLinkWideJSONRoundTrip(t *testing.T) {
	src := ShortLinkWide{
		Code: "c1", AccountNo: 42, VisitTime: 1790610519943, Referer: "r.example.com",
		IsNew: 1, BrowserName: "Chrome", OS: "Windows", OSVersion: "Win64; x64",
		DeviceType: "COMPUTER", DeviceManufacturer: "MICROSOFT",
		UDID: "abc", Province: "GD", City: "SZ", IP: "1.2.3.4",
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	back, err := parseWide(b)
	if err != nil {
		t.Fatalf("parseWide: %v", err)
	}
	if back != src {
		t.Errorf("JSON round-trip mismatch:\n got %+v\nwant %+v", back, src)
	}

	// Contract: exact key names (regression guard for the 2026-09-28 CH
	// column-name class of bug — silent schema drift between producer/consumer).
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"code", "accountNo", "visitTime", "referer", "isNew",
		"browserName", "os", "osVersion", "deviceType", "deviceManufacturer", "udid", "ip"} {
		if _, ok := m[key]; !ok {
			t.Errorf("DWM JSON contract: missing key %q in %s", key, b)
		}
	}
}

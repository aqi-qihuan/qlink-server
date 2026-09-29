package streamer

import (
	"encoding/json"
	"testing"
)

// Golden vectors cross-validated with production: the Redis key
// streamer:visitor:FEF6C023C3E55DFD853FEC5792D23C1E was created on 2026-09-28
// by a Python-urllib/3.12 visit to code a3z3JYk0 (event=SHORT_LINK_TYPE,
// ip=127.0.0.1). If this anchor breaks, the Java-compatible TreeMap format
// drifted and every historical udid becomes inconsistent.
// Note: util.MD5 returns UPPERCASE hex (matching the production Redis key).
func TestGenerateDeviceID_GoldenVectors(t *testing.T) {
	cases := []struct {
		name                 string
		ip, event, bizID, ua string
		want                 string
	}{
		{
			name:  "production anchor 2026-09-28",
			ip:    "127.0.0.1",
			event: "SHORT_LINK_TYPE",
			bizID: "a3z3JYk0",
			ua:    "Python-urllib/3.12",
			want:  "FEF6C023C3E55DFD853FEC5792D23C1E",
		},
		{
			name:  "windows chrome",
			ip:    "1.2.3.4",
			event: "SHORT_LINK_TYPE",
			bizID: "short-link:test:event",
			ua:    "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
			want:  "A41AC896130944FB5B58E78C0EF65237",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obj := map[string]interface{}{
				"ip":    c.ip,
				"event": c.event,
				"bizId": c.bizID,
				"data":  map[string]interface{}{"user-agent": c.ua},
			}
			if got := generateDeviceID(obj); got != c.want {
				t.Errorf("generateDeviceID = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGenerateDeviceID_SensitivityAndDefaults(t *testing.T) {
	base := map[string]interface{}{
		"ip":    "1.2.3.4",
		"event": "SHORT_LINK_TYPE",
		"bizId": "code1",
		"data":  map[string]interface{}{"user-agent": "UA/1.0"},
	}

	if got := generateDeviceID(base); got == "" {
		t.Fatal("generateDeviceID returned empty for complete input")
	}

	// Changing any single field must change the udid.
	mutations := []func(map[string]interface{}){
		func(o map[string]interface{}) { o["ip"] = "5.6.7.8" },
		func(o map[string]interface{}) { o["event"] = "OTHER_EVENT" },
		func(o map[string]interface{}) { o["bizId"] = "code2" },
		func(o map[string]interface{}) { o["data"] = map[string]interface{}{"user-agent": "OtherUA/2.0"} },
	}
	original := generateDeviceID(base)
	for i, mutate := range mutations {
		clone := deepCloneEvent(t, base)
		mutate(clone)
		if got := generateDeviceID(clone); got == original {
			t.Errorf("mutation %d did not change udid (both %q)", i, got)
		}
	}

	// Missing fields degrade to empty strings, not errors.
	empty := generateDeviceID(map[string]interface{}{})
	if empty == "" {
		t.Error("generateDeviceID(empty obj) should still hash the empty TreeMap string")
	}
}

func TestExtractReferer(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"full url", mkEventJSON("https://t.example.com/page?q=1"), "t.example.com"},
		{"no referer", mkEventJSON(""), ""},
		{"referer not a url", mkEventJSON("not a url"), ""}, // url.Parse succeeds on "not a url" but Host is ""
		{"no data field", `{"ip":"1.2.3.4"}`, ""},
		{"garbage json", `{not-json`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var obj map[string]interface{}
			_ = json.Unmarshal([]byte(c.raw), &obj)
			if got := extractReferer(obj); got != c.want {
				t.Errorf("extractReferer(%s) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

func mkEventJSON(referer string) string {
	data := map[string]interface{}{"user-agent": "UA/1.0"}
	if referer != "" {
		data["referer"] = referer
	}
	b, _ := json.Marshal(map[string]interface{}{
		"ip": "1.2.3.4", "event": "SHORT_LINK_TYPE", "bizId": "code1", "data": data,
	})
	return string(b)
}

func deepCloneEvent(t *testing.T, src map[string]interface{}) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(b, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return dst
}

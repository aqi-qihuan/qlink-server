package streamer

import "testing"

// TestParseWide_Errors verifies malformed DWM messages are rejected (not
// silently zero-valued) — the DWS consumer commits them as unparseable.
func TestParseWide_Errors(t *testing.T) {
	for _, raw := range []string{``, `{`, `[]`, `"string"`, `123`} {
		if _, err := parseWide([]byte(raw)); err == nil {
			t.Errorf("parseWide(%q): expected error, got nil", raw)
		}
	}
}

// TestBuildStatsKey_Equivalence locks DWS aggregation semantics: records
// aggregate together iff all 9 key dimensions match. visitTime and accountNo
// are NOT part of the key (they take the first-seen value in the window).
func TestBuildStatsKey_Equivalence(t *testing.T) {
	base := ShortLinkWide{
		Code: "c1", Referer: "r.com", IsNew: 1, Province: "GD", City: "SZ",
		IP: "1.2.3.4", BrowserName: "Chrome", OS: "Windows", DeviceType: "COMPUTER",
		AccountNo: 7, VisitTime: 100,
	}

	// Same 9 dimensions, different non-key fields → same aggregation key.
	variant := base
	variant.AccountNo = 999
	variant.VisitTime = 200
	variant.UDID = "different-udid"
	variant.ISP = "different-isp"
	if buildStatsKey(base) != buildStatsKey(variant) {
		t.Error("records differing only in non-key fields must share the stats key")
	}

	// Each key dimension changing must split the aggregation.
	mutations := []func(*ShortLinkWide){
		func(w *ShortLinkWide) { w.Code = "c2" },
		func(w *ShortLinkWide) { w.Referer = "other.com" },
		func(w *ShortLinkWide) { w.IsNew = 0 },
		func(w *ShortLinkWide) { w.Province = "BJ" },
		func(w *ShortLinkWide) { w.City = "HD" },
		func(w *ShortLinkWide) { w.IP = "5.6.7.8" },
		func(w *ShortLinkWide) { w.BrowserName = "Safari" },
		func(w *ShortLinkWide) { w.OS = "macOS" },
		func(w *ShortLinkWide) { w.DeviceType = "MOBILE" },
	}
	baseKey := buildStatsKey(base)
	for i, mutate := range mutations {
		w := base
		mutate(&w)
		if buildStatsKey(w) == baseKey {
			t.Errorf("mutation %d (key dimension) must change the stats key", i)
		}
	}
}

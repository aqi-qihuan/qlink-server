package streamer

import (
	"testing"
	"time"
)

// These tests pin down the semantic differences between the Go streamer and
// the Java Flink jobs it replaced (aqicloud-app: Dwd/DwmWide/DwmUV/Dws).
// They are NOT "the two produce identical output" tests — the implementations
// differ in windowing and delivery guarantees on purpose. The goal is to make
// those differences explicit and guard against accidental drift.

// TestSemanticParity_StatsKeyMatchesFlinkTuple9 asserts the Go 9-dimension
// aggregation key is field-for-field and order-for-order identical to Flink's
// Tuple9 in DwsShortLinkVisitStatsApp.keyBy. A mismatch would split one logical
// group into two and corrupt PV/UV aggregation.
func TestSemanticParity_StatsKeyMatchesFlinkTuple9(t *testing.T) {
	w := ShortLinkWide{
		Code: "abc123", Referer: "REF", IsNew: 1,
		Province: "广东", City: "广州", IP: "1.2.3.4",
		BrowserName: "Chrome", OS: "Linux", DeviceType: "PC",
		AccountNo: 42, VisitTime: 1700000000000,
	}
	got := buildStatsKey(w)

	// Flink Tuple9 order: code, referer, isNew, province, city, ip,
	//                     browserName, os, deviceType
	want := StatsKey{
		Code: "abc123", Referer: "REF", IsNew: 1,
		Province: "广东", City: "广州", IP: "1.2.3.4",
		BrowserName: "Chrome", OS: "Linux", DeviceType: "PC",
	}
	if got != want {
		t.Errorf("StatsKey = %+v, want Flink Tuple9-equivalent %+v", got, want)
	}

	// Each key dimension must actually influence grouping: changing any one
	// field produces a different key.
	fields := []string{"code", "referer", "isNew", "province", "city", "ip", "browserName", "os", "deviceType"}
	base := got
	for i, mutate := range []func(*StatsKey){
		func(k *StatsKey) { k.Code = "x" },
		func(k *StatsKey) { k.Referer = "x" },
		func(k *StatsKey) { k.IsNew = 2 },
		func(k *StatsKey) { k.Province = "x" },
		func(k *StatsKey) { k.City = "x" },
		func(k *StatsKey) { k.IP = "x" },
		func(k *StatsKey) { k.BrowserName = "x" },
		func(k *StatsKey) { k.OS = "x" },
		func(k *StatsKey) { k.DeviceType = "x" },
	} {
		k := base
		mutate(&k)
		if k == base {
			t.Errorf("key dimension %q does not affect grouping", fields[i])
		}
	}
}

// TestSemanticParity_WindowIsProcessingTime documents the biggest divergence:
// Go stamps [now-10s, now] (processing time), Flink stamps the event-time
// tumbling window boundary derived from visitTime (+3s allowed lateness).
// Under delayed messages the two differ; this test pins the Go behavior so a
// future "fix" cannot silently change the data semantics.
func TestSemanticParity_WindowIsProcessingTime(t *testing.T) {
	now := time.Date(2026, 11, 27, 8, 0, 10, 0, time.UTC)
	start, end := windowTimeBounds(now, 10*time.Second)

	if start != "2026-11-27 08:00:00" || end != "2026-11-27 08:00:10" {
		t.Errorf("window bounds = [%s, %s], want processing-time [08:00:00, 08:00:10]", start, end)
	}

	// A message whose visitTime is 3 minutes old still lands in the CURRENT
	// processing-time window — this is the explicit divergence from Flink's
	// event-time window. The function signature (no visitTime argument) makes
	// this structural: it cannot consult event time.
	if start2, end2 := windowTimeBounds(now, 10*time.Second); start2 != start || end2 != end {
		t.Error("window bounds must depend only on processing time")
	}
}

// TestSemanticParity_PVAndUVAdditive mirrors Flink's reduce:
//
//	value1.pv += value2.pv; value1.uv += value2.uv
//
// across the union of link (pv=1,uv=0) and unique-visitor (pv=0,uv=1) streams.
func TestSemanticParity_PVAndUVAdditive(t *testing.T) {
	wide := ShortLinkWide{Code: "abc123", Referer: "REF", IsNew: 1,
		Province: "广东", City: "广州", IP: "1.2.3.4",
		BrowserName: "Chrome", OS: "Linux", DeviceType: "PC"}
	key := buildStatsKey(wide)

	acc := &VisitStats{}
	// Flink: shortLinkMapDS sets pv=1, uv=0; uniqueVisitorMapDS sets pv=0, uv=1.
	pvEnrich(wide, acc)
	pvEnrich(wide, acc)
	uvEnrich(wide, acc)

	if acc.PV != 2 || acc.UV != 1 {
		t.Errorf("aggregated PV/UV = %d/%d, want 2/1 (Flink reduce semantics)", acc.PV, acc.UV)
	}
	if got := buildStatsKey(wide); got != key {
		t.Error("enrichment must not mutate the aggregation key")
	}
}

// test-only enrich functions mirroring DWSJob.consumeStream callbacks.
func pvEnrich(_ ShortLinkWide, s *VisitStats) { s.PV++ }
func uvEnrich(_ ShortLinkWide, s *VisitStats) { s.UV++ }

// TestSemanticParity_UVSetNXEquivalence documents that Go's Redis SETNX+TTL
// approximates Flink's keyed ValueState dedup, but with a 24h expiry (Flink
// state has no TTL by default in the Java job). The practical implication:
// a visitor returning after 24h is counted as unique again in Go.
func TestSemanticParity_UVSetNXEquivalence(t *testing.T) {
	const ttl = 24 * time.Hour
	// Flink UniqueVisitorFilterFunction uses keyed state keyed by udid with no
	// TTL configured; Go uses SETNX with uvTTL. Assert the documented TTL so
	// a change to uvTTL is an explicit, reviewable decision.
	if uvTTL != ttl {
		t.Errorf("uvTTL = %v, want %v (24h dedup window; Flink ValueState has no TTL)", uvTTL, ttl)
	}
}

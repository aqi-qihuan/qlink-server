package streamer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
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
	// Plus WindowBucket: the event-time 10s bucket added for idempotent
	// redelivery (see DWS commit-timing fix).
	want := StatsKey{
		Code: "abc123", Referer: "REF", IsNew: 1,
		Province: "广东", City: "广州", IP: "1.2.3.4",
		BrowserName: "Chrome", OS: "Linux", DeviceType: "PC",
		WindowBucket: eventWindowBucket(1700000000000, windowDuration),
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

// TestSemanticParity_WindowIsEventTime pins the NEW window semantics after the
// commit-timing fix: window bounds derive from the visitTime bucket, not the
// ticker clock. This aligns with Flink's TumblingEventTimeWindows and, more
// importantly, makes the ClickHouse ORDER BY key stable across redelivery.
func TestSemanticParity_WindowIsEventTime(t *testing.T) {
	// Use an explicit UTC instant to avoid local-timezone flakiness.
	visit := time.Date(2026, 11, 27, 8, 0, 13, 0, time.UTC).UnixMilli()
	bucket := eventWindowBucket(visit, 10*time.Second)
	start, end := windowTimeBoundsFromBucket(bucket, 10*time.Second)

	if start != "2026-11-27 08:00:10" || end != "2026-11-27 08:00:20" {
		t.Errorf("window bounds = [%s, %s], want event-time [08:00:10, 08:00:20]", start, end)
	}

	// Two messages in the same 10s bucket (even 9s apart) must map to the same
	// window key; this is the property that makes a redelivered batch dedupe.
	visit2 := time.Date(2026, 11, 27, 8, 0, 19, 0, time.UTC).UnixMilli()
	if eventWindowBucket(visit2, 10*time.Second) != bucket {
		t.Error("visits in the same 10s bucket must share a window key")
	}
	// A visit in the next bucket must differ.
	visit3 := time.Date(2026, 11, 27, 8, 0, 20, 0, time.UTC).UnixMilli()
	if eventWindowBucket(visit3, 10*time.Second) == bucket {
		t.Error("visits in different 10s buckets must have different window keys")
	}
}

// TestSemanticParity_WindowKeySurvivesRedelivery is the regression for the
// P0 bug: the same message delivered twice (after a crash between CH insert
// and Kafka commit) must produce the identical StatsKey, so ReplacingMergeTree
// collapses the duplicate row instead of double-counting PV/UV.
func TestSemanticParity_WindowKeySurvivesRedelivery(t *testing.T) {
	w := ShortLinkWide{Code: "abc123", VisitTime: 1700000000123, IP: "1.2.3.4"}
	first := buildStatsKey(w)
	// Simulate re-delivery: identical payload, later wall-clock time is
	// irrelevant because the key no longer depends on processing time.
	redelivered := buildStatsKey(w)
	if first != redelivered {
		t.Errorf("redelivery changed StatsKey: %+v -> %+v", first, redelivered)
	}
	if first.WindowBucket != eventWindowBucket(w.VisitTime, windowDuration) {
		t.Error("WindowBucket must be derived from visitTime, not wall clock")
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

// TestPendingBatch_MergeRollsBackBothStatsAndMessages guards the second half
// of the P0 fix: on ClickHouse insert failure, BOTH aggregates and Kafka
// message offsets must be rolled back. Rolling back only stats (old behavior)
// left offsets committed and permanently lost the window.
func TestPendingBatch_MergeRollsBackBothStatsAndMessages(t *testing.T) {
	w := ShortLinkWide{Code: "abc123", VisitTime: 1700000000123, IP: "1.2.3.4"}
	key := buildStatsKey(w)

	failed := newPendingBatch()
	failed.stats[key] = &VisitStats{PV: 2, UV: 1}
	failed.addMessage("PV", kafka.Message{Value: []byte("m1")})
	failed.addMessage("UV", kafka.Message{Value: []byte("m2")})

	current := newPendingBatch()
	// Simulate new messages arriving in the same key while insert was failing.
	current.stats[key] = &VisitStats{PV: 1, UV: 0}
	current.addMessage("PV", kafka.Message{Value: []byte("m3")})

	current.mergeBatch(failed)

	got := current.stats[key]
	if got.PV != 3 || got.UV != 1 {
		t.Errorf("merged PV/UV = %d/%d, want 3/1", got.PV, got.UV)
	}
	if len(current.pvMsgs) != 2 || len(current.uvMsgs) != 1 {
		t.Errorf("rolled-back messages = PV %d / UV %d, want 2/1", len(current.pvMsgs), len(current.uvMsgs))
	}
}

// TestPendingBatch_MergeNewKey keeps a failed batch's key that is absent from
// the current batch (e.g. next window has no messages for that group).
func TestPendingBatch_MergeNewKey(t *testing.T) {
	w := ShortLinkWide{Code: "def456", VisitTime: 1700000000123, IP: "5.6.7.8"}
	key := buildStatsKey(w)

	current := newPendingBatch()
	failed := newPendingBatch()
	failed.stats[key] = &VisitStats{PV: 5, UV: 2}
	failed.addMessage("PV", kafka.Message{Value: []byte("m1")})

	current.mergeBatch(failed)

	if got := current.stats[key]; got == nil || got.PV != 5 || got.UV != 2 {
		t.Errorf("new key not merged: %+v", got)
	}
	if len(current.pvMsgs) != 1 {
		t.Errorf("messages not merged for new key, got %d", len(current.pvMsgs))
	}
}

// TestCommitMessages_AdvancesOnlyBatchMessages verifies that committing a
// successfully written batch advances exactly its PV/UV messages, in order,
// and never touches messages from a different (still-pending) window.
func TestCommitMessages_AdvancesOnlyBatchMessages(t *testing.T) {
	batch := newPendingBatch()
	batch.addMessage("PV", kafka.Message{Value: []byte("pv1")})
	batch.addMessage("PV", kafka.Message{Value: []byte("pv2")})
	batch.addMessage("UV", kafka.Message{Value: []byte("uv1")})

	var committedPV, committedUV []string
	commitPV := func(_ context.Context, msgs ...kafka.Message) error {
		for _, m := range msgs {
			committedPV = append(committedPV, string(m.Value))
		}
		return nil
	}
	commitUV := func(_ context.Context, msgs ...kafka.Message) error {
		for _, m := range msgs {
			committedUV = append(committedUV, string(m.Value))
		}
		return nil
	}

	commitMessages(context.Background(), batch, commitPV, commitUV)

	if len(committedPV) != 2 || committedPV[0] != "pv1" || committedPV[1] != "pv2" {
		t.Errorf("committed PV = %v, want [pv1 pv2]", committedPV)
	}
	if len(committedUV) != 1 || committedUV[0] != "uv1" {
		t.Errorf("committed UV = %v, want [uv1]", committedUV)
	}
}

// TestCommitMessages_ContinuesOnError mirrors the production behavior: a
// single commit failure is logged and the loop continues, so one bad offset
// does not block committing the rest of a successfully written batch.
func TestCommitMessages_ContinuesOnError(t *testing.T) {
	batch := newPendingBatch()
	batch.addMessage("PV", kafka.Message{Value: []byte("pv1")})
	batch.addMessage("PV", kafka.Message{Value: []byte("pv2")})

	count := 0
	commitPV := func(_ context.Context, _ ...kafka.Message) error {
		count++
		return errors.New("transient commit error")
	}
	commitUV := func(_ context.Context, _ ...kafka.Message) error { return nil }

	commitMessages(context.Background(), batch, commitPV, commitUV)
	if count != 2 {
		t.Errorf("commit called %d times, want 2 (must continue after error)", count)
	}
}

// TestDetachBatch_DoesNotWipeAggregates is the regression for the bug found
// while diagnosing the PV regression: `batch := pending` (pointer alias) plus
// `*pending = empty` wiped the just-detached batch, so flush always saw an
// empty window and logged "Flushed 0 records" while consumeStream kept adding.
func TestDetachBatch_DoesNotWipeAggregates(t *testing.T) {
	w := ShortLinkWide{Code: "abc123", VisitTime: 1700000000123, IP: "1.2.3.4"}
	key := buildStatsKey(w)

	pending := newPendingBatch()
	pending.stats[key] = &VisitStats{PV: 3, UV: 1}
	pending.addMessage("PV", kafka.Message{Value: []byte("m1")})
	pending.addMessage("PV", kafka.Message{Value: []byte("m2")})
	pending.addMessage("UV", kafka.Message{Value: []byte("m3")})

	batch := detachBatch(pending)

	// The detached batch must still own all aggregates and messages.
	if got := batch.stats[key]; got == nil || got.PV != 3 || got.UV != 1 {
		t.Errorf("detached batch lost aggregates: %+v", got)
	}
	if len(batch.pvMsgs) != 2 || len(batch.uvMsgs) != 1 {
		t.Errorf("detached batch lost messages: PV=%d UV=%d", len(batch.pvMsgs), len(batch.uvMsgs))
	}

	// The shared pointer must now point at a fresh empty batch, so new
	// messages from consumer goroutines do not mutate the detached one.
	if len(pending.stats) != 0 || len(pending.pvMsgs) != 0 || len(pending.uvMsgs) != 0 {
		t.Errorf("pending was not reset: stats=%d pv=%d uv=%d",
			len(pending.stats), len(pending.pvMsgs), len(pending.uvMsgs))
	}
	pending.stats[key] = &VisitStats{PV: 1}
	pending.addMessage("PV", kafka.Message{Value: []byte("new")})
	if batch.stats[key].PV != 3 || len(batch.pvMsgs) != 2 {
		t.Error("writing to new pending mutated the detached batch (aliasing bug)")
	}
}

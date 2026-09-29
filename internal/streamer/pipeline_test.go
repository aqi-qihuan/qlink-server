package streamer

import (
	"testing"
	"time"
)

// TestFormatDate verifies epoch-millis → "yyyy-MM-dd" under the configured
// timezone, including the production anchor from the 2026-09-28 pipeline run
// (ts=1790610519943 was 2026-09-28 23:48:39.943 +08:00).
func TestFormatDate(t *testing.T) {
	restoreTimezone(t, "Asia/Shanghai")

	cases := []struct {
		tsMillis int64
		want     string
	}{
		{1790610519943, "2026-09-28"}, // production anchor
		{1790610662136, "2026-09-28"}, // second code from same run
		{0, "1970-01-01"},
		{1699999999999, "2023-11-15"},
	}
	for _, c := range cases {
		if got := FormatDate(c.tsMillis); got != c.want {
			t.Errorf("FormatDate(%d) = %q, want %q", c.tsMillis, got, c.want)
		}
	}
}

// TestFormatDate_TimezoneBoundary: 00:30 +08 is calendar day 09-29 in
// Shanghai but still 09-28 16:30 UTC — proves the formatter uses the
// configured timezone, not process-local UTC.
func TestFormatDate_TimezoneBoundary(t *testing.T) {
	// 2026-09-29 00:30:00 +08:00 == 2026-09-28 16:30:00 UTC
	const ts = 1790613000000
	restoreTimezone(t, "Asia/Shanghai")
	if got := FormatDate(ts); got != "2026-09-29" {
		t.Errorf("Asia/Shanghai: FormatDate(%d) = %q, want 2026-09-29", ts, got)
	}
	SetTimezone(time.UTC)
	if got := FormatDate(ts); got != "2026-09-28" {
		t.Errorf("UTC: FormatDate(%d) = %q, want 2026-09-28", ts, got)
	}
}

// TestFormatDateTime verifies "yyyy-MM-dd HH:mm:ss" formatting.
func TestFormatDateTime(t *testing.T) {
	restoreTimezone(t, "Asia/Shanghai")

	// Production anchor: UV sink message visitTime 1790610519943
	// == 2026-09-28 23:48:39 +08:00.
	got := FormatDateTime(time.UnixMilli(1790610519943))
	if want := "2026-09-28 23:48:39"; got != want {
		t.Errorf("FormatDateTime = %q, want %q", got, want)
	}

	// Midnight rollover: 2026-09-29 00:30:00 +08:00.
	got = FormatDateTime(time.UnixMilli(1790613000000))
	if want := "2026-09-29 00:30:00"; got != want {
		t.Errorf("FormatDateTime = %q, want %q", got, want)
	}
}

// restoreTimezone sets the streamer timezone and registers cleanup back to
// UTC (the init default), so tests never leak state into each other.
func restoreTimezone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	SetTimezone(loc)
	t.Cleanup(func() { SetTimezone(time.UTC) })
}

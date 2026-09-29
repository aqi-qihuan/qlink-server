package component

import (
	"strings"
	"testing"

	"github.com/aqi/qlink-server/internal/link/sharding"
)

// TestCreateShortLinkCode_Deterministic verifies the same input always
// produces the same code (MurmurHash3 + base62 are deterministic).
func TestCreateShortLinkCode_Deterministic(t *testing.T) {
	params := []string{
		"1234567890&https://example.com",
		"0&https://aqi125.cn",
		"42&https://example.com/very/long/path?q=1",
	}
	for _, p := range params {
		first := CreateShortLinkCode(p)
		for i := 0; i < 10; i++ {
			if got := CreateShortLinkCode(p); got != first {
				t.Fatalf("CreateShortLinkCode(%q) not deterministic: %q vs %q", p, first, got)
			}
		}
	}
}

// TestCreateShortLinkCode_Format verifies the code layout:
// dbPrefix + base62(hash) + tableSuffix, where prefix ∈ {0,1,a} and
// suffix ∈ {0,a} (matching Java's sharding config).
func TestCreateShortLinkCode_Format(t *testing.T) {
	validPrefix := map[string]bool{"0": true, "1": true, "a": true}
	validSuffix := map[string]bool{"0": true, "a": true}

	for _, p := range []string{"1&https://example.com", "999&https://example.org/x"} {
		code := CreateShortLinkCode(p)
		if len(code) < 3 {
			t.Fatalf("code %q too short", code)
		}
		prefix := string(code[0])
		suffix := string(code[len(code)-1])
		if !validPrefix[prefix] {
			t.Errorf("code %q: invalid DB prefix %q (want one of 0/1/a)", code, prefix)
		}
		if !validSuffix[suffix] {
			t.Errorf("code %q: invalid table suffix %q (want one of 0/a)", code, suffix)
		}
	}
}

// TestCreateShortLinkCode_RouteRoundTrip is the critical invariant from the
// 2026-09-28 incident investigation: RouteShortLink must recover exactly the
// prefix/suffix embedded by CreateShortLinkCode. Verified against 10 rows of
// production data — this pair is the backbone of DB/table sharding.
func TestCreateShortLinkCode_RouteRoundTrip(t *testing.T) {
	for _, p := range []string{
		"1&https://example.com",
		"2&https://example.com/a",
		"3&https://example.com/b",
		"1234567890&https://aqi125.cn",
		"777&https://example.com/collision-retry",
	} {
		code := CreateShortLinkCode(p)
		dbPrefix, tableSuffix := sharding.RouteShortLink(code)
		if got := string(code[0]); dbPrefix != got {
			t.Errorf("code %q: RouteShortLink dbPrefix=%q, want %q", code, dbPrefix, got)
		}
		if got := string(code[len(code)-1]); tableSuffix != got {
			t.Errorf("code %q: RouteShortLink tableSuffix=%q, want %q", code, tableSuffix, got)
		}
	}
}

// TestCreateShortLinkCode_DistinctInputs verifies different params produce
// different codes (hash spread). Same URL with different snowflake versions
// must not collide.
func TestCreateShortLinkCode_DistinctInputs(t *testing.T) {
	codes := make(map[string]bool)
	for _, v := range []string{"1&https://example.com", "2&https://example.com", "3&https://example.com"} {
		code := CreateShortLinkCode(v)
		if codes[code] {
			t.Errorf("collision on %q -> %q", v, code)
		}
		codes[code] = true
	}
}

// TestPrepareUrlForHash covers both branches: version<=0 delegates to
// AddUrlPrefix (snowflake), version>0 formats "version&url".
func TestPrepareUrlForHash(t *testing.T) {
	// version > 0: explicit "version&url" format
	got := PrepareUrlForHash("https://example.com", 3)
	if want := "3&https://example.com"; got != want {
		t.Errorf("PrepareUrlForHash(version=3) = %q, want %q", got, want)
	}

	// version <= 0: snowflake prefix (format "digits&url", snowflake IDs are positive)
	got = PrepareUrlForHash("https://example.com", 0)
	if !strings.Contains(got, "&https://example.com") {
		t.Errorf("PrepareUrlForHash(version=0) = %q, want snowflake-prefixed URL", got)
	}
	idx := strings.Index(got, "&")
	if idx <= 0 || !isAllDigits(got[:idx]) {
		t.Errorf("PrepareUrlForHash(version=0) = %q: prefix before & should be numeric snowflake ID", got)
	}
}

// TestIncrementUrlVersion covers collision-retry version bumping.
func TestIncrementUrlVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1&https://example.com", "2&https://example.com"},
		{"9&https://example.com", "10&https://example.com"},
		{"123&https://example.com/a?b=c", "124&https://example.com/a?b=c"},
		{"https://example.com/no-prefix", "https://example.com/no-prefix"}, // no prefix → unchanged
	}
	for _, c := range cases {
		if got := IncrementUrlVersion(c.in); got != c.want {
			t.Errorf("IncrementUrlVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

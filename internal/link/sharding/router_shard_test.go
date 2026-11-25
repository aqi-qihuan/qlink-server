package sharding

import "testing"

// TestIsValidShortLinkCode locks the public-endpoint guard: only codes whose
// first char is a known DB prefix and last char a known table suffix can be
// routed to an existing physical table. Everything else must be rejected
// before any DB query (otherwise a crawler string produces a 500).
func TestIsValidShortLinkCode(t *testing.T) {
	cases := []struct {
		name string
		code string
		want bool
	}{
		{"valid db0 table0", "03DpMyT0", true},
		{"valid db0 tablea", "021Tgota", true},
		{"valid db1 table0", "1oQKYJ0", true},
		{"valid db1 tablea", "115P8y2a", true},
		{"valid dba table0", "a3z3JYk0", true},
		{"valid dba tablea", "a4hEvL3a", true},
		{"real generated code", "14rBqCZ0", true},
		{"unknown table suffix 3", "NotExistCode123", false},
		{"unknown table suffix b", "Abcdefb", false},
		{"unknown db prefix z", "z3z3JYk0", false},
		{"unknown db prefix 9", "93rCsNC0", false},
		{"too short 2 chars", "0a", false},
		{"too short 1 char", "0", false},
		{"empty", "", false},
		{"mixed invalid both ends", "zABCDEFGHz", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsValidShortLinkCode(c.code); got != c.want {
				t.Errorf("IsValidShortLinkCode(%q) = %v, want %v", c.code, got, c.want)
			}
		})
	}
}

// TestIsValidShortLinkCode_AgreesWithCreateShortLinkCode ensures the guard
// never rejects codes actually produced by the generator.
func TestIsValidShortLinkCode_AgreesWithGenerator(t *testing.T) {
	for _, suffix := range TableSuffixList {
		for _, prefix := range DBPrefixList {
			code := prefix + "ab12cd" + suffix
			if !IsValidShortLinkCode(code) {
				t.Errorf("generated-shaped code %q rejected", code)
			}
		}
	}
}

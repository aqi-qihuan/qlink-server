package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestURLSafeChecker_PatternAndProtocol covers the pure validation branches —
// they return before any network request is made.
func TestURLSafeChecker_PatternAndProtocol(t *testing.T) {
	checker := NewURLSafeChecker()

	cases := []struct {
		name       string
		url        string
		wantSafe   bool
		wantMsgHas string
	}{
		{"javascript scheme", "javascript:alert(1)", false, "Unsupported protocol"},
		{"ftp scheme", "ftp://files.example.com/x", false, "Unsupported protocol"},
		{"phish pattern", "https://example.com/phish-page", false, "suspicious pattern"},
		{"exe pattern", "https://example.com/download.exe", false, "suspicious pattern"},
		{"scr pattern", "http://example.com/screensaver.scr", false, "suspicious pattern"},
		{"cmd pattern uppercase", "http://example.com/CMD.BAT", false, "suspicious pattern"}, // lowercased before match
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := checker.Check(c.url)
			if r.Safe != c.wantSafe {
				t.Errorf("Check(%q).Safe = %v, want %v (msg: %s)", c.url, r.Safe, c.wantSafe, r.Message)
			}
			if c.wantMsgHas != "" && !contains(r.Message, c.wantMsgHas) {
				t.Errorf("Check(%q).Message = %q, want substring %q", c.url, r.Message, c.wantMsgHas)
			}
		})
	}
}

// TestURLSafeChecker_Reachable runs the full pipeline against a local
// httptest server — no external network is touched.
func TestURLSafeChecker_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/gone":
			w.WriteHeader(http.StatusNotFound)
		case "/boom":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	checker := NewURLSafeChecker()

	t.Run("200 reachable", func(t *testing.T) {
		r := checker.Check(srv.URL + "/ok")
		if !r.Safe || !r.Reachable {
			t.Errorf("Safe=%v Reachable=%v, want both true (msg: %s)", r.Safe, r.Reachable, r.Message)
		}
		if r.SSL {
			t.Error("http-scheme URL must not be flagged SSL (https probe to an http server fails)")
		}
	})

	t.Run("404 not reachable", func(t *testing.T) {
		r := checker.Check(srv.URL + "/gone")
		if !r.Safe {
			t.Errorf("404 should still be Safe=true (safety != availability), got false")
		}
		if r.Reachable {
			t.Error("404 must be Reachable=false")
		}
		if !contains(r.Message, "HTTP 404") {
			t.Errorf("Message = %q, want 'HTTP 404' substring", r.Message)
		}
	})

	t.Run("500 still reachable", func(t *testing.T) {
		// Only 4xx is treated as unreachable; 5xx is a server-side issue but
		// the URL itself responds.
		r := checker.Check(srv.URL + "/boom")
		if !r.Reachable {
			t.Errorf("500 must be Reachable=true (only 4xx counts), msg: %s", r.Message)
		}
	})
}

// TestURLSafeChecker_HTTPSSchemeBlockedPort verifies the https fast path:
// scheme=https → SSL=true without any successful request, while reachability
// reflects the actual connection failure (port 1 on loopback refuses fast).
func TestURLSafeChecker_HTTPSSchemeBlockedPort(t *testing.T) {
	checker := NewURLSafeChecker()
	r := checker.Check("https://127.0.0.1:1/never")
	if !r.SSL {
		t.Error("https scheme must set SSL=true directly")
	}
	if r.Reachable {
		t.Error("connection-refused URL must be Reachable=false")
	}
	if !r.Safe {
		t.Errorf("unreachable-but-well-formed https URL must stay Safe=true, msg: %s", r.Message)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

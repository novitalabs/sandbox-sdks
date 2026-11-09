package core

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{"equal", "1.2.3", "1.2.3", 0},
		{"patch lower", "1.2.2", "1.2.3", -1},
		{"patch higher", "1.2.4", "1.2.3", 1},
		{"minor lower", "1.1.9", "1.2.0", -1},
		{"major higher", "2.0.0", "1.9.9", 1},
		{"missing segments equal", "1.2", "1.2.0", 0},
		{"missing segments lower", "1.2", "1.2.1", -1},
		{"pre-release sorts lower", "1.0.0-alpha", "1.0.0", -1},
		{"bare sorts higher", "1.0.0", "1.0.0-rc1", 1},
		{"v prefix ignored", "v1.2.3", "1.2.3", 0},
		{"debug fallback", "99.99.99", "0.5.7", 1},
		{"stdin gate met", "0.3.0", EnvdCommandsStdin, 0},
		{"stdin gate unmet", "0.2.9", EnvdCommandsStdin, -1},
		{"octet stream gate", "0.5.7", EnvdOctetStreamUpload, 0},
		{"default user gate", "0.3.9", EnvdDefaultUser, -1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareVersions(tc.a, tc.b); got != tc.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestIsLegacyDomain(t *testing.T) {
	t.Setenv("NOVITA_IS_LEGACY", "")
	cases := map[string]bool{
		"sandbox.novita.ai":          true,
		"us-ga-1.sandbox.novita.ai":  true,
		"us-01.sandbox.novita.ai":    false,
		"us-ga-01.sandbox.novita.ai": false,
		"us-phx-1.sandbox.novita.ai": false,
		"preview.example.invalid":    false,
		"dev.example.invalid":        false,
	}
	for domain, want := range cases {
		if got := isLegacyDomain(domain); got != want {
			t.Errorf("isLegacyDomain(%q) = %v, want %v", domain, got, want)
		}
	}
}

func TestNormalizeAPIURL(t *testing.T) {
	cases := map[string]string{
		"api.example.com":       "https://api.example.com",
		"https://api.test.com":  "https://api.test.com",
		"http://localhost:3000": "http://localhost:3000",
		"":                      "",
	}

	for in, want := range cases {
		if got := normalizeAPIURL(in); got != want {
			t.Errorf("normalizeAPIURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAuthenticationHeader(t *testing.T) {
	// envd older than 0.4.0 without an explicit user falls back to the default user.
	headers := authenticationHeader("0.3.0", "")
	if headers["Authorization"] != "Basic dXNlcjo=" { // base64("user:")
		t.Errorf("expected default user header, got %q", headers["Authorization"])
	}

	// Newer envd without a user sends no auth header.
	if headers := authenticationHeader("0.4.0", ""); len(headers) != 0 {
		t.Errorf("expected no auth header, got %v", headers)
	}

	// An explicit user is always encoded.
	headers = authenticationHeader("0.4.0", "root")
	if headers["Authorization"] != "Basic cm9vdDo=" { // base64("root:")
		t.Errorf("expected root header, got %q", headers["Authorization"])
	}
}

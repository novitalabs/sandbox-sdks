package core

import (
	"os"
	"testing"
)

func clearNovitaEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"NOVITA_API_KEY", "NOVITA_ACCESS_TOKEN", "NOVITA_DOMAIN",
		"NOVITA_API_URL", "NOVITA_SANDBOX_URL", "NOVITA_DEBUG",
		"NOVITA_REQUEST_TIMEOUT", "NOVITA_IS_LEGACY",
	} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
}

func TestNewConnectionConfigDefaults(t *testing.T) {
	clearNovitaEnv(t)

	opts := &SandboxOpts{APIKey: "test-key"}
	cfg, err := NewConnectionConfig(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Domain != DefaultNovitaDomain {
		t.Errorf("Domain = %q, want %q", cfg.Domain, DefaultNovitaDomain)
	}
	if cfg.APIURL != "https://api."+DefaultNovitaDomain {
		t.Errorf("APIURL = %q, want %q", cfg.APIURL, "https://api."+DefaultNovitaDomain)
	}
	if cfg.IsLegacyDomain {
		t.Error("expected non-legacy domain")
	}
	if cfg.RequestTimeoutMs != RequestTimeoutMS {
		t.Errorf("RequestTimeoutMs = %d, want %d", cfg.RequestTimeoutMs, RequestTimeoutMS)
	}
	if cfg.Headers["X-Novita-SDK-Name"] != SDKName {
		t.Errorf("X-Novita-SDK-Name = %q, want %q", cfg.Headers["X-Novita-SDK-Name"], SDKName)
	}
}

func TestNewConnectionConfigLegacyDomainTimeout(t *testing.T) {
	clearNovitaEnv(t)

	cfg, err := NewConnectionConfig(&SandboxOpts{
		APIKey: "key",
		Domain: "sandbox.novita.ai",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.IsLegacyDomain {
		t.Error("expected legacy domain to be detected")
	}
	if cfg.RequestTimeoutMs != LegacyRequestTimeoutMS {
		t.Errorf("RequestTimeoutMs = %d, want %d", cfg.RequestTimeoutMs, LegacyRequestTimeoutMS)
	}
}

func TestNewConnectionConfigCustomHeaders(t *testing.T) {
	clearNovitaEnv(t)

	cfg, err := NewConnectionConfig(&SandboxOpts{
		APIKey:  "key",
		Headers: map[string]string{"X-Custom": "value"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Headers["X-Custom"] != "value" {
		t.Error("custom header was not preserved")
	}
	if cfg.Headers["X-Novita-SDK-Version"] != SDKVersion {
		t.Error("SDK version header missing")
	}
}

func TestGetSandboxURL(t *testing.T) {
	clearNovitaEnv(t)

	cfg, err := NewConnectionConfig(&SandboxOpts{APIKey: "key"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := cfg.GetSandboxURL("abc123", "", EnvdPort)
	want := "https://49983-abc123." + DefaultNovitaDomain
	if got != want {
		t.Errorf("GetSandboxURL = %q, want %q", got, want)
	}

	// An explicit sandbox URL wins.
	cfg2, err := NewConnectionConfig(&SandboxOpts{
		APIKey:     "key",
		SandboxURL: "https://custom.example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg2.GetSandboxURL("abc123", "", EnvdPort); got != "https://custom.example.com" {
		t.Errorf("GetSandboxURL = %q, want override", got)
	}
}

func TestGetHostDebug(t *testing.T) {
	clearNovitaEnv(t)

	debug := true
	cfg, err := NewConnectionConfig(&SandboxOpts{APIKey: "key", Debug: &debug})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.GetHost("abc", 8080, ""); got != "localhost:8080" {
		t.Errorf("GetHost in debug = %q, want localhost:8080", got)
	}
}

func TestNewClientRequiresAPIKey(t *testing.T) {
	clearNovitaEnv(t)

	if _, err := NewClient(&SandboxOpts{}); err == nil {
		t.Fatal("expected an error when no API key is configured")
	}
}

func TestNewClientDebugDoesNotRequireAPIKey(t *testing.T) {
	clearNovitaEnv(t)
	debug := true
	if _, err := NewClient(&SandboxOpts{Debug: &debug}); err != nil {
		t.Fatalf("NewClient debug failed: %v", err)
	}
}

func TestNewClientReadsAPIKeyFromEnv(t *testing.T) {
	clearNovitaEnv(t)
	t.Setenv("NOVITA_API_KEY", "env-key")

	client, err := NewClient(&SandboxOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.Connection().APIKey != "env-key" {
		t.Errorf("APIKey = %q, want env-key", client.Connection().APIKey)
	}
}

func TestLegacyModePolicy(t *testing.T) {
	cases := []struct {
		name, domain, mode string
		legacy             bool
		timeout            int
	}{
		{"automatic sandbox.novita.ai", "sandbox.novita.ai", "", true, 1800000},
		{"automatic us-ga-1.sandbox.novita.ai", "us-ga-1.sandbox.novita.ai", "", true, 1800000},
		{"automatic us-01.sandbox.novita.ai", "us-01.sandbox.novita.ai", "", false, 60000},
		{"automatic us-ga-01.sandbox.novita.ai", "us-ga-01.sandbox.novita.ai", "", false, 60000},
		{"automatic us-phx-1.sandbox.novita.ai", "us-phx-1.sandbox.novita.ai", "", false, 60000},
		{"automatic preview.example.invalid", "preview.example.invalid", "", false, 60000},
		{"automatic dev.example.invalid", "dev.example.invalid", "", false, 60000},
		{"override true private.example.invalid", "private.example.invalid", "true", true, 1800000},
		{"override  TRUE  private.example.invalid", "private.example.invalid", " TRUE ", true, 1800000},
		{"override false sandbox.novita.ai", "sandbox.novita.ai", "false", false, 60000},
		{"override  FALSE  sandbox.novita.ai", "sandbox.novita.ai", " FALSE ", false, 60000},
		{"override auto sandbox.novita.ai", "sandbox.novita.ai", "auto", true, 1800000},
		{"override auto private.example.invalid", "private.example.invalid", "auto", false, 60000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearNovitaEnv(t)
			t.Setenv("NOVITA_IS_LEGACY", tc.mode)
			cfg, err := NewConnectionConfig(&SandboxOpts{Domain: tc.domain, APIURL: "https://api.modern.example.invalid"})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.IsLegacyDomain != tc.legacy || cfg.RequestTimeoutMs != tc.timeout {
				t.Fatalf("legacy=%v timeout=%d, want legacy=%v timeout=%d", cfg.IsLegacyDomain, cfg.RequestTimeoutMs, tc.legacy, tc.timeout)
			}
			if cfg.APIURL != "https://api.modern.example.invalid" {
				t.Fatal("legacy mode changed the API URL")
			}
			explicit := 42000
			cfg, err = NewConnectionConfig(&SandboxOpts{Domain: tc.domain, RequestTimeoutMs: &explicit})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RequestTimeoutMs != explicit {
				t.Fatal("legacy mode replaced the explicit request timeout")
			}
		})
	}
}

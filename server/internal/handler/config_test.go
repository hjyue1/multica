package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetConfigIncludesRuntimeAuthConfig(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Setenv("ALLOW_SIGNUP", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "google-client-id")
	t.Setenv("POSTHOG_API_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if cfg.CdnDomain != "cdn.example.com" {
		t.Fatalf("cdn_domain: want cdn.example.com, got %q", cfg.CdnDomain)
	}
	if cfg.AllowSignup {
		t.Fatalf("allow_signup: want false, got true")
	}
	if cfg.GoogleClientID != "google-client-id" {
		t.Fatalf("google_client_id: want google-client-id, got %q", cfg.GoogleClientID)
	}
	if !cfg.Auth.EmailLoginEnabled {
		t.Fatal("auth.email_login_enabled: want true, got false")
	}
	if !cfg.Auth.GoogleLoginEnabled {
		t.Fatal("auth.google_login_enabled: want true, got false")
	}
	if cfg.PosthogKey != "phc_test" {
		t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
	}
	if cfg.PosthogHost != "https://eu.i.posthog.com" {
		t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
	}
	if cfg.AnalyticsEnvironment != "dev" {
		t.Fatalf("analytics_environment: want dev, got %q", cfg.AnalyticsEnvironment)
	}
}

func TestGetConfigIncludesCASConfig(t *testing.T) {
	origCfg := testHandler.cfg
	testHandler.cfg = Config{
		EmailLoginDisabled:           true,
		GoogleLoginDisabled:          true,
		InvitationEmailDisabled:      true,
		AutoAcceptInvitationsOnLogin: true,
		CAS: CASConfig{
			Enabled:     true,
			DisplayName: "Acme SSO",
			LoginURL:    "https://sso.example.com/cas/login",
			ValidateURL: "https://sso.example.com/cas/serviceValidate",
			ServiceURL:  "https://api.example.com/auth/cas/callback",
		},
	}
	defer func() { testHandler.cfg = origCfg }()

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Host = "internal:8080"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "api.example.com")
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.Auth.EmailLoginEnabled {
		t.Fatal("auth.email_login_enabled: want false, got true")
	}
	if cfg.Auth.GoogleLoginEnabled {
		t.Fatal("auth.google_login_enabled: want false, got true")
	}
	if cfg.Auth.InvitationEmailEnabled {
		t.Fatal("auth.invitation_email_enabled: want false, got true")
	}
	if !cfg.Auth.AutoAcceptInvitationsOnLogin {
		t.Fatal("auth.auto_accept_invitations_on_login: want true, got false")
	}
	if cfg.GoogleClientID != "" {
		t.Fatalf("google_client_id: want empty when Google disabled, got %q", cfg.GoogleClientID)
	}
	if cfg.Auth.CAS == nil {
		t.Fatal("auth.cas: expected config")
	}
	if cfg.Auth.CAS.DisplayName != "Acme SSO" {
		t.Fatalf("auth.cas.display_name: want Acme SSO, got %q", cfg.Auth.CAS.DisplayName)
	}
	if cfg.Auth.CAS.LoginURL != "https://api.example.com/auth/cas/start" {
		t.Fatalf("auth.cas.login_url: got %q", cfg.Auth.CAS.LoginURL)
	}
}

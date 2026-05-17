package handler

import (
	"net/http"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/analytics"
)

type AppConfig struct {
	CdnDomain string `json:"cdn_domain"`
	// Public auth config consumed by the web app at runtime so self-hosted
	// deployments do not need to rebuild the frontend image when operators
	// toggle signup or wire Google OAuth.
	AllowSignup    bool          `json:"allow_signup"`
	GoogleClientID string        `json:"google_client_id,omitempty"`
	Auth           AppAuthConfig `json:"auth"`

	// PostHog public config for the frontend. The key is the same Project
	// API Key the backend uses; returning it here (instead of baking it
	// into the frontend bundle via NEXT_PUBLIC_*) means self-hosted
	// instances — whose server returns an empty key — automatically
	// disable frontend event shipping too.
	PosthogKey           string `json:"posthog_key"`
	PosthogHost          string `json:"posthog_host"`
	AnalyticsEnvironment string `json:"analytics_environment"`
}

type AppAuthConfig struct {
	EmailLoginEnabled  bool          `json:"email_login_enabled"`
	GoogleLoginEnabled bool          `json:"google_login_enabled"`
	CAS                *AppCASConfig `json:"cas,omitempty"`
}

type AppCASConfig struct {
	Enabled     bool   `json:"enabled"`
	DisplayName string `json:"display_name"`
	LoginURL    string `json:"login_url"`
}

// GetConfig is mounted on the public (unauthenticated) route group because
// the web app calls it before login to decide whether to render the Google
// sign-in button and signup UI. Only add fields here that are safe to expose
// to anonymous callers — never user- or tenant-scoped data.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	googleClientID := os.Getenv("GOOGLE_CLIENT_ID")
	googleLoginEnabled := !h.cfg.GoogleLoginDisabled && googleClientID != ""
	config := AppConfig{
		AllowSignup:    os.Getenv("ALLOW_SIGNUP") != "false",
		GoogleClientID: "",
		Auth: AppAuthConfig{
			EmailLoginEnabled:  !h.cfg.EmailLoginDisabled,
			GoogleLoginEnabled: googleLoginEnabled,
		},
	}
	if googleLoginEnabled {
		config.GoogleClientID = googleClientID
	}
	if h.Storage != nil {
		config.CdnDomain = h.Storage.CdnDomain()
	}
	if h.cfg.CAS.Enabled && h.cfg.CAS.Validate() == nil {
		displayName := h.cfg.CAS.DisplayName
		if strings.TrimSpace(displayName) == "" {
			displayName = "Company SSO"
		}
		config.Auth.CAS = &AppCASConfig{
			Enabled:     true,
			DisplayName: displayName,
			LoginURL:    publicURLFromRequest(r, "/auth/cas/start"),
		}
	}

	// Re-read from env on every request so operators can rotate keys via
	// secret refresh without a server restart.
	if v := os.Getenv("ANALYTICS_DISABLED"); v != "true" && v != "1" {
		config.PosthogKey = os.Getenv("POSTHOG_API_KEY")
		config.PosthogHost = os.Getenv("POSTHOG_HOST")
		config.AnalyticsEnvironment = analytics.EnvironmentFromEnv()
		if config.PosthogHost == "" && config.PosthogKey != "" {
			config.PosthogHost = "https://us.i.posthog.com"
		}
	}

	writeJSON(w, http.StatusOK, config)
}

func publicURLFromRequest(r *http.Request, path string) string {
	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	if forwardedProto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		proto = forwardedProto
	}
	host := r.Host
	if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = forwardedHost
	}
	if host == "" {
		return path
	}
	return proto + "://" + host + path
}

func firstForwardedValue(raw string) string {
	if raw == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(raw, ",")[0])
	return strings.TrimSpace(strings.Trim(first, `"`))
}

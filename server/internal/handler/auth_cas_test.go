package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseCASServiceResponseSuccess(t *testing.T) {
	body := []byte(`
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>zhangsan</cas:user>
    <cas:attributes>
      <cas:email>zhangsan@company.com</cas:email>
      <cas:name>Zhang San</cas:name>
      <cas:avatar>https://example.com/avatar.png</cas:avatar>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`)

	user, err := parseCASServiceResponse(body)
	if err != nil {
		t.Fatalf("parseCASServiceResponse: %v", err)
	}
	if user.Username != "zhangsan" {
		t.Fatalf("username: want zhangsan, got %q", user.Username)
	}
	if user.Attributes["email"] != "zhangsan@company.com" {
		t.Fatalf("email attr: got %q", user.Attributes["email"])
	}
	if user.Attributes["name"] != "Zhang San" {
		t.Fatalf("name attr: got %q", user.Attributes["name"])
	}
}

func TestParseCASServiceResponseFailure(t *testing.T) {
	body := []byte(`
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationFailure code="INVALID_TICKET">Ticket not recognized</cas:authenticationFailure>
</cas:serviceResponse>`)

	_, err := parseCASServiceResponse(body)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Ticket not recognized") {
		t.Fatalf("expected CAS failure message, got %v", err)
	}
}

func TestNormalizeCASIdentity(t *testing.T) {
	identity, err := normalizeCASIdentity(casUser{
		Username: "zhangsan",
		Attributes: map[string]string{
			"mail":        " ZhangSan@Company.COM ",
			"displayName": "Zhang San",
			"photo":       "https://example.com/avatar.png",
		},
	}, CASConfig{
		AttributeEmail:  "mail",
		AttributeName:   "displayName",
		AttributeAvatar: "photo",
		EmailDomain:     "company.com",
	})
	if err != nil {
		t.Fatalf("normalizeCASIdentity: %v", err)
	}
	if identity.Provider != "cas" || identity.ProviderUserID != "zhangsan" {
		t.Fatalf("identity provider fields: %+v", identity)
	}
	if identity.Email != "zhangsan@company.com" {
		t.Fatalf("email: want zhangsan@company.com, got %q", identity.Email)
	}
	if identity.Name != "Zhang San" {
		t.Fatalf("name: got %q", identity.Name)
	}
	if identity.AvatarURL != "https://example.com/avatar.png" {
		t.Fatalf("avatar: got %q", identity.AvatarURL)
	}
}

func TestNormalizeCASIdentityFallsBackToEmailDomain(t *testing.T) {
	identity, err := normalizeCASIdentity(casUser{
		Username:   "lisi",
		Attributes: map[string]string{},
	}, CASConfig{EmailDomain: "company.com"})
	if err != nil {
		t.Fatalf("normalizeCASIdentity: %v", err)
	}
	if identity.Email != "lisi@company.com" {
		t.Fatalf("email: want lisi@company.com, got %q", identity.Email)
	}
}

func TestNormalizeCASIdentityUsesCASUserAsEmail(t *testing.T) {
	identity, err := normalizeCASIdentity(casUser{
		Username: "wangwu@company.com",
		Attributes: map[string]string{
			"displayName":  "Wang Wu",
			"avatarOrigin": "https://example.com/avatar.png",
		},
	}, CASConfig{
		AttributeEmail:  "user",
		AttributeName:   "displayName",
		AttributeAvatar: "avatarOrigin",
	})
	if err != nil {
		t.Fatalf("normalizeCASIdentity: %v", err)
	}
	if identity.Email != "wangwu@company.com" {
		t.Fatalf("email: want wangwu@company.com, got %q", identity.Email)
	}
	if identity.Name != "Wang Wu" {
		t.Fatalf("name: got %q", identity.Name)
	}
	if identity.AvatarURL != "https://example.com/avatar.png" {
		t.Fatalf("avatar: got %q", identity.AvatarURL)
	}
}

func TestNormalizeCASIdentityRejectsWrongDomain(t *testing.T) {
	_, err := normalizeCASIdentity(casUser{
		Username: "evil",
		Attributes: map[string]string{
			"email": "evil@example.com",
		},
	}, CASConfig{EmailDomain: "company.com"})
	if err == nil {
		t.Fatal("expected wrong-domain error")
	}
}

func TestSanitizeCASNextPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "/login"},
		{"relative path", "/login?next=%2Finbox", "/login?next=%2Finbox"},
		{"cli callback query", "/login?cli_callback=http%3A%2F%2Flocalhost%3A9876%2Fcb", "/login?cli_callback=http%3A%2F%2Flocalhost%3A9876%2Fcb"},
		{"absolute url", "https://evil.com", "/login"},
		{"protocol relative", "//evil.com", "/login"},
		{"javascript", "javascript:alert(1)", "/login"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeCASNextPath(tt.in); got != tt.want {
				t.Fatalf("sanitizeCASNextPath(%q): want %q, got %q", tt.in, tt.want, got)
			}
		})
	}
}

func TestValidateCASTicketRejectsFailureResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("service") != "http://app.example.com/auth/cas/callback" {
			t.Fatalf("service query mismatch: %q", r.URL.Query().Get("service"))
		}
		if r.URL.Query().Get("ticket") != "ST-123" {
			t.Fatalf("ticket query mismatch: %q", r.URL.Query().Get("ticket"))
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas"><cas:authenticationFailure code="INVALID_TICKET">Invalid</cas:authenticationFailure></cas:serviceResponse>`))
	}))
	defer server.Close()

	h := newTestHandler(Config{
		CAS: CASConfig{
			Enabled:     true,
			LoginURL:    "http://sso.example.com/login",
			ValidateURL: server.URL,
			ServiceURL:  "http://app.example.com/auth/cas/callback",
		},
	})

	_, err := h.validateCASTicket(context.Background(), "ST-123")
	if err == nil {
		t.Fatal("expected validation error")
	}
}

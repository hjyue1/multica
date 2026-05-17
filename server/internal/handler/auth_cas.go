package handler

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	casNextCookieName = "multica_cas_next"
	casCookieMaxAge   = 10 * 60
	casRequestTimeout = 5 * time.Second
)

type CASConfig struct {
	Enabled         bool
	DisplayName     string
	LoginURL        string
	ValidateURL     string
	ServiceURL      string
	AttributeEmail  string
	AttributeName   string
	AttributeAvatar string
	EmailDomain     string
}

func (c CASConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if err := validateAbsoluteHTTPURL("CAS_LOGIN_URL", c.LoginURL); err != nil {
		return err
	}
	if err := validateAbsoluteHTTPURL("CAS_VALIDATE_URL", c.ValidateURL); err != nil {
		return err
	}
	if err := validateAbsoluteHTTPURL("CAS_SERVICE_URL", c.ServiceURL); err != nil {
		return err
	}
	return nil
}

func validateAbsoluteHTTPURL(fieldName, raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s is required when CAS_ENABLED=true", fieldName)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", fieldName)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", fieldName)
	}
	return nil
}

type casIdentity struct {
	Provider       string
	ProviderUserID string
	Email          string
	Name           string
	AvatarURL      string
}

type casUser struct {
	Username   string
	Attributes map[string]string
}

type casServiceResponse struct {
	AuthenticationSuccess *casAuthenticationSuccess `xml:"authenticationSuccess"`
	AuthenticationFailure *casAuthenticationFailure `xml:"authenticationFailure"`
}

type casAuthenticationSuccess struct {
	User       string        `xml:"user"`
	Attributes casAttributes `xml:"attributes"`
}

type casAuthenticationFailure struct {
	Code    string `xml:"code,attr"`
	Message string `xml:",chardata"`
}

type casAttributes map[string]string

func (a *casAttributes) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	attrs := map[string]string{}
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			var value string
			if err := d.DecodeElement(&value, &t); err != nil {
				return err
			}
			attrs[t.Name.Local] = strings.TrimSpace(value)
		case xml.EndElement:
			if t.Name == start.Name {
				*a = attrs
				return nil
			}
		}
	}
}

func (h *Handler) CASStart(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.CAS.Enabled {
		writeError(w, http.StatusNotFound, "CAS login is disabled")
		return
	}
	if err := h.cfg.CAS.Validate(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "CAS login is not configured")
		return
	}

	next := sanitizeCASNextPath(r.URL.Query().Get("next"))
	setCASNextCookie(w, next, h.cfg.CAS)

	loginURL, err := url.Parse(h.cfg.CAS.LoginURL)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CAS login is not configured")
		return
	}
	q := loginURL.Query()
	q.Set("service", h.cfg.CAS.ServiceURL)
	loginURL.RawQuery = q.Encode()

	http.Redirect(w, r, loginURL.String(), http.StatusFound)
}

func (h *Handler) CASCallback(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.CAS.Enabled {
		writeError(w, http.StatusNotFound, "CAS login is disabled")
		return
	}
	if err := h.cfg.CAS.Validate(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "CAS login is not configured")
		return
	}

	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket == "" {
		writeError(w, http.StatusBadRequest, "missing CAS ticket")
		return
	}

	casUser, err := h.validateCASTicket(r.Context(), ticket)
	if err != nil {
		slog.Warn("CAS ticket validation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusUnauthorized, "CAS ticket validation failed")
		return
	}

	identity, err := normalizeCASIdentity(casUser, h.cfg.CAS)
	if err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}

	user, isNew, err := h.findOrCreateUser(r.Context(), identity.Email)
	if err != nil {
		var signupErr SignupError
		if errors.As(err, &signupErr) {
			writeError(w, http.StatusForbidden, signupErr.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if isNew {
		evt := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		evt.Properties["auth_method"] = identity.Provider
		h.Analytics.Capture(evt)
	}

	user = h.updateUserProfileFromCAS(r.Context(), user, identity)

	tokenString, err := h.issueJWT(user)
	if err != nil {
		slog.Warn("CAS login failed", append(logger.RequestAttrs(r), "error", err, "email", identity.Email)...)
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("failed to set auth cookies", "error", err)
	}
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(time.Now().Add(30 * 24 * time.Hour)) {
			http.SetCookie(w, cookie)
		}
	}

	next := casNextFromCookie(r)
	clearCASNextCookie(w, h.cfg.CAS)
	slog.Info("user logged in via CAS", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "email", user.Email)...)
	http.Redirect(w, r, frontendRedirectURL(next), http.StatusFound)
}

func (h *Handler) validateCASTicket(ctx context.Context, ticket string) (casUser, error) {
	validateURL, err := url.Parse(h.cfg.CAS.ValidateURL)
	if err != nil {
		return casUser{}, err
	}
	q := validateURL.Query()
	q.Set("service", h.cfg.CAS.ServiceURL)
	q.Set("ticket", ticket)
	validateURL.RawQuery = q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, casRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, validateURL.String(), nil)
	if err != nil {
		return casUser{}, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return casUser{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return casUser{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return casUser{}, fmt.Errorf("CAS validate returned status %d", resp.StatusCode)
	}
	return parseCASServiceResponse(body)
}

func parseCASServiceResponse(body []byte) (casUser, error) {
	var parsed casServiceResponse
	if err := xml.Unmarshal(body, &parsed); err != nil {
		return casUser{}, err
	}
	if parsed.AuthenticationFailure != nil {
		return casUser{}, fmt.Errorf("CAS authentication failed: %s", strings.TrimSpace(parsed.AuthenticationFailure.Message))
	}
	if parsed.AuthenticationSuccess == nil {
		return casUser{}, errors.New("CAS authentication success missing")
	}
	username := strings.TrimSpace(parsed.AuthenticationSuccess.User)
	if username == "" {
		return casUser{}, errors.New("CAS user is empty")
	}
	attrs := map[string]string(parsed.AuthenticationSuccess.Attributes)
	if attrs == nil {
		attrs = map[string]string{}
	}
	return casUser{Username: username, Attributes: attrs}, nil
}

func normalizeCASIdentity(user casUser, cfg CASConfig) (casIdentity, error) {
	username := strings.TrimSpace(user.Username)
	if username == "" {
		return casIdentity{}, errors.New("CAS user is empty")
	}

	emailAttr := envFieldOrDefault(cfg.AttributeEmail, "email")
	nameAttr := envFieldOrDefault(cfg.AttributeName, "name")
	avatarAttr := envFieldOrDefault(cfg.AttributeAvatar, "avatar")

	email := ""
	if strings.EqualFold(emailAttr, "user") {
		email = username
	} else {
		email = user.Attributes[emailAttr]
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" && cfg.EmailDomain != "" {
		email = strings.ToLower(username + "@" + strings.TrimSpace(cfg.EmailDomain))
	}
	if email == "" {
		return casIdentity{}, errors.New("CAS user email is empty")
	}
	if err := validateNormalizedEmail(email); err != nil {
		return casIdentity{}, err
	}
	if cfg.EmailDomain != "" && !strings.EqualFold(emailDomain(email), strings.TrimSpace(cfg.EmailDomain)) {
		return casIdentity{}, errors.New("CAS user email domain is not allowed")
	}

	return casIdentity{
		Provider:       "cas",
		ProviderUserID: username,
		Email:          email,
		Name:           strings.TrimSpace(user.Attributes[nameAttr]),
		AvatarURL:      strings.TrimSpace(user.Attributes[avatarAttr]),
	}, nil
}

func envFieldOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func validateNormalizedEmail(email string) error {
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return errors.New("CAS user email is invalid")
	}
	if emailDomain(email) == "" {
		return errors.New("CAS user email is invalid")
	}
	return nil
}

func emailDomain(email string) string {
	if at := strings.LastIndex(email, "@"); at >= 0 && at < len(email)-1 {
		return strings.ToLower(email[at+1:])
	}
	return ""
}

func (h *Handler) updateUserProfileFromCAS(ctx context.Context, user db.User, identity casIdentity) db.User {
	needsUpdate := false
	newName := user.Name
	newAvatar := user.AvatarUrl
	defaultName := identity.Email
	if at := strings.Index(identity.Email, "@"); at > 0 {
		defaultName = identity.Email[:at]
	}

	if identity.Name != "" && (user.Name == defaultName || user.Name == user.Email) {
		newName = identity.Name
		needsUpdate = true
	}
	if identity.AvatarURL != "" && !user.AvatarUrl.Valid {
		newAvatar = pgtype.Text{String: identity.AvatarURL, Valid: true}
		needsUpdate = true
	}
	if !needsUpdate {
		return user
	}

	updated, err := h.Queries.UpdateUser(ctx, db.UpdateUserParams{
		ID:        user.ID,
		Name:      newName,
		AvatarUrl: newAvatar,
	})
	if err != nil {
		slog.Warn("failed to update CAS user profile", "error", err, "user_id", uuidToString(user.ID))
		return user
	}
	return updated
}

func sanitizeCASNextPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/login"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return "/login"
	}
	if strings.HasPrefix(raw, "//") {
		return "/login"
	}
	return raw
}

func casNextFromCookie(r *http.Request) string {
	cookie, err := r.Cookie(casNextCookieName)
	if err != nil {
		return "/login"
	}
	value, err := url.QueryUnescape(cookie.Value)
	if err != nil {
		return "/login"
	}
	return sanitizeCASNextPath(value)
}

func setCASNextCookie(w http.ResponseWriter, next string, cfg CASConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     casNextCookieName,
		Value:    url.QueryEscape(next),
		Path:     "/auth/cas",
		MaxAge:   casCookieMaxAge,
		Expires:  time.Now().Add(time.Duration(casCookieMaxAge) * time.Second),
		HttpOnly: true,
		Secure:   casSecureCookie(cfg),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCASNextCookie(w http.ResponseWriter, cfg CASConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     casNextCookieName,
		Value:    "",
		Path:     "/auth/cas",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   casSecureCookie(cfg),
		SameSite: http.SameSiteLaxMode,
	})
}

func casSecureCookie(cfg CASConfig) bool {
	if u, err := url.Parse(cfg.ServiceURL); err == nil && strings.EqualFold(u.Scheme, "https") {
		return true
	}
	if u, err := url.Parse(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))); err == nil {
		return strings.EqualFold(u.Scheme, "https")
	}
	return false
}

func frontendRedirectURL(next string) string {
	next = sanitizeCASNextPath(next)
	origin := strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")), "/")
	if origin == "" {
		return next
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return next
	}
	return origin + next
}

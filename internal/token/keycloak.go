package token

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"outbox-dispatcher/internal/logger"
)

// Provider obtains and caches a bearer token from Keycloak (OpenID token endpoint).
type Provider struct {
	mu sync.Mutex

	httpClient *http.Client
	tokenURL   string
	clientID   string
	secret     string
	scope      string // optional; sent when non-empty

	cached    string
	expiresAt time.Time
}

// NewKeycloak creates a token provider. tokenURL must be the full OpenID token URL, e.g.
// https://keycloak.example.com/realms/myrealm/protocol/openid-connect/token
// Requests use grant_type=client_credentials, client_id, client_secret (and optional scope).
func NewKeycloak(httpClient *http.Client, tokenURL, clientID, clientSecret, scope string) *Provider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Provider{
		httpClient: httpClient,
		tokenURL:   strings.TrimSpace(tokenURL),
		clientID:   strings.TrimSpace(clientID),
		secret:     strings.TrimSpace(clientSecret),
		scope:      strings.TrimSpace(scope),
	}
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// BearerToken returns a valid access token, refreshing from Keycloak when needed.
func (p *Provider) BearerToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached != "" && time.Now().Before(p.expiresAt.Add(-30*time.Second)) {
		return p.cached, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.secret)
	if p.scope != "" {
		form.Set("scope", p.scope)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	logger.L.Printf("keycloak token request: POST %s Content-Type=%s body=%s",
		p.tokenURL, req.Header.Get("Content-Type"), redactedFormBodyForLog(form))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("keycloak token POST %s: %w", p.tokenURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.L.Printf("keycloak token response: POST %s -> HTTP %d body=%s",
			p.tokenURL, resp.StatusCode, truncateForLog(string(body), 2048))
		return "", fmt.Errorf("keycloak token: %s", formatOAuthTokenError(resp.StatusCode, body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("keycloak token json: %w", err)
	}
	if strings.TrimSpace(tr.AccessToken) == "" {
		return "", fmt.Errorf("keycloak token: empty access_token")
	}
	exp := tr.ExpiresIn
	if exp <= 0 {
		exp = 300
	}
	p.cached = tr.AccessToken
	p.expiresAt = time.Now().Add(time.Duration(exp) * time.Second)
	logger.L.Printf("keycloak token refreshed (expires_in=%ds)", exp)
	return p.cached, nil
}

type oauthTokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// redactedFormBodyForLog returns application/x-www-form-urlencoded body with client_secret masked.
func redactedFormBodyForLog(form url.Values) string {
	out := url.Values{}
	for k, vals := range form {
		if k == "client_secret" {
			out.Set(k, "(redacted)")
			continue
		}
		for _, v := range vals {
			out.Add(k, v)
		}
	}
	return out.Encode()
}

func truncateForLog(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func formatOAuthTokenError(status int, body []byte) string {
	s := strings.TrimSpace(string(body))
	var oe oauthTokenError
	if json.Unmarshal(body, &oe) == nil && (oe.Error != "" || oe.ErrorDescription != "") {
		msg := fmt.Sprintf("HTTP %d", status)
		if oe.Error != "" {
			msg += ", " + oe.Error
		}
		if oe.ErrorDescription != "" {
			msg += ": " + oe.ErrorDescription
		}
		return msg
	}
	if s == "" {
		return fmt.Sprintf("HTTP %d (empty body)", status)
	}
	return fmt.Sprintf("HTTP %d: %s", status, s)
}

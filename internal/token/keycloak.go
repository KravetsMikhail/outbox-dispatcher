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

	cached    string
	expiresAt time.Time
}

// NewKeycloak creates a token provider. tokenURL must be the full OpenID token URL, e.g.
// https://keycloak.example.com/realms/myrealm/protocol/openid-connect/token
func NewKeycloak(httpClient *http.Client, tokenURL, clientID, clientSecret string) *Provider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Provider{
		httpClient: httpClient,
		tokenURL:   strings.TrimSpace(tokenURL),
		clientID:   strings.TrimSpace(clientID),
		secret:     strings.TrimSpace(clientSecret),
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("keycloak token: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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

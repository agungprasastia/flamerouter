// Package tokenrefresh provides token refresh management, deduplication, and caching.
package tokenrefresh

import (
	"context"
	"errors"
	"flamerouter/internal/oauth"
	"fmt"
	"sync"
	"time"
)

const (
	// TokenExpiryBuffer defines how far in advance of actual token expiration a refresh should be triggered.
	TokenExpiryBuffer = 5 * time.Minute
	// MaxRetryAttempts is the maximum number of retry attempts for token refresh operations.
	MaxRetryAttempts = 2
	// RetryDelay is the base duration to wait between refresh retry attempts.
	RetryDelay = 500 * time.Millisecond
)

// RefreshResult represents the outcome of an OAuth token refresh.
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresAt    time.Time
	Error        string
}

// Refresher defines the interface for refreshing provider OAuth tokens.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error)
}

// RefreshManager manages provider refreshers and handles deduplication and retries.
type RefreshManager struct {
	refreshers map[string]Refresher
	dedup      *DedupGroup
	mu         sync.RWMutex
}

// NewRefreshManager creates a new RefreshManager with default configuration and TTL.
func NewRefreshManager() *RefreshManager {
	return NewRefreshManagerWithTTL(DefaultRefreshResultTTL)
}

// NewRefreshManagerWithTTL creates a new RefreshManager with custom deduplication TTL.
func NewRefreshManagerWithTTL(ttl time.Duration) *RefreshManager {
	rm := &RefreshManager{
		refreshers: make(map[string]Refresher),
		dedup:      NewDedupGroup(ttl),
		mu:         sync.RWMutex{},
	}
	rm.registerDefaults()

	return rm
}

func (rm *RefreshManager) registerDefaults() {
	providers := []string{
		"claude", "gemini", "github", "xai", "codex", "kiro", "cursor",
		"iflow", "qwen", "kimi", "grok-cli", "antigravity", "kilocode",
		"cline", "clinepass", "gitlab", "codebuddy", "kimchi", "qoder",
		"copilot", "vertex",
	}

	for _, p := range providers {
		rm.Register(p, &providerOAuthRefresher{provider: p})
	}
}

// Register registers a new Refresher implementation for a given provider.
func (rm *RefreshManager) Register(provider string, refresher Refresher) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.refreshers[provider] = refresher
}

// Refresh executes a token refresh for a provider, using deduplication and retries.
func (rm *RefreshManager) Refresh(ctx context.Context, provider string, refreshToken string) (*RefreshResult, error) {
	rm.mu.RLock()
	refresher, ok := rm.refreshers[provider]
	rm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no refresher for provider: %s", provider)
	}

	key := fmt.Sprintf("%s:%s", provider, refreshToken)
	if rm.dedup != nil && refreshToken != "" {
		return rm.dedup.Do(ctx, key, func() (*RefreshResult, error) {
			return rm.refreshWithRetry(ctx, provider, refreshToken, refresher)
		})
	}

	return rm.refreshWithRetry(ctx, provider, refreshToken, refresher)
}

func waitRetry(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(RetryDelay * time.Duration(attempt)):
		return nil
	}
}

func extractRefreshError(result *RefreshResult, err error) error {
	switch {
	case err != nil:
		return err
	case result != nil:
		return fmt.Errorf("%s", result.Error)
	default:
		return errors.New("empty result returned")
	}
}

func (rm *RefreshManager) refreshWithRetry(ctx context.Context, provider, refreshToken string, refresher Refresher) (*RefreshResult, error) {
	var lastErr error

	for attempt := 0; attempt <= MaxRetryAttempts; attempt++ {
		if err := waitRetry(ctx, attempt); err != nil {
			return nil, err
		}

		result, err := refresher.Refresh(ctx, refreshToken)
		if err == nil && result != nil && result.Error == "" {
			return result, nil
		}

		lastErr = extractRefreshError(result, err)
	}

	return nil, fmt.Errorf("refresh failed after %d attempts for %s: %w", MaxRetryAttempts+1, provider, lastErr)
}

// NeedsRefresh reports whether the token expiring at expiresAt should be refreshed now.
func (rm *RefreshManager) NeedsRefresh(expiresAt time.Time) bool {
	return time.Until(expiresAt) < TokenExpiryBuffer
}

// GenericOAuthRefresher refreshes OAuth tokens using an OAuthConfig.
type GenericOAuthRefresher struct {
	Config *oauth.OAuthConfig
}

// Refresh refreshes the OAuth token for the generic provider.
func (g *GenericOAuthRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()

	token, err := handler.RefreshToken(ctx, g.Config.Provider, refreshToken)
	if err != nil {
		return &RefreshResult{
			AccessToken:  "",
			RefreshToken: "",
			IDToken:      "",
			ExpiresAt:    time.Time{},
			Error:        err.Error(),
		}, err
	}

	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      token.IDToken,
		ExpiresAt:    token.ExpiresAt,
		Error:        "",
	}, nil
}

type providerOAuthRefresher struct {
	provider string
}

func (p *providerOAuthRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()

	token, err := handler.RefreshToken(ctx, p.provider, refreshToken)
	if err != nil {
		return &RefreshResult{
			AccessToken:  "",
			RefreshToken: "",
			IDToken:      "",
			ExpiresAt:    time.Time{},
			Error:        err.Error(),
		}, err
	}

	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      token.IDToken,
		ExpiresAt:    token.ExpiresAt,
		Error:        "",
	}, nil
}

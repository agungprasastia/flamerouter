package tokenrefresh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"flamerouter/internal/oauth"
)

const (
	TokenExpiryBuffer = 5 * time.Minute
	MaxRetryAttempts  = 2
	RetryDelay        = 500 * time.Millisecond
)

type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	Error        string
}

type RefreshManager struct {
	refreshers map[string]Refresher
	mu         sync.RWMutex
	retryMap   sync.Map
}

type Refresher interface {
	Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error)
}

func NewRefreshManager() *RefreshManager {
	rm := &RefreshManager{
		refreshers: make(map[string]Refresher),
	}
	rm.registerDefaults()
	return rm
}

func (rm *RefreshManager) registerDefaults() {
	rm.Register("claude", &ClaudeRefresher{})
	rm.Register("gemini", &GoogleRefresher{})
	rm.Register("github", &GitHubRefresher{})
	rm.Register("xai", &XAIRefresher{})
	rm.Register("codex", &CodexRefresher{})
	rm.Register("kiro", &KiroRefresher{})
	rm.Register("cursor", &CursorRefresher{})
	rm.Register("iflow", &IFlowRefresher{})
	rm.Register("qwen", &QwenRefresher{})
	rm.Register("kimi", &KimiRefresher{})
	rm.Register("grok-cli", &GrokCLIRefresher{})
	rm.Register("antigravity", &AntigravityRefresher{})
	rm.Register("kilocode", &KilocodeRefresher{})
	rm.Register("cline", &ClineRefresher{})
	rm.Register("clinepass", &ClinePassRefresher{})
	rm.Register("gitlab", &GitLabRefresher{})
	rm.Register("codebuddy", &CodeBuddyRefresher{})
	rm.Register("kimchi", &KimchiRefresher{})
	rm.Register("qoder", &QoderRefresher{})
	rm.Register("copilot", &CopilotRefresher{})
	rm.Register("vertex", &VertexRefresher{})
}

func (rm *RefreshManager) Register(provider string, refresher Refresher) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.refreshers[provider] = refresher
}

func (rm *RefreshManager) Refresh(ctx context.Context, provider string, refreshToken string) (*RefreshResult, error) {
	rm.mu.RLock()
	refresher, ok := rm.refreshers[provider]
	rm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no refresher for provider: %s", provider)
	}

	return rm.refreshWithRetry(ctx, provider, refreshToken, refresher)
}

func (rm *RefreshManager) refreshWithRetry(ctx context.Context, provider, refreshToken string, refresher Refresher) (*RefreshResult, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxRetryAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(RetryDelay * time.Duration(attempt)):
			}
		}

		result, err := refresher.Refresh(ctx, refreshToken)
		if err == nil && result.Error == "" {
			return result, nil
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("%s", result.Error)
		}
	}

	return nil, fmt.Errorf("refresh failed after %d attempts for %s: %w", MaxRetryAttempts+1, provider, lastErr)
}

func (rm *RefreshManager) NeedsRefresh(expiresAt time.Time) bool {
	return time.Until(expiresAt) < TokenExpiryBuffer
}

type GenericOAuthRefresher struct {
	Config *oauth.OAuthConfig
}

func (g *GenericOAuthRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, g.Config.Provider, refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type ClaudeRefresher struct{}

func (c *ClaudeRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "claude", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type GoogleRefresher struct{}

func (g *GoogleRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "gemini", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type GitHubRefresher struct{}

func (g *GitHubRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "github", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type XAIRefresher struct{}

func (x *XAIRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "xai", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type CodexRefresher struct{}

func (c *CodexRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "codex", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type KiroRefresher struct{}

func (k *KiroRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "kiro", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type CursorRefresher struct{}

func (c *CursorRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "cursor", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type IFlowRefresher struct{}

func (i *IFlowRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "iflow", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type QwenRefresher struct{}

func (q *QwenRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "qwen", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type KimiRefresher struct{}

func (k *KimiRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "kimi", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type GrokCLIRefresher struct{}

func (g *GrokCLIRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "grok-cli", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type AntigravityRefresher struct{}

func (a *AntigravityRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "antigravity", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type KilocodeRefresher struct{}

func (k *KilocodeRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "kilocode", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type ClineRefresher struct{}

func (c *ClineRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "cline", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type ClinePassRefresher struct{}

func (c *ClinePassRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "clinepass", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type GitLabRefresher struct{}

func (g *GitLabRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "gitlab", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type CodeBuddyRefresher struct{}

func (c *CodeBuddyRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "codebuddy", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type KimchiRefresher struct{}

func (k *KimchiRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "kimchi", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type QoderRefresher struct{}

func (q *QoderRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "qoder", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type CopilotRefresher struct{}

func (c *CopilotRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "copilot", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

type VertexRefresher struct{}

func (v *VertexRefresher) Refresh(ctx context.Context, refreshToken string) (*RefreshResult, error) {
	handler := oauth.NewHandler()
	token, err := handler.RefreshToken(ctx, "vertex", refreshToken)
	if err != nil {
		return &RefreshResult{Error: err.Error()}, err
	}
	return &RefreshResult{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}, nil
}

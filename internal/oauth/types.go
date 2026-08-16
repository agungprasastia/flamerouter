package oauth

import (
	"os"
	"strings"
	"time"
)

type OAuthConfig struct {
	Provider      string        `json:"provider"`
	ClientID      string        `json:"client_id"`
	ClientSecret  string        `json:"client_secret,omitempty"`
	AuthURL       string        `json:"auth_url"`
	TokenURL      string        `json:"token_url"`
	RefreshURL    string        `json:"refresh_url,omitempty"`
	RedirectURL   string        `json:"redirect_url"`
	Scopes        []string      `json:"scopes"`
	AuthStyle     string        `json:"auth_style"`
	DeviceURL     string        `json:"device_url,omitempty"`
	TokenExpiry   time.Duration `json:"token_expiry"`
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
}

type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type OAuthState struct {
	State       string `json:"state"`
	Provider    string `json:"provider"`
	RedirectURI string `json:"redirect_uri"`
	CreatedAt   time.Time `json:"created_at"`
}

var ProviderConfigs = map[string]*OAuthConfig{
	"claude": {
		Provider:     "claude",
		ClientID:     "aa70b58f81965147b47f18c3c2d54fc6",
		AuthURL:      "https://claude.ai/oauth/authorize",
		TokenURL:     "https://claude.ai/oauth/token",
		RefreshURL:   "https://claude.ai/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/claude/callback",
		Scopes:       []string{"user:inference"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"gemini": {
		Provider:     "gemini",
		ClientID:     "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com",
		ClientSecret: "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",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		RefreshURL:   "https://oauth2.googleapis.com/token",
		RedirectURL:  "http://localhost:20128/api/oauth/gemini/callback",
		Scopes:       []string{"openid", "email", "profile", "https://www.googleapis.com/auth/cloud-platform"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"github": {
		Provider:     "github",
		ClientID:     "Iv1.b507a08c87ecfe98",
		AuthURL:      "https://github.com/login/device/code",
		TokenURL:     "https://github.com/login/oauth/access_token",
		DeviceURL:    "https://github.com/login/device/code",
		RedirectURL:  "http://localhost:20128/api/oauth/github/callback",
		Scopes:       []string{"read:user"},
		AuthStyle:    "device",
		TokenExpiry:  time.Hour * 8,
	},
	"xai": {
		Provider:     "xai",
		ClientID:     "b1a00492-073a-47ea-816f-4c329264a828",
		AuthURL:      "https://accounts.x.ai/oauth2/authorize",
		TokenURL:     "https://accounts.x.ai/oauth2/token",
		RefreshURL:   "https://accounts.x.ai/oauth2/token",
		RedirectURL:  "http://localhost:20128/api/oauth/xai/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "oidc",
		TokenExpiry:  time.Hour,
	},
	"qwen": {
		Provider:     "qwen",
		ClientID:     "f0304373b74a44d2b584a3fb70ca9e56",
		AuthURL:      "https://chat.qwen.ai/api/v1/oauth2/device/code",
		TokenURL:     "https://chat.qwen.ai/api/v1/oauth2/token",
		DeviceURL:    "https://chat.qwen.ai/api/v1/oauth2/device/code",
		RedirectURL:  "http://localhost:20128/api/oauth/qwen/callback",
		Scopes:       []string{"openid", "profile", "email", "model.completion"},
		AuthStyle:    "device",
		TokenExpiry:  time.Hour * 24,
	},
	"codex": {
		Provider:     "codex",
		ClientID:     "app_EMoamEEZ73f0CkXaXp7hrann",
		AuthURL:      "https://auth.openai.com/oauth/authorize",
		TokenURL:     "https://auth.openai.com/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/codex/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"kiro": {
		Provider:     "kiro",
		ClientID:     "kiro-desktop",
		AuthURL:      "https://prod.us-east-1.auth.desktop.kiro.dev/login",
		TokenURL:     "https://prod.us-east-1.auth.desktop.kiro.dev/oauth/token",
		DeviceURL:    "https://oidc.us-east-1.amazonaws.com/device_authorization",
		RefreshURL:   "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken",
		RedirectURL:  "kiro://kiro.kiroAgent/authenticate-success",
		Scopes:       []string{"openid", "profile", "offline_access"},
		AuthStyle:    "device",
		TokenExpiry:  time.Hour,
	},
	"cursor": {
		Provider:     "cursor",
		AuthURL:      "https://authenticator.cursor.sh/authorize",
		TokenURL:     "https://authenticator.cursor.sh/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/cursor/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"iflow": {
		Provider:     "iflow",
		AuthURL:      "https://api.iflow.cn/oauth/authorize",
		TokenURL:     "https://api.iflow.cn/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/iflow/callback",
		Scopes:       []string{"openid"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"kimi": {
		Provider:     "kimi",
		AuthURL:      "https://kimi.moonshot.cn/oauth/authorize",
		TokenURL:     "https://kimi.moonshot.cn/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/kimi/callback",
		Scopes:       []string{"openid"},
		AuthStyle:    "device",
		TokenExpiry:  time.Hour * 24,
	},
	"grok-cli": {
		Provider:     "grok-cli",
		ClientID:     "b1a00492-073a-47ea-816f-4c329264a828",
		AuthURL:      "https://accounts.x.ai/oauth2/authorize",
		TokenURL:     "https://accounts.x.ai/oauth2/token",
		DeviceURL:    "https://auth.x.ai/oauth2/device/code",
		RefreshURL:   "https://accounts.x.ai/oauth2/token",
		RedirectURL:  "http://localhost:20128/api/oauth/grok-cli/callback",
		Scopes:       []string{"openid", "offline_access", "grok-cli:access", "api:access", "conversations:read", "conversations:write"},
		AuthStyle:    "device",
		TokenExpiry:  time.Hour,
	},
	"antigravity": {
		Provider:     "antigravity",
		ClientID:     "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
		ClientSecret: "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",
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		RefreshURL:   "https://oauth2.googleapis.com/token",
		RedirectURL:  "http://localhost:20128/api/oauth/antigravity/callback",
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"https://www.googleapis.com/auth/cclog",
			"https://www.googleapis.com/auth/experimentsandconfigs",
		},
		AuthStyle:   "pkce",
		TokenExpiry: time.Hour,
	},
	"kilocode": {
		Provider:     "kilocode",
		AuthURL:      "https://auth.kilocode.ai/authorize",
		TokenURL:     "https://auth.kilocode.ai/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/kilocode/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"cline": {
		Provider:     "cline",
		AuthURL:      "https://auth.cline.bot/authorize",
		TokenURL:     "https://auth.cline.bot/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/cline/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"clinepass": {
		Provider:     "clinepass",
		AuthURL:      "https://auth.clinepass.com/authorize",
		TokenURL:     "https://auth.clinepass.com/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/clinepass/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"gitlab": {
		Provider:     "gitlab",
		AuthURL:      "https://gitlab.com/oauth/authorize",
		TokenURL:     "https://gitlab.com/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/gitlab/callback",
		Scopes:       []string{"read_user"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour * 2,
	},
	"codebuddy": {
		Provider:     "codebuddy",
		AuthURL:      "https://auth.codebuddy.ai/authorize",
		TokenURL:     "https://auth.codebuddy.ai/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/codebuddy/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"kimchi": {
		Provider:     "kimchi",
		AuthURL:      "https://auth.kimchi.ai/authorize",
		TokenURL:     "https://auth.kimchi.ai/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/kimchi/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"qoder": {
		Provider:     "qoder",
		AuthURL:      "https://auth.qoder.ai/authorize",
		TokenURL:     "https://auth.qoder.ai/oauth/token",
		RedirectURL:  "http://localhost:20128/api/oauth/qoder/callback",
		Scopes:       []string{"openid", "offline_access"},
		AuthStyle:    "pkce",
		TokenExpiry:  time.Hour,
	},
	"copilot": {
		Provider:     "copilot",
		ClientID:     "Iv1.b507a08c87ecfe98",
		AuthURL:      "https://github.com/login/device/code",
		TokenURL:     "https://github.com/login/oauth/access_token",
		DeviceURL:    "https://github.com/login/device/code",
		RedirectURL:  "http://localhost:20128/api/oauth/copilot/callback",
		Scopes:       []string{"read:user"},
		AuthStyle:    "device",
		TokenExpiry:  time.Hour * 30,
	},
}

// CopilotTokenURL exchanges GitHub OAuth token → short-lived Copilot token.
const CopilotTokenURL = "https://api.github.com/copilot_internal/v2/token"

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}


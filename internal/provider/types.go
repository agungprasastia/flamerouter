// Package provider defines model provider configurations, registry listings,
// and model capabilities for all supported AI backends.
package provider

// Provider describes an AI service provider, its transport configuration,
// display metadata, and supported models.
type Provider struct {
	Transport    Transport `json:"transport"`
	Thinking     *Thinking `json:"thinkingConfig,omitempty"`
	Display      Display   `json:"display"`
	ID           string    `json:"id"`
	Alias        string    `json:"alias,omitempty"`
	Category     string    `json:"category"`
	Models       []Model   `json:"models"`
	ServiceKinds []string  `json:"serviceKinds,omitempty"`
	Priority     int       `json:"priority"`
	HasFree      bool      `json:"hasFree,omitempty"`
}

// Display holds presentation metadata for a provider in user interfaces.
type Display struct {
	Notice       *Notice `json:"notice,omitempty"`
	Name         string  `json:"name"`
	Icon         string  `json:"icon"`
	Color        string  `json:"color"`
	TextIcon     string  `json:"textIcon,omitempty"`
	Website      string  `json:"website,omitempty"`
	DeprecNotice string  `json:"deprecationNotice,omitempty"`
	Deprecated   bool    `json:"deprecated,omitempty"`
}

// Notice contains user notices, registration URLs, or API key links.
type Notice struct {
	Text      string `json:"text,omitempty"`
	SignupURL string `json:"signupUrl,omitempty"`
	APIKeyURL string `json:"apiKeyUrl,omitempty"`
}

// Transport contains HTTP endpoint and protocol configuration for communicating
// with a provider backend.
type Transport struct {
	Headers      map[string]string `json:"headers,omitempty"`
	Auth         *AuthConfig       `json:"auth,omitempty"`
	Quirks       *Quirks           `json:"quirks,omitempty"`
	BaseURL      string            `json:"baseUrl"`
	Format       string            `json:"format,omitempty"`
	URLSuffix    string            `json:"urlSuffix,omitempty"`
	ClientID     string            `json:"clientId,omitempty"`
	ClientSecret string            `json:"clientSecret,omitempty"`
	ForceStream  bool              `json:"forceStream,omitempty"`
}

// AuthConfig defines API key, OAuth, or request hook authentication options.
type AuthConfig struct {
	APIKey *AuthStyle `json:"apiKey,omitempty"`
	OAuth  *AuthStyle `json:"oauth,omitempty"`
	Hooks  []string   `json:"hooks,omitempty"`
}

// AuthStyle specifies header name and authorization scheme (e.g. Bearer).
type AuthStyle struct {
	Header string `json:"header"`
	Scheme string `json:"scheme"`
}

// Quirks specifies provider-specific behavior workarounds.
type Quirks struct {
	CloakToolsOnOAuth bool `json:"cloakToolsOnOAuth,omitempty"`
}

// Thinking defines thinking / reasoning mode options and defaults.
type Thinking struct {
	DefaultMode string   `json:"defaultMode,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Model represents an individual model supported by a provider.
type Model struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind,omitempty"`
	Params     []string `json:"params,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}

// Capabilities describes the features and limits supported by a model.
type Capabilities struct {
	ThinkingFormat string `json:"thinkingFormat,omitempty"`
	ContextWindow  int    `json:"contextWindow"`
	MaxOutput      int    `json:"maxOutput"`
	Vision         bool   `json:"vision"`
	PDF            bool   `json:"pdf"`
	AudioInput     bool   `json:"audioInput"`
	VideoInput     bool   `json:"videoInput"`
	ImageOutput    bool   `json:"imageOutput"`
	AudioOutput    bool   `json:"audioOutput"`
	Search         bool   `json:"search"`
	Tools          bool   `json:"tools"`
	Reasoning      bool   `json:"reasoning"`
}

package provider

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

type Notice struct {
	Text      string `json:"text,omitempty"`
	SignupURL string `json:"signupUrl,omitempty"`
	APIKeyURL string `json:"apiKeyUrl,omitempty"`
}

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

type AuthConfig struct {
	APIKey *AuthStyle `json:"apiKey,omitempty"`
	OAuth  *AuthStyle `json:"oauth,omitempty"`
	Hooks  []string   `json:"hooks,omitempty"`
}

type AuthStyle struct {
	Header string `json:"header"`
	Scheme string `json:"scheme"`
}

type Quirks struct {
	CloakToolsOnOAuth bool `json:"cloakToolsOnOAuth,omitempty"`
}

type Thinking struct {
	DefaultMode string   `json:"defaultMode,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type Model struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind,omitempty"`
	Params     []string `json:"params,omitempty"`
	Dimensions int      `json:"dimensions,omitempty"`
}

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

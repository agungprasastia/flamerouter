package provider

type Provider struct {
	ID           string        `json:"id"`
	Priority     int           `json:"priority"`
	Alias        string        `json:"alias,omitempty"`
	HasFree      bool          `json:"hasFree,omitempty"`
	Display      Display       `json:"display"`
	Category     string        `json:"category"`
	Transport    Transport     `json:"transport"`
	Models       []Model       `json:"models"`
	ServiceKinds []string      `json:"serviceKinds,omitempty"`
	Thinking     *Thinking     `json:"thinkingConfig,omitempty"`
}

type Display struct {
	Name         string   `json:"name"`
	Icon         string   `json:"icon"`
	Color        string   `json:"color"`
	TextIcon     string   `json:"textIcon,omitempty"`
	Website      string   `json:"website,omitempty"`
	Notice       *Notice  `json:"notice,omitempty"`
	Deprecated   bool     `json:"deprecated,omitempty"`
	DeprecNotice string   `json:"deprecationNotice,omitempty"`
}

type Notice struct {
	Text       string `json:"text,omitempty"`
	SignupURL  string `json:"signupUrl,omitempty"`
	APIKeyURL  string `json:"apiKeyUrl,omitempty"`
}

type Transport struct {
	BaseURL     string            `json:"baseUrl"`
	Format      string            `json:"format,omitempty"`
	ForceStream bool              `json:"forceStream,omitempty"`
	URLSuffix   string            `json:"urlSuffix,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Auth        *AuthConfig       `json:"auth,omitempty"`
	ClientID    string            `json:"clientId,omitempty"`
	ClientSecret string           `json:"clientSecret,omitempty"`
	Quirks      *Quirks           `json:"quirks,omitempty"`
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
	Options       []string `json:"options,omitempty"`
	DefaultMode   string   `json:"defaultMode,omitempty"`
}

type Model struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind,omitempty"`
	Params      []string `json:"params,omitempty"`
	Dimensions  int      `json:"dimensions,omitempty"`
}

type Capabilities struct {
	Vision         bool    `json:"vision"`
	PDF            bool    `json:"pdf"`
	AudioInput     bool    `json:"audioInput"`
	VideoInput     bool    `json:"videoInput"`
	ImageOutput    bool    `json:"imageOutput"`
	AudioOutput    bool    `json:"audioOutput"`
	Search         bool    `json:"search"`
	Tools          bool    `json:"tools"`
	Reasoning      bool    `json:"reasoning"`
	ThinkingFormat string  `json:"thinkingFormat,omitempty"`
	ContextWindow  int     `json:"contextWindow"`
	MaxOutput      int     `json:"maxOutput"`
}

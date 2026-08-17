package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("opencode", &OpenCodeExecutor{
		Base: Base{
			Provider: "opencode",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://opencode.ai/zen/v1/chat/completions",
			BaseURLs: nil,
		},
	})
}

// OpenCodeExecutor handles OpenCode completions.
type OpenCodeExecutor struct{ Base }

func ocRandomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		_ = err
	}

	return hex.EncodeToString(b)
}

func resolveOpenCodeURL(baseURL, credBase string) string {
	if credBase == "" {
		return baseURL
	}

	switch {
	case !strings.Contains(credBase, "/zen/v1") && !strings.Contains(credBase, "/chat/completions"):
		return credBase + "/zen/v1/chat/completions"
	case !strings.Contains(credBase, "/chat/completions"):
		return credBase + "/chat/completions"
	default:
		return credBase
	}
}

// Execute executes OpenCode chat requests.
func (e *OpenCodeExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	m["model"] = model
	m["stream"] = stream

	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(cred.BaseURL, "/")
	url := resolveOpenCodeURL(e.BaseURL, base)

	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	tok := cred.APIKey
	if tok == "" {
		tok = cred.AccessToken
	}

	if tok == "" {
		tok = "public"
	}

	h.Set("Authorization", "Bearer "+tok)
	h.Set("User-Agent", "opencode")
	h.Set("x-opencode-client", "desktop")
	h.Set("x-opencode-session", "ses_"+ocRandomUUID())
	h.Set("x-opencode-request", "msg_"+ocRandomUUID())
	h.Set("x-opencode-project", "global")

	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "*/*")
	}

	return e.DoPOST(ctx, url, h, payload)
}

package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("cursor", &CursorExecutor{Base: Base{Provider: "cursor", BaseURL: "https://api2.cursor.sh"}})
	RegisterSpecialized("cu", &CursorExecutor{Base: Base{Provider: "cursor", BaseURL: "https://api2.cursor.sh"}})
}

// CursorExecutor — ConnectRPC/protobuf path is deferred.
// Current path: OpenAI-compatible chat when baseURL points to a proxy;
// otherwise posts to Cursor agent API with JSON (best-effort).
// ponytail: full protobuf framing + checksum (cursorProtobuf.js / cursorChecksum.js)
// add when live Cursor account testing is available.
type CursorExecutor struct{ Base }

func (e *CursorExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
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
	if base == "" {
		base = e.BaseURL
	}
	// Prefer OpenAI-compat surface if configured
	url := base
	if !strings.Contains(base, "/chat") && !strings.Contains(base, "AgentService") {
		url = base + "/chat/completions"
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}
	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
		h.Set("Cookie", "WorkosCursorSessionToken="+tok)
	}
	h.Set("User-Agent", "Mozilla/5.0 (compatible; FlameRouter/1.0)")
	h.Set("x-cursor-client-version", "0.50.0")
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return e.DoPOST(ctx, url, h, payload)
}

package rtk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultHeadroomTimeout = 3 * time.Second

// HeadroomStats matches 9router compress response stats (optional).
type HeadroomStats struct {
	TokensBefore int
	TokensAfter  int
	Saved        int
}

// CompressWithHeadroom POSTs OpenAI-shaped messages to headroom proxy /v1/compress.
// Fail-open: returns nil stats and leaves body untouched on any error.
func CompressWithHeadroom(body map[string]any, enabled bool, proxyURL, model, format string, compressUserMessages bool) *HeadroomStats {
	if !enabled || body == nil || proxyURL == "" {
		return nil
	}
	defer func() { recover() }()

	// Only OpenAI-shaped messages[] for simplicity; Claude/Kiro need translators — fail-open skip
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil
	}

	endpoint := buildCompressEndpoint(proxyURL)
	payload := map[string]any{"messages": messages, "model": model}
	if compressUserMessages {
		payload["config"] = map[string]any{"compress_user_messages": true}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultHeadroomTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil
	}
	var data struct {
		Messages []any `json:"messages"`
		Stats    struct {
			TokensBefore int `json:"tokens_before"`
			TokensAfter  int `json:"tokens_after"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || len(data.Messages) == 0 {
		return nil
	}
	// Only replace if count matches (preserve tool structure safety)
	if len(data.Messages) != len(messages) {
		return nil
	}
	body["messages"] = data.Messages
	st := &HeadroomStats{
		TokensBefore: data.Stats.TokensBefore,
		TokensAfter:  data.Stats.TokensAfter,
	}
	if st.TokensBefore > st.TokensAfter {
		st.Saved = st.TokensBefore - st.TokensAfter
	}
	return st
}

func buildCompressEndpoint(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if strings.HasSuffix(raw, "/v1/compress") {
		return raw
	}
	return raw + "/v1/compress"
}

// FormatHeadroomLog formats headroom stats for logs.
func FormatHeadroomLog(st *HeadroomStats) string {
	if st == nil || st.Saved <= 0 {
		return ""
	}
	return fmt.Sprintf("[HEADROOM] saved %d tokens (%d→%d)", st.Saved, st.TokensBefore, st.TokensAfter)
}

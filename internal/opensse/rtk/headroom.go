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

func sendHeadroomRequest(endpoint string, payload []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultHeadroomTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		return nil, http.ErrHandlerTimeout
	}

	defer func() {
		//nolint:errcheck // best effort body close
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, http.ErrHandlerTimeout
	}

	return io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
}

func parseHeadroomResponse(raw []byte, origCount int) ([]any, *HeadroomStats) {
	var data struct {
		Messages []any `json:"messages"`
		Stats    struct {
			TokensBefore int `json:"tokens_before"`
			TokensAfter  int `json:"tokens_after"`
		} `json:"stats"`
	}

	if err := json.Unmarshal(raw, &data); err != nil || len(data.Messages) == 0 || len(data.Messages) != origCount {
		return nil, nil
	}

	st := &HeadroomStats{
		TokensBefore: data.Stats.TokensBefore,
		TokensAfter:  data.Stats.TokensAfter,
		Saved:        0,
	}
	if st.TokensBefore > st.TokensAfter {
		st.Saved = st.TokensBefore - st.TokensAfter
	}

	return data.Messages, st
}

// CompressWithHeadroom POSTs OpenAI-shaped messages to headroom proxy /v1/compress.
// Fail-open: returns nil stats and leaves body untouched on any error.
func CompressWithHeadroom(body map[string]any, enabled bool, proxyURL, model, _ string, compressUserMessages bool) *HeadroomStats {
	if !enabled || body == nil || proxyURL == "" {
		return nil
	}

	defer func() {
		//nolint:errcheck // recovery cleanup
		_ = recover()
	}()

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil
	}

	payload := map[string]any{"messages": messages, "model": model}
	if compressUserMessages {
		payload["config"] = map[string]any{"compress_user_messages": true}
	}

	b, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	raw, err := sendHeadroomRequest(buildCompressEndpoint(proxyURL), b)
	if err != nil {
		return nil
	}

	newMsgs, st := parseHeadroomResponse(raw, len(messages))
	if newMsgs == nil {
		return nil
	}

	body["messages"] = newMsgs

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

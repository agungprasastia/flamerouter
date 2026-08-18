package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/netutil"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Fetch handles web fetch requests.
func Fetch(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, _ executor.Executor, fb *fallback.Fallback, usageSink UsageSink) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	rawURL, _ := m["url"].(string) //nolint:errcheck // optional type assertion
	if rawURL == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: url")
		return nil
	}

	modelStr, _ := m["model"].(string) //nolint:errcheck // optional type assertion
	if shouldDirectFetch(modelStr) {
		return directFetch(ctx, w, rawURL, m)
	}

	return fetchViaProvider(ctx, w, rawURL, modelStr, m, body, st, fb, usageSink)
}

func shouldDirectFetch(modelStr string) bool {
	return modelStr == "" || strings.HasPrefix(modelStr, "local/") || strings.HasPrefix(modelStr, "fetch/")
}

func fetchViaProvider(ctx context.Context, w http.ResponseWriter, rawURL, modelStr string, m map[string]any, reqBody []byte, st *store.Store, fb *fallback.Fallback, usageSink UsageSink) error {
	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" || conn == nil {
		return directFetch(ctx, w, rawURL, m)
	}

	cred := mediaCredentials(conn)

	payload, err := json.Marshal(m)
	if err != nil {
		return directFetch(ctx, w, rawURL, m)
	}

	ex := executor.GetExecutor(providerID)

	res, err := ex.Execute(ctx, cred, modelName, payload, false)
	if err != nil {
		return directFetch(ctx, w, rawURL, m)
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResultRecordUsage(w, res, st, providerID, modelName, conn.ID, reqBody, usageSink)
}

func directFetch(ctx context.Context, w http.ResponseWriter, rawURL string, _ map[string]any) error {
	if err := netutil.AssertPublicURL(rawURL); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return nil
	}

	body, ct, status, err := doFetchRequest(ctx, rawURL)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	text := string(body)
	if strings.Contains(ct, "html") {
		text = stripHTML(text)
	}

	out := map[string]any{
		"url":         rawURL,
		"status":      status,
		"contentType": ct,
		"content":     text,
	}

	j, err := json.Marshal(out)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to marshal response")
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(j) //nolint:errcheck // best effort write

	return nil
}

func doFetchRequest(ctx context.Context, rawURL string) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", 0, err
	}

	req.Header.Set("User-Agent", "FlameRouter/1.0")

	client := &http.Client{
		Transport: nil,
		Jar:       nil,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}

			if req == nil || req.URL == nil {
				return fmt.Errorf("invalid redirect URL")
			}

			if reqErr := netutil.AssertPublicURL(req.URL.String()); reqErr != nil {
				return fmt.Errorf("redirect blocked: %w", reqErr)
			}

			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}

	if resp == nil || resp.Body == nil {
		return nil, "", 0, fmt.Errorf("nil response from upstream")
	}

	defer resp.Body.Close() //nolint:errcheck // best-effort body close

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, "", 0, err
	}

	return body, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func stripHTML(s string) string {
	var b strings.Builder

	inTag := false

	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	// collapse whitespace
	return strings.Join(strings.Fields(b.String()), " ")
}

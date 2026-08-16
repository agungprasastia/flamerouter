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

func Fetch(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	rawURL, _ := m["url"].(string)
	if rawURL == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: url")
		return nil
	}

	// Direct fetch when no model / provider is "fetch" / "jina" / "local"
	modelStr, _ := m["model"].(string)
	if modelStr == "" || strings.HasPrefix(modelStr, "local/") || strings.HasPrefix(modelStr, "fetch/") {
		return directFetch(ctx, w, rawURL, m)
	}

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" || conn == nil {
		return directFetch(ctx, w, rawURL, m)
	}

	cred := mediaCredentials(conn)
	payload, _ := json.Marshal(m)
	ex := executor.GetExecutor(providerID)

	res, err := ex.Execute(ctx, cred, modelName, payload, false)
	if err != nil {
		return directFetch(ctx, w, rawURL, m)
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res, true)
}

func directFetch(ctx context.Context, w http.ResponseWriter, rawURL string, opts map[string]any) error {
	if err := netutil.AssertPublicURL(rawURL); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "FlameRouter/1.0")

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return err
	}

	ct := resp.Header.Get("Content-Type")
	// Prefer text extraction for HTML
	text := string(body)
	if strings.Contains(ct, "html") {
		text = stripHTML(text)
	}

	out := map[string]any{
		"url":         rawURL,
		"status":      resp.StatusCode,
		"contentType": ct,
		"content":     text,
	}
	j, _ := json.Marshal(out)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(j)

	return nil
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

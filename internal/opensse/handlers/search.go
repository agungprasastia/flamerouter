package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/config"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Search handles search requests.
func Search(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, _ executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	query, _ := m["query"].(string) //nolint:errcheck // optional type assertion
	if query == "" {
		// also accept "q"
		query, _ = m["q"].(string) //nolint:errcheck // optional type assertion
	}

	if query == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: query")
		return nil
	}

	modelStr, _ := m["model"].(string) //nolint:errcheck // optional type assertion
	if modelStr == "" {
		modelStr = "searxng/search"
	}

	return executeSearch(ctx, w, st, fb, modelStr, query, m)
}

func executeSearch(ctx context.Context, w http.ResponseWriter, st *store.Store, fb *fallback.Fallback, modelStr, query string, m map[string]any) error {
	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)

	// Built-in searxng path when no connection or provider is searxng
	if providerID == "searxng" || conn == nil || strings.Contains(modelStr, "searxng") || errMsg != "" {
		return searxngSearch(ctx, w, query, m)
	}

	_ = modelName
	cred := mediaCredentials(conn)

	payload, err := json.Marshal(m)
	if err != nil {
		return searxngSearch(ctx, w, query, m)
	}

	ex := executor.GetExecutor(providerID)

	res, err := ex.Execute(ctx, cred, modelName, payload, false)
	if err != nil {
		return searxngSearch(ctx, w, query, m)
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res)
}

func searxngSearch(ctx context.Context, w http.ResponseWriter, query string, opts map[string]any) error {
	reqURL, err := buildSearxngURL(query, opts)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "invalid SEARXNG_URL")
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		jsonError(w, http.StatusBadGateway, fmt.Sprintf("searxng unreachable: %v", err))
		return err
	}

	if resp == nil || resp.Body == nil {
		jsonError(w, http.StatusBadGateway, "searxng nil response")
		return fmt.Errorf("searxng nil response")
	}

	defer resp.Body.Close() //nolint:errcheck // best-effort body close

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	writeSearxngResponse(w, query, resp.StatusCode, body)

	return nil
}

func buildSearxngURL(query string, opts map[string]any) (string, error) {
	base := config.SEARXNGURL()

	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}

	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")

	if cat, ok := opts["categories"].(string); ok {
		q.Set("categories", cat)
	}

	if lang, ok := opts["language"].(string); ok {
		q.Set("language", lang)
	}

	u.RawQuery = q.Encode()

	return u.String(), nil
}

func writeSearxngResponse(w http.ResponseWriter, query string, statusCode int, body []byte) {
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		out := map[string]any{
			"query":   query,
			"results": raw["results"],
		}
		if out["results"] == nil {
			out["results"] = []any{}
		}

		j, err := json.Marshal(out)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(j) //nolint:errcheck // best effort write

			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body) //nolint:errcheck // best effort write
}

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

func Search(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	query, _ := m["query"].(string)
	if query == "" {
		// also accept "q"
		query, _ = m["q"].(string)
	}

	if query == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: query")
		return nil
	}

	modelStr, _ := m["model"].(string)
	if modelStr == "" {
		modelStr = "searxng/search"
	}

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)

	// Built-in searxng path when no connection or provider is searxng
	if providerID == "searxng" || conn == nil || strings.Contains(modelStr, "searxng") {
		return searxngSearch(ctx, w, query, m)
	}

	if errMsg != "" {
		// fallback searxng
		return searxngSearch(ctx, w, query, m)
	}

	_ = modelName
	cred := mediaCredentials(conn)
	payload, _ := json.Marshal(m)
	ex := executor.GetExecutor(providerID)

	res, err := ex.Execute(ctx, cred, modelName, payload, false)
	if err != nil {
		return searxngSearch(ctx, w, query, m)
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res, true)
}

func searxngSearch(ctx context.Context, w http.ResponseWriter, query string, opts map[string]any) error {
	base := config.SEARXNGURL()

	u, err := url.Parse(base)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "invalid SEARXNG_URL")
		return err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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

	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Normalize to simple results array if possible
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		out := map[string]any{
			"query":   query,
			"results": raw["results"],
		}
		if out["results"] == nil {
			out["results"] = []any{}
		}

		j, _ := json.Marshal(out)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(j)

		return nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)

	return nil
}

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/provider"
	"flamerouter/internal/store"
	"io"
	"net/http"
	"strings"
)

const geminiNativeBase = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiV1Beta handles /v1beta/* (list models + generateContent passthrough/convert).
func GeminiV1Beta(ctx context.Context, w http.ResponseWriter, r *http.Request, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	path := strings.TrimPrefix(r.URL.Path, "/v1beta/")
	path = strings.TrimPrefix(path, "/")

	// GET /v1beta/models
	if r.Method == http.MethodGet && (path == "models" || path == "models/") {
		return geminiListModels(w)
	}

	// POST /v1beta/models/{model}:action
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}

	if !strings.HasPrefix(path, "models/") {
		jsonError(w, http.StatusNotFound, "not found")
		return nil
	}

	rest := strings.TrimPrefix(path, "models/")
	stream := strings.Contains(rest, ":streamGenerateContent")

	modelPart := rest
	for _, suf := range []string{":streamGenerateContent", ":generateContent"} {
		if i := strings.Index(modelPart, suf); i >= 0 {
			modelPart = modelPart[:i]
			break
		}
	}

	modelPart = strings.TrimPrefix(modelPart, "models/")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "read body")
		return err
	}

	var geminiBody map[string]any
	if err := json.Unmarshal(body, &geminiBody); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	// Prefer native forward when gemini account exists
	conn, _ := fb.SelectAccountExcluding("gemini", map[string]bool{})
	if conn == nil {
		conn, _ = fb.SelectAccountExcluding("gemini-cli", map[string]bool{})
	}

	if conn != nil && looksLikeNativeGemini(modelPart) {
		if err := forwardGeminiNative(ctx, w, r, conn, modelPart, rest, body); err == nil {
			return nil
		}
		// fall through to chat convert on forward fail without account panic
	}

	// Convert Gemini → OpenAI chat
	model := modelPart
	if !strings.Contains(model, "/") {
		model = "gemini/" + model
	}

	converted := convertGeminiToInternal(geminiBody, model, stream)
	payload, _ := json.Marshal(converted)

	return ChatWithOptions(ctx, w, payload, st, exec, fb, ChatOptions{SourceFormat: "openai"})
}

func looksLikeNativeGemini(model string) bool {
	m := strings.TrimPrefix(model, "models/")
	m = strings.TrimPrefix(m, "gemini/")

	return !strings.Contains(m, "/") || strings.HasPrefix(model, "gemini/") || strings.HasPrefix(model, "models/")
}

func forwardGeminiNative(ctx context.Context, w http.ResponseWriter, r *http.Request, conn *store.Connection, modelID, rest string, body []byte) error {
	modelID = strings.TrimPrefix(modelID, "models/")
	modelID = strings.TrimPrefix(modelID, "gemini/")

	action := ":generateContent"
	if strings.Contains(rest, ":streamGenerateContent") {
		action = ":streamGenerateContent"
	}

	url := geminiNativeBase + "/" + modelID + action

	if q := r.URL.RawQuery; q != "" {
		// strip client key param
		parts := strings.Split(q, "&")

		var keep []string

		for _, p := range parts {
			if strings.HasPrefix(p, "key=") {
				continue
			}

			keep = append(keep, p)
		}

		if len(keep) > 0 {
			url += "?" + strings.Join(keep, "&")
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	if conn.APIKey != "" {
		req.Header.Set("x-goog-api-key", conn.APIKey)
	} else if tok := firstNonEmpty(conn.AccessToken, ""); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	} else {
		return errNoAccount
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		if lk == "content-encoding" || lk == "content-length" || lk == "transfer-encoding" {
			continue
		}

		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	return nil
}

func geminiListModels(w http.ResponseWriter) error {
	var models []map[string]any

	seen := map[string]bool{}
	add := func(name, display, desc string, methods []string) {
		if seen[name] {
			return
		}

		seen[name] = true

		models = append(models, map[string]any{
			"name":                       name,
			"displayName":                display,
			"description":                desc,
			"supportedGenerationMethods": methods,
			"inputTokenLimit":            128000,
			"outputTokenLimit":           8192,
		})
	}

	for _, p := range provider.ListProviders() {
		alias := p.Alias
		if alias == "" {
			alias = p.ID
		}

		for _, m := range p.Models {
			add("models/"+alias+"/"+m.ID, m.Name, alias+" model: "+m.Name, []string{"generateContent"})

			if p.ID == "gemini" {
				add("models/"+m.ID, m.Name, "Gemini model: "+m.Name, []string{"generateContent", "streamGenerateContent"})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")

	return json.NewEncoder(w).Encode(map[string]any{"models": models})
}

func convertGeminiToInternal(geminiBody map[string]any, model string, stream bool) map[string]any {
	var messages []any

	if si, ok := geminiBody["systemInstruction"].(map[string]any); ok {
		if parts, ok := si["parts"].([]any); ok {
			var texts []string

			for _, p := range parts {
				if pm, ok := p.(map[string]any); ok {
					if t, ok := pm["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}

			if len(texts) > 0 {
				messages = append(messages, map[string]any{"role": "system", "content": strings.Join(texts, "\n")})
			}
		}
	}

	if contents, ok := geminiBody["contents"].([]any); ok {
		for _, c := range contents {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}

			role := "user"
			if cm["role"] == "model" {
				role = "assistant"
			}

			var texts []string

			if parts, ok := cm["parts"].([]any); ok {
				for _, p := range parts {
					if pm, ok := p.(map[string]any); ok {
						if t, ok := pm["text"].(string); ok {
							texts = append(texts, t)
						}
					}
				}
			}

			messages = append(messages, map[string]any{"role": role, "content": strings.Join(texts, "\n")})
		}
	}

	out := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	if gc, ok := geminiBody["generationConfig"].(map[string]any); ok {
		if v, ok := gc["maxOutputTokens"]; ok {
			out["max_tokens"] = v
		}

		if v, ok := gc["temperature"]; ok {
			out["temperature"] = v
		}

		if v, ok := gc["topP"]; ok {
			out["top_p"] = v
		}
	}

	return out
}

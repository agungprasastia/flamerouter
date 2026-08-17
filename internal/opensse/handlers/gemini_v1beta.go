package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/rtk"
	"flamerouter/internal/provider"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const geminiNativeBase = "https://generativelanguage.googleapis.com/v1beta/models"

// GeminiV1Beta handles /v1beta/* (list models + generateContent passthrough/convert).
func GeminiV1Beta(ctx context.Context, w http.ResponseWriter, r *http.Request, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	path := strings.TrimPrefix(r.URL.Path, "/v1beta/")
	path = strings.TrimPrefix(path, "/")

	if r.Method == http.MethodGet && (path == "models" || path == "models/") {
		return geminiListModels(w)
	}

	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return nil
	}

	if !strings.HasPrefix(path, "models/") {
		jsonError(w, http.StatusNotFound, "not found")
		return nil
	}

	return handleGeminiPost(ctx, w, r, path, st, exec, fb)
}

func handleGeminiPost(ctx context.Context, w http.ResponseWriter, r *http.Request, path string, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	rest := strings.TrimPrefix(path, "models/")
	stream := strings.Contains(rest, ":streamGenerateContent")
	modelPart := parseGeminiModelPart(rest)

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

	conn := findGeminiAccount(fb)
	if conn != nil && looksLikeNativeGemini(modelPart) {
		if err := forwardGeminiNative(ctx, w, r, conn, modelPart, rest, body); err == nil {
			return nil
		}
	}

	model := modelPart
	if !strings.Contains(model, "/") {
		model = "gemini/" + model
	}

	converted := convertGeminiToInternal(geminiBody, model, stream)
	payload, _ := json.Marshal(converted) //nolint:errcheck // safe internal map marshal

	return ChatWithOptions(ctx, w, payload, st, exec, fb, ChatOptions{
		Usage:           nil,
		ClientHeaders:   nil,
		SourceFormat:    "openai",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})
}

func parseGeminiModelPart(rest string) string {
	modelPart := rest
	for _, suf := range []string{":streamGenerateContent", ":generateContent"} {
		if i := strings.Index(modelPart, suf); i >= 0 {
			modelPart = modelPart[:i]
			break
		}
	}

	return strings.TrimPrefix(modelPart, "models/")
}

func findGeminiAccount(fb *fallback.Fallback) *store.Connection {
	conn, _ := fb.SelectAccountExcluding("gemini", map[string]bool{}) //nolint:errcheck // optional account
	if conn == nil {
		conn, _ = fb.SelectAccountExcluding("gemini-cli", map[string]bool{}) //nolint:errcheck // optional account
	}

	return conn
}

func looksLikeNativeGemini(model string) bool {
	m := strings.TrimPrefix(model, "models/")
	m = strings.TrimPrefix(m, "gemini/")

	return !strings.Contains(m, "/") || strings.HasPrefix(model, "gemini/") || strings.HasPrefix(model, "models/")
}

func forwardGeminiNative(ctx context.Context, w http.ResponseWriter, r *http.Request, conn *store.Connection, modelID, rest string, body []byte) error {
	url := buildGeminiNativeURL(modelID, rest, r.URL.RawQuery)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	authErr := applyGeminiAuth(req, conn)
	if authErr != nil {
		return authErr
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp == nil || resp.Body == nil {
		return fmt.Errorf("nil response from upstream")
	}

	defer resp.Body.Close() //nolint:errcheck // best-effort body close

	copyGeminiHeaders(w, resp.Header)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body) //nolint:errcheck // handler write

	return nil
}

func buildGeminiNativeURL(modelID, rest, rawQuery string) string {
	modelID = strings.TrimPrefix(modelID, "models/")
	modelID = strings.TrimPrefix(modelID, "gemini/")

	action := ":generateContent"
	if strings.Contains(rest, ":streamGenerateContent") {
		action = ":streamGenerateContent"
	}

	url := geminiNativeBase + "/" + modelID + action
	if rawQuery == "" {
		return url
	}

	var keep []string

	for _, p := range strings.Split(rawQuery, "&") {
		if !strings.HasPrefix(p, "key=") {
			keep = append(keep, p)
		}
	}

	if len(keep) > 0 {
		url += "?" + strings.Join(keep, "&")
	}

	return url
}

func applyGeminiAuth(req *http.Request, conn *store.Connection) error {
	if conn.APIKey != "" {
		req.Header.Set("x-goog-api-key", conn.APIKey)
		return nil
	}

	if tok := firstNonEmpty(conn.AccessToken, ""); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
		return nil
	}

	return errNoAccount
}

func copyGeminiHeaders(w http.ResponseWriter, header http.Header) {
	for k, vs := range header {
		lk := strings.ToLower(k)
		if lk == "content-encoding" || lk == "content-length" || lk == "transfer-encoding" {
			continue
		}

		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
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

	if sysMsg := extractSystemInstruction(geminiBody); sysMsg != nil {
		messages = append(messages, sysMsg)
	}

	messages = append(messages, extractContentMessages(geminiBody)...)

	out := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	applyGenerationConfig(out, geminiBody)

	return out
}

func extractSystemInstruction(geminiBody map[string]any) map[string]any {
	si, ok := geminiBody["systemInstruction"].(map[string]any)
	if !ok {
		return nil
	}

	parts, ok := si["parts"].([]any)
	if !ok {
		return nil
	}

	texts := extractTextParts(parts)
	if len(texts) == 0 {
		return nil
	}

	return map[string]any{"role": "system", "content": strings.Join(texts, "\n")}
}

func extractContentMessages(geminiBody map[string]any) []any {
	contents, ok := geminiBody["contents"].([]any)
	if !ok {
		return nil
	}

	messages := make([]any, 0, len(contents))

	for _, c := range contents {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}

		role := "user"
		if cm["role"] == "model" {
			role = "assistant"
		}

		parts, _ := cm["parts"].([]any) //nolint:errcheck // optional cast
		texts := extractTextParts(parts)

		messages = append(messages, map[string]any{"role": role, "content": strings.Join(texts, "\n")})
	}

	return messages
}

func extractTextParts(parts []any) []string {
	texts := make([]string, 0, len(parts))

	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}

		if t, ok := pm["text"].(string); ok {
			texts = append(texts, t)
		}
	}

	return texts
}

func applyGenerationConfig(out, geminiBody map[string]any) {
	gc, ok := geminiBody["generationConfig"].(map[string]any)
	if !ok {
		return
	}

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

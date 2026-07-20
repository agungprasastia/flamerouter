package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/ops"
	"flamerouter/internal/store"
	"flamerouter/internal/translator"
)

var translatorAllowedFiles = map[string]bool{
	"1_req_client.json": true,
	"2_req_source.json": true,
	"3_req_openai.json": true,
	"4_req_target.json": true,
	"5_res_provider.txt": true,
	"6_res_openai.txt":  true,
	"7_res_client.txt":  true,
	"7_res_client.json": true,
}

func (s *Server) translatorLogsDir() string {
	base := ""
	if s.cfg != nil {
		base = s.cfg.DataDir
	}
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "logs", "translator")
}

// GET /api/translator/load?file=
func (s *Server) handleTranslatorLoad(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "File parameter required"})
		return
	}
	if !translatorAllowedFiles[file] {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid file name"})
		return
	}
	path := filepath.Join(s.translatorLogsDir(), file)
	// prevent path escape
	if filepath.Base(path) != file {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid file name"})
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{"success": false, "error": "File not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSONOK(w, map[string]any{"success": true, "content": string(b)})
}

// POST /api/translator/save — {file, content}
func (s *Server) handleTranslatorSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File    string `json:"file"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if req.File == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "File and content required"})
		return
	}
	if !translatorAllowedFiles[req.File] {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid file name"})
		return
	}
	dir := s.translatorLogsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	path := filepath.Join(dir, req.File)
	if filepath.Base(path) != req.File {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid file name"})
		return
	}
	if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	writeJSONOK(w, map[string]any{"success": true})
}

// GET /api/translator/console-logs/stream — SSE of console lines
func (s *Server) handleTranslatorConsoleLogsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// init buffered
	if logs := ops.DefaultConsole.Get(); len(logs) > 0 {
		payload, _ := json.Marshal(map[string]any{"type": "init", "logs": logs})
		_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
		flusher.Flush()
	}

	ch := ops.DefaultConsole.Subscribe()
	defer ops.DefaultConsole.Unsubscribe(ch)

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case line, open := <-ch:
			if !open {
				return
			}
			if line == "" {
				payload, _ := json.Marshal(map[string]any{"type": "clear"})
				_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
			} else {
				payload, _ := json.Marshal(map[string]any{"type": "line", "line": line})
				_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
			}
			flusher.Flush()
		}
	}
}

// POST /api/translator/translate — playground steps 1–3 (9router parity).
func (s *Server) handleTranslatorTranslate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Step int            `json:"step"`
		Body map[string]any `json:"body"`
		// also accept nested body.body (1_req_client shape)
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if req.Step == 0 || req.Body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Step and body required"})
		return
	}

	clientBody := req.Body
	if nested, ok := req.Body["body"].(map[string]any); ok && nested != nil {
		// only unwrap if outer looks like envelope without messages
		if _, hasMsg := req.Body["messages"]; !hasMsg {
			if _, hasContents := req.Body["contents"]; !hasContents {
				clientBody = nested
			}
		}
	}

	switch req.Step {
	case 1:
		modelStr, _ := clientBody["model"].(string)
		ref := model.ParseModel(modelStr)
		provider, m := ref.Provider, ref.Model
		if provider == "" {
			provider = "openai"
		}
		if m == "" {
			m = modelStr
		}
		sourceFormat := translator.DetectSourceFormat(clientBody)
		targetFormat := playgroundTargetFormat(provider)
		writeJSONOK(w, map[string]any{
			"success": true,
			"result": map[string]any{
				"provider": provider, "model": m,
				"sourceFormat": sourceFormat, "targetFormat": targetFormat,
			},
		})
	case 2:
		modelStr, _ := clientBody["model"].(string)
		ref := model.ParseModel(modelStr)
		provider, m := ref.Provider, ref.Model
		if provider == "" {
			provider = "openai"
		}
		if m == "" {
			m = modelStr
		}
		sourceFormat := translator.DetectSourceFormat(clientBody)
		stream := true
		if v, ok := clientBody["stream"].(bool); ok {
			stream = v
		}
		result := translator.DefaultRegistry.TranslateRequest(sourceFormat, translator.FormatOpenAI, clientBody, translator.TranslateOptions{
			Model: m, Stream: stream, Provider: provider,
		})
		delete(result, "_toolNameMap")
		writeJSONOK(w, map[string]any{"success": true, "result": map[string]any{"body": result}})
	case 3:
		// body = { body: openaiBody, provider, model } or flat openai body + provider/model
		openaiBody := clientBody
		if nested, ok := req.Body["body"].(map[string]any); ok && nested != nil {
			openaiBody = nested
		}
		provider := req.Provider
		modelName := req.Model
		if provider == "" {
			provider, _ = req.Body["provider"].(string)
		}
		if modelName == "" {
			modelName, _ = req.Body["model"].(string)
		}
		if provider == "" || modelName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "provider and model required"})
			return
		}
		targetFormat := playgroundTargetFormat(provider)
		stream := true
		if v, ok := openaiBody["stream"].(bool); ok {
			stream = v
		}
		conn := s.firstActiveConn(provider)
		if conn == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "No active connection for provider: " + provider})
			return
		}
		credMap := connCredMap(conn)
		translated := translator.DefaultRegistry.TranslateRequest(translator.FormatOpenAI, targetFormat, openaiBody, translator.TranslateOptions{
			Model: modelName, Stream: stream, Provider: provider, Credentials: credMap,
		})
		delete(translated, "_toolNameMap")
		cred := connCred(conn)
		url, headers, finalBody := playgroundBuild(provider, modelName, stream, cred, translated)
		writeJSONOK(w, map[string]any{
			"success": true,
			"result":  map[string]any{"url": url, "headers": headers, "body": finalBody},
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid step (1-3)"})
	}
}

// POST /api/translator/send — execute translated body once against active connection.
func (s *Server) handleTranslatorSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string         `json:"provider"`
		Model    string         `json:"model"`
		Body     map[string]any `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}
	if req.Provider == "" || req.Model == "" || req.Body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "provider, model, and body required"})
		return
	}
	conn := s.firstActiveConn(req.Provider)
	if conn == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "No active connection for provider: " + req.Provider})
		return
	}
	stream := true
	if v, ok := req.Body["stream"].(bool); ok {
		stream = v
	}
	payload, err := json.Marshal(req.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	exec := executor.GetExecutor(req.Provider)
	res, err := exec.Execute(r.Context(), connCred(conn), req.Model, payload, stream)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errText, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		writeJSON(w, res.StatusCode, map[string]any{
			"success": false,
			"error":   "Provider error: " + http.StatusText(res.StatusCode),
			"details": string(errText),
		})
		return
	}
	ct := res.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/event-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

// GET/DELETE /api/translator/console-logs
func (s *Server) handleTranslatorConsoleLogs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, map[string]any{"success": true, "logs": ops.DefaultConsole.Get()})
	case http.MethodDelete:
		ops.DefaultConsole.Clear()
		writeJSONOK(w, map[string]any{"success": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) firstActiveConn(provider string) *store.Connection {
	if s.st == nil {
		return nil
	}
	conns, err := s.st.ListActiveByProvider(provider)
	if err != nil || len(conns) == 0 {
		return nil
	}
	c := conns[0]
	return &c
}

func connCred(c *store.Connection) executor.Credentials {
	if c == nil {
		return executor.Credentials{}
	}
	return executor.Credentials{
		APIKey: c.APIKey, AccessToken: c.AccessToken, RefreshToken: c.RefreshToken,
		BaseURL: c.BaseURL, ProviderSpecificData: c.ProviderSpecificData,
	}
}

func connCredMap(c *store.Connection) map[string]any {
	if c == nil {
		return nil
	}
	m := map[string]any{
		"apiKey": c.APIKey, "accessToken": c.AccessToken, "refreshToken": c.RefreshToken,
		"baseUrl": c.BaseURL, "providerSpecificData": c.ProviderSpecificData,
	}
	return m
}

func playgroundTargetFormat(providerID string) string {
	providerFormats := map[string]string{
		"claude": translator.FormatClaude, "anthropic": translator.FormatClaude,
		"anthropic-compatible": translator.FormatClaude,
		"gemini": translator.FormatGemini, "gemini-cli": translator.FormatGeminiCLI,
		"vertex": translator.FormatVertex, "vertex-partner": translator.FormatVertex,
		"antigravity": translator.FormatAntigravity, "kiro": translator.FormatKiro,
		"cursor": translator.FormatCursor, "cu": translator.FormatCursor,
		"ollama": translator.FormatOllama, "ollama-local": translator.FormatOllama,
		"commandcode": translator.FormatCommandCode, "codex": translator.FormatCodex,
	}
	if f, ok := providerFormats[providerID]; ok {
		return f
	}
	if strings.HasPrefix(providerID, "anthropic-compatible") {
		return translator.FormatClaude
	}
	return translator.FormatOpenAI
}

// playgroundBuild: minimal url/headers/body for step-3 preview (no full executor surface).
func playgroundBuild(provider, model string, stream bool, cred executor.Credentials, body map[string]any) (string, map[string]string, map[string]any) {
	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	url := base
	switch playgroundTargetFormat(provider) {
	case translator.FormatClaude:
		if !strings.HasSuffix(base, "/messages") {
			url = base + "/messages"
		}
	case translator.FormatGemini, translator.FormatGeminiCLI:
		// leave base; real path is model-specific
	default:
		if !strings.HasSuffix(base, "/chat/completions") && !strings.HasSuffix(base, "/responses") {
			url = base + "/chat/completions"
		}
	}
	headers := map[string]string{"Content-Type": "application/json"}
	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}
	if tok != "" {
		headers["Authorization"] = "Bearer " + tok
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	if body != nil {
		body["model"] = model
		body["stream"] = stream
	}
	return url, headers, body
}

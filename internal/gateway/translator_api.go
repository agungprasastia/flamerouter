package gateway

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/ops"
	"flamerouter/internal/store"
	"flamerouter/internal/translator"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var translatorAllowedFiles = map[string]bool{
	"1_req_client.json":  true,
	"2_req_source.json":  true,
	"3_req_openai.json":  true,
	"4_req_target.json":  true,
	"5_res_provider.txt": true,
	"6_res_openai.txt":   true,
	"7_res_client.txt":   true,
	"7_res_client.json":  true,
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

// GET /api/translator/load?file=.
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

	path := filepath.Clean(filepath.Join(s.translatorLogsDir(), file))
	// prevent path escape
	if filepath.Base(path) != file {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid file name"})
		return
	}

	b, err := os.ReadFile(path) //nolint:gosec
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

// POST /api/translator/save — {file, content}.
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
	if err := os.MkdirAll(dir, 0o750); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}

	path := filepath.Clean(filepath.Join(dir, req.File))
	if filepath.Base(path) != req.File {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid file name"})
		return
	}

	if err := os.WriteFile(path, []byte(req.Content), 0o600); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}

	writeJSONOK(w, map[string]any{"success": true})
}

// GET /api/translator/console-logs/stream — SSE of console lines.
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
		payload, err := json.Marshal(map[string]any{"type": "init", "logs": logs})
		if err == nil {
			//nolint:errcheck // sse write
			_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
		}

		flusher.Flush()
	}

	ch := ops.DefaultConsole.Subscribe()
	defer ops.DefaultConsole.Unsubscribe(ch)

	s.streamConsoleLogs(r.Context(), w, flusher, ch)
}

func writeConsoleEvent(w http.ResponseWriter, line string, flusher http.Flusher) {
	if line == "" {
		payload, err := json.Marshal(map[string]any{"type": "clear"})
		if err == nil {
			//nolint:errcheck // sse write
			_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
		}
	} else {
		payload, err := json.Marshal(map[string]any{"type": "line", "line": line})
		if err == nil {
			//nolint:errcheck // sse write
			_, _ = w.Write([]byte("data: " + string(payload) + "\n\n"))
		}
	}

	flusher.Flush()
}

func (s *Server) streamConsoleLogs(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, ch <-chan string) {
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ping.C:
			//nolint:errcheck // sse ping
			_, _ = w.Write([]byte(": ping\n\n"))

			flusher.Flush()
		case line, open := <-ch:
			if !open {
				return
			}

			writeConsoleEvent(w, line, flusher)
		}
	}
}

func (s *Server) translateStep1(w http.ResponseWriter, clientBody map[string]any) {
	modelStr, _ := clientBody["model"].(string) //nolint:errcheck // safe type assertion
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
}

func (s *Server) translateStep2(w http.ResponseWriter, clientBody map[string]any) {
	modelStr, _ := clientBody["model"].(string) //nolint:errcheck // safe type assertion
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
		Model:        m,
		Stream:       stream,
		Provider:     provider,
		ClientTool:   false,
		Credentials:  nil,
		ConnectionID: "",
		StripList:    nil,
	})
	delete(result, "_toolNameMap")
	writeJSONOK(w, map[string]any{"success": true, "result": map[string]any{"body": result}})
}

func (s *Server) translateStep3(w http.ResponseWriter, reqBody map[string]any, clientBody map[string]any, reqProvider, reqModel string) {
	openaiBody := clientBody
	if nested, ok := reqBody["body"].(map[string]any); ok && nested != nil {
		openaiBody = nested
	}

	provider := reqProvider
	modelName := reqModel

	if provider == "" {
		provider, _ = reqBody["provider"].(string) //nolint:errcheck // safe type assertion
	}

	if modelName == "" {
		modelName, _ = reqBody["model"].(string) //nolint:errcheck // safe type assertion
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
		Model:        modelName,
		Stream:       stream,
		Provider:     provider,
		Credentials:  credMap,
		ClientTool:   false,
		ConnectionID: "",
		StripList:    nil,
	})
	delete(translated, "_toolNameMap")

	cred := connCred(conn)
	url, headers, finalBody := playgroundBuild(provider, modelName, stream, cred, translated)
	writeJSONOK(w, map[string]any{
		"success": true,
		"result":  map[string]any{"url": url, "headers": headers, "body": finalBody},
	})
}

func extractPlaygroundClientBody(reqBody map[string]any) map[string]any {
	clientBody := reqBody

	if nested, ok := reqBody["body"].(map[string]any); ok && nested != nil {
		if _, hasMsg := reqBody["messages"]; !hasMsg {
			if _, hasContents := reqBody["contents"]; !hasContents {
				clientBody = nested
			}
		}
	}

	return clientBody
}

// POST /api/translator/translate — playground steps 1–3 (9router parity).
func (s *Server) handleTranslatorTranslate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body     map[string]any `json:"body"`
		Provider string         `json:"provider"`
		Model    string         `json:"model"`
		Step     int            `json:"step"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "invalid json"})
		return
	}

	if req.Step == 0 || req.Body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Step and body required"})
		return
	}

	clientBody := extractPlaygroundClientBody(req.Body)

	switch req.Step {
	case 1:
		s.translateStep1(w, clientBody)
	case 2:
		s.translateStep2(w, clientBody)
	case 3:
		s.translateStep3(w, req.Body, clientBody, req.Provider, req.Model)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": "Invalid step (1-3)"})
	}
}

// POST /api/translator/send — execute translated body once against active connection.
func (s *Server) handleTranslatorSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body     map[string]any `json:"body"`
		Provider string         `json:"provider"`
		Model    string         `json:"model"`
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

	s.executeTranslatorRequest(w, r, conn, req.Provider, req.Model, req.Body)
}

func (s *Server) executeTranslatorRequest(w http.ResponseWriter, r *http.Request, conn *store.Connection, providerID, model string, body map[string]any) {
	stream := true
	if v, ok := body["stream"].(bool); ok {
		stream = v
	}

	payload, err := json.Marshal(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}

	exec := executor.GetExecutor(providerID)

	res, err := exec.Execute(r.Context(), connCred(conn), model, payload, stream)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": err.Error()})
		return
	}

	defer res.Body.Close() //nolint:errcheck // best-effort body close

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		forwardTranslatorError(w, res)
		return
	}

	forwardTranslatorStream(w, res)
}

func forwardTranslatorError(w http.ResponseWriter, res *executor.Result) {
	errText, err := io.ReadAll(io.LimitReader(res.Body, 8192))
	if err != nil {
		errText = nil
	}

	writeJSON(w, res.StatusCode, map[string]any{
		"success": false,
		"error":   "Provider error: " + http.StatusText(res.StatusCode),
		"details": string(errText),
	})
}

func forwardTranslatorStream(w http.ResponseWriter, res *executor.Result) {
	ct := res.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/event-stream"
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(res.StatusCode)

	if _, err := io.Copy(w, res.Body); err != nil {
		_ = err
	}
}

// GET/DELETE /api/translator/console-logs.
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

	return &conns[0]
}

func playgroundTargetURL(provider, base string) string {
	switch playgroundTargetFormat(provider) {
	case translator.FormatClaude:
		if !strings.HasSuffix(base, "/messages") {
			return base + "/messages"
		}

		return base
	case translator.FormatGemini, translator.FormatGeminiCLI:
		return base
	default:
		if !strings.HasSuffix(base, "/chat/completions") && !strings.HasSuffix(base, "/responses") {
			return base + "/chat/completions"
		}

		return base
	}
}

// playgroundBuild: minimal url/headers/body for step-3 preview (no full executor surface).
func playgroundBuild(provider, model string, stream bool, cred executor.Credentials, body map[string]any) (string, map[string]string, map[string]any) {
	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}

	url := playgroundTargetURL(provider, base)
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

func connCred(c *store.Connection) executor.Credentials {
	if c == nil {
		return executor.Credentials{
			APIKey:               "",
			AccessToken:          "",
			RefreshToken:         "",
			BaseURL:              "",
			ProjectID:            "",
			ProviderSpecificData: nil,
		}
	}

	return executor.Credentials{
		APIKey:               c.APIKey,
		AccessToken:          c.AccessToken,
		RefreshToken:         c.RefreshToken,
		BaseURL:              c.BaseURL,
		ProjectID:            "",
		ProviderSpecificData: c.ProviderSpecificData,
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
		"gemini":               translator.FormatGemini, "gemini-cli": translator.FormatGeminiCLI,
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

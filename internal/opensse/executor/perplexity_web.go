package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterSpecialized("perplexity-web", &PerplexityWebExecutor{
		Base: Base{
			Provider: "perplexity-web",
			BaseURL:  "https://www.perplexity.ai/rest/sse/perplexity_ask",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
	RegisterSpecialized("pplx-web", &PerplexityWebExecutor{
		Base: Base{
			Provider: "perplexity-web",
			BaseURL:  "https://www.perplexity.ai/rest/sse/perplexity_ask",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
}

const (
	pplxSSEEndpoint = "https://www.perplexity.ai/rest/sse/perplexity_ask"
	pplxAPIVersion  = "2.18"
	pplxUserAgent   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
)

var (
	pplxModelMap = map[string][2]string{
		"pplx-auto":     {"concise", "pplx_pro"},
		"pplx-sonar":    {"copilot", "experimental"},
		"pplx-gpt":      {"copilot", "gpt54"},
		"pplx-gemini":   {"copilot", "gemini31pro_high"},
		"pplx-sonnet":   {"copilot", "claude46sonnet"},
		"pplx-opus":     {"copilot", "claude46opus"},
		"pplx-nemotron": {"copilot", "nv_nemotron_3_super"},
	}

	pplxThinkingMap = map[string]string{
		"pplx-gpt":    "gpt54_thinking",
		"pplx-sonnet": "claude46sonnetthinking",
		"pplx-opus":   "claude46opusthinking",
	}

	pplxCitationRE = regexp.MustCompile(`\[\d+\]`)
	pplxGrokTagRE  = regexp.MustCompile(`(?s)<grok:[^>]*>.*?</grok:[^>]*>`)
	pplxGrokSelfRE = regexp.MustCompile(`<grok:[^>]*/>`)
	pplxXMLDeclRE  = regexp.MustCompile(`<\?[xX][mM][lL][^?]*\?>`)
	pplxResponseRE = regexp.MustCompile(`(?i)</?response\b[^>]*>`)
	pplxMultiSpace = regexp.MustCompile(` {2,}`)
	pplxMultiNL    = regexp.MustCompile(`\n{3,}`)

	pplxSessionCache   = make(map[string]pplxSessionEntry)
	pplxSessionCacheMu sync.Mutex
)

type pplxSessionEntry struct {
	backendUUID string
	ts          int64
}

// PerplexityWebExecutor handles queries to Perplexity Web backend.
type PerplexityWebExecutor struct {
	Base
}

type pplxHistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type pplxParsedMessages struct {
	SystemMsg  string
	CurrentMsg string
	History    []pplxHistoryItem
}

func pplxSessionKey(history []pplxHistoryItem) string {
	parts := make([]string, 0, len(history))
	for _, h := range history {
		parts = append(parts, h.Role+":"+h.Content)
	}

	joined := strings.Join(parts, "\n")

	var hash uint32 = 0x811c9dc5

	for i := 0; i < len(joined); i++ {
		hash ^= uint32(joined[i])
		hash = (hash * 0x01000193)
	}

	return fmt.Sprintf("%08x", hash)
}

func pplxSessionLookup(history []pplxHistoryItem) string {
	if len(history) == 0 {
		return ""
	}

	key := pplxSessionKey(history)

	pplxSessionCacheMu.Lock()
	defer pplxSessionCacheMu.Unlock()

	entry, ok := pplxSessionCache[key]
	if !ok {
		return ""
	}

	if time.Now().UnixMilli()-entry.ts > 3600*1000 {
		delete(pplxSessionCache, key)
		return ""
	}

	return entry.backendUUID
}

func pplxSessionStore(history []pplxHistoryItem, currentMsg, responseText, backendUUID string) {
	if backendUUID == "" {
		return
	}

	full := make([]pplxHistoryItem, len(history), len(history)+2)
	copy(full, history)
	full = append(full, pplxHistoryItem{Role: "user", Content: currentMsg})
	full = append(full, pplxHistoryItem{Role: "assistant", Content: responseText})

	key := pplxSessionKey(full)

	pplxSessionCacheMu.Lock()
	defer pplxSessionCacheMu.Unlock()

	pplxSessionCache[key] = pplxSessionEntry{
		backendUUID: backendUUID,
		ts:          time.Now().UnixMilli(),
	}
	if len(pplxSessionCache) > 200 {
		var oldestKey string

		var oldestTS int64 = math.MaxInt64
		for k, v := range pplxSessionCache {
			if v.ts < oldestTS {
				oldestTS = v.ts
				oldestKey = k
			}
		}

		if oldestKey != "" {
			delete(pplxSessionCache, oldestKey)
		}
	}
}

func cleanPplxResponse(text string, strip bool) string {
	t := text
	t = pplxXMLDeclRE.ReplaceAllString(t, "")
	t = pplxCitationRE.ReplaceAllString(t, "")
	t = pplxGrokTagRE.ReplaceAllString(t, "")
	t = pplxGrokSelfRE.ReplaceAllString(t, "")
	t = pplxResponseRE.ReplaceAllString(t, "")

	if strip {
		t = pplxMultiSpace.ReplaceAllString(t, " ")
		t = pplxMultiNL.ReplaceAllString(t, "\n\n")
		t = strings.TrimSpace(t)
	}

	return t
}

func parsePplxContent(c any) string {
	switch content := c.(type) {
	case string:
		return content
	case []any:
		parts := make([]string, 0, len(content))

		for _, p := range content {
			if pm, ok := p.(map[string]any); ok {
				if t, ok := pm["type"].(string); ok && t == "text" {
					parts = append(parts, fmt.Sprint(pm["text"]))
				}
			}
		}

		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func parsePplxRole(role string) string {
	if role == "" || role == "developer" {
		if role == "developer" {
			return "system"
		}

		return "user"
	}

	return role
}

func parsePplxOpenAIMessages(messages []any) pplxParsedMessages {
	var systemMsg strings.Builder

	history := make([]pplxHistoryItem, 0, len(messages))

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok {
			role = "user"
		}

		role = parsePplxRole(role)

		content := parsePplxContent(msg["content"])
		if strings.TrimSpace(content) == "" {
			continue
		}

		switch role {
		case "system":
			_, _ = systemMsg.WriteString(content) // nolint:errcheck
			_, _ = systemMsg.WriteString("\n")    // nolint:errcheck
		case "user", "assistant":
			history = append(history, pplxHistoryItem{Role: role, Content: content})
		}
	}

	currentMsg := ""
	if len(history) > 0 && history[len(history)-1].Role == "user" {
		currentMsg = history[len(history)-1].Content
		history = history[:len(history)-1]
	}

	return pplxParsedMessages{
		SystemMsg:  systemMsg.String(),
		History:    history,
		CurrentMsg: currentMsg,
	}
}

func formatSinglePplxTool(tm map[string]any) string {
	fn, ok := tm["function"].(map[string]any)
	if !ok {
		fn = tm
	}

	name, ok := fn["name"].(string)
	if !ok || name == "" {
		name = "unnamed"
	}

	desc, ok := fn["description"].(string)
	if !ok {
		desc = ""
	}

	if firstLine := strings.Split(desc, "\n")[0]; len(firstLine) > 200 {
		desc = firstLine[:200]
	} else {
		desc = firstLine
	}

	return fmt.Sprintf("- %s: %s", name, desc)
}

func formatPplxToolsHint(tools any) string {
	toolsArr, ok := tools.([]any)
	if !ok || len(toolsArr) == 0 {
		return ""
	}

	lines := make([]string, 0, len(toolsArr))

	for _, t := range toolsArr {
		if tm, ok := t.(map[string]any); ok {
			lines = append(lines, formatSinglePplxTool(tm))
		}
	}

	if len(lines) == 0 {
		return ""
	}

	return "Available tools (reference only, cannot invoke):\n" + strings.Join(lines, "\n")
}

func buildPplxQuery(parsed pplxParsedMessages, followUpUUID string, tools any) string {
	if followUpUUID != "" {
		return parsed.CurrentMsg
	}

	obj := make(map[string]any)

	var instr []string
	if strings.TrimSpace(parsed.SystemMsg) != "" {
		instr = append(instr, strings.TrimSpace(parsed.SystemMsg))
	}

	if hint := formatPplxToolsHint(tools); hint != "" {
		instr = append(instr, hint)
	}

	instr = append(instr, "You have built-in web search. Answer questions directly using search results.")

	obj["instructions"] = instr
	if len(parsed.History) > 0 {
		obj["history"] = parsed.History
	}

	if parsed.CurrentMsg != "" {
		obj["query"] = parsed.CurrentMsg
	} else if len(parsed.History) == 0 {
		obj["query"] = ""
	}

	b, _ := json.Marshal(obj) // nolint:errcheck

	s := string(b)
	if len(s) > 96000 {
		return s[len(s)-96000:]
	}

	return s
}

func buildPplxRequestBody(query, mode, modelPref, followUpUUID string) map[string]any {
	var followUpVal any
	if followUpUUID != "" {
		followUpVal = followUpUUID
	}

	return map[string]any{
		"query_str": query,
		"params": map[string]any{
			"query_str":             query,
			"search_focus":          "internet",
			"mode":                  mode,
			"model_preference":      modelPref,
			"sources":               []string{"web"},
			"attachments":           []any{},
			"frontend_uuid":         randomUUID(),
			"frontend_context_uuid": randomUUID(),
			"version":               pplxAPIVersion,
			"language":              "en-US",
			"timezone":              "UTC",
			"search_recency_filter": nil,
			"is_incognito":          true,
			"use_schematized_api":   true,
			"last_backend_uuid":     followUpVal,
		},
	}
}

func resolvePplxModelAndMode(model string, m map[string]any) (string, string) {
	thinking := false
	if th, ok := m["thinking"].(bool); ok && th {
		thinking = true
	} else if re, ok := m["reasoning_effort"].(string); ok && re != "" && re != "none" {
		thinking = true
	}

	pplxMode := "copilot"
	modelPref := model

	if thinking && pplxThinkingMap[model] != "" {
		pplxMode = "copilot"
		modelPref = pplxThinkingMap[model]
	} else if mapped, ok := pplxModelMap[model]; ok {
		pplxMode = mapped[0]
		modelPref = mapped[1]
	}

	return pplxMode, modelPref
}

func buildPplxRequestHeaders(cred Credentials) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")
	h.Set("Origin", "https://www.perplexity.ai")
	h.Set("Referer", "https://www.perplexity.ai/")
	h.Set("User-Agent", pplxUserAgent)
	h.Set("X-App-ApiClient", "default")
	h.Set("X-App-ApiVersion", pplxAPIVersion)

	if cred.AccessToken != "" {
		h.Set("Authorization", "Bearer "+cred.AccessToken)
	} else if cred.APIKey != "" {
		h.Set("Cookie", fmt.Sprintf("__Secure-next-auth.session-token=%s", cred.APIKey))
	}

	return h
}

func (e *PerplexityWebExecutor) sendPplxRequest(ctx context.Context, cred Credentials, query, pplxMode, modelPref, followUpUUID string) (*Result, error) {
	payloadBytes, err := json.Marshal(buildPplxRequestBody(query, pplxMode, modelPref, followUpUUID))
	if err != nil {
		return nil, err
	}

	h := buildPplxRequestHeaders(cred)

	url := strings.TrimRight(cred.BaseURL, "/")
	if url == "" {
		url = pplxSSEEndpoint
	}

	return e.DoPOST(ctx, url, h, payloadBytes)
}

// Execute performs Perplexity Web chat completion requests.
func (e *PerplexityWebExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	messages, ok := m["messages"].([]any)
	if !ok || len(messages) == 0 {
		return jsonErr(400, "Missing or empty messages array", "invalid_request", ""), nil
	}

	pplxMode, modelPref := resolvePplxModelAndMode(model, m)
	parsed := parsePplxOpenAIMessages(messages)
	followUpUUID := pplxSessionLookup(parsed.History)

	query := buildPplxQuery(parsed, followUpUUID, m["tools"])
	if strings.TrimSpace(query) == "" {
		return jsonErr(400, "Empty query after processing", "invalid_request", ""), nil
	}

	res, err := e.sendPplxRequest(ctx, cred, query, pplxMode, modelPref, followUpUUID)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return handlePplxHTTPError(res), nil
	}

	cid := fmt.Sprintf("chatcmpl-pplx-%s", randomUUID()[:12])
	created := time.Now().Unix()

	if stream {
		sseBody := wrapPplxStream(res.Body, model, cid, created, parsed.History, parsed.CurrentMsg)

		return &Result{
			StatusCode: 200,
			Header: http.Header{
				"Content-Type":  []string{"text/event-stream"},
				"Cache-Control": []string{"no-cache"},
				"Connection":    []string{"keep-alive"},
			},
			Body: sseBody,
		}, nil
	}

	jsonResp, err := collectPplxNonStreaming(res.Body, model, cid, created, parsed.History, parsed.CurrentMsg)
	if err != nil {
		return jsonErr(502, err.Error(), "upstream_error", "PPLX_ERROR"), nil
	}

	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(jsonResp)),
	}, nil
}

func handlePplxHTTPError(res *Result) *Result {
	status := res.StatusCode
	msg := fmt.Sprintf("Perplexity returned HTTP %d", status)

	switch status {
	case 401, 403:
		msg = "Perplexity auth failed — session cookie may be expired. Re-paste your __Secure-next-auth.session-token."
	case 429:
		msg = "Perplexity rate limited. Wait a moment and retry."
	}

	DrainBody(res.Body)

	return jsonErr(status, msg, "upstream_error", fmt.Sprintf("HTTP_%d", status))
}

type pplxExtractedChunk struct {
	delta       string
	answer      string
	thinking    string
	backendUUID string
	errorMsg    string
	done        bool
}

func parsePplxSearchWebQuery(qm map[string]any, seenThinking map[string]bool, backendUUID string, out chan<- pplxExtractedChunk) {
	qr, ok := qm["query"].(string)
	if ok && qr != "" && !seenThinking[qr] {
		seenThinking[qr] = true
		out <- pplxExtractedChunk{
			delta:       "",
			answer:      "",
			thinking:    "Searching: " + qr,
			errorMsg:    "",
			backendUUID: backendUUID,
			done:        false,
		}
	}
}

func parsePplxProSearchStep(sRaw any, seenThinking map[string]bool, backendUUID string, out chan<- pplxExtractedChunk) {
	s, ok := sRaw.(map[string]any)
	if !ok || s["step_type"] != "SEARCH_WEB" {
		return
	}

	swc, ok := s["search_web_content"].(map[string]any)
	if !ok {
		return
	}

	queries, ok := swc["queries"].([]any)
	if !ok {
		return
	}

	for _, qRaw := range queries {
		if qm, ok := qRaw.(map[string]any); ok {
			parsePplxSearchWebQuery(qm, seenThinking, backendUUID, out)
		}
	}
}

func parsePplxProSearchSteps(b map[string]any, seenThinking map[string]bool, backendUUID string, out chan<- pplxExtractedChunk) {
	pb, ok := b["plan_block"].(map[string]any)
	if !ok {
		return
	}

	steps, ok := pb["steps"].([]any)
	if !ok {
		return
	}

	for _, sRaw := range steps {
		parsePplxProSearchStep(sRaw, seenThinking, backendUUID, out)
	}
}

func parsePplxMarkdownBlock(b map[string]any, fullAnswer *string, seenLen *int, backendUUID string, out chan<- pplxExtractedChunk) {
	mb, ok := b["markdown_block"].(map[string]any)
	if !ok {
		return
	}

	chunks, ok := mb["chunks"].([]any)
	if !ok || len(chunks) == 0 {
		return
	}

	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(fmt.Sprint(c))
	}

	chunkText := sb.String()

	prog, ok := mb["progress"].(string)
	if ok && prog == "DONE" {
		*fullAnswer = chunkText
	} else {
		*fullAnswer += chunkText
	}

	if len(*fullAnswer) > *seenLen {
		delta := (*fullAnswer)[*seenLen:]
		*seenLen = len(*fullAnswer)
		out <- pplxExtractedChunk{
			delta:       delta,
			answer:      *fullAnswer,
			thinking:    "",
			errorMsg:    "",
			backendUUID: backendUUID,
			done:        false,
		}
	}
}

func handlePplxSingleBlock(bRaw any, fullAnswer *string, seenLen *int, seenThinking map[string]bool, backendUUID string, out chan<- pplxExtractedChunk) {
	b, ok := bRaw.(map[string]any)
	if !ok {
		return
	}

	usage, ok := b["intended_usage"].(string)
	if !ok {
		return
	}

	switch {
	case usage == "pro_search_steps":
		parsePplxProSearchSteps(b, seenThinking, backendUUID, out)
	case strings.Contains(usage, "markdown"):
		parsePplxMarkdownBlock(b, fullAnswer, seenLen, backendUUID, out)
	}
}

func handlePplxEventBlocks(blocks []any, fullAnswer *string, seenLen *int, seenThinking map[string]bool, backendUUID string, out chan<- pplxExtractedChunk) {
	for _, bRaw := range blocks {
		handlePplxSingleBlock(bRaw, fullAnswer, seenLen, seenThinking, backendUUID, out)
	}
}

func handlePplxEventText(txt string, fullAnswer *string, seenLen *int, backendUUID string, out chan<- pplxExtractedChunk) {
	t := strings.TrimSpace(txt)
	if len(t) > *seenLen {
		delta := t[*seenLen:]
		*fullAnswer = t
		*seenLen = len(t)
		out <- pplxExtractedChunk{
			delta:       delta,
			answer:      *fullAnswer,
			thinking:    "",
			errorMsg:    "",
			backendUUID: backendUUID,
			done:        false,
		}
	}
}

func handlePplxEventContent(event map[string]any, fullAnswer *string, seenLen *int, seenThinking map[string]bool, backendUUID string, out chan<- pplxExtractedChunk) {
	if blocks, ok := event["blocks"].([]any); ok && len(blocks) > 0 {
		handlePplxEventBlocks(blocks, fullAnswer, seenLen, seenThinking, backendUUID, out)
	} else if txt, ok := event["text"].(string); ok && txt != "" {
		handlePplxEventText(txt, fullAnswer, seenLen, backendUUID, out)
	}
}

func handlePplxSingleEvent(event map[string]any, fullAnswer *string, seenLen *int, seenThinking map[string]bool, backendUUID *string, out chan<- pplxExtractedChunk) (bool, bool) {
	if errMsg, ok := event["error_message"].(string); ok && errMsg != "" {
		out <- pplxExtractedChunk{
			delta:       "",
			answer:      "",
			thinking:    "",
			errorMsg:    errMsg,
			backendUUID: "",
			done:        true,
		}

		return true, true
	}

	if bu, ok := event["backend_uuid"].(string); ok && bu != "" {
		*backendUUID = bu
	}

	handlePplxEventContent(event, fullAnswer, seenLen, seenThinking, *backendUUID, out)

	fin, okFin := event["final"].(bool)
	status, okStatus := event["status"].(string)

	return false, (okFin && fin) || (okStatus && status == "COMPLETED")
}

func readPplxEvents(r io.Reader, out chan<- pplxExtractedChunk) {
	defer close(out)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var fullAnswer string

	var backendUUID string

	seenLen := 0
	seenThinking := make(map[string]bool)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		shouldReturn, shouldBreak := handlePplxSingleEvent(event, &fullAnswer, &seenLen, seenThinking, &backendUUID, out)
		if shouldReturn {
			return
		}

		if shouldBreak {
			break
		}
	}

	out <- pplxExtractedChunk{
		delta:       "",
		answer:      fullAnswer,
		thinking:    "",
		errorMsg:    "",
		backendUUID: backendUUID,
		done:        true,
	}
}

func sendPplxSSEChunk(pw *io.PipeWriter, cid, model string, created int64, delta map[string]any, finishReason any) {
	b, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
		"system_fingerprint": nil,
		"choices": []any{map[string]any{
			"index": 0, "delta": delta, "finish_reason": finishReason, "logprobs": nil,
		}},
	})
	_, _ = pw.Write([]byte("data: ")) // nolint:errcheck
	_, _ = pw.Write(b)                // nolint:errcheck
	_, _ = pw.Write([]byte("\n\n"))   // nolint:errcheck
}

func streamPplxChunks(ch <-chan pplxExtractedChunk, pw *io.PipeWriter, cid, model string, created int64) (string, string) {
	var (
		fullAnswer      string
		respBackendUUID string
	)

	for chunk := range ch {
		if chunk.backendUUID != "" {
			respBackendUUID = chunk.backendUUID
		}

		if chunk.errorMsg != "" {
			sendPplxSSEChunk(pw, cid, model, created, map[string]any{"content": "[Error: " + chunk.errorMsg + "]"}, nil)
			break
		}

		if chunk.thinking != "" {
			sendPplxSSEChunk(pw, cid, model, created, map[string]any{"reasoning_content": chunk.thinking + "\n"}, nil)
			continue
		}

		if chunk.done {
			if chunk.answer != "" {
				fullAnswer = chunk.answer
			}

			break
		}

		if chunk.delta != "" {
			if dt := cleanPplxResponse(chunk.delta, false); dt != "" {
				sendPplxSSEChunk(pw, cid, model, created, map[string]any{"content": dt}, nil)
			}
		}

		if chunk.answer != "" {
			fullAnswer = chunk.answer
		}
	}

	return fullAnswer, respBackendUUID
}

func wrapPplxStream(r io.ReadCloser, model, cid string, created int64, history []pplxHistoryItem, currentMsg string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = r.Close() }()  // nolint:errcheck
		defer func() { _ = pw.Close() }() // nolint:errcheck

		sendPplxSSEChunk(pw, cid, model, created, map[string]any{"role": "assistant"}, nil)

		ch := make(chan pplxExtractedChunk, 16)
		go readPplxEvents(r, ch)

		fullAnswer, respBackendUUID := streamPplxChunks(ch, pw, cid, model, created)

		sendPplxSSEChunk(pw, cid, model, created, map[string]any{}, "stop")
		_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck

		pplxSessionStore(history, currentMsg, cleanPplxResponse(fullAnswer, true), respBackendUUID)
	}()

	return pr
}

func collectPplxNonStreaming(r io.ReadCloser, model, cid string, created int64, history []pplxHistoryItem, currentMsg string) ([]byte, error) {
	defer func() { _ = r.Close() }() // nolint:errcheck

	ch := make(chan pplxExtractedChunk, 16)
	go readPplxEvents(r, ch)

	var (
		fullAnswer      string
		respBackendUUID string
		thinkingParts   []string
	)

	for chunk := range ch {
		if chunk.backendUUID != "" {
			respBackendUUID = chunk.backendUUID
		}

		if chunk.errorMsg != "" {
			return nil, fmt.Errorf("%s", chunk.errorMsg)
		}

		if chunk.thinking != "" {
			thinkingParts = append(thinkingParts, chunk.thinking)
			continue
		}

		if chunk.done || chunk.answer != "" {
			fullAnswer = chunk.answer

			if chunk.done {
				break
			}
		}
	}

	fullAnswer = cleanPplxResponse(fullAnswer, true)
	pplxSessionStore(history, currentMsg, fullAnswer, respBackendUUID)

	msg := map[string]any{"role": "assistant", "content": fullAnswer}
	if len(thinkingParts) > 0 {
		msg["reasoning_content"] = strings.Join(thinkingParts, "\n")
	}

	promptTokens := (len(currentMsg) + 3) / 4
	completionTokens := (len(fullAnswer) + 3) / 4

	return json.Marshal(map[string]any{
		"id": cid, "object": "chat.completion", "created": created, "model": model,
		"system_fingerprint": nil,
		"choices": []any{map[string]any{
			"index": 0, "message": msg, "finish_reason": "stop", "logprobs": nil,
		}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	})
}

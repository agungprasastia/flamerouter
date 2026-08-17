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
	var parts []string
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

		var oldestTs int64 = math.MaxInt64
		for k, v := range pplxSessionCache {
			if v.ts < oldestTs {
				oldestTs = v.ts
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

func parsePplxOpenAIMessages(messages []any) pplxParsedMessages {
	var systemMsg strings.Builder

	var history []pplxHistoryItem

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		if role == "" {
			role = "user"
		}

		if role == "developer" {
			role = "system"
		}

		content := ""
		switch c := msg["content"].(type) {
		case string:
			content = c
		case []any:
			var parts []string

			for _, p := range c {
				if pm, ok := p.(map[string]any); ok {
					if t, _ := pm["type"].(string); t == "text" {
						parts = append(parts, fmt.Sprint(pm["text"]))
					}
				}
			}

			content = strings.Join(parts, " ")
		}

		if strings.TrimSpace(content) == "" {
			continue
		}

		if role == "system" {
			systemMsg.WriteString(content)
			systemMsg.WriteString("\n")
		} else if role == "user" || role == "assistant" {
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

func formatPplxToolsHint(tools any) string {
	toolsArr, ok := tools.([]any)
	if !ok || len(toolsArr) == 0 {
		return ""
	}

	var lines []string

	for _, t := range toolsArr {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}

		fn, ok := tm["function"].(map[string]any)
		if !ok {
			fn = tm
		}

		name, _ := fn["name"].(string)
		if name == "" {
			name = "unnamed"
		}

		desc, _ := fn["description"].(string)
		if firstLine := strings.Split(desc, "\n")[0]; len(firstLine) > 200 {
			desc = firstLine[:200]
		} else {
			desc = firstLine
		}

		lines = append(lines, fmt.Sprintf("- %s: %s", name, desc))
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

	b, _ := json.Marshal(obj)

	s := string(b)
	if len(s) > 96000 {
		return s[len(s)-96000:]
	}

	return s
}

func buildPplxRequestBody(query, mode, modelPref, followUpUUID string) map[string]any {
	var followUpVal any = nil
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

func (e *PerplexityWebExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	messages, _ := m["messages"].([]any)
	if len(messages) == 0 {
		return jsonErr(400, "Missing or empty messages array", "invalid_request", ""), nil
	}

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

	parsed := parsePplxOpenAIMessages(messages)
	followUpUUID := pplxSessionLookup(parsed.History)

	query := buildPplxQuery(parsed, followUpUUID, m["tools"])
	if strings.TrimSpace(query) == "" {
		return jsonErr(400, "Empty query after processing", "invalid_request", ""), nil
	}

	reqPayload := buildPplxRequestBody(query, pplxMode, modelPref, followUpUUID)

	payloadBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, err
	}

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

	url := strings.TrimRight(cred.BaseURL, "/")
	if url == "" {
		url = pplxSSEEndpoint
	}

	res, err := e.DoPOST(ctx, url, h, payloadBytes)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		status := res.StatusCode
		msg := fmt.Sprintf("Perplexity returned HTTP %d", status)

		if status == 401 || status == 403 {
			msg = "Perplexity auth failed — session cookie may be expired. Re-paste your __Secure-next-auth.session-token."
		} else if status == 429 {
			msg = "Perplexity rate limited. Wait a moment and retry."
		}

		DrainBody(res.Body)

		return jsonErr(status, msg, "upstream_error", fmt.Sprintf("HTTP_%d", status)), nil
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

type pplxExtractedChunk struct {
	delta       string
	answer      string
	thinking    string
	backendUUID string
	errorMsg    string
	done        bool
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

		if errMsg, _ := event["error_message"].(string); errMsg != "" {
			out <- pplxExtractedChunk{
				delta:       "",
				answer:      "",
				thinking:    "",
				errorMsg:    errMsg,
				backendUUID: "",
				done:        true,
			}
			return
		}

		if bu, _ := event["backend_uuid"].(string); bu != "" {
			backendUUID = bu
		}

		blocks, _ := event["blocks"].([]any)
		for _, bRaw := range blocks {
			b, ok := bRaw.(map[string]any)
			if !ok {
				continue
			}

			usage, _ := b["intended_usage"].(string)

			if usage == "pro_search_steps" {
				if pb, ok := b["plan_block"].(map[string]any); ok {
					if steps, ok := pb["steps"].([]any); ok {
						for _, sRaw := range steps {
							if s, ok := sRaw.(map[string]any); ok {
								st, _ := s["step_type"].(string)
								if st == "SEARCH_WEB" {
									if swc, ok := s["search_web_content"].(map[string]any); ok {
										if queries, ok := swc["queries"].([]any); ok {
											for _, qRaw := range queries {
												if qm, ok := qRaw.(map[string]any); ok {
													if qr, _ := qm["query"].(string); qr != "" && !seenThinking[qr] {
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
											}
										}
									}
								}
							}
						}
					}
				}
			}

			if strings.Contains(usage, "markdown") {
				if mb, ok := b["markdown_block"].(map[string]any); ok {
					if chunks, ok := mb["chunks"].([]any); ok && len(chunks) > 0 {
						var sb strings.Builder
						for _, c := range chunks {
							sb.WriteString(fmt.Sprint(c))
						}

						chunkText := sb.String()
						prog, _ := mb["progress"].(string)

						if prog == "DONE" {
							fullAnswer = chunkText
							if len(fullAnswer) > seenLen {
								delta := fullAnswer[seenLen:]
								seenLen = len(fullAnswer)
								out <- pplxExtractedChunk{
									delta:       delta,
									answer:      fullAnswer,
									thinking:    "",
									errorMsg:    "",
									backendUUID: backendUUID,
									done:        false,
								}
							}
						} else {
							cumulative := fullAnswer + chunkText
							if len(cumulative) > seenLen {
								delta := cumulative[seenLen:]
								fullAnswer = cumulative
								seenLen = len(cumulative)
								out <- pplxExtractedChunk{
									delta:       delta,
									answer:      fullAnswer,
									thinking:    "",
									errorMsg:    "",
									backendUUID: backendUUID,
									done:        false,
								}
							}
						}
					}
				}
			}
		}

		if len(blocks) == 0 {
			if txt, _ := event["text"].(string); txt != "" {
				t := strings.TrimSpace(txt)
				if len(t) > seenLen {
					delta := t[seenLen:]
					fullAnswer = t
					seenLen = len(t)
					out <- pplxExtractedChunk{
						delta:       delta,
						answer:      fullAnswer,
						thinking:    "",
						errorMsg:    "",
						backendUUID: backendUUID,
						done:        false,
					}
				}
			}
		}

		if fin, _ := event["final"].(bool); fin {
			break
		}

		if st, _ := event["status"].(string); st == "COMPLETED" {
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

func wrapPplxStream(r io.ReadCloser, model, cid string, created int64, history []pplxHistoryItem, currentMsg string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer r.Close()
		defer pw.Close()

		writeSSE := func(obj map[string]any) {
			b, _ := json.Marshal(obj)
			_, _ = pw.Write([]byte("data: "))
			_, _ = pw.Write(b)
			_, _ = pw.Write([]byte("\n\n"))
		}

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"system_fingerprint": nil,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil, "logprobs": nil,
			}},
		})

		ch := make(chan pplxExtractedChunk, 16)
		go readPplxEvents(r, ch)

		var fullAnswer string

		var respBackendUUID string

		for chunk := range ch {
			if chunk.backendUUID != "" {
				respBackendUUID = chunk.backendUUID
			}

			if chunk.errorMsg != "" {
				writeSSE(map[string]any{
					"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
					"system_fingerprint": nil,
					"choices": []any{map[string]any{
						"index": 0, "delta": map[string]any{"content": "[Error: " + chunk.errorMsg + "]"},
						"finish_reason": nil, "logprobs": nil,
					}},
				})

				break
			}

			if chunk.thinking != "" {
				writeSSE(map[string]any{
					"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
					"system_fingerprint": nil,
					"choices": []any{map[string]any{
						"index": 0, "delta": map[string]any{"reasoning_content": chunk.thinking + "\n"},
						"finish_reason": nil, "logprobs": nil,
					}},
				})

				continue
			}

			if chunk.done {
				if chunk.answer != "" {
					fullAnswer = chunk.answer
				}

				break
			}

			if chunk.delta != "" {
				dt := cleanPplxResponse(chunk.delta, false)
				if dt != "" {
					writeSSE(map[string]any{
						"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
						"system_fingerprint": nil,
						"choices": []any{map[string]any{
							"index": 0, "delta": map[string]any{"content": dt},
							"finish_reason": nil, "logprobs": nil,
						}},
					})
				}
			}

			if chunk.answer != "" {
				fullAnswer = chunk.answer
			}
		}

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"system_fingerprint": nil,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop", "logprobs": nil,
			}},
		})

		_, _ = pw.Write([]byte("data: [DONE]\n\n"))

		pplxSessionStore(history, currentMsg, cleanPplxResponse(fullAnswer, true), respBackendUUID)
	}()

	return pr
}

func collectPplxNonStreaming(r io.ReadCloser, model, cid string, created int64, history []pplxHistoryItem, currentMsg string) ([]byte, error) {
	defer r.Close()

	ch := make(chan pplxExtractedChunk, 16)
	go readPplxEvents(r, ch)

	var fullAnswer string

	var respBackendUUID string

	var thinkingParts []string

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

		if chunk.done {
			if chunk.answer != "" {
				fullAnswer = chunk.answer
			}

			break
		}

		if chunk.answer != "" {
			fullAnswer = chunk.answer
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
	out := map[string]any{
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
	}

	return json.Marshal(out)
}

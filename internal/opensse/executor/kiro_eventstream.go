package executor

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// parseEventFrame parses one AWS EventStream frame (big-endian).
// Layout: totalLen(4) | headersLen(4) | preludeCRC(4) | headers | payload | messageCRC(4)
func parseEventFrame(data []byte) (headers map[string]string, payload map[string]any, ok bool) {
	if len(data) < 16 {
		return nil, nil, false
	}
	headersLen := binary.BigEndian.Uint32(data[4:8])
	headers = map[string]string{}
	offset := 12
	headerEnd := 12 + int(headersLen)
	if headerEnd > len(data) {
		return nil, nil, false
	}
	for offset < headerEnd && offset < len(data) {
		nameLen := int(data[offset])
		offset++
		if offset+nameLen > len(data) {
			break
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen
		if offset >= len(data) {
			break
		}
		headerType := data[offset]
		offset++
		if headerType == 7 { // string
			if offset+2 > len(data) {
				break
			}
			valueLen := int(data[offset])<<8 | int(data[offset+1])
			offset += 2
			if offset+valueLen > len(data) {
				break
			}
			headers[name] = string(data[offset : offset+valueLen])
			offset += valueLen
		} else {
			break
		}
	}
	payloadStart := 12 + int(headersLen)
	payloadEnd := len(data) - 4
	if payloadEnd > payloadStart {
		payloadStr := string(data[payloadStart:payloadEnd])
		if strings.TrimSpace(payloadStr) != "" {
			var p map[string]any
			if json.Unmarshal([]byte(payloadStr), &p) == nil {
				payload = p
			} else {
				payload = map[string]any{"raw": payloadStr}
			}
		}
	}
	return headers, payload, true
}

type kiroStreamState struct {
	endDetected         bool
	finishEmitted       bool
	hasToolCalls        bool
	hasReasoningContent bool
	reasoningChunkCount int
	toolCallIndex       int
	seenToolIds         map[string]int
	inThinking          bool
	totalContentLength  int
	contextUsagePct     float64
	hasContextUsage     bool
	hasMeteringEvent    bool
	usage               map[string]any
	chunkIndex          int
	responseId          string
	created             int64
	model               string
	contextWindow       int
}

func newKiroStreamState(model string) *kiroStreamState {
	return &kiroStreamState{
		seenToolIds:   map[string]int{},
		responseId:    fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		created:       time.Now().Unix(),
		model:         model,
		contextWindow: 200000,
	}
}

func (s *kiroStreamState) emitSSE(w io.Writer, chunk map[string]any) {
	j, _ := json.Marshal(chunk)
	w.Write([]byte("data: " + string(j) + "\n\n"))
}

func (s *kiroStreamState) processEvent(w io.Writer, eventType string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}

	// assistantResponseEvent
	if eventType == "assistantResponseEvent" {
		content, _ := payload["content"].(string)
		if content == "" {
			return
		}
		// strip leaked <thinking> blocks
		if s.inThinking {
			if strings.Contains(content, "</thinking>") {
				s.inThinking = false
				parts := strings.SplitN(content, "</thinking>", 2)
				content = parts[1]
				if strings.HasPrefix(content, "\n") {
					content = content[1:]
				}
			} else {
				content = ""
			}
		} else if strings.Contains(content, "<thinking>") {
			s.inThinking = true
			if strings.Contains(content, "</thinking>") {
				s.inThinking = false
				before := strings.SplitN(content, "<thinking>", 2)[0]
				after := ""
				if parts := strings.SplitN(content, "</thinking>", 2); len(parts) > 1 {
					after = parts[1]
					if strings.HasPrefix(after, "\n") {
						after = after[1:]
					}
				}
				content = before + after
			} else {
				content = strings.SplitN(content, "<thinking>", 2)[0]
			}
		}
		if content == "" && s.hasReasoningContent {
			return
		}
		s.totalContentLength += len(content)
		delta := map[string]any{"content": content}
		if s.chunkIndex == 0 {
			delta["role"] = "assistant"
		}
		s.emitSSE(w, map[string]any{
			"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
		})
		s.chunkIndex++
		return
	}

	// reasoningContentEvent
	if eventType == "reasoningContentEvent" {
		reasoningText := ""
		if t, ok := payload["text"].(string); ok {
			reasoningText = t
		} else if t, ok := payload["content"].(string); ok {
			reasoningText = t
		} else if nested, ok := payload["reasoningContentEvent"].(map[string]any); ok {
			if t, ok := nested["text"].(string); ok {
				reasoningText = t
			}
		}
		if reasoningText == "" {
			return
		}
		s.hasReasoningContent = true
		s.totalContentLength += len(reasoningText)
		delta := map[string]any{"reasoning_content": reasoningText}
		if s.reasoningChunkCount == 0 && s.chunkIndex == 0 {
			delta["role"] = "assistant"
		}
		s.emitSSE(w, map[string]any{
			"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
		})
		s.chunkIndex++
		s.reasoningChunkCount++
		return
	}

	// codeEvent
	if eventType == "codeEvent" {
		if content, ok := payload["content"].(string); ok && content != "" {
			s.emitSSE(w, map[string]any{
				"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil}},
			})
			s.chunkIndex++
		}
		return
	}

	// toolUseEvent
	if eventType == "toolUseEvent" {
		s.hasToolCalls = true
		tools := []any{payload}
		if arr, ok := payload["toolUses"].([]any); ok {
			tools = arr
		}
		// also accept payload itself as single tool when name present
		if _, hasName := payload["name"]; hasName {
			tools = []any{payload}
		}
		for _, tRaw := range tools {
			t, ok := tRaw.(map[string]any)
			if !ok {
				continue
			}
			toolCallId, _ := t["toolUseId"].(string)
			if toolCallId == "" {
				toolCallId = fmt.Sprintf("call_%d", time.Now().UnixNano())
			}
			toolName, _ := t["name"].(string)
			toolIndex, isNew := s.seenToolIds[toolCallId]
			if !isNew {
				toolIndex = s.toolCallIndex
				s.toolCallIndex++
				s.seenToolIds[toolCallId] = toolIndex
				delta := map[string]any{
					"tool_calls": []any{map[string]any{
						"index": toolIndex, "id": toolCallId, "type": "function",
						"function": map[string]any{"name": toolName, "arguments": ""},
					}},
				}
				if s.chunkIndex == 0 {
					delta["role"] = "assistant"
				}
				s.emitSSE(w, map[string]any{
					"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
					"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
				})
				s.chunkIndex++
			}
			if input, has := t["input"]; has {
				var argsStr string
				switch v := input.(type) {
				case string:
					argsStr = v
				default:
					b, _ := json.Marshal(v)
					argsStr = string(b)
				}
				s.emitSSE(w, map[string]any{
					"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
					"choices": []any{map[string]any{
						"index": 0,
						"delta": map[string]any{
							"tool_calls": []any{map[string]any{
								"index": toolIndex,
								"function": map[string]any{"arguments": argsStr},
							}},
						},
						"finish_reason": nil,
					}},
				})
				s.chunkIndex++
			}
		}
		return
	}

	// messageStopEvent
	if eventType == "messageStopEvent" {
		fr := "stop"
		if s.hasToolCalls {
			fr = "tool_calls"
		}
		s.finishEmitted = true
		s.emitSSE(w, map[string]any{
			"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
		})
		return
	}

	if eventType == "contextUsageEvent" {
		if pct, ok := payload["contextUsagePercentage"].(float64); ok {
			s.contextUsagePct = pct
			s.hasContextUsage = true
		}
		return
	}
	if eventType == "meteringEvent" {
		s.hasMeteringEvent = true
		return
	}
	if eventType == "metricsEvent" {
		metrics := payload
		if nested, ok := payload["metricsEvent"].(map[string]any); ok {
			metrics = nested
		}
		inT := nIntAny(metrics["inputTokens"])
		outT := nIntAny(metrics["outputTokens"])
		cached := nIntAny(metrics["cacheReadInputTokens"])
		if cached == 0 {
			cached = nIntAny(metrics["cache_read_input_tokens"])
		}
		cacheCreate := nIntAny(metrics["cacheCreationInputTokens"])
		if cacheCreate == 0 {
			cacheCreate = nIntAny(metrics["cache_creation_input_tokens"])
		}
		if inT > 0 || outT > 0 {
			s.usage = map[string]any{
				"prompt_tokens":     inT,
				"completion_tokens": outT,
				"total_tokens":      inT + outT,
			}
			if cached > 0 {
				s.usage["cache_read_input_tokens"] = cached
			}
			if cacheCreate > 0 {
				s.usage["cache_creation_input_tokens"] = cacheCreate
			}
		}
	}

	// final after metering + context
	if s.hasMeteringEvent && s.hasContextUsage && !s.finishEmitted {
		s.finishEmitted = true
		if s.usage == nil {
			estOut := 0
			if s.totalContentLength > 0 {
				estOut = s.totalContentLength / 4
				if estOut < 1 {
					estOut = 1
				}
			}
			estIn := 0
			if s.contextUsagePct > 0 {
				estIn = int(s.contextUsagePct * float64(s.contextWindow) / 100)
			}
			s.usage = map[string]any{
				"prompt_tokens": estIn, "completion_tokens": estOut, "total_tokens": estIn + estOut,
			}
		}
		fr := "stop"
		if s.hasToolCalls {
			fr = "tool_calls"
		}
		chunk := map[string]any{
			"id": s.responseId, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
			"usage":   s.usage,
		}
		s.emitSSE(w, chunk)
	}
}

func nIntAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

// transformKiroEventStream reads AWS EventStream body and writes OpenAI SSE.
func transformKiroEventStream(src io.Reader, model string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		state := newKiroStreamState(model)
		buf := make([]byte, 0, 64*1024)
		tmp := make([]byte, 32*1024)
		for {
			n, err := src.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				chunksBefore := state.chunkIndex
				for len(buf) >= 16 {
					totalLength := int(binary.BigEndian.Uint32(buf[0:4]))
					if totalLength < 16 || totalLength > len(buf) {
						break
					}
					frame := buf[:totalLength]
					buf = buf[totalLength:]
					headers, payload, ok := parseEventFrame(frame)
					if !ok {
						continue
					}
					eventType := headers[":event-type"]
					state.processEvent(pw, eventType, payload)
				}
				if state.chunkIndex == chunksBefore && !state.finishEmitted {
					pw.Write([]byte(": ka\n\n"))
				}
			}
			if err != nil {
				break
			}
		}
		if !state.finishEmitted {
			fr := "stop"
			if state.hasToolCalls {
				fr = "tool_calls"
			}
			state.emitSSE(pw, map[string]any{
				"id": state.responseId, "object": "chat.completion.chunk", "created": state.created, "model": state.model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
			})
		}
		pw.Write([]byte("data: [DONE]\n\n"))
	}()
	return pr
}

// wrapKiroResult transforms successful EventStream response to SSE Result.
func wrapKiroResult(res *Result, model string) *Result {
	if res == nil || res.StatusCode >= 400 || res.Body == nil {
		return res
	}
	// If already SSE text, pass through
	ct := res.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") || strings.Contains(ct, "json") {
		// peek: if starts with '{' or 'd' for data: might already be text
		br := bufio.NewReader(res.Body)
		peek, _ := br.Peek(1)
		if len(peek) > 0 && (peek[0] == 'd' || peek[0] == ':' || peek[0] == '{') {
			res.Body = io.NopCloser(br)
			return res
		}
		res.Body = io.NopCloser(io.MultiReader(br, res.Body))
	}
	transformed := transformKiroEventStream(res.Body, model)
	h := res.Header.Clone()
	if h == nil {
		h = make(http.Header)
	}
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	return &Result{StatusCode: res.StatusCode, Header: h, Body: transformed}
}

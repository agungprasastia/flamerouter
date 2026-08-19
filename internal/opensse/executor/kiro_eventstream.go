package executor

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Layout: totalLen(4) | headersLen(4) | preludeCRC(4) | headers | payload | messageCRC(4).
func parseEventFrame(data []byte) (headers map[string]string, payload map[string]any, ok bool) {
	if len(data) < 16 {
		return nil, nil, false
	}

	headersLen := binary.BigEndian.Uint32(data[4:8])
	if headersLen > math.MaxInt32-12 || int(headersLen) > len(data) { // #nosec G115
		return nil, nil, false
	}

	hLenInt := int(headersLen) // #nosec G115
	headerEnd := 12 + hLenInt

	if headerEnd > len(data) {
		return nil, nil, false
	}

	headers = parseEventHeaders(data[12:headerEnd])
	payload = parseEventPayload(data, headerEnd)

	return headers, payload, true
}

func parseEventHeaders(data []byte) map[string]string {
	headers := make(map[string]string)
	offset := 0

	for offset < len(data) {
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

		if headerType != 7 || offset+2 > len(data) { // 7 == string
			break
		}

		valueLen := int(data[offset])<<8 | int(data[offset+1])
		offset += 2

		if offset+valueLen > len(data) {
			break
		}

		headers[name] = string(data[offset : offset+valueLen])
		offset += valueLen
	}

	return headers
}

func parseEventPayload(data []byte, payloadStart int) map[string]any {
	payloadEnd := len(data) - 4
	if payloadEnd <= payloadStart {
		return nil
	}

	payloadStr := string(data[payloadStart:payloadEnd])
	if strings.TrimSpace(payloadStr) == "" {
		return nil
	}

	var p map[string]any
	if json.Unmarshal([]byte(payloadStr), &p) == nil {
		return p
	}

	return map[string]any{"raw": payloadStr}
}

type kiroStreamState struct {
	seenToolIds         map[string]int
	usage               map[string]any
	model               string
	responseID          string
	reasoningChunkCount int
	toolCallIndex       int
	contextWindow       int
	created             int64
	totalContentLength  int
	contextUsagePct     float64
	chunkIndex          int
	hasMeteringEvent    bool
	hasReasoningContent bool
	hasContextUsage     bool
	hasToolCalls        bool
	inThinking          bool
	finishEmitted       bool
}

func newKiroStreamState(model string) *kiroStreamState {
	return &kiroStreamState{
		seenToolIds:         make(map[string]int),
		usage:               nil,
		responseID:          fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		created:             time.Now().Unix(),
		model:               model,
		reasoningChunkCount: 0,
		toolCallIndex:       0,
		contextWindow:       200000,
		totalContentLength:  0,
		contextUsagePct:     0,
		chunkIndex:          0,
		hasMeteringEvent:    false,
		hasReasoningContent: false,
		hasContextUsage:     false,
		hasToolCalls:        false,
		inThinking:          false,
		finishEmitted:       false,
	}
}

func (s *kiroStreamState) emitSSE(w io.Writer, chunk map[string]any) {
	j, err := json.Marshal(chunk)
	if err != nil {
		return
	}

	_, writeErr := w.Write([]byte("data: " + string(j) + "\n\n"))
	if writeErr != nil {
		_ = writeErr
	}
}

func (s *kiroStreamState) stripThinkingTags(content string) string {
	if s.inThinking {
		if !strings.Contains(content, "</thinking>") {
			return ""
		}

		s.inThinking = false
		parts := strings.SplitN(content, "</thinking>", 2)

		if len(parts) > 1 {
			return strings.TrimPrefix(parts[1], "\n")
		}

		return ""
	}

	if !strings.Contains(content, "<thinking>") {
		return content
	}

	s.inThinking = true
	beforeParts := strings.SplitN(content, "<thinking>", 2)

	var before string
	if len(beforeParts) > 0 {
		before = beforeParts[0]
	}

	if !strings.Contains(content, "</thinking>") {
		return before
	}

	s.inThinking = false
	afterParts := strings.SplitN(content, "</thinking>", 2)

	after := ""
	if len(afterParts) > 1 {
		after = strings.TrimPrefix(afterParts[1], "\n")
	}

	return before + after
}

func (s *kiroStreamState) processAssistantResponse(w io.Writer, payload map[string]any) {
	content, okContent := payload["content"].(string)
	if !okContent || content == "" {
		return
	}

	content = s.stripThinkingTags(content)
	if content == "" && s.hasReasoningContent {
		return
	}

	s.totalContentLength += len(content)
	delta := map[string]any{"content": content}

	if s.chunkIndex == 0 {
		delta["role"] = "assistant"
	}

	s.emitSSE(w, map[string]any{
		"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
	})

	s.chunkIndex++
}

func (s *kiroStreamState) processReasoningContent(w io.Writer, payload map[string]any) {
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
		"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
	})

	s.chunkIndex++
	s.reasoningChunkCount++
}

func (s *kiroStreamState) processCodeEvent(w io.Writer, payload map[string]any) {
	if content, ok := payload["content"].(string); ok && content != "" {
		s.emitSSE(w, map[string]any{
			"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil}},
		})

		s.chunkIndex++
	}
}

func (s *kiroStreamState) emitToolCallDecl(w io.Writer, toolIndex int, toolCallID, toolName string) {
	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": toolIndex, "id": toolCallID, "type": "function",
			"function": map[string]any{"name": toolName, "arguments": ""},
		}},
	}

	if s.chunkIndex == 0 {
		delta["role"] = "assistant"
	}

	s.emitSSE(w, map[string]any{
		"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}},
	})

	s.chunkIndex++
}

func (s *kiroStreamState) emitToolCallInput(w io.Writer, toolIndex int, input any) {
	var argsStr string
	switch v := input.(type) {
	case string:
		argsStr = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			argsStr = "{}"
		} else {
			argsStr = string(b)
		}
	}

	s.emitSSE(w, map[string]any{
		"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    toolIndex,
					"function": map[string]any{"arguments": argsStr},
				}},
			},
			"finish_reason": nil,
		}},
	})

	s.chunkIndex++
}

func (s *kiroStreamState) emitSingleToolCall(w io.Writer, t map[string]any) {
	toolCallID, _ := t["toolUseId"].(string) // nolint:errcheck
	if toolCallID == "" {
		toolCallID = fmt.Sprintf("call_%d", time.Now().UnixNano())
	}

	toolName, _ := t["name"].(string) // nolint:errcheck
	toolIndex, isOld := s.seenToolIds[toolCallID]

	if !isOld {
		toolIndex = s.toolCallIndex
		s.toolCallIndex++
		s.seenToolIds[toolCallID] = toolIndex
		s.emitToolCallDecl(w, toolIndex, toolCallID, toolName)
	}

	if input, has := t["input"]; has {
		s.emitToolCallInput(w, toolIndex, input)
	}
}

func (s *kiroStreamState) processToolUse(w io.Writer, payload map[string]any) {
	s.hasToolCalls = true

	tools := []any{payload}
	if arr, ok := payload["toolUses"].([]any); ok {
		tools = arr
	}

	if _, hasName := payload["name"]; hasName {
		tools = []any{payload}
	}

	for _, tRaw := range tools {
		if t, ok := tRaw.(map[string]any); ok {
			s.emitSingleToolCall(w, t)
		}
	}
}

func (s *kiroStreamState) processMetrics(payload map[string]any) {
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

func (s *kiroStreamState) calculateEstimatedUsage() map[string]any {
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

	return map[string]any{
		"prompt_tokens": estIn, "completion_tokens": estOut, "total_tokens": estIn + estOut,
	}
}

func (s *kiroStreamState) emitFinalMetering(w io.Writer) {
	if !s.hasMeteringEvent || !s.hasContextUsage || s.finishEmitted {
		return
	}

	s.finishEmitted = true

	if s.usage == nil {
		s.usage = s.calculateEstimatedUsage()
	}

	fr := "stop"
	if s.hasToolCalls {
		fr = "tool_calls"
	}

	s.emitSSE(w, map[string]any{
		"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
		"usage":   s.usage,
	})
}

func (s *kiroStreamState) processControlEvent(w io.Writer, eventType string, payload map[string]any) {
	switch eventType {
	case "messageStopEvent":
		fr := "stop"
		if s.hasToolCalls {
			fr = "tool_calls"
		}

		s.finishEmitted = true

		s.emitSSE(w, map[string]any{
			"id": s.responseID, "object": "chat.completion.chunk", "created": s.created, "model": s.model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
		})
	case "contextUsageEvent":
		if pct, ok := payload["contextUsagePercentage"].(float64); ok {
			s.contextUsagePct = pct
			s.hasContextUsage = true
		}
	case "meteringEvent":
		s.hasMeteringEvent = true
	case "metricsEvent":
		s.processMetrics(payload)
	}
}

func (s *kiroStreamState) processEvent(w io.Writer, eventType string, payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any)
	}

	switch eventType {
	case "assistantResponseEvent":
		s.processAssistantResponse(w, payload)
	case "reasoningContentEvent":
		s.processReasoningContent(w, payload)
	case "codeEvent":
		s.processCodeEvent(w, payload)
	case "toolUseEvent":
		s.processToolUse(w, payload)
	default:
		s.processControlEvent(w, eventType, payload)
	}

	s.emitFinalMetering(w)
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

func processFramesFromBuffer(buf []byte, pw *io.PipeWriter, state *kiroStreamState) []byte {
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

	return buf
}

func streamKiroFrames(src io.Reader, pw *io.PipeWriter, state *kiroStreamState) {
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)

	for {
		n, err := src.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			chunksBefore := state.chunkIndex

			buf = processFramesFromBuffer(buf, pw, state)

			if state.chunkIndex == chunksBefore && !state.finishEmitted {
				_, writeErr := pw.Write([]byte(": ka\n\n"))
				if writeErr != nil {
					_ = writeErr
				}
			}
		}

		if err != nil {
			break
		}
	}
}

// transformKiroEventStream reads AWS EventStream body and writes OpenAI SSE.
func transformKiroEventStream(src io.Reader, model string) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		defer func() {
			if closer, ok := src.(io.Closer); ok {
				_ = closer.Close() //nolint:errcheck // best-effort close on stream exit
			}

			if err := pw.Close(); err != nil {
				_ = err
			}
		}()

		state := newKiroStreamState(model)
		streamKiroFrames(src, pw, state)

		if !state.finishEmitted {
			fr := "stop"
			if state.hasToolCalls {
				fr = "tool_calls"
			}

			state.emitSSE(pw, map[string]any{
				"id": state.responseID, "object": "chat.completion.chunk", "created": state.created, "model": state.model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": fr}},
			})
		}

		if _, err := pw.Write([]byte("data: [DONE]\n\n")); err != nil {
			_ = err
		}
	}()

	return pr
}

func shouldKeepOriginalBody(res *Result) bool {
	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") && !strings.Contains(ct, "json") {
		return false
	}

	br := bufio.NewReader(res.Body)

	peek, err := br.Peek(1)
	if err == nil && len(peek) > 0 && (peek[0] == 'd' || peek[0] == ':' || peek[0] == '{') {
		res.Body = io.NopCloser(br)
		return true
	}

	res.Body = io.NopCloser(io.MultiReader(br, res.Body))

	return false
}

// wrapKiroResult transforms successful EventStream response to SSE Result.
func wrapKiroResult(res *Result, model string) *Result {
	if res == nil || res.StatusCode >= 400 || res.Body == nil {
		return res
	}

	if shouldKeepOriginalBody(res) {
		return res
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

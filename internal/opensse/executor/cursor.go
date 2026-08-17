package executor

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

func init() {
	RegisterSpecialized("cursor", &CursorExecutor{
		Base: Base{
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
			Provider: "cursor",
			BaseURL:  "https://api2.cursor.sh",
		},
	})
	RegisterSpecialized("cu", &CursorExecutor{
		Base: Base{
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
			Provider: "cursor",
			BaseURL:  "https://api2.cursor.sh",
		},
	})
}

// CursorExecutor handles ConnectRPC protobuf requests to Cursor API with checksum.
type CursorExecutor struct {
	Base
}

// BuildCursorHeaders creates full authentication headers for Cursor API.
func BuildCursorHeaders(accessToken, machineID string, ghostMode bool) http.Header {
	cleanToken := accessToken
	if strings.Contains(cleanToken, "::") {
		parts := strings.Split(cleanToken, "::")
		if len(parts) > 1 {
			cleanToken = parts[1]
		}
	}

	effectiveMachineID := machineID
	if effectiveMachineID == "" {
		h := sha256.Sum256([]byte(cleanToken + "machineId"))
		effectiveMachineID = hex.EncodeToString(h[:])
	}

	clientKeyHash := sha256.Sum256([]byte(cleanToken))
	clientKey := hex.EncodeToString(clientKeyHash[:])

	checksum := GenerateCursorChecksum(effectiveMachineID)
	reqID := randomUUID()
	sessID := randomUUID()

	h := make(http.Header)
	h.Set("authorization", "Bearer "+cleanToken)
	h.Set("connect-accept-encoding", "gzip")
	h.Set("connect-protocol-version", "1")
	h.Set("content-type", "application/connect+proto")
	h.Set("user-agent", "connect-es/1.6.1")
	h.Set("x-amzn-trace-id", "Root="+randomUUID())
	h.Set("x-client-key", clientKey)
	h.Set("x-cursor-checksum", checksum)
	h.Set("x-cursor-client-version", "3.12.17")
	h.Set("x-cursor-client-commit", "0fb762053c34788bb7760d5673f8a6d4c8589d50")
	h.Set("x-cursor-client-type", "ide")
	h.Set("x-cursor-client-os", "linux")
	h.Set("x-cursor-client-arch", "x64")
	h.Set("x-cursor-client-device-type", "desktop")
	h.Set("x-cursor-config-version", randomUUID())
	h.Set("x-cursor-timezone", "UTC")

	if ghostMode {
		h.Set("x-ghost-mode", "true")
	} else {
		h.Set("x-ghost-mode", "false")
	}

	h.Set("x-request-id", reqID)
	h.Set("x-session-id", sessID)

	return h
}

// WrapConnectRPCFrame wraps payload in a 5-byte ConnectRPC envelope [flag, 4-byte BE length, data].
func WrapConnectRPCFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0x00 // uncompressed
	// #nosec G115
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)

	return frame
}

func extractMessageContentString(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string

		for _, p := range c {
			if pm, ok := p.(map[string]any); ok {
				if t, okType := pm["type"].(string); okType && t == "text" {
					parts = append(parts, fmt.Sprint(pm["text"]))
				}
			}
		}

		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func encodeProtoMessage(msg map[string]any) []byte {
	roleStr, _ := msg["role"].(string) // nolint:errcheck
	contentStr := extractMessageContentString(msg["content"])

	var roleVal uint64 = 1 // USER
	if roleStr == "assistant" {
		roleVal = 2 // ASSISTANT
	}

	var msgBuf bytes.Buffer
	// Field 1: content (string)
	if contentStr != "" {
		writeProtoField(&msgBuf, 1, []byte(contentStr))
	}
	// Field 2: role (varint)
	writeProtoVarintField(&msgBuf, 2, roleVal)
	// Field 13: message_id (string)
	writeProtoField(&msgBuf, 13, []byte(randomUUID()))

	return msgBuf.Bytes()
}

// BuildCursorProtobufRequest builds the protobuf wire payload for Cursor chat.
func BuildCursorProtobufRequest(messages []any, model string) []byte {
	var bodyBuf bytes.Buffer

	// Conversation messages
	for _, mRaw := range messages {
		msg, ok := mRaw.(map[string]any)
		if !ok {
			continue
		}

		encodedMsg := encodeProtoMessage(msg)
		// Write ConversationMessage to bodyBuf under Field 1 (repeated)
		writeProtoField(&bodyBuf, 1, encodedMsg)
	}

	// Field 5: Model
	var modelBuf bytes.Buffer

	writeProtoField(&modelBuf, 1, []byte(model))
	writeProtoField(&bodyBuf, 5, modelBuf.Bytes())

	// Field 23: Conversation ID
	writeProtoField(&bodyBuf, 23, []byte(randomUUID()))

	// Top level request: Field 1 (StreamUnifiedChatRequest)
	var topBuf bytes.Buffer

	writeProtoField(&topBuf, 1, bodyBuf.Bytes())

	return topBuf.Bytes()
}

func writeProtoField(w *bytes.Buffer, fieldNum uint32, data []byte) {
	tag := (uint64(fieldNum) << 3) | 2 // length-delimited wire type 2

	var tagBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tagBuf[:], tag)
	_, _ = w.Write(tagBuf[:n])

	var lenBuf [binary.MaxVarintLen64]byte
	ln := binary.PutUvarint(lenBuf[:], uint64(len(data)))
	_, _ = w.Write(lenBuf[:ln])
	_, _ = w.Write(data)
}

func writeProtoVarintField(w *bytes.Buffer, fieldNum uint32, val uint64) {
	tag := uint64(fieldNum) << 3

	var tagBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tagBuf[:], tag)
	_, _ = w.Write(tagBuf[:n])

	var valBuf [binary.MaxVarintLen64]byte
	vn := binary.PutUvarint(valBuf[:], val)
	_, _ = w.Write(valBuf[:vn])
}

func decompressCursorPayload(payload []byte) []byte {
	gr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return payload
	}

	defer func() {
		_ = gr.Close() // nolint:errcheck
	}()

	decompressed, errRead := io.ReadAll(gr)
	if errRead == nil {
		return decompressed
	}

	return payload
}

// ExtractTextFromCursorResponse decodes streamed or unary ConnectRPC frames into text/thinking deltas.
func ExtractTextFromCursorResponse(data []byte) (text string, thinking string) {
	pos := 0
	for pos < len(data) {
		if pos+5 > len(data) {
			break
		}

		flag := data[pos]
		length := binary.BigEndian.Uint32(data[pos+1 : pos+5])

		frameEnd := pos + 5 + int(length)
		if frameEnd > len(data) {
			break
		}

		payload := data[pos+5 : frameEnd]
		pos = frameEnd

		if flag == 0x01 { // gzip
			payload = decompressCursorPayload(payload)
		}

		t, th := parseCursorProtoPayload(payload)
		text += t
		thinking += th
	}

	return text, thinking
}

func parseCursorProtoPayload(payload []byte) (text string, thinking string) {
	p := 0
	for p < len(payload) {
		tag, n := binary.Uvarint(payload[p:])
		if n <= 0 {
			break
		}

		p += n
		fieldNum, wireType := tag>>3, tag&0x07

		if wireType == 0 {
			_, vn := binary.Uvarint(payload[p:])
			if vn <= 0 {
				return text, thinking
			}

			p += vn

			continue
		}

		if wireType != 2 {
			return text, thinking
		}

		length, ln := binary.Uvarint(payload[p:])
		if ln <= 0 || length > math.MaxInt {
			return text, thinking
		}

		p += ln
		end := p + int(length) // nolint:gosec

		if end > len(payload) {
			return text, thinking
		}

		if fieldNum == 2 {
			subT, subTh := parseStreamUnifiedChatResponse(payload[p:end])
			text += subT
			thinking += subTh
		}

		p = end
	}

	return text, thinking
}

func parseStreamUnifiedChatResponse(payload []byte) (text string, thinking string) {
	p := 0
	for p < len(payload) {
		tag, n := binary.Uvarint(payload[p:])
		if n <= 0 {
			break
		}

		p += n
		fieldNum := tag >> 3
		wireType := tag & 0x07

		switch wireType {
		case 0:
			_, vn := binary.Uvarint(payload[p:])
			if vn <= 0 {
				return
			}

			p += vn
		case 2:
			tDelta, thDelta, newP, ok := handleUnifiedChatField(payload, p, fieldNum)
			if !ok {
				return
			}

			text += tDelta
			thinking += thDelta
			p = newP
		default:
			return
		}
	}

	return text, thinking
}

func handleUnifiedChatField(payload []byte, p int, fieldNum uint64) (string, string, int, bool) {
	length, ln := binary.Uvarint(payload[p:])
	if ln <= 0 || length > math.MaxInt {
		return "", "", 0, false
	}

	p += ln

	end := p + int(length) // nolint:gosec
	if end > len(payload) {
		return "", "", 0, false
	}

	fieldBytes := payload[p:end]

	var (
		text     string
		thinking string
	)

	switch fieldNum {
	case 1:
		text = string(fieldBytes)
	case 25:
		thinking = parseCursorThinkingField(fieldBytes)
	}

	return text, thinking, end, true
}

func parseCursorThinkingField(payload []byte) string {
	p := 0
	for p < len(payload) {
		tag, n := binary.Uvarint(payload[p:])
		if n <= 0 {
			break
		}

		p += n
		fieldNum, wireType := tag>>3, tag&0x07

		if wireType == 0 {
			_, vn := binary.Uvarint(payload[p:])
			if vn <= 0 {
				return ""
			}

			p += vn

			continue
		}

		if wireType != 2 {
			return ""
		}

		length, ln := binary.Uvarint(payload[p:])
		if ln <= 0 || length > math.MaxInt {
			return ""
		}

		p += ln
		end := p + int(length) // nolint:gosec

		if end > len(payload) {
			return ""
		}

		if fieldNum == 1 {
			return string(payload[p:end])
		}

		p = end
	}

	return ""
}

func (e *CursorExecutor) executeOpenAICompatible(ctx context.Context, base string, cred Credentials, model string, m map[string]any, stream bool) (*Result, error) {
	m["model"] = model
	m["stream"] = stream
	payload, _ := json.Marshal(m) // nolint:errcheck
	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	if stream {
		h.Set("Accept", "text/event-stream")
	}

	return e.DoPOST(ctx, base, h, payload)
}

func (e *CursorExecutor) executeUnaryConnectRPC(res *Result, model, cid string, created int64) (*Result, error) {
	defer func() {
		_ = res.Body.Close() // nolint:errcheck
	}()

	allBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	text, thinking := ExtractTextFromCursorResponse(allBytes)

	msg := map[string]any{
		"role":    "assistant",
		"content": text,
	}
	if thinking != "" {
		msg["reasoning_content"] = thinking
	}

	out := map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": "stop",
		}},
	}

	outBytes, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}

	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(outBytes)),
	}, nil
}

func (e *CursorExecutor) executeConnectRPC(ctx context.Context, base string, cred Credentials, model string, m map[string]any, stream bool) (*Result, error) {
	messages, _ := m["messages"].([]any) // nolint:errcheck
	protoPayload := BuildCursorProtobufRequest(messages, model)
	framedPayload := WrapConnectRPCFrame(protoPayload)

	token := cred.AccessToken
	if token == "" {
		token = cred.APIKey
	}

	machineID := ""
	if cred.ProviderSpecificData != nil {
		if mid, ok := cred.ProviderSpecificData["machineId"].(string); ok {
			machineID = mid
		}
	}

	headers := BuildCursorHeaders(token, machineID, true)
	url := base + "/aiserver.v1.AiService/StreamUnifiedChatWithTools"

	reqRes, err := e.DoPOST(ctx, url, headers, framedPayload)
	if err != nil {
		return nil, err
	}

	if reqRes.StatusCode < 200 || reqRes.StatusCode >= 300 {
		return reqRes, nil
	}

	cid := fmt.Sprintf("chatcmpl-cursor-%d", time.Now().UnixMilli())
	created := time.Now().Unix()

	if stream {
		sseBody := wrapCursorStream(reqRes.Body, model, cid, created)

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

	return e.executeUnaryConnectRPC(reqRes, model, cid, created)
}

// Execute executes Cursor requests using ConnectRPC or fallback direct API.
func (e *CursorExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" {
		base = e.BaseURL
	}

	// If OpenAI-compatible proxy path is provided, use json direct forwarding
	if strings.Contains(base, "/chat/completions") || strings.Contains(base, "/v1") {
		return e.executeOpenAICompatible(ctx, base, cred, model, m, stream)
	}

	return e.executeConnectRPC(ctx, base, cred, model, m, stream)
}

func emitCursorSSEDelta(pw *io.PipeWriter, cid, model string, created int64, chunk []byte) {
	text, thinking := ExtractTextFromCursorResponse(chunk)
	if thinking != "" {
		writeCursorSSE(pw, map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"reasoning_content": thinking}, "finish_reason": nil,
			}},
		})
	}

	if text != "" {
		writeCursorSSE(pw, map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil,
			}},
		})
	}
}

func writeCursorSSE(pw *io.PipeWriter, obj map[string]any) {
	b, _ := json.Marshal(obj) // nolint:errcheck

	_, _ = pw.Write([]byte("data: ")) // nolint:errcheck
	_, _ = pw.Write(b)                // nolint:errcheck
	_, _ = pw.Write([]byte("\n\n"))   // nolint:errcheck
}

func pumpCursorStream(r io.ReadCloser, pw *io.PipeWriter, model, cid string, created int64) {
	defer func() {
		_ = r.Close()  // nolint:errcheck
		_ = pw.Close() // nolint:errcheck
	}()

	writeCursorSSE(pw, map[string]any{
		"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil,
		}},
	})

	buf := make([]byte, 64*1024)

	for {
		n, err := r.Read(buf)
		if n > 0 {
			emitCursorSSEDelta(pw, cid, model, created, buf[:n])
		}

		if err != nil {
			break
		}
	}

	writeCursorSSE(pw, map[string]any{
		"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
		}},
	})

	_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck
}

func wrapCursorStream(r io.ReadCloser, model, cid string, created int64) io.ReadCloser {
	pr, pw := io.Pipe()
	go pumpCursorStream(r, pw, model, cid, created)

	return pr
}

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
	"net/http"
	"strings"
	"time"
)

func init() {
	RegisterSpecialized("cursor", &CursorExecutor{Base: Base{Provider: "cursor", BaseURL: "https://api2.cursor.sh"}})
	RegisterSpecialized("cu", &CursorExecutor{Base: Base{Provider: "cursor", BaseURL: "https://api2.cursor.sh"}})
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
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
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
		roleStr, _ := msg["role"].(string)
		contentStr := ""
		switch c := msg["content"].(type) {
		case string:
			contentStr = c
		case []any:
			var parts []string
			for _, p := range c {
				if pm, ok := p.(map[string]any); ok {
					if t, _ := pm["type"].(string); t == "text" {
						parts = append(parts, fmt.Sprint(pm["text"]))
					}
				}
			}
			contentStr = strings.Join(parts, " ")
		}

		var roleVal uint64 = 1 // USER
		if roleStr == "assistant" {
			roleVal = 2 // ASSISTANT
		}

		var msgBuf bytes.Buffer
		// Field 1: content (string)
		if contentStr != "" {
			writeProtoField(&msgBuf, 1, 2, []byte(contentStr))
		}
		// Field 2: role (varint)
		writeProtoVarintField(&msgBuf, 2, roleVal)
		// Field 13: message_id (string)
		writeProtoField(&msgBuf, 13, 2, []byte(randomUUID()))

		// Write ConversationMessage to bodyBuf under Field 1 (repeated)
		writeProtoField(&bodyBuf, 1, 2, msgBuf.Bytes())
	}

	// Field 5: Model
	var modelBuf bytes.Buffer
	writeProtoField(&modelBuf, 1, 2, []byte(model))
	writeProtoField(&bodyBuf, 5, 2, modelBuf.Bytes())

	// Field 23: Conversation ID
	writeProtoField(&bodyBuf, 23, 2, []byte(randomUUID()))

	// Top level request: Field 1 (StreamUnifiedChatRequest)
	var topBuf bytes.Buffer
	writeProtoField(&topBuf, 1, 2, bodyBuf.Bytes())
	return topBuf.Bytes()
}

func writeProtoField(w *bytes.Buffer, fieldNum int, wireType int, data []byte) {
	tag := uint64((fieldNum << 3) | wireType)
	var tagBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tagBuf[:], tag)
	w.Write(tagBuf[:n])

	if wireType == 2 { // length-delimited
		var lenBuf [binary.MaxVarintLen64]byte
		ln := binary.PutUvarint(lenBuf[:], uint64(len(data)))
		w.Write(lenBuf[:ln])
		w.Write(data)
	}
}

func writeProtoVarintField(w *bytes.Buffer, fieldNum int, val uint64) {
	tag := uint64((fieldNum << 3) | 0)
	var tagBuf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tagBuf[:], tag)
	w.Write(tagBuf[:n])

	var valBuf [binary.MaxVarintLen64]byte
	vn := binary.PutUvarint(valBuf[:], val)
	w.Write(valBuf[:vn])
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
			gr, err := gzip.NewReader(bytes.NewReader(payload))
			if err == nil {
				decompressed, _ := io.ReadAll(gr)
				gr.Close()
				payload = decompressed
			}
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
			length, ln := binary.Uvarint(payload[p:])
			if ln <= 0 {
				return
			}
			p += ln
			end := p + int(length)
			if end > len(payload) {
				return
			}
			fieldBytes := payload[p:end]
			p = end

			// Field 2 of top response: StreamUnifiedChatResponse
			if fieldNum == 2 {
				subT, subTh := parseStreamUnifiedChatResponse(fieldBytes)
				text += subT
				thinking += subTh
			}
		default:
			return
		}
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
			length, ln := binary.Uvarint(payload[p:])
			if ln <= 0 {
				return
			}
			p += ln
			end := p + int(length)
			if end > len(payload) {
				return
			}
			fieldBytes := payload[p:end]
			p = end

			// Field 1: response text
			if fieldNum == 1 {
				text += string(fieldBytes)
			}
			// Field 25: thinking object -> Field 1 text
			if fieldNum == 25 {
				thinking += parseCursorThinkingField(fieldBytes)
			}
		default:
			return
		}
	}
	return text, thinking
}

func parseCursorThinkingField(payload []byte) string {
	p := 0
	for p < len(payload) {
		tag, n := binary.Uvarint(payload[p:])
		if n <= 0 {
			break
		}
		p += n
		fieldNum := tag >> 3
		wireType := tag & 0x07
		if wireType == 2 {
			length, ln := binary.Uvarint(payload[p:])
			if ln <= 0 {
				break
			}
			p += ln
			end := p + int(length)
			if end > len(payload) {
				break
			}
			if fieldNum == 1 {
				return string(payload[p:end])
			}
			p = end
		} else if wireType == 0 {
			_, vn := binary.Uvarint(payload[p:])
			if vn <= 0 {
				break
			}
			p += vn
		} else {
			break
		}
	}
	return ""
}

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
		m["model"] = model
		m["stream"] = stream
		payload, _ := json.Marshal(m)
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

	// ConnectRPC protobuf flow
	messages, _ := m["messages"].([]any)
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

	res, err := e.DoPOST(ctx, url, headers, framedPayload)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res, nil
	}

	cid := fmt.Sprintf("chatcmpl-cursor-%d", time.Now().UnixMilli())
	created := time.Now().Unix()

	if stream {
		sseBody := wrapCursorStream(res.Body, model, cid, created)
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

	allBytes, err := io.ReadAll(res.Body)
	res.Body.Close()
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
	outBytes, _ := json.Marshal(out)
	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(outBytes)),
	}, nil
}

func wrapCursorStream(r io.ReadCloser, model, cid string, created int64) io.ReadCloser {
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
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil,
			}},
		})

		buf := make([]byte, 64*1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				text, thinking := ExtractTextFromCursorResponse(buf[:n])
				if thinking != "" {
					writeSSE(map[string]any{
						"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []any{map[string]any{
							"index": 0, "delta": map[string]any{"reasoning_content": thinking}, "finish_reason": nil,
						}},
					})
				}
				if text != "" {
					writeSSE(map[string]any{
						"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
						"choices": []any{map[string]any{
							"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil,
						}},
					})
				}
			}
			if err != nil {
				break
			}
		}

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
			}},
		})
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()
	return pr
}

package executor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	RegisterSpecialized("windsurf", &WindsurfExecutor{
		Base: Base{
			Provider: "windsurf",
			BaseURL:  "https://server.codeium.com/exa.language_server_pb.LanguageServerService/GetChatMessage",
		},
	})
	RegisterSpecialized("ws", &WindsurfExecutor{
		Base: Base{
			Provider: "windsurf",
			BaseURL:  "https://server.codeium.com/exa.language_server_pb.LanguageServerService/GetChatMessage",
		},
	})
}

const (
	windsurfDefaultURL   = "https://server.codeium.com/exa.language_server_pb.LanguageServerService/GetChatMessage"
	windsurfIDEVersion   = "3.14.0"
	windsurfIDEName      = "windsurf"
	windsurfLocale       = "en-US"
)

var windsurfModelAliasMap = map[string]string{
	"swe-1.6-fast":                  "swe-1-6-fast",
	"swe-1.6":                       "swe-1-6",
	"swe-1.5-fast":                  "swe-1-5-fast",
	"swe-1.5":                       "swe-1-5",
	"claude-opus-4.7-max":           "claude-opus-4-7-max",
	"claude-opus-4.7-xhigh":         "claude-opus-4-7-xhigh",
	"claude-opus-4.7-high":          "claude-opus-4-7-high",
	"claude-opus-4.7-medium":        "claude-opus-4-7-medium",
	"claude-opus-4.7-low":           "claude-opus-4-7-low",
	"claude-opus-4.7-review":        "opus-4-7-review",
	"claude-sonnet-4.6-thinking-1m": "claude-sonnet-4-6-thinking-1m",
	"claude-sonnet-4.6-1m":          "claude-sonnet-4-6-1m",
	"claude-sonnet-4.6-thinking":    "claude-sonnet-4-6-thinking",
	"claude-sonnet-4.6":             "claude-sonnet-4-6",
	"claude-opus-4.6-thinking":      "claude-opus-4-6-thinking",
	"claude-opus-4.6":               "claude-opus-4-6",
	"claude-opus-4.5-thinking":      "MODEL_CLAUDE_4_5_OPUS_THINKING",
	"claude-opus-4.5":               "MODEL_CLAUDE_4_5_OPUS",
	"claude-sonnet-4.5-thinking":    "MODEL_PRIVATE_3",
	"claude-sonnet-4.5":             "MODEL_PRIVATE_2",
	"claude-haiku-4.5":              "MODEL_PRIVATE_11",
	"gpt-5.5-xhigh-fast":            "gpt-5-5-xhigh-priority",
	"gpt-5.5-high-fast":             "gpt-5-5-high-priority",
	"gpt-5.5-medium-fast":           "gpt-5-5-medium-priority",
	"gpt-5.5-low-fast":              "gpt-5-5-low-priority",
	"gpt-5.5-none-fast":             "gpt-5-5-none-priority",
	"gpt-5.5-xhigh":                 "gpt-5-5-xhigh",
	"gpt-5.5-high":                  "gpt-5-5-high",
	"gpt-5.5-medium":                "gpt-5-5-medium",
	"gpt-5.5-low":                   "gpt-5-5-low",
	"gpt-5.5-none":                  "gpt-5-5-none",
	"gpt-5.5-review":                "gpt-5-5-review",
	"gpt-5.5":                       "gpt-5-5-medium",
	"gpt-5.4-xhigh-fast":            "gpt-5-4-xhigh-priority",
	"gpt-5.4-high-fast":             "gpt-5-4-high-priority",
	"gpt-5.4-medium-fast":           "gpt-5-4-medium-priority",
	"gpt-5.4-low-fast":              "gpt-5-4-low-priority",
	"gpt-5.4-none-fast":             "gpt-5-4-none-priority",
	"gpt-5.4-xhigh":                 "gpt-5-4-xhigh",
	"gpt-5.4-high":                  "gpt-5-4-high",
	"gpt-5.4-medium":                "gpt-5-4-medium",
	"gpt-5.4-low":                   "gpt-5-4-low",
	"gpt-5.4-none":                  "gpt-5-4-none",
	"gpt-5.4-mini-xhigh":            "gpt-5-4-mini-xhigh",
	"gpt-5.4-mini-high":             "gpt-5-4-mini-high",
	"gpt-5.4-mini-medium":           "gpt-5-4-mini-medium",
	"gpt-5.4-mini-low":              "gpt-5-4-mini-low",
	"gpt-5.4":                       "gpt-5-4-medium",
	"gpt-5.3-codex-xhigh-fast":      "gpt-5-3-codex-xhigh-priority",
	"gpt-5.3-codex-high-fast":       "gpt-5-3-codex-high-priority",
	"gpt-5.3-codex-medium-fast":     "gpt-5-3-codex-medium-priority",
	"gpt-5.3-codex-low-fast":        "gpt-5-3-codex-low-priority",
	"gpt-5.3-codex-xhigh":           "gpt-5-3-codex-xhigh",
	"gpt-5.3-codex-high":            "gpt-5-3-codex-high",
	"gpt-5.3-codex-medium":          "gpt-5-3-codex-medium",
	"gpt-5.3-codex-low":             "gpt-5-3-codex-low",
	"gpt-5.3-codex":                 "gpt-5-3-codex-medium",
	"gpt-5.2-xhigh":                 "MODEL_GPT_5_2_XHIGH",
	"gpt-5.2-high":                  "MODEL_GPT_5_2_HIGH",
	"gpt-5.2-medium":                "MODEL_GPT_5_2_MEDIUM",
	"gpt-5.2-low":                   "MODEL_GPT_5_2_LOW",
	"gpt-5.2-none":                  "MODEL_GPT_5_2_NONE",
	"gpt-5.2":                       "MODEL_GPT_5_2_MEDIUM",
	"gpt-5":                         "gpt-5",
	"gpt-4.1":                       "MODEL_CHAT_GPT_4_1_2025_04_14",
	"gpt-4.1-mini":                  "gpt-4.1-mini",
	"gpt-4o":                        "MODEL_CHAT_GPT_4O_2024_08_06",
	"gemini-3.1-pro-high":           "gemini-3-1-pro-high",
	"gemini-3.1-pro-low":            "gemini-3-1-pro-low",
	"gemini-3.1-pro":                "gemini-3-1-pro-high",
	"gemini-3.0-flash-high":         "MODEL_GOOGLE_GEMINI_3_0_FLASH_HIGH",
	"gemini-3.0-flash-medium":       "MODEL_GOOGLE_GEMINI_3_0_FLASH_MEDIUM",
	"gemini-3.0-flash-low":          "MODEL_GOOGLE_GEMINI_3_0_FLASH_LOW",
	"gemini-3.0-flash-minimal":      "MODEL_GOOGLE_GEMINI_3_0_FLASH_MINIMAL",
	"gemini-3.0-flash":              "MODEL_GOOGLE_GEMINI_3_0_FLASH_HIGH",
	"gemini-2.5-pro":                "MODEL_GOOGLE_GEMINI_2_5_PRO",
	"deepseek-v4":                   "deepseek-v4",
	"kimi-k2.6":                     "kimi-k2-6",
	"kimi-k2.5":                     "kimi-k2-5",
	"glm-5.1":                       "glm-5-1",
}

type WindsurfExecutor struct {
	Base
}

func ResolveWsModelID(model string) string {
	if mapped, ok := windsurfModelAliasMap[model]; ok {
		return mapped
	}
	return model
}

func BuildWindsurfGetChatMessageRequest(apiKey, model string, messages []any) []byte {
	sessionID := randomUUID()
	cascadeID := randomUUID()

	var metaBuf bytes.Buffer
	// Field 1: apiKey
	writeProtoField(&metaBuf, 1, 2, []byte(apiKey))
	// Field 2: ide_name
	writeProtoField(&metaBuf, 2, 2, []byte(windsurfIDEName))
	// Field 3: ide_version
	writeProtoField(&metaBuf, 3, 2, []byte(windsurfIDEVersion))
	// Field 4: extension_version
	writeProtoField(&metaBuf, 4, 2, []byte(windsurfIDEVersion))
	// Field 5: session_id
	writeProtoField(&metaBuf, 5, 2, []byte(sessionID))
	// Field 6: locale
	writeProtoField(&metaBuf, 6, 2, []byte(windsurfLocale))

	var modelBuf bytes.Buffer
	writeProtoField(&modelBuf, 1, 2, []byte(model))

	var reqBuf bytes.Buffer
	// Field 1: metadata
	writeProtoField(&reqBuf, 1, 2, metaBuf.Bytes())
	// Field 2: cascade_id
	writeProtoField(&reqBuf, 2, 2, []byte(cascadeID))
	// Field 3: model_or_alias
	writeProtoField(&reqBuf, 3, 2, modelBuf.Bytes())

	// Field 4: repeated ChatMessage
	for _, mRaw := range messages {
		msg, ok := mRaw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "" {
			role = "user"
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

		var chatMsgBuf bytes.Buffer
		writeProtoField(&chatMsgBuf, 1, 2, []byte(role))
		writeProtoField(&chatMsgBuf, 2, 2, []byte(content))
		if tcid, _ := msg["tool_call_id"].(string); tcid != "" {
			writeProtoField(&chatMsgBuf, 3, 2, []byte(tcid))
		}

		writeProtoField(&reqBuf, 4, 2, chatMsgBuf.Bytes())
	}

	return reqBuf.Bytes()
}

type wsDecodedChunk struct {
	kind             string // "content", "done", "error"
	text             string
	message          string
	promptTokens     int
	completionTokens int
}

func decodeWsCompletionChunk(payload []byte) wsDecodedChunk {
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
			fieldBytes := payload[p:end]
			p = end

			if fieldNum == 1 { // ContentChunk -> field 1 string
				txt := decodeWsStringField(fieldBytes, 1)
				if txt != "" {
					return wsDecodedChunk{kind: "content", text: txt}
				}
			} else if fieldNum == 3 { // DoneChunk -> field 1 UsageStats
				pt, ct := decodeWsDoneChunk(fieldBytes)
				return wsDecodedChunk{kind: "done", promptTokens: pt, completionTokens: ct}
			} else if fieldNum == 4 { // ErrorChunk -> field 1 string
				errMsg := decodeWsStringField(fieldBytes, 1)
				return wsDecodedChunk{kind: "error", message: errMsg}
			}
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
	return wsDecodedChunk{kind: "unknown"}
}

func decodeWsStringField(payload []byte, targetField uint64) string {
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
			if fieldNum == targetField {
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

func decodeWsDoneChunk(payload []byte) (promptTokens, completionTokens int) {
	p := 0
	for p < len(payload) {
		tag, n := binary.Uvarint(payload[p:])
		if n <= 0 {
			break
		}
		p += n
		fieldNum := tag >> 3
		wireType := tag & 0x07
		if wireType == 2 { // Field 1 = UsageStats
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
				usageBytes := payload[p:end]
				up := 0
				for up < len(usageBytes) {
					utag, un := binary.Uvarint(usageBytes[up:])
					if un <= 0 {
						break
					}
					up += un
					ufield := utag >> 3
					uwire := utag & 0x07
					if uwire == 0 {
						v, vn := binary.Uvarint(usageBytes[up:])
						if vn <= 0 {
							break
						}
						up += vn
						if ufield == 1 {
							promptTokens = int(v)
						} else if ufield == 2 {
							completionTokens = int(v)
						}
					} else {
						break
					}
				}
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
	return promptTokens, completionTokens
}

func (e *WindsurfExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	apiKey := cred.AccessToken
	if apiKey == "" {
		apiKey = cred.APIKey
	}
	wsModel := ResolveWsModelID(model)
	messages, _ := m["messages"].([]any)
	if len(messages) == 0 {
		messages = []any{map[string]any{"role": "user", "content": ""}}
	}

	protoPayload := BuildWindsurfGetChatMessageRequest(apiKey, wsModel, messages)
	framedPayload := WrapConnectRPCFrame(protoPayload) // gRPC-web uses same 5-byte framing [0x00, 4-byte BE length, data]

	url := strings.TrimRight(cred.BaseURL, "/")
	if url == "" {
		url = windsurfDefaultURL
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/grpc-web+proto")
	h.Set("Accept", "application/grpc-web+proto")
	h.Set("User-Agent", fmt.Sprintf("windsurf/%s", windsurfIDEVersion))
	h.Set("X-Grpc-Web", "1")
	if apiKey != "" {
		h.Set("Authorization", "Bearer "+apiKey)
	}

	res, err := e.DoPOST(ctx, url, h, framedPayload)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res, nil
	}

	cid := fmt.Sprintf("chatcmpl-ws-%d", time.Now().UnixMilli())
	created := time.Now().Unix()

	if stream {
		sseBody := wrapWindsurfStream(res.Body, model, cid, created)
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

	jsonResp, err := collectWindsurfNonStreaming(res.Body, model, cid, created)
	if err != nil {
		return jsonErr(502, err.Error(), "windsurf_error", ""), nil
	}
	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(jsonResp)),
	}, nil
}

func readWindsurfFrames(r io.Reader, handleFrame func(flag byte, payload []byte)) {
	var pending []byte
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			offset := 0
			for offset+5 <= len(pending) {
				flag := pending[offset]
				length := int(binary.BigEndian.Uint32(pending[offset+1 : offset+5]))
				if length < 0 || offset+5+length > len(pending) {
					break
				}
				framePayload := pending[offset+5 : offset+5+length]
				handleFrame(flag, framePayload)
				offset += 5 + length
			}
			if offset > 0 {
				pending = pending[offset:]
			}
		}
		if err != nil {
			break
		}
	}
}

func wrapWindsurfStream(r io.ReadCloser, model, cid string, created int64) io.ReadCloser {
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

		var promptTokens, completionTokens int
		var hadError string

		readWindsurfFrames(r, func(flag byte, payload []byte) {
			if flag == 0x80 { // Trailer frame
				trailer := string(payload)
				if strings.Contains(trailer, "grpc-status:") && !strings.Contains(trailer, "grpc-status: 0") && !strings.Contains(trailer, "grpc-status:0") {
					hadError = trailer
				}
				return
			}
			if flag != 0x00 {
				return
			}

			chunk := decodeWsCompletionChunk(payload)
			if chunk.kind == "content" && chunk.text != "" {
				writeSSE(map[string]any{
					"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
					"choices": []any{map[string]any{
						"index": 0, "delta": map[string]any{"content": chunk.text}, "finish_reason": nil,
					}},
				})
			} else if chunk.kind == "done" {
				promptTokens = chunk.promptTokens
				completionTokens = chunk.completionTokens
			} else if chunk.kind == "error" {
				hadError = chunk.message
			}
		})

		if hadError != "" {
			writeSSE(map[string]any{
				"error": map[string]any{
					"message": hadError,
					"type":    "windsurf_error",
					"code":    "upstream_error",
				},
			})
		} else {
			choiceChunk := map[string]any{
				"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []any{map[string]any{
					"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
				}},
			}
			if promptTokens > 0 || completionTokens > 0 {
				choiceChunk["usage"] = map[string]any{
					"prompt_tokens":     promptTokens,
					"completion_tokens": completionTokens,
					"total_tokens":      promptTokens + completionTokens,
				}
			}
			writeSSE(choiceChunk)
		}
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()
	return pr
}

func collectWindsurfNonStreaming(r io.ReadCloser, model, cid string, created int64) ([]byte, error) {
	defer r.Close()

	var totalText string
	var promptTokens, completionTokens int
	var hadError string

	readWindsurfFrames(r, func(flag byte, payload []byte) {
		if flag == 0x80 {
			trailer := string(payload)
			if strings.Contains(trailer, "grpc-status:") && !strings.Contains(trailer, "grpc-status: 0") && !strings.Contains(trailer, "grpc-status:0") {
				hadError = trailer
			}
			return
		}
		if flag != 0x00 {
			return
		}

		chunk := decodeWsCompletionChunk(payload)
		if chunk.kind == "content" {
			totalText += chunk.text
		} else if chunk.kind == "done" {
			promptTokens = chunk.promptTokens
			completionTokens = chunk.completionTokens
		} else if chunk.kind == "error" {
			hadError = chunk.message
		}
	})

	if hadError != "" {
		return nil, fmt.Errorf("windsurf error: %s", hadError)
	}

	out := map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": totalText},
			"finish_reason": "stop",
		}},
	}
	if promptTokens > 0 || completionTokens > 0 {
		out["usage"] = map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		}
	}
	return json.Marshal(out)
}

package executor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func init() {
	RegisterSpecialized("iflow", &IFlowExecutor{
		Base: Base{
			Provider: "iflow",
			BaseURL:  "https://apis.iflow.cn/v1/chat/completions",
			Headers:  map[string]string{"User-Agent": "iFlow-Cli"},
		},
	})
}

const iflowUserAgent = "iFlow-Cli"

// IFlowExecutor — OpenAI-compatible chat with HMAC-SHA256 request signature.
type IFlowExecutor struct{ Base }

func createIFlowSignature(userAgent, sessionID string, timestamp int64, apiKey string) string {
	if apiKey == "" {
		return ""
	}
	payload := fmt.Sprintf("%s:%s:%d", userAgent, sessionID, timestamp)
	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (e *IFlowExecutor) buildHeaders(cred Credentials, stream bool) http.Header {
	sessionID := "session-" + uuid.NewString()
	timestamp := time.Now().UnixMilli()
	ua := iflowUserAgent
	if e.Headers != nil {
		if v := e.Headers["User-Agent"]; v != "" {
			ua = v
		}
	}
	apiKey := cred.APIKey
	if apiKey == "" {
		apiKey = cred.AccessToken
	}
	sig := createIFlowSignature(ua, sessionID, timestamp, apiKey)

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("User-Agent", ua)
	h.Set("session-id", sessionID)
	h.Set("x-iflow-timestamp", strconv.FormatInt(timestamp, 10))
	h.Set("x-iflow-signature", sig)
	if apiKey != "" {
		h.Set("Authorization", "Bearer "+apiKey)
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return h
}

func (e *IFlowExecutor) transform(body map[string]any, stream bool) map[string]any {
	if stream {
		if _, hasMsgs := body["messages"]; hasMsgs {
			if _, hasSO := body["stream_options"]; !hasSO {
				body["stream_options"] = map[string]any{"include_usage": true}
			}
		}
	}
	return body
}

func (e *IFlowExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["model"] = model
	m["stream"] = stream
	m = e.transform(m, stream)
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	url := e.BaseURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		url = base
		if !strings.Contains(base, "/chat/completions") {
			url = base + "/chat/completions"
		}
	}
	return e.DoPOST(ctx, url, e.buildHeaders(cred, stream), payload)
}

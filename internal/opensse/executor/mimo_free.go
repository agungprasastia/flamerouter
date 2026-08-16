package executor

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"strings"
	"sync"
	"time"
)

func init() {
	exec := NewMimoFreeExecutor(nil)
	RegisterSpecialized("mimo-free", exec)
	RegisterSpecialized("mmf", exec)
}

const (
	MimoBootstrapURL       = "https://api.xiaomimimo.com/api/free-ai/bootstrap"
	MimoChatURL            = "https://api.xiaomimimo.com/v1/chat/completions"
	MimoSessionAffinity    = "ses_"
	MimoSessionIDLength    = 24
	MimoJWTFallbackTTLSec  = 3000
	MimoJWTExpiryBufferMS  = 300000 // 5 minutes in ms
	MimoSessionChars       = "abcdefghijklmnopqrstuvwxyz0123456789"
	MimoSystemMarker       = "You are MiMoCode, an interactive CLI tool that helps users with software engineering tasks."
)

var mimoUserAgents = []string{
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
}

type MimoFreeExecutor struct {
	Base
	sessionID     string
	bootstrapURL  string
	chatURL       string
	jwtMu         sync.Mutex
	cachedJWT     string
	jwtExpiresAt  int64
}

func NewMimoFreeExecutor(client *http.Client) *MimoFreeExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &MimoFreeExecutor{
		Base: Base{
			Provider: "mimo-free",
			Client:   client,
			BaseURL:  MimoChatURL,
		},
		sessionID:    generateMimoSessionID(),
		bootstrapURL: MimoBootstrapURL,
		chatURL:      MimoChatURL,
	}
}

func generateMimoFingerprint() string {
	username := "unknown-user"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}
	hostname, _ := os.Hostname()
	seed := fmt.Sprintf("%s|%s|%s|%s|%s", hostname, runtime.GOOS, runtime.GOARCH, "unknown-cpu", username)
	h := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(h[:])
}

func generateMimoSessionID() string {
	var sb strings.Builder
	sb.WriteString(MimoSessionAffinity)
	charsLen := big.NewInt(int64(len(MimoSessionChars)))
	for i := 0; i < MimoSessionIDLength; i++ {
		idx, err := rand.Int(rand.Reader, charsLen)
		if err != nil {
			sb.WriteByte(MimoSessionChars[i%len(MimoSessionChars)])
		} else {
			sb.WriteByte(MimoSessionChars[idx.Int64()])
		}
	}
	return sb.String()
}

func parseMimoJWTExp(jwt string) int64 {
	parts := strings.Split(jwt, ".")
	if len(parts) >= 2 {
		payloadSegment := parts[1]
		// Add base64 padding if needed
		if rem := len(payloadSegment) % 4; rem > 0 {
			payloadSegment += strings.Repeat("=", 4-rem)
		}
		data, err := base64.URLEncoding.DecodeString(payloadSegment)
		if err != nil {
			data, err = base64.StdEncoding.DecodeString(payloadSegment)
		}
		if err == nil {
			var payload struct {
				Exp int64 `json:"exp"`
			}
			if err := json.Unmarshal(data, &payload); err == nil && payload.Exp > 0 {
				return payload.Exp * 1000
			}
		}
	}
	return time.Now().UnixMilli() + MimoJWTFallbackTTLSec*1000
}

func injectMimoSystemMarker(body map[string]any) map[string]any {
	if body == nil {
		return body
	}
	messages, ok := body["messages"].([]any)
	if !ok {
		return body
	}

	for _, item := range messages {
		if msg, ok := item.(map[string]any); ok {
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)
			if role == "system" && strings.Contains(content, MimoSystemMarker) {
				return body
			}
		}
	}

	cloned := make(map[string]any, len(body))
	for k, v := range body {
		cloned[k] = v
	}
	newMsgs := make([]any, 0, len(messages)+1)
	newMsgs = append(newMsgs, map[string]any{
		"role":    "system",
		"content": MimoSystemMarker,
	})
	newMsgs = append(newMsgs, messages...)
	cloned["messages"] = newMsgs
	return cloned
}

func (e *MimoFreeExecutor) resetJWTCache() {
	e.jwtMu.Lock()
	defer e.jwtMu.Unlock()
	e.cachedJWT = ""
	e.jwtExpiresAt = 0
}

func (e *MimoFreeExecutor) bootstrapJWT(ctx context.Context) (string, error) {
	e.jwtMu.Lock()
	if e.cachedJWT != "" && time.Now().UnixMilli() < e.jwtExpiresAt-MimoJWTExpiryBufferMS {
		jwt := e.cachedJWT
		e.jwtMu.Unlock()
		return jwt, nil
	}
	e.jwtMu.Unlock()

	reqBody, _ := json.Marshal(map[string]string{
		"client": generateMimoFingerprint(),
	})

	ua := mimoUserAgents[0]
	if n, err := rand.Int(rand.Reader, big.NewInt(int64(len(mimoUserAgents)))); err == nil {
		ua = mimoUserAgents[n.Int64()]
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.bootstrapURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", ua)

	resp, err := e.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("MiMo bootstrap failed: %d", resp.StatusCode)
	}

	var data struct {
		JWT string `json:"jwt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decode bootstrap response: %w", err)
	}
	if data.JWT == "" {
		return "", errors.New("MiMo bootstrap returned no JWT")
	}

	exp := parseMimoJWTExp(data.JWT)

	e.jwtMu.Lock()
	e.cachedJWT = data.JWT
	e.jwtExpiresAt = exp
	e.jwtMu.Unlock()

	return data.JWT, nil
}

func (e *MimoFreeExecutor) buildHeaders(stream bool, jwt string) http.Header {
	ua := mimoUserAgents[0]
	if n, err := rand.Int(rand.Reader, big.NewInt(int64(len(mimoUserAgents)))); err == nil {
		ua = mimoUserAgents[n.Int64()]
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("X-Mimo-Source", "mimocode-cli-free")
	h.Set("User-Agent", ua)
	h.Set("x-session-affinity", e.sessionID)
	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}
	if jwt != "" {
		h.Set("Authorization", "Bearer "+jwt)
	}
	return h
}

func (e *MimoFreeExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	jwt, err := e.bootstrapJWT(ctx)
	if err != nil {
		return nil, err
	}

	var parsedBody map[string]any
	if err := json.Unmarshal(body, &parsedBody); err != nil {
		return nil, err
	}

	transformedBody := injectMimoSystemMarker(parsedBody)
	transformedBody["stream"] = stream
	if model != "" {
		transformedBody["model"] = model
	}

	payload, err := json.Marshal(transformedBody)
	if err != nil {
		return nil, err
	}

	chatURL := e.chatURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		chatURL = base
		if !strings.Contains(base, "/chat/completions") {
			chatURL = base + "/chat/completions"
		}
	}

	headers := e.buildHeaders(stream, jwt)
	res, err := e.DoPOST(ctx, chatURL, headers, payload)
	if err != nil {
		return nil, err
	}

	// On auth failure (401 or 403), invalidate cache and retry once with fresh JWT
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		DrainBody(res.Body)
		e.resetJWTCache()
		jwt, err = e.bootstrapJWT(ctx)
		if err != nil {
			return nil, err
		}
		headers.Set("Authorization", "Bearer "+jwt)
		return e.DoPOST(ctx, chatURL, headers, payload)
	}

	return res, nil
}

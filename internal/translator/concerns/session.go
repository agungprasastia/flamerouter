package concerns

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	sessionTTLMs             = 2 * 60 * 60 * 1000
	sessionCleanupIntervalMs = 30 * 60 * 1000
	assistantMinLen          = 50
	assistantCapLen          = 50
	maxSessions              = 1000
	maxAssistantSessions     = 5000
	maxContinuationSessions  = 5000
)

type sessionEntry struct {
	sessionID string
	lastUsed  int64
}

type continuationEntry struct {
	continuationID string
	lastUsed       int64
}

var (
	runtimeSessionStore   = map[string]*sessionEntry{}
	assistantSessionStore = map[string]*sessionEntry{}
	continuationStore     = map[string]*continuationEntry{}
	sessionMu             sync.Mutex
)

func init() {
	go func() {
		ticker := time.NewTicker(sessionCleanupIntervalMs * time.Millisecond)
		for range ticker.C {
			now := time.Now().UnixMilli()

			sessionMu.Lock()
			for k, e := range runtimeSessionStore {
				if now-e.lastUsed > sessionTTLMs {
					delete(runtimeSessionStore, k)
				}
			}

			for k, e := range assistantSessionStore {
				if now-e.lastUsed > sessionTTLMs {
					delete(assistantSessionStore, k)
				}
			}

			for k, e := range continuationStore {
				if now-e.lastUsed > sessionTTLMs {
					delete(continuationStore, k)
				}
			}
			sessionMu.Unlock()
		}
	}()
}

// GenerateBinaryStyleID generates a unique binary style session ID.
func GenerateBinaryStyleID() string {
	return randomUUID() + fmt.Sprintf("%d", time.Now().UnixMilli())
}

func randomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// DeriveSessionID derives or recovers a session ID from a connection ID.
func DeriveSessionID(connectionID string) string {
	if connectionID == "" {
		return GenerateBinaryStyleID()
	}

	sessionMu.Lock()
	defer sessionMu.Unlock()

	if existing, ok := runtimeSessionStore[connectionID]; ok {
		existing.lastUsed = time.Now().UnixMilli()
		return existing.sessionID
	}

	if len(runtimeSessionStore) >= maxSessions {
		var oldestKey string

		var oldestTS int64 = 1<<63 - 1
		for k, e := range runtimeSessionStore {
			if e.lastUsed < oldestTS {
				oldestTS = e.lastUsed
				oldestKey = k
			}
		}

		if oldestKey != "" {
			delete(runtimeSessionStore, oldestKey)
		}
	}

	sid := GenerateBinaryStyleID()
	runtimeSessionStore[connectionID] = &sessionEntry{sessionID: sid, lastUsed: time.Now().UnixMilli()}

	return sid
}

// ClearSessionStore clears all in-memory session mappings.
func ClearSessionStore() {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	runtimeSessionStore = map[string]*sessionEntry{}
	assistantSessionStore = map[string]*sessionEntry{}
	continuationStore = map[string]*continuationEntry{}
}

func sha16(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:16]
}

func normalizeSessionID(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}

	v := strings.TrimSpace(s)
	if v == "" || len(v) > 256 {
		return ""
	}

	return v
}

func extractClaudeCodeSession(userID string) string {
	if userID == "" {
		return ""
	}

	if idx := strings.LastIndex(userID, "_session_"); idx >= 0 {
		rest := userID[idx+len("_session_"):]
		// uuid-ish
		if len(rest) >= 8 {
			return rest
		}
	}

	if len(userID) > 0 && userID[0] == '{' {
		var m map[string]any
		if json.Unmarshal([]byte(userID), &m) == nil {
			return normalizeSessionID(m["session_id"])
		}
	}

	return ""
}

func extractAntigravitySession(body map[string]any) string {
	if body == nil {
		return ""
	}

	if req, ok := body["request"].(map[string]any); ok {
		if sid := normalizeSessionID(req["sessionId"]); sid != "" {
			return sid
		}
	}

	if rid, ok := body["requestId"].(string); ok {
		// agent/uuid/...
		parts := strings.Split(rid, "/")
		if len(parts) >= 2 {
			return normalizeSessionID(parts[1])
		}
	}

	return ""
}

func headerValue(headers map[string]any, key string) string {
	if headers == nil {
		return ""
	}

	if v := normalizeSessionID(headers[key]); v != "" {
		return v
	}

	return normalizeSessionID(headers[strings.ToLower(key)])
}

var sessionHeaderKeys = []string{"x-session-id", "session-id", "session_id", "x-amp-thread-id"}

func extractClaudeUserSession(body map[string]any) string {
	if body == nil {
		return ""
	}

	meta, ok := body["metadata"].(map[string]any)
	if !ok {
		return ""
	}

	uid, ok := meta["user_id"].(string)
	if !ok {
		return ""
	}

	if claude := extractClaudeCodeSession(uid); claude != "" {
		return "claude:" + claude
	}

	return ""
}

func extractClientSessionID(headers map[string]any, body map[string]any, scope string) string {
	if s := extractClaudeUserSession(body); s != "" {
		return s
	}

	if ag := extractAntigravitySession(body); ag != "" {
		return "antigravity:" + ag
	}

	for _, key := range sessionHeaderKeys {
		if v := headerValue(headers, key); v != "" {
			return v
		}
	}

	if scope != "kiro" {
		if v := headerValue(headers, "x-client-request-id"); v != "" {
			return v
		}
	}

	return extractBodySessionID(body, scope)
}

func extractBodySessionID(body map[string]any, scope string) string {
	if body == nil {
		return ""
	}

	if v := normalizeSessionID(body["prompt_cache_key"]); v != "" {
		return v
	}

	if v := normalizeSessionID(body["session_id"]); v != "" {
		return v
	}

	if v := normalizeSessionID(body["conversation_id"]); v != "" {
		return v
	}

	if scope != "kiro" {
		if meta, ok := body["metadata"].(map[string]any); ok {
			if v := normalizeSessionID(meta["user_id"]); v != "" {
				return v
			}
		}
	}

	return ""
}

func requestMessages(body map[string]any) []any {
	if body == nil {
		return nil
	}

	if messages, ok := body["messages"].([]any); ok {
		return messages
	}

	if input, ok := body["input"].([]any); ok {
		return input
	}

	return nil
}

func extractBlockAssistantText(blocks []any) string {
	var text string

	for _, b := range blocks {
		block, ok := b.(map[string]any)
		if !ok {
			continue
		}

		if t, ok := block["text"].(string); ok {
			text += t
		} else if o, ok := block["output"].(string); ok {
			text += o
		}
	}

	return text
}

func accumulateAssistantText(body map[string]any) string {
	items := requestMessages(body)
	text := ""

	for _, itemRaw := range items {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}

		if role, ok := item["role"].(string); !ok || role != "assistant" {
			continue
		}

		switch c := item["content"].(type) {
		case string:
			text += c
		case []any:
			text += extractBlockAssistantText(c)
		}

		if len(text) >= assistantCapLen {
			break
		}
	}

	return text
}

func assistantTextSessionID(scope string, body map[string]any) string {
	text := accumulateAssistantText(body)
	if len(text) < assistantMinLen {
		return ""
	}

	cap := text
	if len(cap) > assistantCapLen {
		cap = cap[:assistantCapLen]
	}

	hash := sha16(scope + ":" + cap)

	sessionMu.Lock()
	defer sessionMu.Unlock()

	if existing, ok := assistantSessionStore[hash]; ok {
		existing.lastUsed = time.Now().UnixMilli()
		return existing.sessionID
	}

	if len(assistantSessionStore) >= maxAssistantSessions {
		var oldestKey string

		var oldestTS int64 = 1<<63 - 1
		for k, e := range assistantSessionStore {
			if e.lastUsed < oldestTS {
				oldestTS = e.lastUsed
				oldestKey = k
			}
		}

		if oldestKey != "" {
			delete(assistantSessionStore, oldestKey)
		}
	}

	sid := GenerateBinaryStyleID()
	assistantSessionStore[hash] = &sessionEntry{sessionID: sid, lastUsed: time.Now().UnixMilli()}

	return sid
}

// SessionIdentity represents the resolved session and its persistence state.
type SessionIdentity struct {
	SessionID string
	Ephemeral bool
}

// ResolveSessionIdentity determines the appropriate session identifier.
func ResolveSessionIdentity(headers map[string]any, body map[string]any, connectionID, workspaceID, scope string) SessionIdentity {
	if client := extractClientSessionID(headers, body, scope); client != "" {
		return SessionIdentity{SessionID: client, Ephemeral: false}
	}

	if scope != "kiro" {
		if fromAssistant := assistantTextSessionID(scope+":"+connectionID, body); fromAssistant != "" {
			return SessionIdentity{SessionID: fromAssistant, Ephemeral: false}
		}
	}

	if ws := normalizeSessionID(workspaceID); ws != "" {
		return SessionIdentity{SessionID: ws, Ephemeral: false}
	}

	if scope == "kiro" {
		return SessionIdentity{SessionID: GenerateBinaryStyleID(), Ephemeral: true}
	}

	return SessionIdentity{SessionID: DeriveSessionID(connectionID), Ephemeral: false}
}

// ResolveSessionID resolves a session ID from headers, body, or connection info.
func ResolveSessionID(headers map[string]any, body map[string]any, connectionID, workspaceID, scope string) string {
	return ResolveSessionIdentity(headers, body, connectionID, workspaceID, scope).SessionID
}

// ResolveContinuationID resolves or creates a continuation ID for session context.
func ResolveContinuationID(sessionID, connectionID, scope string, ephemeral bool) string {
	if ephemeral {
		return randomUUID()
	}

	key := scope + ":" + connectionID + ":" + sessionID

	sessionMu.Lock()
	defer sessionMu.Unlock()

	if existing, ok := continuationStore[key]; ok {
		existing.lastUsed = time.Now().UnixMilli()
		return existing.continuationID
	}

	cid := randomUUID()

	if len(continuationStore) >= maxContinuationSessions {
		var oldestKey string

		var oldestTS int64 = 1<<63 - 1
		for k, e := range continuationStore {
			if e.lastUsed < oldestTS {
				oldestTS = e.lastUsed
				oldestKey = k
			}
		}

		if oldestKey != "" {
			delete(continuationStore, oldestKey)
		}
	}

	continuationStore[key] = &continuationEntry{continuationID: cid, lastUsed: time.Now().UnixMilli()}

	return cid
}

// CaptureSessionID matches 9router captureSessionId.
func CaptureSessionID(body map[string]any, credentials map[string]any, connectionID, scope string) string {
	var headers map[string]any

	if credentials != nil {
		if rh, ok := credentials["rawHeaders"].(map[string]any); ok {
			headers = rh
		}
	}

	return ResolveSessionID(headers, body, connectionID, "", scope)
}

func extractEffortThinking(effort string) map[string]any {
	e := strings.ToLower(effort)
	if e == "none" || e == "off" {
		return map[string]any{"mode": "none"}
	}

	if e == "auto" {
		return map[string]any{"mode": "auto"}
	}

	return map[string]any{"mode": "level", "level": e}
}

func extractClaudeThinking(t map[string]any) map[string]any {
	ttype, ok := t["type"].(string)
	if !ok {
		return nil
	}

	if ttype == "disabled" {
		return map[string]any{"mode": "none"}
	}

	if ttype == "adaptive" || ttype == "enabled" {
		if bt, ok := t["budget_tokens"].(float64); ok && bt > 0 {
			return map[string]any{"mode": "budget", "budget": int(bt)}
		}

		if bt, ok := t["budget_tokens"].(int); ok && bt > 0 {
			return map[string]any{"mode": "budget", "budget": bt}
		}

		return map[string]any{"mode": "auto"}
	}

	return nil
}

func extractGeminiThinkingConfig(body map[string]any) map[string]any {
	if t, ok := body["thinkingConfig"].(map[string]any); ok {
		return t
	}

	if gc, ok := body["generationConfig"].(map[string]any); ok {
		if t, ok := gc["thinkingConfig"].(map[string]any); ok {
			return t
		}
	}

	if req, ok := body["request"].(map[string]any); ok {
		if gc, ok := req["generationConfig"].(map[string]any); ok {
			if t, ok := gc["thinkingConfig"].(map[string]any); ok {
				return t
			}
		}
	}

	return nil
}

func extractGeminiThinking(body map[string]any) map[string]any {
	tc := extractGeminiThinkingConfig(body)
	if tc == nil {
		return nil
	}

	if tl, ok := tc["thinkingLevel"].(string); ok {
		return map[string]any{"mode": "level", "level": strings.ToLower(tl)}
	}

	var tb float64
	switch v := tc["thinkingBudget"].(type) {
	case float64:
		tb = v
	case int:
		tb = float64(v)
	default:
		tb = -999
	}

	if tb != -999 {
		if tb == 0 {
			return map[string]any{"mode": "none"}
		}

		if tb < 0 {
			return map[string]any{"mode": "auto"}
		}

		return map[string]any{"mode": "budget", "budget": int(tb)}
	}

	return nil
}

func extractEnableThinking(body map[string]any) map[string]any {
	et, ok := body["enable_thinking"]
	if !ok {
		return nil
	}

	if et == false {
		return map[string]any{"mode": "none"}
	}

	if et == true {
		if tb, ok := body["thinking_budget"].(float64); ok && tb > 0 {
			return map[string]any{"mode": "budget", "budget": int(tb)}
		}

		if tb, ok := body["thinking_budget"].(int); ok && tb > 0 {
			return map[string]any{"mode": "budget", "budget": tb}
		}

		return map[string]any{"mode": "auto"}
	}

	return nil
}

// CaptureThinking extracts unified thinking intent (alias of extractThinking).
func CaptureThinking(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}

	if res := extractThinkingConfigModes(body); res != nil {
		return res
	}

	if res := extractGeminiThinking(body); res != nil {
		return res
	}

	return extractEnableThinking(body)
}

func extractThinkingConfigModes(body map[string]any) map[string]any {
	if oc, ok := body["output_config"].(map[string]any); ok {
		if effort, ok := oc["effort"].(string); ok && effort != "" {
			return extractEffortThinking(effort)
		}
	}

	if t, ok := body["thinking"].(map[string]any); ok {
		if res := extractClaudeThinking(t); res != nil {
			return res
		}
	}

	return extractReasoningEffortModes(body)
}

func extractReasoningEffortModes(body map[string]any) map[string]any {
	if effort, ok := body["reasoning_effort"].(string); ok && effort != "" {
		return extractEffortThinking(effort)
	}

	if r, ok := body["reasoning"].(map[string]any); ok {
		if effort, ok := r["effort"].(string); ok && effort != "" {
			return extractEffortThinking(effort)
		}
	}

	return nil
}

// ToNumericSessionID converts any session id to Antigravity numeric format "-<int64>".
func ToNumericSessionID(sessionID string) string {
	v := normalizeSessionID(sessionID)
	if v == "" {
		return ""
	}

	if strings.Trim(v, "0123456789-") == "" {
		return v
	}

	h := sha256.Sum256([]byte(v))
	// take first 8 bytes as uint64, clear sign bit
	var n uint64
	for i := 0; i < 8; i++ {
		n = (n << 8) | uint64(h[i])
	}

	n &= 0x7fffffffffffffff

	return fmt.Sprintf("-%d", n)
}

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
	sessionTtlMs             = 2 * 60 * 60 * 1000
	sessionCleanupIntervalMs = 30 * 60 * 1000
	assistantMinLen          = 50
	assistantCapLen          = 50
	maxSessions              = 1000
	maxAssistantSessions     = 5000
	maxContinuationSessions  = 5000
)

type sessionEntry struct {
	sessionId string
	lastUsed  int64
}

type continuationEntry struct {
	continuationId string
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
				if now-e.lastUsed > sessionTtlMs {
					delete(runtimeSessionStore, k)
				}
			}
			for k, e := range assistantSessionStore {
				if now-e.lastUsed > sessionTtlMs {
					delete(assistantSessionStore, k)
				}
			}
			for k, e := range continuationStore {
				if now-e.lastUsed > sessionTtlMs {
					delete(continuationStore, k)
				}
			}
			sessionMu.Unlock()
		}
	}()
}

func GenerateBinaryStyleId() string {
	return randomUUID() + fmt.Sprintf("%d", time.Now().UnixMilli())
}

func randomUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func DeriveSessionId(connectionId string) string {
	if connectionId == "" {
		return GenerateBinaryStyleId()
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if existing, ok := runtimeSessionStore[connectionId]; ok {
		existing.lastUsed = time.Now().UnixMilli()
		return existing.sessionId
	}
	if len(runtimeSessionStore) >= maxSessions {
		var oldestKey string
		var oldestTs int64 = 1<<63 - 1
		for k, e := range runtimeSessionStore {
			if e.lastUsed < oldestTs {
				oldestTs = e.lastUsed
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(runtimeSessionStore, oldestKey)
		}
	}
	sid := GenerateBinaryStyleId()
	runtimeSessionStore[connectionId] = &sessionEntry{sessionId: sid, lastUsed: time.Now().UnixMilli()}
	return sid
}

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

func normalizeSessionId(value any) string {
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

func extractClaudeCodeSession(userId string) string {
	if userId == "" {
		return ""
	}
	if idx := strings.LastIndex(userId, "_session_"); idx >= 0 {
		rest := userId[idx+len("_session_"):]
		// uuid-ish
		if len(rest) >= 8 {
			return rest
		}
	}
	if len(userId) > 0 && userId[0] == '{' {
		var m map[string]any
		if json.Unmarshal([]byte(userId), &m) == nil {
			return normalizeSessionId(m["session_id"])
		}
	}
	return ""
}

func extractAntigravitySession(body map[string]any) string {
	if body == nil {
		return ""
	}
	if req, ok := body["request"].(map[string]any); ok {
		if sid := normalizeSessionId(req["sessionId"]); sid != "" {
			return sid
		}
	}
	if rid, ok := body["requestId"].(string); ok {
		// agent/uuid/...
		parts := strings.Split(rid, "/")
		if len(parts) >= 2 {
			return normalizeSessionId(parts[1])
		}
	}
	return ""
}

func headerValue(headers map[string]any, key string) string {
	if headers == nil {
		return ""
	}
	if v := normalizeSessionId(headers[key]); v != "" {
		return v
	}
	return normalizeSessionId(headers[strings.ToLower(key)])
}

var sessionHeaderKeys = []string{"x-session-id", "session-id", "session_id", "x-amp-thread-id"}

func extractClientSessionId(headers map[string]any, body map[string]any, scope string) string {
	if body != nil {
		if meta, ok := body["metadata"].(map[string]any); ok {
			if uid, ok := meta["user_id"].(string); ok {
				if claude := extractClaudeCodeSession(uid); claude != "" {
					return "claude:" + claude
				}
			}
		}
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
	if body != nil {
		if v := normalizeSessionId(body["prompt_cache_key"]); v != "" {
			return v
		}
		if v := normalizeSessionId(body["session_id"]); v != "" {
			return v
		}
		if v := normalizeSessionId(body["conversation_id"]); v != "" {
			return v
		}
		if scope != "kiro" {
			if meta, ok := body["metadata"].(map[string]any); ok {
				if v := normalizeSessionId(meta["user_id"]); v != "" {
					return v
				}
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

func accumulateAssistantText(body map[string]any) string {
	items := requestMessages(body)
	var text string
	for _, itemRaw := range items {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := item["role"].(string); role != "assistant" {
			continue
		}
		switch c := item["content"].(type) {
		case string:
			text += c
		case []any:
			for _, b := range c {
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
		}
		if len(text) >= assistantCapLen {
			break
		}
	}
	return text
}

func assistantTextSessionId(scope string, body map[string]any) string {
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
		return existing.sessionId
	}
	if len(assistantSessionStore) >= maxAssistantSessions {
		var oldestKey string
		var oldestTs int64 = 1<<63 - 1
		for k, e := range assistantSessionStore {
			if e.lastUsed < oldestTs {
				oldestTs = e.lastUsed
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(assistantSessionStore, oldestKey)
		}
	}
	sid := GenerateBinaryStyleId()
	assistantSessionStore[hash] = &sessionEntry{sessionId: sid, lastUsed: time.Now().UnixMilli()}
	return sid
}

type SessionIdentity struct {
	SessionId string
	Ephemeral bool
}

func ResolveSessionIdentity(headers map[string]any, body map[string]any, connectionId, workspaceId, scope string) SessionIdentity {
	if client := extractClientSessionId(headers, body, scope); client != "" {
		return SessionIdentity{SessionId: client, Ephemeral: false}
	}
	if scope != "kiro" {
		if fromAssistant := assistantTextSessionId(scope+":"+connectionId, body); fromAssistant != "" {
			return SessionIdentity{SessionId: fromAssistant, Ephemeral: false}
		}
	}
	if ws := normalizeSessionId(workspaceId); ws != "" {
		return SessionIdentity{SessionId: ws, Ephemeral: false}
	}
	if scope == "kiro" {
		return SessionIdentity{SessionId: GenerateBinaryStyleId(), Ephemeral: true}
	}
	return SessionIdentity{SessionId: DeriveSessionId(connectionId), Ephemeral: false}
}

func ResolveSessionId(headers map[string]any, body map[string]any, connectionId, workspaceId, scope string) string {
	return ResolveSessionIdentity(headers, body, connectionId, workspaceId, scope).SessionId
}

func ResolveContinuationId(sessionId, connectionId, scope string, ephemeral bool) string {
	if ephemeral {
		return randomUUID()
	}
	key := scope + ":" + connectionId + ":" + sessionId
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if existing, ok := continuationStore[key]; ok {
		existing.lastUsed = time.Now().UnixMilli()
		return existing.continuationId
	}
	cid := randomUUID()
	if len(continuationStore) >= maxContinuationSessions {
		var oldestKey string
		var oldestTs int64 = 1<<63 - 1
		for k, e := range continuationStore {
			if e.lastUsed < oldestTs {
				oldestTs = e.lastUsed
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(continuationStore, oldestKey)
		}
	}
	continuationStore[key] = &continuationEntry{continuationId: cid, lastUsed: time.Now().UnixMilli()}
	return cid
}

// CaptureSessionId matches 9router captureSessionId.
func CaptureSessionId(body map[string]any, credentials map[string]any, connectionId, scope string) string {
	var headers map[string]any
	if credentials != nil {
		if rh, ok := credentials["rawHeaders"].(map[string]any); ok {
			headers = rh
		}
	}
	return ResolveSessionId(headers, body, connectionId, "", scope)
}

// CaptureThinking extracts unified thinking intent (alias of extractThinking).
func CaptureThinking(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}

	if oc, ok := body["output_config"].(map[string]any); ok {
		if effort, ok := oc["effort"].(string); ok && effort != "" {
			e := strings.ToLower(effort)
			if e == "none" || e == "off" {
				return map[string]any{"mode": "none"}
			}
			if e == "auto" {
				return map[string]any{"mode": "auto"}
			}
			return map[string]any{"mode": "level", "level": e}
		}
	}

	if t, ok := body["thinking"].(map[string]any); ok {
		ttype, _ := t["type"].(string)
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
	}

	if effort, ok := body["reasoning_effort"].(string); ok && effort != "" {
		e := strings.ToLower(effort)
		if e == "none" || e == "off" {
			return map[string]any{"mode": "none"}
		}
		if e == "auto" {
			return map[string]any{"mode": "auto"}
		}
		return map[string]any{"mode": "level", "level": e}
	}

	if r, ok := body["reasoning"].(map[string]any); ok {
		if effort, ok := r["effort"].(string); ok && effort != "" {
			e := strings.ToLower(effort)
			if e == "none" || e == "off" {
				return map[string]any{"mode": "none"}
			}
			if e == "auto" {
				return map[string]any{"mode": "auto"}
			}
			return map[string]any{"mode": "level", "level": e}
		}
	}

	// Gemini shapes
	var tc map[string]any
	if t, ok := body["thinkingConfig"].(map[string]any); ok {
		tc = t
	} else if gc, ok := body["generationConfig"].(map[string]any); ok {
		if t, ok := gc["thinkingConfig"].(map[string]any); ok {
			tc = t
		}
	} else if req, ok := body["request"].(map[string]any); ok {
		if gc, ok := req["generationConfig"].(map[string]any); ok {
			if t, ok := gc["thinkingConfig"].(map[string]any); ok {
				tc = t
			}
		}
	}
	if tc != nil {
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
	}

	if et, ok := body["enable_thinking"]; ok {
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
	}

	return nil
}

// ToNumericSessionId converts any session id to Antigravity numeric format "-<int64>".
func ToNumericSessionId(sessionId string) string {
	v := normalizeSessionId(sessionId)
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

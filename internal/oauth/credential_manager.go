package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"flamerouter/internal/store"
)

// Default refresh lead — matches tokenrefresh.TokenExpiryBuffer / 9router TOKEN_EXPIRY_BUFFER_MS.
const DefaultRefreshLead = 5 * time.Minute

// CodexMaxRefreshAge is 8 days (codex.oauth.maxRefreshAgeMs).
const CodexMaxRefreshAge = 8 * 24 * time.Hour

// MaxRefreshAge returns provider maxRefreshAge (0 = none).
func MaxRefreshAge(provider string) time.Duration {
	switch provider {
	case "codex":
		return CodexMaxRefreshAge
	default:
		return 0
	}
}

// ShouldRefresh mirrors 9router shouldRefreshCredentials.
// expiresAt zero = unknown expiry (skip expiry check).
// lastRefreshAt zero + maxRefreshAge > 0 = stale.
// lead zero defaults to DefaultRefreshLead.
func ShouldRefresh(provider string, expiresAt, lastRefreshAt time.Time, maxRefreshAge, lead time.Duration) bool {
	_ = provider
	if lead <= 0 {
		lead = DefaultRefreshLead
	}
	now := time.Now()
	if !expiresAt.IsZero() && expiresAt.Sub(now) < lead {
		return true
	}
	if maxRefreshAge > 0 {
		if lastRefreshAt.IsZero() || now.Sub(lastRefreshAt) >= maxRefreshAge {
			return true
		}
	}
	return false
}

// MergeRefreshed merges refresh response into current credential map (9router mergeRefreshedCredentials).
func MergeRefreshed(current, next map[string]any) map[string]any {
	if next == nil {
		return nil
	}
	out := map[string]any{}
	nowIso := time.Now().UTC().Format(time.RFC3339)

	if v, ok := next["accessToken"].(string); ok && v != "" {
		out["accessToken"] = v
	}
	if v, ok := next["apiKey"].(string); ok && v != "" {
		out["apiKey"] = v
	}
	if v, ok := next["token"].(string); ok && v != "" {
		out["token"] = v
	}

	rt := strAny(next, "refreshToken")
	if rt == "" {
		rt = strAny(current, "refreshToken")
	}
	if rt != "" {
		out["refreshToken"] = rt
	}

	id := strAny(next, "idToken")
	if id == "" {
		id = strAny(current, "idToken")
	}
	if id != "" {
		out["idToken"] = id
	}

	if expIn, ok := asFloat(next["expiresIn"]); ok && expIn > 0 {
		out["expiresIn"] = expIn
		out["expiresAt"] = time.Now().UTC().Add(time.Duration(expIn) * time.Second).Format(time.RFC3339)
	} else if v := strAny(next, "expiresAt"); v != "" {
		out["expiresAt"] = v
	}

	if v, ok := next["projectId"]; ok {
		out["projectId"] = v
	}

	if nextPSD, ok := next["providerSpecificData"].(map[string]any); ok {
		curPSD, _ := current["providerSpecificData"].(map[string]any)
		out["providerSpecificData"] = mergeMaps(curPSD, nextPSD)
	}

	if v, ok := next["copilotToken"]; ok {
		out["copilotToken"] = v
	}
	if v := strAny(next, "copilotTokenExpiresAt"); v != "" {
		out["copilotTokenExpiresAt"] = v
	}

	if out["accessToken"] != nil || out["apiKey"] != nil || out["token"] != nil ||
		out["refreshToken"] != nil || out["copilotToken"] != nil {
		if v := strAny(next, "lastRefreshAt"); v != "" {
			out["lastRefreshAt"] = v
		} else {
			out["lastRefreshAt"] = nowIso
		}
	}

	return out
}

// TokenRefresher is satisfied by tokenrefresh.RefreshManager (avoids import cycle).
type TokenRefresher interface {
	Refresh(ctx context.Context, provider, refreshToken string) (accessToken, newRefreshToken string, expiresAt time.Time, err error)
}

// CredManager refresh-if-needed with per-connection mutex.
type CredManager struct {
	refresher TokenRefresher
	mu        sync.Mutex
	locks     map[string]*sync.Mutex
}

func NewCredManager(r TokenRefresher) *CredManager {
	return &CredManager{
		refresher: r,
		locks:     make(map[string]*sync.Mutex),
	}
}

func (cm *CredManager) lockFor(id string) *sync.Mutex {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if m, ok := cm.locks[id]; ok {
		return m
	}
	m := &sync.Mutex{}
	cm.locks[id] = m
	return m
}

// RefreshIfNeeded refreshes OAuth tokens when near expiry / max age; persists via st.
// Returns (possibly updated) connection. Fail-open: refresh errors return original conn + err.
func (cm *CredManager) RefreshIfNeeded(ctx context.Context, st *store.Store, conn *store.Connection) (*store.Connection, error) {
	if cm == nil || conn == nil || st == nil {
		return conn, nil
	}
	if conn.AuthType != "oauth" && conn.RefreshToken == "" {
		return conn, nil
	}
	if conn.RefreshToken == "" {
		return conn, nil
	}

	expiresAt := parseTime(conn.ExpiresAt)
	lastRefresh := lastRefreshFromPSD(conn.ProviderSpecificData)
	maxAge := MaxRefreshAge(conn.Provider)
	if !ShouldRefresh(conn.Provider, expiresAt, lastRefresh, maxAge, DefaultRefreshLead) {
		return conn, nil
	}

	lk := cm.lockFor(conn.ID)
	lk.Lock()
	defer lk.Unlock()

	// Re-check after lock (another goroutine may have refreshed).
	if fresh, err := st.GetConnection(conn.ID); err == nil && fresh != nil {
		conn = fresh
		expiresAt = parseTime(conn.ExpiresAt)
		lastRefresh = lastRefreshFromPSD(conn.ProviderSpecificData)
		if !ShouldRefresh(conn.Provider, expiresAt, lastRefresh, maxAge, DefaultRefreshLead) {
			return conn, nil
		}
	}

	if cm.refresher == nil {
		return conn, fmt.Errorf("no token refresher")
	}

	access, refresh, exp, err := cm.refresher.Refresh(ctx, conn.Provider, conn.RefreshToken)
	if err != nil {
		return conn, err
	}
	if access == "" {
		return conn, fmt.Errorf("empty access token after refresh")
	}
	if refresh == "" {
		refresh = conn.RefreshToken
	}
	expStr := conn.ExpiresAt
	if !exp.IsZero() {
		expStr = exp.UTC().Format(time.RFC3339)
	}

	if err := st.UpdateConnectionTokens(conn.ID, access, refresh, expStr); err != nil {
		return conn, err
	}

	// Stamp lastRefreshAt in PSD for max-age tracking (codex).
	if conn.ProviderSpecificData == nil {
		conn.ProviderSpecificData = map[string]any{}
	}
	conn.ProviderSpecificData["lastRefreshAt"] = time.Now().UTC().Format(time.RFC3339)
	if b, err := json.Marshal(conn.ProviderSpecificData); err == nil {
		_ = st.UpdateConnectionPSD(conn.ID, string(b))
	}

	conn.AccessToken = access
	conn.RefreshToken = refresh
	conn.ExpiresAt = expStr
	return conn, nil
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	return time.Time{}
}

func lastRefreshFromPSD(psd map[string]any) time.Time {
	if psd == nil {
		return time.Time{}
	}
	for _, k := range []string{"lastRefreshAt", "lastRefresh"} {
		if v := strAny(psd, k); v != "" {
			return parseTime(v)
		}
	}
	return time.Time{}
}

func strAny(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}

func mergeMaps(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

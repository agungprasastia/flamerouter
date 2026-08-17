// Package oauth provides OAuth authentication flows, token lifecycle helpers,
// and specialized third-party login providers.
package oauth

import (
	"context"
	"encoding/json"
	"flamerouter/internal/store"
	"fmt"
	"sync"
	"time"
)

// DefaultRefreshLead is the default refresh lead time matching tokenrefresh.TokenExpiryBuffer.
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

	if maxRefreshAge > 0 && (lastRefreshAt.IsZero() || now.Sub(lastRefreshAt) >= maxRefreshAge) {
		return true
	}

	return false
}

func mergeTokenKeys(next, out map[string]any) {
	for _, key := range []string{"accessToken", "apiKey", "token"} {
		if v, ok := next[key].(string); ok && v != "" {
			out[key] = v
		}
	}
}

func mergeBasicTokens(current, next, out map[string]any) {
	mergeTokenKeys(next, out)

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
}

func mergeExpiryAndPSD(current, next, out map[string]any) {
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
		var curPSD map[string]any
		if cp, ok := current["providerSpecificData"].(map[string]any); ok {
			curPSD = cp
		}

		out["providerSpecificData"] = mergeMaps(curPSD, nextPSD)
	}
}

func mergeCopilotFields(next, out map[string]any, nowIso string) {
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
}

// MergeRefreshed merges refresh response into current credential map (9router mergeRefreshedCredentials).
func MergeRefreshed(current, next map[string]any) map[string]any {
	if next == nil {
		return nil
	}

	out := map[string]any{}
	nowIso := time.Now().UTC().Format(time.RFC3339)

	mergeBasicTokens(current, next, out)
	mergeExpiryAndPSD(current, next, out)
	mergeCopilotFields(next, out, nowIso)

	return out
}

// TokenRefresher is satisfied by tokenrefresh.RefreshManager (avoids import cycle).
type TokenRefresher interface {
	Refresh(ctx context.Context, provider, refreshToken string) (accessToken, newRefreshToken string, expiresAt time.Time, err error)
}

// CredManager refresh-if-needed with per-connection mutex.
type CredManager struct {
	refresher TokenRefresher
	locks     map[string]*sync.Mutex
	mu        sync.Mutex
}

// NewCredManager constructs a CredManager using the given TokenRefresher.
func NewCredManager(r TokenRefresher) *CredManager {
	return &CredManager{
		refresher: r,
		locks:     make(map[string]*sync.Mutex),
		mu:        sync.Mutex{},
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

func (cm *CredManager) shouldRefreshConn(conn *store.Connection, maxAge time.Duration) bool {
	expiresAt := parseTime(conn.ExpiresAt)
	lastRefresh := lastRefreshFromPSD(conn.ProviderSpecificData)

	return ShouldRefresh(conn.Provider, expiresAt, lastRefresh, maxAge, DefaultRefreshLead)
}

func (cm *CredManager) executeRefresh(ctx context.Context, st *store.Store, conn *store.Connection) (*store.Connection, error) {
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

	if conn.ProviderSpecificData == nil {
		conn.ProviderSpecificData = map[string]any{}
	}

	conn.ProviderSpecificData["lastRefreshAt"] = time.Now().UTC().Format(time.RFC3339)
	if b, err := json.Marshal(conn.ProviderSpecificData); err == nil {
		if err := st.UpdateConnectionPSD(conn.ID, string(b)); err != nil {
			return conn, err
		}
	}

	conn.AccessToken = access
	conn.RefreshToken = refresh
	conn.ExpiresAt = expStr

	return conn, nil
}

// RefreshIfNeeded refreshes OAuth tokens when near expiry / max age; persists via st.
// Returns (possibly updated) connection. Fail-open: refresh errors return original conn + err.
func (cm *CredManager) RefreshIfNeeded(ctx context.Context, st *store.Store, conn *store.Connection) (*store.Connection, error) {
	if cm == nil || conn == nil || st == nil || conn.RefreshToken == "" {
		return conn, nil
	}

	maxAge := MaxRefreshAge(conn.Provider)
	if !cm.shouldRefreshConn(conn, maxAge) {
		return conn, nil
	}

	lk := cm.lockFor(conn.ID)
	lk.Lock()
	defer lk.Unlock()

	// Re-check after lock (another goroutine may have refreshed).
	if fresh, err := st.GetConnection(conn.ID); err == nil && fresh != nil {
		conn = fresh
		if !cm.shouldRefreshConn(conn, maxAge) {
			return conn, nil
		}
	}

	return cm.executeRefresh(ctx, st, conn)
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

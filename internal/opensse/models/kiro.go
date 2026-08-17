package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flamerouter/internal/store"
	"flamerouter/internal/tokenrefresh"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	kiroRuntimeSDKVersion = "1.0.0"
	kiroAgentOS           = "windows"
	kiroAgentOSVersion    = "10.0.26200"
	kiroNodeVersion       = "22.21.1"
	kiroVersion           = "0.10.32"
	kiroDefaultRegion     = "us-east-1"
	kiroDefaultTimeout    = 30 * time.Second
	kiroCacheTTL          = 5 * time.Minute
)

// KiroResolver resolves dynamic models from AWS CodeWhisperer's ListAvailableModels endpoint for Kiro accounts.
type KiroResolver struct {
	Client         *http.Client
	RefreshManager *tokenrefresh.RefreshManager
}

// TTL returns cache TTL for Kiro models.
func (r *KiroResolver) TTL() time.Duration {
	return kiroCacheTTL
}

func (r *KiroResolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return http.DefaultClient
}

func regionFromProfileArn(profileArn string) string {
	if profileArn == "" {
		return kiroDefaultRegion
	}

	parts := strings.Split(profileArn, ":")
	if len(parts) >= 4 && parts[3] != "" {
		return parts[3]
	}

	return kiroDefaultRegion
}

func buildKiroFingerprintHeaders(conn *store.Connection) http.Header {
	seed := ""

	if conn.ProviderSpecificData != nil {
		if cid, ok := conn.ProviderSpecificData["clientId"].(string); ok && cid != "" {
			seed = cid
		} else if arn, ok := conn.ProviderSpecificData["profileArn"].(string); ok && arn != "" {
			seed = arn
		}
	}

	if seed == "" {
		seed = conn.RefreshToken
	}

	if seed == "" {
		seed = conn.AccessToken
	}

	if seed == "" {
		seed = "kiro-anonymous"
	}

	sum := sha256.Sum256([]byte(seed))
	machineID := hex.EncodeToString(sum[:])

	userAgent := fmt.Sprintf("aws-sdk-js/%s ua/2.1 os/%s#%s lang/js md/nodejs#%s api/codewhispererruntime#%s m/N,E KiroIDE-%s-%s",
		kiroRuntimeSDKVersion, kiroAgentOS, kiroAgentOSVersion, kiroNodeVersion, kiroRuntimeSDKVersion, kiroVersion, machineID)
	amzUserAgent := fmt.Sprintf("aws-sdk-js/%s KiroIDE-%s-%s", kiroRuntimeSDKVersion, kiroVersion, machineID)

	h := make(http.Header)
	h.Set("User-Agent", userAgent)
	h.Set("x-amz-user-agent", amzUserAgent)
	h.Set("x-amzn-kiro-agent-mode", "vibe")
	h.Set("x-amzn-codewhisperer-optout", "true")
	h.Set("amz-sdk-request", "attempt=1; max=1")
	h.Set("amz-sdk-invocation-id", uuid.New().String())
	h.Set("Accept", "application/json")

	return h
}

type kiroRawModel struct {
	TokenLimits *struct {
		MaxInputTokens int `json:"maxInputTokens"`
	} `json:"tokenLimits"`
	ModelID        string  `json:"modelId"`
	ID             string  `json:"id"`
	ModelName      string  `json:"modelName"`
	Description    string  `json:"description"`
	RateMultiplier float64 `json:"rateMultiplier"`
}

type kiroResponse struct {
	Models []kiroRawModel `json:"models"`
}

func stripSyntheticSuffixes(id string) string {
	out := id
	out = strings.TrimSuffix(out, "-agentic")
	out = strings.TrimSuffix(out, "-thinking")

	return out
}

func formatKiroDisplayName(modelName, modelID string, rateMultiplier float64) string {
	base := strings.TrimSpace(modelName)
	if base == "" {
		base = strings.TrimSpace(modelID)
	}

	if base == "" {
		base = "Kiro"
	}

	if rateMultiplier <= 0 || math.Abs(rateMultiplier-1.0) < 1e-9 {
		return fmt.Sprintf("Kiro %s", base)
	}

	return fmt.Sprintf("Kiro %s (%.1fx credit)", base, rateMultiplier)
}

func (r *KiroResolver) fetchRaw(ctx context.Context, conn *store.Connection) ([]kiroRawModel, int, error) {
	reqURL := buildKiroRequestURL(conn)

	ctx, cancel := context.WithTimeout(ctx, kiroDefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header = buildKiroFingerprintHeaders(conn)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", conn.AccessToken))

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, 0, err
	}

	if resp == nil || resp.Body == nil {
		return nil, 0, fmt.Errorf("nil response from upstream")
	}

	defer func() {
		//nolint:errcheck // best effort close
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("read kiro error response: %w", err)
		}

		return nil, resp.StatusCode, fmt.Errorf("kiro ListAvailableModels returned status %d: %s", resp.StatusCode, string(b))
	}

	var parsed kiroResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode kiro models: %w", err)
	}

	return parsed.Models, resp.StatusCode, nil
}

func buildKiroRequestURL(conn *store.Connection) string {
	profileArn := ""

	if conn.ProviderSpecificData != nil {
		if arn, ok := conn.ProviderSpecificData["profileArn"].(string); ok {
			profileArn = arn
		}
	}

	region := regionFromProfileArn(profileArn)
	vals := url.Values{}
	vals.Set("origin", "AI_EDITOR")

	if profileArn != "" {
		vals.Set("profileArn", profileArn)
	}

	return fmt.Sprintf("https://q.%s.amazonaws.com/ListAvailableModels?%s", region, vals.Encode())
}

func (r *KiroResolver) tryRefresh(ctx context.Context, conn *store.Connection) ([]kiroRawModel, error) {
	refreshMgr := r.RefreshManager
	if refreshMgr == nil {
		refreshMgr = tokenrefresh.NewRefreshManager()
	}

	outcome, refreshErr := refreshMgr.Refresh(ctx, "kiro", conn.RefreshToken)
	if refreshErr != nil || outcome == nil || outcome.AccessToken == "" {
		return nil, refreshErr
	}

	targetConn := *conn
	targetConn.AccessToken = outcome.AccessToken

	if outcome.RefreshToken != "" {
		targetConn.RefreshToken = outcome.RefreshToken
	}

	items, _, callErr := r.fetchRaw(ctx, &targetConn)

	return items, callErr
}

func createKiroVariants(id, name, desc string, ctxLen int, rateMult float64) (DynamicModel, DynamicModel) {
	base := DynamicModel{
		ID:              id,
		Name:            name,
		ContextLength:   ctxLen,
		MaxOutputTokens: 0,
		IsReasoning:     false,
		IsVL:            false,
		Capabilities:    map[string]any{"thinking": false, "agentic": false},
		RawConfig:       nil,
		UpstreamModelID: id,
		Description:     desc,
		RateMultiplier:  rateMult,
	}

	thinking := DynamicModel{
		ID:              id + "-thinking",
		Name:            name + " (Thinking)",
		ContextLength:   ctxLen,
		MaxOutputTokens: 0,
		IsReasoning:     true,
		IsVL:            false,
		Capabilities:    map[string]any{"thinking": true, "agentic": false},
		RawConfig:       nil,
		UpstreamModelID: id,
		Description:     desc,
		RateMultiplier:  rateMult,
	}

	return base, thinking
}

func createKiroAgenticVariants(id, name, desc string, ctxLen int, rateMult float64) (DynamicModel, DynamicModel) {
	agentic := DynamicModel{
		ID:              id + "-agentic",
		Name:            name + " (Agentic)",
		ContextLength:   ctxLen,
		MaxOutputTokens: 0,
		IsReasoning:     false,
		IsVL:            false,
		Capabilities:    map[string]any{"thinking": false, "agentic": true},
		RawConfig:       nil,
		UpstreamModelID: id,
		Description:     desc,
		RateMultiplier:  rateMult,
	}

	thinkingAgentic := DynamicModel{
		ID:              id + "-thinking-agentic",
		Name:            name + " (Thinking + Agentic)",
		ContextLength:   ctxLen,
		MaxOutputTokens: 0,
		IsReasoning:     true,
		IsVL:            false,
		Capabilities:    map[string]any{"thinking": true, "agentic": true},
		RawConfig:       nil,
		UpstreamModelID: id,
		Description:     desc,
		RateMultiplier:  rateMult,
	}

	return agentic, thinkingAgentic
}

func buildKiroModelVariants(m kiroRawModel) []DynamicModel {
	upstreamID := m.ModelID
	if upstreamID == "" {
		upstreamID = m.ID
	}

	if upstreamID == "" {
		return nil
	}

	safeUpstream := stripSyntheticSuffixes(upstreamID)
	display := formatKiroDisplayName(m.ModelName, safeUpstream, m.RateMultiplier)

	ctxLen := 200000
	if m.TokenLimits != nil && m.TokenLimits.MaxInputTokens > 0 {
		ctxLen = m.TokenLimits.MaxInputTokens
	}

	rateMult := m.RateMultiplier
	if rateMult <= 0 {
		rateMult = 1.0
	}

	base, thinking := createKiroVariants(safeUpstream, display, m.Description, ctxLen, rateMult)
	if safeUpstream == "auto" {
		return []DynamicModel{base, thinking}
	}

	agentic, thinkingAgentic := createKiroAgenticVariants(safeUpstream, display, m.Description, ctxLen, rateMult)

	return []DynamicModel{base, thinking, agentic, thinkingAgentic}
}

// Resolve retrieves dynamic model variants for Kiro connection.
func (r *KiroResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	if conn.AccessToken == "" {
		return nil, nil
	}

	raw, statusCode, err := r.fetchRaw(ctx, conn)
	if err != nil && statusCode == http.StatusUnauthorized && conn.RefreshToken != "" {
		refRaw, refErr := r.tryRefresh(ctx, conn)
		if refErr == nil {
			raw = refRaw
			err = nil
		}
	}

	if err != nil {
		return nil, err
	}

	var expanded []DynamicModel

	for _, m := range raw {
		variants := buildKiroModelVariants(m)
		expanded = append(expanded, variants...)
	}

	return expanded, nil
}

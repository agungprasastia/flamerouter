package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGithubDeviceFlow(t *testing.T) {
	resp, err := StartDeviceFlowForProvider(context.Background(), "github")
	if err != nil {
		t.Fatalf("StartDeviceFlowForProvider failed: %v", err)
	}

	if resp.UserCode == "" {
		t.Errorf("expected UserCode, got empty")
	}

	if resp.VerificationURI == "" {
		t.Errorf("expected VerificationURI, got empty")
	}

	t.Logf("GitHub Device Response: user_code=%s, verification_uri=%s, device_code=%s", resp.UserCode, resp.VerificationURI, resp.DeviceCode)
}

func TestGitHubRefreshConfig(t *testing.T) {
	cfg, ok := ProviderConfigs["github"]
	if !ok || cfg == nil {
		t.Fatal("github config not found")
	}

	if cfg.RefreshURL == "" {
		t.Fatal("github RefreshURL should not be empty")
	}
}

func TestRefreshToken_IDTokenAndGeneric(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}

		_ = r.ParseForm() //nolint:errcheck // best effort parse

		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "rt-123" {
			http.Error(w, "invalid params", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-acc","refresh_token":"new-rt","id_token":"id-tok-xyz","expires_in":3600,"token_type":"Bearer"}`)) //nolint:errcheck // best effort write
	}))
	defer srv.Close()

	ProviderConfigs["test-provider"] = &OAuthConfig{ //nolint:exhaustruct // test fixture
		Provider:   "test-provider",
		ClientID:   "test-client",
		RefreshURL: srv.URL,
	}
	defer delete(ProviderConfigs, "test-provider")

	h := NewHandler()

	tok, err := h.RefreshToken(context.Background(), "test-provider", "rt-123")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}

	if tok.AccessToken != "new-acc" {
		t.Errorf("expected new-acc, got %s", tok.AccessToken)
	}

	if tok.RefreshToken != "new-rt" {
		t.Errorf("expected new-rt, got %s", tok.RefreshToken)
	}

	if tok.IDToken != "id-tok-xyz" {
		t.Errorf("expected id-tok-xyz, got %s", tok.IDToken)
	}

	if tok.ExpiresAt.Before(time.Now()) {
		t.Errorf("expected future ExpiresAt, got %v", tok.ExpiresAt)
	}
}

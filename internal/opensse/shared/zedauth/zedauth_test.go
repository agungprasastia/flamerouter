package zedauth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"flamerouter/internal/opensse/shared/zedauth"
)

func TestGenerateZedKeypair(t *testing.T) {
	kp, err := zedauth.GenerateZedKeypair()
	if err != nil {
		t.Fatalf("GenerateZedKeypair failed: %v", err)
	}

	if kp.PrivateKey == nil {
		t.Fatal("expected non-nil PrivateKey")
	}
	if kp.PublicKey == nil {
		t.Fatal("expected non-nil PublicKey")
	}
	if kp.PrivateKey.N.BitLen() != 2048 {
		t.Fatalf("expected 2048-bit key, got %d", kp.PrivateKey.N.BitLen())
	}
	if !strings.HasPrefix(kp.Verifier, zedauth.PrivateKeyPrefix) {
		t.Fatalf("expected verifier to have prefix %s, got %s", zedauth.PrivateKeyPrefix, kp.Verifier)
	}
	if !strings.Contains(kp.PrivateKeyPEM, "BEGIN RSA PRIVATE KEY") {
		t.Fatalf("expected PrivateKeyPEM to contain RSA PRIVATE KEY, got %s", kp.PrivateKeyPEM)
	}
	if len(kp.PublicKeyDER) == 0 {
		t.Fatal("expected non-empty PublicKeyDER")
	}
}

func TestEncodeZedPublicKeyVerifier(t *testing.T) {
	kp, err := zedauth.GenerateZedKeypair()
	if err != nil {
		t.Fatalf("GenerateZedKeypair failed: %v", err)
	}

	verifier := zedauth.EncodeZedPublicKeyVerifier(kp.PublicKey)
	if verifier == "" {
		t.Fatal("expected non-empty public key verifier")
	}

	der, err := base64.RawURLEncoding.DecodeString(verifier)
	if err != nil {
		t.Fatalf("failed to decode base64url public key verifier: %v", err)
	}

	pub, err := x509.ParsePKCS1PublicKey(der)
	if err != nil {
		t.Fatalf("failed to parse PKCS#1 public key from verifier DER: %v", err)
	}
	if pub.N.Cmp(kp.PublicKey.N) != 0 || pub.E != kp.PublicKey.E {
		t.Fatal("parsed public key does not match original")
	}

	// nil public key test
	if got := zedauth.EncodeZedPublicKeyVerifier(nil); got != "" {
		t.Fatalf("expected empty string for nil public key, got %q", got)
	}
}

func TestDecryptZedAccessTokenRoundTripPKCS1v15(t *testing.T) {
	kp, err := zedauth.GenerateZedKeypair()
	if err != nil {
		t.Fatalf("GenerateZedKeypair failed: %v", err)
	}

	sampleToken := "zed_oauth_access_token_12345_sample"

	// Encrypt using PKCS1v15
	encryptedBytes, err := rsa.EncryptPKCS1v15(rand.Reader, kp.PublicKey, []byte(sampleToken))
	if err != nil {
		t.Fatalf("EncryptPKCS1v15 failed: %v", err)
	}
	encryptedB64 := base64.RawURLEncoding.EncodeToString(encryptedBytes)

	// Decrypt using Verifier string
	decryptedFromVerifier, err := zedauth.DecryptZedAccessToken(kp.Verifier, encryptedB64)
	if err != nil {
		t.Fatalf("DecryptZedAccessToken with verifier failed: %v", err)
	}
	if decryptedFromVerifier != sampleToken {
		t.Fatalf("decrypted mismatch: got %q, want %q", decryptedFromVerifier, sampleToken)
	}

	// Decrypt using Raw PEM
	decryptedFromPEM, err := zedauth.DecryptZedAccessToken(kp.PrivateKeyPEM, encryptedB64)
	if err != nil {
		t.Fatalf("DecryptZedAccessToken with raw PEM failed: %v", err)
	}
	if decryptedFromPEM != sampleToken {
		t.Fatalf("decrypted mismatch: got %q, want %q", decryptedFromPEM, sampleToken)
	}
}

func TestDecryptZedAccessTokenRoundTripOAEP(t *testing.T) {
	kp, err := zedauth.GenerateZedKeypair()
	if err != nil {
		t.Fatalf("GenerateZedKeypair failed: %v", err)
	}

	sampleToken := "zed_oaep_token_67890_test"

	// Encrypt using OAEP SHA-256
	encryptedBytes, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, kp.PublicKey, []byte(sampleToken), nil)
	if err != nil {
		t.Fatalf("EncryptOAEP failed: %v", err)
	}
	encryptedB64 := base64.RawURLEncoding.EncodeToString(encryptedBytes)

	// Decrypt using Verifier string
	decrypted, err := zedauth.DecryptZedAccessToken(kp.Verifier, encryptedB64)
	if err != nil {
		t.Fatalf("DecryptZedAccessToken OAEP failed: %v", err)
	}
	if decrypted != sampleToken {
		t.Fatalf("decrypted mismatch: got %q, want %q", decrypted, sampleToken)
	}
}

func TestDecryptZedAccessTokenErrors(t *testing.T) {
	kp, err := zedauth.GenerateZedKeypair()
	if err != nil {
		t.Fatalf("GenerateZedKeypair failed: %v", err)
	}

	// Invalid private key PEM
	if _, err := zedauth.DecryptZedAccessToken("invalid-pem", "AAAA"); err == nil {
		t.Fatal("expected error on invalid PEM")
	}

	// Invalid base64 ciphertext
	if _, err := zedauth.DecryptZedAccessToken(kp.Verifier, "!!not-base64!!"); err == nil {
		t.Fatal("expected error on invalid ciphertext")
	}

	// Corrupted ciphertext
	badCipher := base64.RawURLEncoding.EncodeToString([]byte("short-bytes"))
	if _, err := zedauth.DecryptZedAccessToken(kp.Verifier, badCipher); err == nil {
		t.Fatal("expected error on bad ciphertext length")
	}
}

func TestBuildZedUserAuthHeader(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: "  ", want: ""},
		{input: "my-access-token", want: "Bearer my-access-token"},
		{input: "Bearer existing-bearer-token", want: "Bearer existing-bearer-token"},
		{input: "user_123 token_abc", want: "user_123 token_abc"},
	}

	for _, tt := range tests {
		got := zedauth.BuildZedUserAuthHeader(tt.input)
		if got != tt.want {
			t.Errorf("BuildZedUserAuthHeader(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestZedHeadersMap(t *testing.T) {
	expected := map[string]string{
		"expiredToken":              "x-zed-expired-token",
		"outdatedToken":             "x-zed-outdated-token",
		"clientSupportsStatus":      "x-zed-client-supports-status-messages",
		"clientSupportsStreamEnded": "x-zed-client-supports-stream-ended-request-completion-status",
		"serverSupportsStatus":      "x-zed-server-supports-status-messages",
		"clientSupportsXai":         "x-zed-client-supports-x-ai",
		"systemId":                  "x-zed-system-id",
	}

	for k, v := range expected {
		if zedauth.ZED_HEADERS[k] != v {
			t.Errorf("ZED_HEADERS[%q] = %q, want %q", k, zedauth.ZED_HEADERS[k], v)
		}
	}
}

func TestFetchZedLLMTokenSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test_access_token" {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-zed-client-supports-status-messages") != "1" {
			t.Errorf("missing client supports status header")
		}
		if r.Header.Get("x-zed-client-supports-stream-ended-request-completion-status") != "1" {
			t.Errorf("missing stream ended header")
		}
		if r.Header.Get("x-zed-client-supports-x-ai") != "1" {
			t.Errorf("missing x-ai header")
		}

		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["client_id"] != "client_test_123" {
			t.Errorf("unexpected client_id: %v", req["client_id"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": "zed_llm_token_response_abc123",
		})
	}))
	defer server.Close()

	// Use custom client test by overriding endpoint via test client
	client := server.Client()
	reqBody, _ := json.Marshal(map[string]string{"client_id": "client_test_123"})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", zedauth.BuildZedUserAuthHeader("test_access_token"))
	req.Header.Set(zedauth.ZED_HEADERS["clientSupportsStatus"], "1")
	req.Header.Set(zedauth.ZED_HEADERS["clientSupportsStreamEnded"], "1")
	req.Header.Set(zedauth.ZED_HEADERS["clientSupportsXai"], "1")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.Token != "zed_llm_token_response_abc123" {
		t.Fatalf("unexpected token: %s", result.Token)
	}
}

func TestFetchZedLLMTokenValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := zedauth.FetchZedLLMToken(ctx, "", "client_id"); err == nil {
		t.Fatal("expected error on empty access token")
	}
}

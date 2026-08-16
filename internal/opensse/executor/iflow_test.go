package executor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestIFlowSignature(t *testing.T) {
	sig := createIFlowSignature("iFlow-Cli", "session-1", 1000, "key")
	mac := hmac.New(sha256.New, []byte("key"))
	mac.Write([]byte("iFlow-Cli:session-1:1000"))

	want := hex.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Fatalf("%s != %s", sig, want)
	}
}

func TestIFlowSignatureEmptyKey(t *testing.T) {
	if got := createIFlowSignature("iFlow-Cli", "session-1", 1000, ""); got != "" {
		t.Fatalf("empty key want empty sig, got %q", got)
	}
}

func TestIFlowBearerAccessTokenFallback(t *testing.T) {
	e := &IFlowExecutor{Base: Base{Provider: "iflow", Headers: map[string]string{"User-Agent": "iFlow-Cli"}}}

	h := e.buildHeaders(Credentials{AccessToken: "tok-only"}, false)
	if got := h.Get("Authorization"); got != "Bearer tok-only" {
		t.Fatalf("Authorization=%q want Bearer tok-only", got)
	}

	if h.Get("x-iflow-signature") == "" {
		t.Fatal("signature empty with AccessToken-only cred")
	}
}

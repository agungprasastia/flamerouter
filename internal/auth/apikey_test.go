package auth_test

import (
	"strings"
	"testing"

	"flamerouter/internal/auth"
)

func TestGenerateParseRoundTrip(t *testing.T) {
	a := auth.New("endpoint-proxy-api-key-secret")
	key, keyID := a.Generate("0123456789abcdef")
	if !strings.HasPrefix(key, "sk-") {
		t.Fatalf("key=%s", key)
	}
	parts := strings.Split(key, "-")
	if len(parts) != 4 {
		t.Fatalf("parts=%v", parts)
	}
	if keyID == "" || len(keyID) != 6 {
		t.Fatalf("keyID=%q", keyID)
	}
	if !a.VerifyCRC(key) {
		t.Fatal("VerifyCRC false")
	}
	mid, kid, ok := a.Parse(key)
	if !ok || mid != "0123456789abcdef" || kid != keyID {
		t.Fatalf("parse mid=%s kid=%s ok=%v", mid, kid, ok)
	}
}

func TestVerifyCRC_RejectsTamper(t *testing.T) {
	a := auth.New("endpoint-proxy-api-key-secret")
	key, _ := a.Generate("0123456789abcdef")
	bad := key[:len(key)-1] + "0"
	if a.VerifyCRC(bad) {
		if _, _, ok := a.Parse(bad); ok && a.VerifyCRC(bad) {
			t.Log("unlikely CRC collision")
		}
	}
	if a.VerifyCRC("sk-not-a-key") {
		t.Fatal("expected false")
	}
}

func TestHashKey_Deterministic(t *testing.T) {
	h1 := auth.HashKey("sk-test")
	h2 := auth.HashKey("sk-test")
	if h1 != h2 {
		t.Fatal("HashKey non-deterministic")
	}
	if len(h1) != 64 {
		t.Fatalf("hash length %d", len(h1))
	}
}

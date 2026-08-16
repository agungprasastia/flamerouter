package auth

import (
	"testing"
	"time"
)

func TestJWT_GenerateAndValidate(t *testing.T) {
	mgr := NewJWTManager("test-secret-key")

	token, err := mgr.Generate(map[string]any{"sub": "admin"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	claims, err := mgr.Validate(token)
	if err != nil {
		t.Fatal(err)
	}

	if claims["sub"] != "admin" {
		t.Fatalf("expected sub=admin, got %v", claims["sub"])
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	mgr := NewJWTManager("test-secret-key")
	token, _ := mgr.Generate(map[string]any{"sub": "admin"}, -time.Hour)

	_, err := mgr.Validate(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestJWT_InvalidSignature(t *testing.T) {
	mgr := NewJWTManager("test-secret-key")
	token, _ := mgr.Generate(map[string]any{"sub": "admin"}, time.Hour)
	token = token[:len(token)-5] + "XXXXX"

	_, err := mgr.Validate(token)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

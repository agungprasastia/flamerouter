package oauth

import (
	"context"
	"testing"
)

func TestRegisterAndPollProxySession(t *testing.T) {
	ok := RegisterProxySession("codex", "st1", "cv1", "http://127.0.0.1:1455/auth/callback")
	if !ok {
		t.Fatal("register failed")
	}

	st := GetProxySessionStatus("codex", "st1")
	if st == nil || st["status"] != "pending" {
		t.Fatalf("status=%v", st)
	}

	ClearProxySession("codex", "st1")

	if GetProxySessionStatus("codex", "st1") != nil {
		t.Fatal("expected cleared")
	}
}

func TestCompleteXaiManualCode_NoSession(t *testing.T) {
	h := NewHandler()

	_, err := CompleteXaiManualCode(context.Background(), h, nil, "code", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProxyForOnlyCodexXai(t *testing.T) {
	if proxyFor("claude") != nil {
		t.Fatal("claude must not have proxy")
	}

	if proxyFor("codex") == nil || proxyFor("xai") == nil {
		t.Fatal("codex/xai need proxy")
	}
}

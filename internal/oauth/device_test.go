package oauth

import (
	"context"
	"testing"
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

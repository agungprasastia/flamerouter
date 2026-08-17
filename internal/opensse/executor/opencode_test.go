package executor

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestOpenCodeExecutorLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	ex := GetExecutor("opencode")
	if ex == nil {
		t.Fatal("opencode executor not found")
	}

	body, err := json.Marshal(map[string]any{
		"model": "laguna-s-2.1-free",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	cred := Credentials{
		ProviderSpecificData: nil,
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProjectID:            "",
	}

	res, err := ex.Execute(context.Background(), cred, "laguna-s-2.1-free", body, false)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			_ = err
		}
	}()

	respBytes, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		_ = readErr
	}

	t.Logf("Status: %d, Response: %s", res.StatusCode, string(respBytes))

	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

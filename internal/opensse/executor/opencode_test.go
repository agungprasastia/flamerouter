package executor

import (
	"context"
	"encoding/json"
	"io"
	"testing"
)

func TestOpenCodeExecutorLive(t *testing.T) {
	ex := GetExecutor("opencode")
	if ex == nil {
		t.Fatal("opencode executor not found")
	}

	body, _ := json.Marshal(map[string]any{
		"model": "laguna-s-2.1-free",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})

	res, err := ex.Execute(context.Background(), Credentials{}, "laguna-s-2.1-free", body, false)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	defer res.Body.Close()

	respBytes, _ := io.ReadAll(res.Body)
	t.Logf("Status: %d, Response: %s", res.StatusCode, string(respBytes))

	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}

package executor_test

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func makeDevinMockScript(t *testing.T, tmpDir, name, delta string) string {
	t.Helper()

	var (
		scriptName    string
		scriptContent string
	)

	if runtime.GOOS == "windows" {
		scriptName = filepath.Join(tmpDir, name+".bat")
		scriptContent = fmt.Sprintf(`@echo off
echo {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}
echo {"jsonrpc":"2.0","id":2,"result":{"sessionId":"mock-devin-ses-1"}}
echo {"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"%s"}}}
echo {"jsonrpc":"2.0","method":"_cognition.ai/agent_stopped","params":{"cause":"done"}}
`, delta)
	} else {
		scriptName = filepath.Join(tmpDir, name+".sh")
		scriptContent = fmt.Sprintf(`#!/bin/sh
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"mock-devin-ses-1"}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"%s"}}}'
echo '{"jsonrpc":"2.0","method":"_cognition.ai/agent_stopped","params":{"cause":"done"}}'
`, delta)
	}

	// #nosec G306 -- mock executable script created in t.TempDir() requires execution permission
	if err := os.WriteFile(scriptName, []byte(scriptContent), 0o700); err != nil {
		t.Fatal(err)
	}

	return scriptName
}

func TestDevinCliExecutor_PromptBuilderAndExecutionMock(t *testing.T) {
	scriptName := makeDevinMockScript(t, t.TempDir(), "devin", "Hello from Devin CLI")
	t.Setenv("CLI_DEVIN_BIN", scriptName)

	ex := executor.GetExecutor("devin-cli")
	if ex == nil {
		t.Fatal("devin-cli executor not registered")
	}

	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]any{
			{"role": "user", "content": "solve this task"},
		},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProviderSpecificData: nil,
		ProjectID:            "",
	}, "swe-1.6", body, true)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }() // nolint:errcheck

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	outBytes, _ := io.ReadAll(res.Body) // nolint:errcheck
	outStr := string(outBytes)

	if !strings.Contains(outStr, "Hello from Devin CLI") {
		t.Errorf("stream missing text delta: %s", outStr)
	}
}

func TestDevinCliExecutor_NonStreaming(t *testing.T) {
	scriptName := makeDevinMockScript(t, t.TempDir(), "devin_nonstream", "Non-streaming result")
	t.Setenv("CLI_DEVIN_BIN", scriptName)

	ex := executor.GetExecutor("devin-cli")
	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]any{
			{"role": "user", "content": "solve this task"},
		},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProviderSpecificData: nil,
		ProjectID:            "",
	}, "swe-1.6", body, false)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }() // nolint:errcheck

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m) // nolint:errcheck

	choices, ok := m["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("expected choices in non-streaming response: %v", m)
	}

	first, ok := choices[0].(map[string]any)
	if !ok {
		t.Fatal("first choice not map")
	}

	msg, ok := first["message"].(map[string]any)
	if !ok || msg["content"] != "Non-streaming result" {
		t.Errorf("content = %v, want Non-streaming result", msg["content"])
	}
}

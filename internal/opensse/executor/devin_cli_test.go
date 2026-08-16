package executor_test

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDevinCliExecutor_PromptBuilderAndExecutionMock(t *testing.T) {
	tmpDir := t.TempDir()

	var scriptName string

	var scriptContent string

	if runtime.GOOS == "windows" {
		scriptName = filepath.Join(tmpDir, "devin.bat")
		scriptContent = `@echo off
echo {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}
echo {"jsonrpc":"2.0","id":2,"result":{"sessionId":"mock-devin-ses-1"}}
echo {"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"Hello from Devin CLI"}}}
echo {"jsonrpc":"2.0","method":"_cognition.ai/agent_stopped","params":{"cause":"done"}}
`
	} else {
		scriptName = filepath.Join(tmpDir, "devin.sh")
		scriptContent = `#!/bin/sh
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"mock-devin-ses-1"}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"Hello from Devin CLI"}}}'
echo '{"jsonrpc":"2.0","method":"_cognition.ai/agent_stopped","params":{"cause":"done"}}'
`
	}

	if err := os.WriteFile(scriptName, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLI_DEVIN_BIN", scriptName)

	ex := executor.GetExecutor("devin-cli")
	if ex == nil {
		t.Fatal("devin-cli executor not registered")
	}

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "solve this task"},
		},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{}, "swe-1.6", body, true)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	outBytes, _ := io.ReadAll(res.Body)
	outStr := string(outBytes)

	if !strings.Contains(outStr, "Hello from Devin CLI") {
		t.Errorf("stream missing text delta: %s", outStr)
	}
}

func TestDevinCliExecutor_NonStreaming(t *testing.T) {
	tmpDir := t.TempDir()

	var scriptName string

	var scriptContent string

	if runtime.GOOS == "windows" {
		scriptName = filepath.Join(tmpDir, "devin_nonstream.bat")
		scriptContent = `@echo off
echo {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}
echo {"jsonrpc":"2.0","id":2,"result":{"sessionId":"mock-devin-ses-2"}}
echo {"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"Non-streaming result"}}}
echo {"jsonrpc":"2.0","method":"_cognition.ai/agent_stopped","params":{"cause":"done"}}
`
	} else {
		scriptName = filepath.Join(tmpDir, "devin_nonstream.sh")
		scriptContent = `#!/bin/sh
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"0.3"}}'
echo '{"jsonrpc":"2.0","id":2,"result":{"sessionId":"mock-devin-ses-2"}}'
echo '{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_message_chunk","content":"Non-streaming result"}}}'
echo '{"jsonrpc":"2.0","method":"_cognition.ai/agent_stopped","params":{"cause":"done"}}'
`
	}

	if err := os.WriteFile(scriptName, []byte(scriptContent), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLI_DEVIN_BIN", scriptName)

	ex := executor.GetExecutor("devin-cli")
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{
			{"role": "user", "content": "quick test"},
		},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{}, "swe-1.6", body, false)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var respObj map[string]any
	if err := json.NewDecoder(res.Body).Decode(&respObj); err != nil {
		t.Fatal(err)
	}

	choices, _ := respObj["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("empty choices")
	}

	first, _ := choices[0].(map[string]any)

	msg, _ := first["message"].(map[string]any)
	if msg["content"] != "Non-streaming result" {
		t.Errorf("content = %v, want Non-streaming result", msg["content"])
	}
}

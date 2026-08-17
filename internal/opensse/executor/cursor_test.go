package executor_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func writeVarintField(buf *bytes.Buffer, fieldNum uint32, data []byte) {
	tag := (uint64(fieldNum) << 3) | 2

	var tagBuf [10]byte
	n := binary.PutUvarint(tagBuf[:], tag)
	buf.Write(tagBuf[:n])

	var lenBuf [10]byte
	ln := binary.PutUvarint(lenBuf[:], uint64(len(data)))
	buf.Write(lenBuf[:ln])
	buf.Write(data)
}

func buildMockCursorResponseFrame(text, thinking string) []byte {
	var respBuf bytes.Buffer

	if text != "" {
		writeVarintField(&respBuf, 1, []byte(text))
	}

	if thinking != "" {
		var thBuf bytes.Buffer

		writeVarintField(&thBuf, 1, []byte(thinking))
		writeVarintField(&respBuf, 25, thBuf.Bytes())
	}

	var topBuf bytes.Buffer

	writeVarintField(&topBuf, 2, respBuf.Bytes())

	return executor.WrapConnectRPCFrame(topBuf.Bytes())
}

type cursorTestState struct {
	gotChecksum    string
	gotAuth        string
	gotContentType string
	gotBody        []byte
}

func verifyCursorStreamHeaders(t *testing.T, s *cursorTestState) {
	t.Helper()

	if s.gotChecksum == "" {
		t.Error("missing x-cursor-checksum header")
	}

	if s.gotAuth != "Bearer user-token-123456" {
		t.Errorf("got auth %q, want Bearer user-token-123456", s.gotAuth)
	}

	if s.gotContentType != "application/connect+proto" {
		t.Errorf("content-type = %q", s.gotContentType)
	}

	if len(s.gotBody) < 5 {
		t.Errorf("request body too short: %d bytes", len(s.gotBody))
	}
}

func verifyCursorStreamOutput(t *testing.T, body io.Reader) {
	t.Helper()

	outBytes, _ := io.ReadAll(body) // nolint:errcheck
	outStr := string(outBytes)

	if !strings.Contains(outStr, "Hello from Cursor Protobuf") {
		t.Errorf("stream missing text: %s", outStr)
	}

	if !strings.Contains(outStr, "Thinking through solution") {
		t.Errorf("stream missing thinking: %s", outStr)
	}

	if !strings.Contains(outStr, "[DONE]") {
		t.Errorf("stream missing [DONE]: %s", outStr)
	}
}

func TestCursorExecutor_ProtobufAndChecksumExecution(t *testing.T) {
	var state cursorTestState

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.gotChecksum = r.Header.Get("x-cursor-checksum")
		state.gotAuth = r.Header.Get("authorization")
		state.gotContentType = r.Header.Get("content-type")

		defer func() {
			_ = r.Body.Close() // nolint:errcheck
		}()

		state.gotBody, _ = io.ReadAll(r.Body) // nolint:errcheck

		w.Header().Set("Content-Type", "application/connect+proto")

		frame := buildMockCursorResponseFrame("Hello from Cursor Protobuf", "Thinking through solution")
		_, _ = w.Write(frame) // nolint:errcheck
	}))
	defer srv.Close()

	ex := executor.GetExecutor("cursor")
	if ex == nil {
		t.Fatal("cursor executor not registered")
	}

	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "hello world"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "user-token-123456",
		BaseURL:     srv.URL,
		ProviderSpecificData: map[string]any{
			"machineId": "test-machine-id",
		},
		APIKey:       "",
		RefreshToken: "",
		ProjectID:    "",
	}, "claude-4.5-sonnet", body, true)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = res.Body.Close() // nolint:errcheck
	}()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	verifyCursorStreamHeaders(t, &state)
	verifyCursorStreamOutput(t, res.Body)
}

func TestCursorExecutor_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/connect+proto")

		frame := buildMockCursorResponseFrame("Unary response text", "")
		_, _ = w.Write(frame) // nolint:errcheck
	}))
	defer srv.Close()

	ex := executor.GetExecutor("cursor")

	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken:          "token-1",
		BaseURL:              srv.URL,
		ProviderSpecificData: nil,
		APIKey:               "",
		RefreshToken:         "",
		ProjectID:            "",
	}, "gpt-5.2-codex", body, false)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = res.Body.Close() // nolint:errcheck
	}()

	var respObj map[string]any
	if err := json.NewDecoder(res.Body).Decode(&respObj); err != nil {
		t.Fatal(err)
	}

	choices, okChoices := respObj["choices"].([]any)
	if !okChoices || len(choices) == 0 {
		t.Fatal("empty choices")
	}

	first, okFirst := choices[0].(map[string]any)
	if !okFirst {
		t.Fatal("invalid choice element")
	}

	msg, okMsg := first["message"].(map[string]any)
	if !okMsg || msg["content"] != "Unary response text" {
		t.Errorf("content = %v, want Unary response text", msg["content"])
	}
}

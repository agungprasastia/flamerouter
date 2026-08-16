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

func buildMockCursorResponseFrame(text, thinking string) []byte {
	var respBuf bytes.Buffer

	if text != "" {
		// Field 1: text
		tag := uint64((1 << 3) | 2)

		var tagBuf [10]byte
		n := binary.PutUvarint(tagBuf[:], tag)
		respBuf.Write(tagBuf[:n])

		var lenBuf [10]byte
		ln := binary.PutUvarint(lenBuf[:], uint64(len(text)))
		respBuf.Write(lenBuf[:ln])
		respBuf.WriteString(text)
	}

	if thinking != "" {
		// Field 25: thinking -> Field 1 text
		var thBuf bytes.Buffer

		tag1 := uint64((1 << 3) | 2)

		var tag1Buf [10]byte
		n1 := binary.PutUvarint(tag1Buf[:], tag1)
		thBuf.Write(tag1Buf[:n1])

		var len1Buf [10]byte
		ln1 := binary.PutUvarint(len1Buf[:], uint64(len(thinking)))
		thBuf.Write(len1Buf[:ln1])
		thBuf.WriteString(thinking)

		tag25 := uint64((25 << 3) | 2)

		var tag25Buf [10]byte
		n25 := binary.PutUvarint(tag25Buf[:], tag25)
		respBuf.Write(tag25Buf[:n25])

		var len25Buf [10]byte
		ln25 := binary.PutUvarint(len25Buf[:], uint64(thBuf.Len()))
		respBuf.Write(len25Buf[:ln25])
		respBuf.Write(thBuf.Bytes())
	}

	// Wrap in top response: Field 2 (StreamUnifiedChatResponse)
	var topBuf bytes.Buffer

	tagTop := uint64((2 << 3) | 2)

	var tagTopBuf [10]byte
	nt := binary.PutUvarint(tagTopBuf[:], tagTop)
	topBuf.Write(tagTopBuf[:nt])

	var lenTopBuf [10]byte
	lnt := binary.PutUvarint(lenTopBuf[:], uint64(respBuf.Len()))
	topBuf.Write(lenTopBuf[:lnt])
	topBuf.Write(respBuf.Bytes())

	return executor.WrapConnectRPCFrame(topBuf.Bytes())
}

func TestCursorExecutor_ProtobufAndChecksumExecution(t *testing.T) {
	var gotChecksum string

	var gotAuth string

	var gotContentType string

	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotChecksum = r.Header.Get("x-cursor-checksum")
		gotAuth = r.Header.Get("authorization")
		gotContentType = r.Header.Get("content-type")

		defer r.Body.Close()
		gotBody, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/connect+proto")

		frame := buildMockCursorResponseFrame("Hello from Cursor Protobuf", "Thinking through solution")
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	ex := executor.GetExecutor("cursor")
	if ex == nil {
		t.Fatal("cursor executor not registered")
	}

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello world"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "user-token-123456",
		BaseURL:     srv.URL,
		ProviderSpecificData: map[string]any{
			"machineId": "test-machine-id",
		},
	}, "claude-4.5-sonnet", body, true)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	if gotChecksum == "" {
		t.Error("missing x-cursor-checksum header")
	}

	if gotAuth != "Bearer user-token-123456" {
		t.Errorf("got auth %q, want Bearer user-token-123456", gotAuth)
	}

	if gotContentType != "application/connect+proto" {
		t.Errorf("content-type = %q", gotContentType)
	}

	if len(gotBody) < 5 {
		t.Errorf("request body too short: %d bytes", len(gotBody))
	}

	outBytes, _ := io.ReadAll(res.Body)

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

func TestCursorExecutor_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/connect+proto")

		frame := buildMockCursorResponseFrame("Unary response text", "")
		_, _ = w.Write(frame)
	}))
	defer srv.Close()

	ex := executor.GetExecutor("cursor")
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "token-1",
		BaseURL:     srv.URL,
	}, "gpt-5.2-codex", body, false)
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
	if msg["content"] != "Unary response text" {
		t.Errorf("content = %v, want Unary response text", msg["content"])
	}
}

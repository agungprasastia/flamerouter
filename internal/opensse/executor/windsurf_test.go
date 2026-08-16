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

func buildMockWindsurfContentFrame(text string) []byte {
	// ContentChunk: Field 1 = nested { Field 1 = text }
	var inner bytes.Buffer

	tag1 := uint64((1 << 3) | 2)

	var tag1Buf [10]byte
	n1 := binary.PutUvarint(tag1Buf[:], tag1)
	inner.Write(tag1Buf[:n1])

	var len1Buf [10]byte
	ln1 := binary.PutUvarint(len1Buf[:], uint64(len(text)))
	inner.Write(len1Buf[:ln1])
	inner.WriteString(text)

	var outer bytes.Buffer

	tagOuter := uint64((1 << 3) | 2)

	var tagOBuf [10]byte
	no := binary.PutUvarint(tagOBuf[:], tagOuter)
	outer.Write(tagOBuf[:no])

	var lenOBuf [10]byte
	lno := binary.PutUvarint(lenOBuf[:], uint64(inner.Len()))
	outer.Write(lenOBuf[:lno])
	outer.Write(inner.Bytes())

	return executor.WrapConnectRPCFrame(outer.Bytes())
}

func buildMockWindsurfDoneFrame(promptTokens, completionTokens int) []byte {
	// UsageStats: Field 1 = pt (varint), Field 2 = ct (varint)
	var usageBuf bytes.Buffer

	tag1 := uint64(1 << 3)

	var t1 [10]byte
	n1 := binary.PutUvarint(t1[:], tag1)
	usageBuf.Write(t1[:n1])

	var v1 [10]byte
	vn1 := binary.PutUvarint(v1[:], uint64(promptTokens))
	usageBuf.Write(v1[:vn1])

	tag2 := uint64(2 << 3)

	var t2 [10]byte
	n2 := binary.PutUvarint(t2[:], tag2)
	usageBuf.Write(t2[:n2])

	var v2 [10]byte
	vn2 := binary.PutUvarint(v2[:], uint64(completionTokens))
	usageBuf.Write(v2[:vn2])

	// DoneChunk: Field 1 = UsageStats
	var doneInner bytes.Buffer

	tagDone1 := uint64((1 << 3) | 2)

	var td1 [10]byte
	nd1 := binary.PutUvarint(td1[:], tagDone1)
	doneInner.Write(td1[:nd1])

	var ld1 [10]byte
	lnd1 := binary.PutUvarint(ld1[:], uint64(usageBuf.Len()))
	doneInner.Write(ld1[:lnd1])
	doneInner.Write(usageBuf.Bytes())

	// Top CompletionChunk: Field 3 = DoneChunk
	var outer bytes.Buffer

	tagOuter := uint64((3 << 3) | 2)

	var tagOBuf [10]byte
	no := binary.PutUvarint(tagOBuf[:], tagOuter)
	outer.Write(tagOBuf[:no])

	var lenOBuf [10]byte
	lno := binary.PutUvarint(lenOBuf[:], uint64(doneInner.Len()))
	outer.Write(lenOBuf[:lno])
	outer.Write(doneInner.Bytes())

	return executor.WrapConnectRPCFrame(outer.Bytes())
}

func TestWindsurfExecutor_ExecutionAndStreaming(t *testing.T) {
	var gotAuth string

	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")

		w.Header().Set("Content-Type", "application/grpc-web+proto")
		_, _ = w.Write(buildMockWindsurfContentFrame("Hello from Windsurf"))
		_, _ = w.Write(buildMockWindsurfDoneFrame(12, 8))
	}))
	defer srv.Close()

	ex := executor.GetExecutor("windsurf")
	if ex == nil {
		t.Fatal("windsurf executor not registered")
	}

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "write code"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "sk-ws-token-12345",
		BaseURL:     srv.URL,
	}, "swe-1.6", body, true)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	if gotAuth != "Bearer sk-ws-token-12345" {
		t.Errorf("got auth %q", gotAuth)
	}

	if gotContentType != "application/grpc-web+proto" {
		t.Errorf("got content-type %q", gotContentType)
	}

	outBytes, _ := io.ReadAll(res.Body)

	outStr := string(outBytes)
	if !strings.Contains(outStr, "Hello from Windsurf") {
		t.Errorf("stream missing content: %s", outStr)
	}

	if !strings.Contains(outStr, "[DONE]") {
		t.Errorf("stream missing [DONE]: %s", outStr)
	}
}

func TestWindsurfExecutor_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/grpc-web+proto")
		_, _ = w.Write(buildMockWindsurfContentFrame("Unary code result"))
		_, _ = w.Write(buildMockWindsurfDoneFrame(5, 5))
	}))
	defer srv.Close()

	ex := executor.GetExecutor("windsurf")
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "test"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "sk-ws-test",
		BaseURL:     srv.URL,
	}, "claude-sonnet-4.6", body, false)
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
	if msg["content"] != "Unary code result" {
		t.Errorf("content = %v, want Unary code result", msg["content"])
	}
}

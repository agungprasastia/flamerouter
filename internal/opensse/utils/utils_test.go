package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetectClient(t *testing.T) {
	cases := []struct {
		ua      string
		headers map[string]string
		want    string
	}{
		{"claude-code/1.0", nil, "claude-code"},
		{"claude-cli/2.0", nil, "claude-code"},
		{"Cursor/1.0", nil, "cursor"},
		{"codex-cli/0.1", nil, "codex"},
		{"opencode/1", nil, "opencode"},
		{"kiro-ide/1", nil, "kiro"},
		{"gemini-cli/1", nil, "gemini-cli"},
		{"GitHubCopilotChat/1", nil, "github-copilot"},
		{"deepseek-tui/1", nil, "deepseek-tui"},
		{"Mozilla/5.0", map[string]string{"x-stainless-os": "Windows"}, "codex"},
		{"Mozilla/5.0", map[string]string{"x-app": "cli"}, "claude-code"},
		{"unknown-agent/1", nil, ""},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set("User-Agent", tc.ua)

		for k, v := range tc.headers {
			r.Header.Set(k, v)
		}

		got := DetectClient(r)
		if got != tc.want {
			t.Fatalf("ua=%q headers=%v: got %q want %q", tc.ua, tc.headers, got, tc.want)
		}
	}
}

func TestShouldPassthrough(t *testing.T) {
	if !ShouldPassthrough("claude-code", "claude") {
		t.Fatal("claude-code→claude")
	}

	if !ShouldPassthrough("claude-code", "anthropic-compatible-foo") {
		t.Fatal("claude-code→anthropic-compatible")
	}

	if !ShouldPassthrough("codex", "codex") {
		t.Fatal("codex→codex")
	}

	if ShouldPassthrough("cursor", "claude") {
		t.Fatal("cursor→claude should be false")
	}

	if ShouldPassthrough("", "claude") {
		t.Fatal("empty client")
	}
}

func assertClaudeBypasses(t *testing.T, warmup, count, title, naming []byte) {
	t.Helper()

	if !ShouldBypass(warmup, "claude-code") {
		t.Fatal("Warmup + claude-code")
	}

	if !ShouldBypass(count, "claude-code") {
		t.Fatal("count + claude-code")
	}

	if !ShouldBypass(title, "claude-code") {
		t.Fatal("skip pattern + claude-code")
	}

	if !ShouldBypass(naming, "claude-code") {
		t.Fatal("isNewTopic + claude-code")
	}

	if !ShouldBypass(warmup, "claude") {
		t.Fatal("Warmup + claude")
	}
}

func assertNonClaudeNeverBypasses(t *testing.T, warmup, count, title, naming, hi []byte) {
	t.Helper()

	for _, client := range []string{"cursor", "codex", "opencode", "github-copilot", ""} {
		if ShouldBypass(warmup, client) {
			t.Fatalf("Warmup must not bypass for client=%q", client)
		}

		if ShouldBypass(count, client) {
			t.Fatalf("count must not bypass for client=%q", client)
		}

		if ShouldBypass(title, client) {
			t.Fatalf("title must not bypass for client=%q", client)
		}

		if ShouldBypass(naming, client) {
			t.Fatalf("isNewTopic must not bypass for client=%q", client)
		}

		if ShouldBypass(hi, client) {
			t.Fatalf("hi must not bypass for client=%q", client)
		}
	}
}

func TestShouldBypass(t *testing.T) {
	warmup := []byte(`{"messages":[{"role":"user","content":"Warmup"}]}`)
	count := []byte(`{"messages":[{"role":"user","content":"count"}]}`)
	hi := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	title := []byte(`{"messages":[{"role":"user","content":"Please write a 5-10 word title for the following conversation: hello"}]}`)
	naming := []byte(`{"system":"isNewTopic true","messages":[{"role":"user","content":"hello world"}]}`)
	long := []byte(`{"messages":[{"role":"user","content":"` + strings.Repeat("x", 80) + `"}]}`)

	assertClaudeBypasses(t, warmup, count, title, naming)

	if ShouldBypass(hi, "claude-code") {
		t.Fatal("short hi must not bypass")
	}

	if ShouldBypass(long, "claude-code") {
		t.Fatal("long message should not bypass")
	}

	if ShouldBypass([]byte(`not-json`), "claude-code") {
		t.Fatal("bad json")
	}

	assertNonClaudeNeverBypasses(t, warmup, count, title, naming, hi)
}

func TestDetectClientCopilotNotInitiatorAlone(t *testing.T) {
	r, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	r.Header.Set("User-Agent", "Mozilla/5.0")
	r.Header.Set("x-initiator", "user")

	if got := DetectClient(r); got == "github-copilot" {
		t.Fatalf("x-initiator alone must not be github-copilot, got %q", got)
	}

	r2, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", nil)
	if err != nil {
		t.Fatal(err)
	}

	r2.Header.Set("User-Agent", "GitHubCopilotChat/1")

	if got := DetectClient(r2); got != "github-copilot" {
		t.Fatalf("copilot UA: got %q", got)
	}
}

func testDedupeToolsRules(t *testing.T) {
	t.Helper()

	body := []byte(`{"tools":[
		{"type":"function","function":{"name":"WebSearch"}},
		{"type":"function","function":{"name":"mcp__exa__web_search_exa"}},
		{"type":"function","function":{"name":"WebFetch"}},
		{"type":"function","function":{"name":"other"}}
	]}`)
	out := DedupeTools(body)

	var req map[string]any
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}

	tools, ok := req["tools"].([]any)
	if !ok {
		t.Fatal("missing tools in response")
	}

	names := map[string]bool{}
	for _, t0 := range tools {
		names[toolName(t0)] = true
	}

	if names["WebSearch"] || names["WebFetch"] {
		t.Fatalf("built-ins should be stripped: %v", names)
	}

	if !names["mcp__exa__web_search_exa"] || !names["other"] {
		t.Fatalf("mcp+other kept: %v", names)
	}
}

func testDedupeToolsExactName(t *testing.T) {
	t.Helper()

	dup := []byte(`{"tools":[
		{"function":{"name":"foo"}},
		{"function":{"name":"foo"}}
	]}`)
	out2 := DedupeTools(dup)

	var req2 map[string]any
	if err := json.Unmarshal(out2, &req2); err != nil {
		t.Fatal(err)
	}

	tArr, ok := req2["tools"].([]any)
	if !ok || len(tArr) != 1 {
		t.Fatalf("want 1 tool after name dedupe, got %v", req2["tools"])
	}
}

func TestDedupeTools(t *testing.T) {
	testDedupeToolsRules(t)
	testDedupeToolsExactName(t)
}

func TestInjectReasoning(t *testing.T) {
	chunk := []byte(`{"choices":[{"delta":{"content":"hi"}}]}`)
	out := InjectReasoning(chunk, "think")

	var c map[string]any

	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatal(err)
	}

	chList, ok := c["choices"].([]any)
	if !ok || len(chList) == 0 {
		t.Fatal("empty choices")
	}

	ch, ok := chList[0].(map[string]any)
	if !ok {
		t.Fatal("invalid choice map")
	}

	delta, ok := ch["delta"].(map[string]any)
	if !ok {
		t.Fatal("missing delta map")
	}

	if delta["reasoning_content"] != "think" {
		t.Fatalf("got %v", delta)
	}

	if delta["content"] != "hi" {
		t.Fatalf("content lost: %v", delta)
	}

	if string(InjectReasoning(chunk, "")) != string(chunk) {
		t.Fatal("empty reasoning should passthrough")
	}
}

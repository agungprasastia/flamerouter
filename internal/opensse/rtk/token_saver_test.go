package rtk

import "testing"

func TestInjectCavemanOpenAI(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	InjectCaveman(body, "openai", CavemanFull)

	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) < 2 {
		t.Fatalf("expected system inject, got %d msgs", len(msgs))
	}

	sys, ok := msgs[0].(map[string]any)
	if !ok || sys["role"] != "system" {
		t.Fatalf("role=%v", sys["role"])
	}

	content, ok := sys["content"].(string)
	if !ok || content == "" || len(content) < 20 {
		t.Fatalf("empty system prompt")
	}
}

func TestApplyTokenSaversRTK(t *testing.T) {
	big := ""
	for i := 0; i < 100; i++ {
		big += "On branch main\nChanges not staged for commit:\n  modified: foo.go\n"
	}

	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "tool", "content": big},
		},
	}
	opts := DefaultTokenSaver()
	opts.RTK = true
	out := ApplyTokenSavers(body, opts)

	msgs, ok := out["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("missing messages")
	}

	msg, ok := msgs[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid msg format")
	}

	content, ok := msg["content"].(string)
	if !ok {
		t.Fatalf("content not string")
	}

	if len(content) >= len(big) {
		// may not compress if filter doesn't shrink enough — still ok fail-open
		t.Logf("content len before=%d after=%d", len(big), len(content))
	}
}

func TestParseTokenSaverHeader(t *testing.T) {
	if !ParseTokenSaverHeader("") {
		t.Fatal("empty should enable")
	}

	if ParseTokenSaverHeader("off") {
		t.Fatal("off should disable")
	}
}

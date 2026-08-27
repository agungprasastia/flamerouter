package combo

import (
	"reflect"
	"testing"
)

func TestStripHistoryForContext_NilOrEmpty(t *testing.T) {
	// Nil body
	if res := StripHistoryForContext(nil, 1000); res != nil {
		t.Fatalf("expected nil for nil body, got %v", res)
	}

	// No matching messages key
	bodyNoKey := map[string]any{"other": "data"}
	if res := StripHistoryForContext(bodyNoKey, 1000); !reflect.DeepEqual(res, bodyNoKey) {
		t.Fatalf("expected original body returned when key missing")
	}

	// Key is not []any
	bodyInvalidKey := map[string]any{"messages": "not-a-slice"}
	if res := StripHistoryForContext(bodyInvalidKey, 1000); !reflect.DeepEqual(res, bodyInvalidKey) {
		t.Fatalf("expected original body returned when messages key is not []any")
	}

	// Empty messages array
	bodyEmpty := map[string]any{"messages": []any{}}
	if res := StripHistoryForContext(bodyEmpty, 1000); !reflect.DeepEqual(res, bodyEmpty) {
		t.Fatalf("expected original body returned when messages array is empty")
	}
}

func TestStripHistoryForContext_KeysAndRoles(t *testing.T) {
	// Test matching different payload keys ("messages", "input", "contents")
	// Test developer role as system and model role as assistant
	keys := []string{"messages", "input", "contents"}

	for _, k := range keys {
		t.Run("key_"+k, func(t *testing.T) {
			body := map[string]any{
				k: []any{
					map[string]any{"role": "developer", "content": "system instruction"},
					map[string]any{"role": "user", "content": "turn 1 user question that is quite long"},
					map[string]any{"role": "model", "content": "turn 1 assistant response"},
					map[string]any{"role": "user", "content": "turn 2 user final prompt"},
				},
			}

			// Tiny context window triggers trimming of older turn 1 user
			stripped := StripHistoryForContext(body, 10)

			msgs, ok := stripped[k].([]any)
			if !ok {
				t.Fatalf("expected []any slice under key %s", k)
			}

			// System developer msg should remain, older head turned off, tail preserved
			if len(msgs) < 2 {
				t.Fatalf("expected at least 2 msgs after stripping, got %d", len(msgs))
			}

			firstRole := msgs[0].(map[string]any)["role"].(string)
			if firstRole != "developer" {
				t.Fatalf("expected developer system role preserved, got %s", firstRole)
			}

			lastRole := msgs[len(msgs)-1].(map[string]any)["role"].(string)
			if lastRole != "user" {
				t.Fatalf("expected tail user role preserved, got %s", lastRole)
			}
		})
	}
}

func TestStripHistoryForContext_NoAssistant(t *testing.T) {
	// If there's no assistant message in rest, older is empty so body is returned as-is
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "user only"},
		},
	}

	res := StripHistoryForContext(body, 10)
	if !reflect.DeepEqual(res, body) {
		t.Fatalf("expected body unchanged when no assistant message exists")
	}
}

func TestStripHistoryForContext_OnlySystemMessages(t *testing.T) {
	// Rest is empty
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "sys 1"},
			map[string]any{"role": "developer", "content": "sys 2"},
		},
	}

	res := StripHistoryForContext(body, 10)
	if !reflect.DeepEqual(res, body) {
		t.Fatalf("expected body unchanged when only system messages exist")
	}
}

func TestStripHistoryForContext_DefaultContextWindowAndUnderBudget(t *testing.T) {
	// When cw <= 0, default cw is 200000, so budget is large (640,000 chars).
	// Short history should not be trimmed.
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "system prompt"},
			map[string]any{"role": "user", "content": "user 1"},
			map[string]any{"role": "assistant", "content": "assistant 1"},
			map[string]any{"role": "user", "content": "user 2"},
		},
	}

	res0 := StripHistoryForContext(body, 0)
	if !reflect.DeepEqual(res0, body) {
		t.Fatalf("expected body unchanged with default context window for short body")
	}

	resNeg := StripHistoryForContext(body, -100)
	if !reflect.DeepEqual(resNeg, body) {
		t.Fatalf("expected body unchanged with negative context window")
	}
}

func TestStripHistoryForContext_ContentFormats(t *testing.T) {
	// Test content formats: string, parts (Gemini format), array with non-text objects (50 chars default)
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "parts": []any{map[string]any{"text": "system part"}}},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "long user text that exceeds budget"},
					map[string]any{"inline_data": "base64data"}, // non-text object -> 50 chars
					"raw_string_in_parts_ignored",               // non-map in parts array
				},
			},
			map[string]any{"role": "assistant", "content": "assistant response"},
			map[string]any{"role": "user", "content": "latest prompt"},
		},
		"extra_field": "preserved",
	}

	// Tiny budget forcing head trimming
	stripped := StripHistoryForContext(body, 10)

	// Confirm copy-on-write (original body extra_field intact, messages unmodified in original)
	if len(body["messages"].([]any)) != 4 {
		t.Fatalf("original body was mutated")
	}

	if stripped["extra_field"] != "preserved" {
		t.Fatalf("expected extra fields preserved in output body")
	}

	msgs := stripped["messages"].([]any)
	// Trimming should drop the head user message because total length > budgetChars (32)
	// Remaining should be system msg + tail msg (user 2)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages remaining after trimming, got %d", len(msgs))
	}
}

func TestStripHistoryForContext_MalformedElementsInSlice(t *testing.T) {
	// Slice contains non-map items or maps missing roles/contents
	body := map[string]any{
		"messages": []any{
			"invalid_string_item",
			map[string]any{"no_role": "test"},
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": "hi"},
			map[string]any{"role": "user", "content": "how are you?"},
		},
	}

	stripped := StripHistoryForContext(body, 10)
	msgs, ok := stripped["messages"].([]any)
	if !ok {
		t.Fatalf("expected valid messages slice returned")
	}
	if len(msgs) == 0 {
		t.Fatalf("expected non-empty stripped messages")
	}
}

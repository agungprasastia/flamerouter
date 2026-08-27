package combo

import (
	"reflect"
	"strings"
	"testing"
)

func TestStripHistoryForContext_NilOrInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "nil body",
			body: nil,
		},
		{
			name: "empty body",
			body: map[string]any{},
		},
		{
			name: "no message key",
			body: map[string]any{"other_key": "value"},
		},
		{
			name: "message key not array",
			body: map[string]any{"messages": "not an array"},
		},
		{
			name: "empty messages array",
			body: map[string]any{"messages": []any{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripHistoryForContext(tt.body, 100)
			if !reflect.DeepEqual(got, tt.body) {
				t.Errorf("StripHistoryForContext() = %v, want %v", got, tt.body)
			}
		})
	}
}

func TestStripHistoryForContext_MessageKeyVariations(t *testing.T) {
	keys := []string{"messages", "input", "contents"}

	for _, key := range keys {
		t.Run("key_"+key, func(t *testing.T) {
			longText := strings.Repeat("a", 1000)
			body := map[string]any{
				key: []any{
					map[string]any{"role": "system", "content": "system prompt"},
					map[string]any{"role": "user", "content": "user 1 " + longText},
					map[string]any{"role": "assistant", "content": "assistant 1 " + longText},
					map[string]any{"role": "user", "content": "user 2 " + longText},
					map[string]any{"role": "assistant", "content": "assistant 2 " + longText},
					map[string]any{"role": "user", "content": "final prompt"},
				},
			}

			// Small context window forces trimming of older messages
			res := StripHistoryForContext(body, 50)
			if res == nil {
				t.Fatalf("expected non-nil result")
			}
			arr, ok := res[key].([]any)
			if !ok {
				t.Fatalf("expected key %s to be slice", key)
			}
			if len(arr) >= 6 {
				t.Errorf("expected trimmed array length < 6, got %d", len(arr))
			}
		})
	}
}

func TestStripHistoryForContext_RolesAndNoAssistant(t *testing.T) {
	t.Run("developer and model roles", func(t *testing.T) {
		longText := strings.Repeat("a", 1000)
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "developer", "content": "dev prompt"},
				map[string]any{"role": "user", "content": "u1 " + longText},
				map[string]any{"role": "model", "content": "m1 " + longText},
				map[string]any{"role": "user", "content": "u2"},
			},
		}

		res := StripHistoryForContext(body, 10)
		arr := res["messages"].([]any)
		// First item should be system/developer message
		devMsg := arr[0].(map[string]any)
		if devMsg["role"] != "developer" {
			t.Errorf("expected system/developer msg preserved, got %v", devMsg["role"])
		}
	})

	t.Run("only system messages (rest empty)", func(t *testing.T) {
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "sys 1"},
				map[string]any{"role": "developer", "content": "sys 2"},
			},
		}
		got := StripHistoryForContext(body, 10)
		if !reflect.DeepEqual(got, body) {
			t.Errorf("expected untouched body when no rest messages")
		}
	})

	t.Run("no assistant message in rest", func(t *testing.T) {
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "sys"},
				map[string]any{"role": "user", "content": "user only"},
			},
		}
		got := StripHistoryForContext(body, 10)
		if !reflect.DeepEqual(got, body) {
			t.Errorf("expected untouched body when no assistant message found")
		}
	})

	t.Run("non-map items in messages", func(t *testing.T) {
		body := map[string]any{
			"messages": []any{
				"invalid string item",
				12345,
			},
		}
		got := StripHistoryForContext(body, 10)
		if !reflect.DeepEqual(got, body) {
			t.Errorf("expected untouched body when items are invalid types")
		}
	})
}

func TestStripHistoryForContext_BudgetingAndFits(t *testing.T) {
	t.Run("history fits in budget - returns original body", func(t *testing.T) {
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "sys"},
				map[string]any{"role": "user", "content": "hi"},
				map[string]any{"role": "assistant", "content": "hello"},
				map[string]any{"role": "user", "content": "how are you?"},
			},
		}
		res := StripHistoryForContext(body, 100000)
		if !reflect.DeepEqual(res, body) {
			t.Errorf("expected untouched body when content fits budget")
		}
	})

	t.Run("default contextWindow when <= 0", func(t *testing.T) {
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "sys"},
				map[string]any{"role": "user", "content": "hi"},
				map[string]any{"role": "assistant", "content": "hello"},
				map[string]any{"role": "user", "content": "how are you?"},
			},
		}
		// budgetChars = 200,000 * 0.8 * 4 = 640,000 chars (fits comfortably)
		res0 := StripHistoryForContext(body, 0)
		if !reflect.DeepEqual(res0, body) {
			t.Errorf("expected untouched body for contextWindow 0")
		}

		resNeg := StripHistoryForContext(body, -500)
		if !reflect.DeepEqual(resNeg, body) {
			t.Errorf("expected untouched body for contextWindow < 0")
		}
	})

	t.Run("immutability when trimmed", func(t *testing.T) {
		longText := strings.Repeat("x", 500)
		origArray := []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": longText},
			map[string]any{"role": "assistant", "content": longText},
			map[string]any{"role": "user", "content": "tail"},
		}
		body := map[string]any{
			"messages": origArray,
			"model":    "test-model",
		}

		res := StripHistoryForContext(body, 10)
		if reflect.ValueOf(res).Pointer() == reflect.ValueOf(body).Pointer() {
			t.Errorf("expected new map returned when trimmed")
		}

		// Ensure original map's messages key is unmodified
		if len(body["messages"].([]any)) != 4 {
			t.Errorf("original body was mutated!")
		}
	})
}

func TestStripHistoryForContext_ContentStructures(t *testing.T) {
	t.Run("slice of content parts (OpenAI multimodal format)", func(t *testing.T) {
		longText := strings.Repeat("word ", 100)
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "sys"},
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "text", "text": longText},
						map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://img"}},
					},
				},
				map[string]any{"role": "assistant", "content": "resp"},
				map[string]any{"role": "user", "content": "final"},
			},
		}

		res := StripHistoryForContext(body, 10)
		msgs := res["messages"].([]any)
		// Should drop the middle user message because budget exceeded
		if len(msgs) >= 4 {
			t.Errorf("expected trimmed messages slice, got len %d", len(msgs))
		}
	})

	t.Run("parts field (Gemini content format)", func(t *testing.T) {
		longText := strings.Repeat("text ", 100)
		body := map[string]any{
			"contents": []any{
				map[string]any{"role": "system", "parts": []any{map[string]any{"text": "sys"}}},
				map[string]any{"role": "user", "parts": []any{map[string]any{"text": longText}}},
				map[string]any{"role": "model", "parts": []any{map[string]any{"text": "model response"}}},
				map[string]any{"role": "user", "parts": []any{map[string]any{"text": "next user question"}}},
			},
		}

		res := StripHistoryForContext(body, 10)
		contents := res["contents"].([]any)
		if len(contents) >= 4 {
			t.Errorf("expected trimmed contents slice for gemini format, got len %d", len(contents))
		}
	})

	t.Run("non-standard or nil content types", func(t *testing.T) {
		body := map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": 12345},
				map[string]any{"role": "user", "content": true},
				map[string]any{"role": "assistant"},
				map[string]any{"role": "user", "content": "final"},
			},
		}

		// Unknown content types contribute 0 length, so budget won't be exceeded
		res := StripHistoryForContext(body, 10)
		if !reflect.DeepEqual(res, body) {
			t.Errorf("expected body untouched when content lengths sum to 0")
		}
	})
}

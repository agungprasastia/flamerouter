package combo

import (
	"reflect"
	"strings"
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

			firstMap, ok := msgs[0].(map[string]any)
			if !ok {
				t.Fatalf("expected map[string]any for first msg")
			}

			firstRole, ok := firstMap["role"].(string)
			if !ok || firstRole != "developer" {
				t.Fatalf("expected developer system role preserved, got %s", firstRole)
			}

			lastMap, ok := msgs[len(msgs)-1].(map[string]any)
			if !ok {
				t.Fatalf("expected map[string]any for last msg")
			}

			lastRole, ok := lastMap["role"].(string)
			if !ok || lastRole != "user" {
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
	origMsgs, ok := body["messages"].([]any)
	if !ok || len(origMsgs) != 4 {
		t.Fatalf("original body was mutated or invalid")
	}

	if stripped["extra_field"] != "preserved" {
		t.Fatalf("expected extra fields preserved in output body")
	}

	msgs, ok := stripped["messages"].([]any)
	if !ok {
		t.Fatalf("expected []any slice under messages")
	}

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
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected valid non-empty messages slice returned")
	}
}

func TestStripHistoryForContext_MultimodalAndParts(t *testing.T) {
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

		msgs, ok := res["messages"].([]any)
		if !ok {
			t.Fatalf("expected messages slice in result")
		}

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

		contents, ok := res["contents"].([]any)
		if !ok {
			t.Fatalf("expected contents slice in result")
		}

		if len(contents) >= 4 {
			t.Errorf("expected trimmed contents slice for gemini format, got len %d", len(contents))
		}
	})
}

func TestStripHistoryForContext_NilOrInvalidInput(t *testing.T) {
	tests := []struct {
		body map[string]any
		name string
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

		arr, ok := res["messages"].([]any)
		if !ok || len(arr) == 0 {
			t.Fatalf("expected messages slice in result")
		}

		devMsg, ok := arr[0].(map[string]any)
		if !ok {
			t.Fatalf("expected message item to be map[string]any")
		}

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

		origMsgs, ok := body["messages"].([]any)
		if !ok || len(origMsgs) != 4 {
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

		msgs, ok := res["messages"].([]any)
		if !ok {
			t.Fatalf("expected messages slice in result")
		}

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

		contents, ok := res["contents"].([]any)
		if !ok {
			t.Fatalf("expected contents slice in result")
		}

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

		res := StripHistoryForContext(body, 10)

		if !reflect.DeepEqual(res, body) {
			t.Errorf("expected body untouched when content lengths sum to 0")
		}
	})
}

package combo

import (
	"flamerouter/internal/provider"
	"reflect"
	"testing"
)

func TestSplitProviderModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"openai/gpt-4o", "gpt-4o"},
		{"claude-3-5-sonnet", "claude-3-5-sonnet"},
		{"provider/sub/model", "sub/model"},
		{"", ""},
	}

	for _, tt := range tests {
		got := splitProviderModel(tt.input)
		if got != tt.expected {
			t.Errorf("splitProviderModel(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestScanBlockType(t *testing.T) {
	tests := []struct {
		block    map[string]any
		expected map[string]bool
		name     string
	}{
		{
			name:     "invalid type field",
			block:    map[string]any{"type": 123},
			expected: map[string]bool{},
		},
		{
			name:     "image_url type",
			block:    map[string]any{"type": "image_url"},
			expected: map[string]bool{"vision": true},
		},
		{
			name:     "input_image type",
			block:    map[string]any{"type": "input_image"},
			expected: map[string]bool{"vision": true},
		},
		{
			name:     "file type",
			block:    map[string]any{"type": "file"},
			expected: map[string]bool{"pdf": true},
		},
		{
			name:     "document type",
			block:    map[string]any{"type": "document"},
			expected: map[string]bool{"pdf": true},
		},
		{
			name:     "input_audio type",
			block:    map[string]any{"type": "input_audio"},
			expected: map[string]bool{"audioInput": true},
		},
		{
			name:     "audio type",
			block:    map[string]any{"type": "audio"},
			expected: map[string]bool{"audioInput": true},
		},
		{
			name:     "unknown type",
			block:    map[string]any{"type": "text"},
			expected: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := map[string]bool{}
			scanBlockType(tt.block, req)

			if !reflect.DeepEqual(req, tt.expected) {
				t.Errorf("scanBlockType() = %v; want %v", req, tt.expected)
			}
		})
	}
}

func TestScanInlineOrFileData(t *testing.T) {
	tests := []struct {
		block    map[string]any
		expected map[string]bool
		name     string
		key      string
	}{
		{
			name:     "missing key",
			block:    map[string]any{},
			key:      "inlineData",
			expected: map[string]bool{},
		},
		{
			name:     "key not map",
			block:    map[string]any{"inlineData": "invalid"},
			key:      "inlineData",
			expected: map[string]bool{},
		},
		{
			name:     "mimeType not string",
			block:    map[string]any{"inlineData": map[string]any{"mimeType": 123}},
			key:      "inlineData",
			expected: map[string]bool{},
		},
		{
			name:     "image mimeType",
			block:    map[string]any{"inlineData": map[string]any{"mimeType": "image/png"}},
			key:      "inlineData",
			expected: map[string]bool{"vision": true},
		},
		{
			name:     "pdf mimeType",
			block:    map[string]any{"fileData": map[string]any{"mimeType": "application/pdf"}},
			key:      "fileData",
			expected: map[string]bool{"pdf": true},
		},
		{
			name:     "other mimeType",
			block:    map[string]any{"fileData": map[string]any{"mimeType": "text/plain"}},
			key:      "fileData",
			expected: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := map[string]bool{}
			scanInlineOrFileData(tt.block, tt.key, req)

			if !reflect.DeepEqual(req, tt.expected) {
				t.Errorf("scanInlineOrFileData() = %v; want %v", req, tt.expected)
			}
		})
	}
}

func TestScanBlock(t *testing.T) {
	req := map[string]bool{}
	scanBlock(nil, req)

	if len(req) != 0 {
		t.Errorf("scanBlock(nil) should not mutate req, got %v", req)
	}

	b := map[string]any{
		"inlineData": map[string]any{"mimeType": "image/jpeg"},
	}
	scanBlock(b, req)

	if !req["vision"] {
		t.Errorf("expected vision in req")
	}
}

func TestScanContent(t *testing.T) {
	req := map[string]bool{}
	scanContent("not an array", req)

	if len(req) != 0 {
		t.Errorf("scanContent with non-slice content should do nothing")
	}

	content := []any{
		"plain string element",
		map[string]any{"type": "input_file"},
	}
	scanContent(content, req)

	if !req["pdf"] {
		t.Errorf("expected pdf capability detected")
	}
}

func TestExtractContentsField(t *testing.T) {
	if got := extractContentsField(map[string]any{"contents": "direct"}); got != "direct" {
		t.Errorf("expected 'direct', got %v", got)
	}

	reqBody := map[string]any{
		"request": map[string]any{
			"contents": "nested",
		},
	}
	if got := extractContentsField(reqBody); got != "nested" {
		t.Errorf("expected 'nested', got %v", got)
	}

	if got := extractContentsField(map[string]any{"other": "data"}); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestTrailingUserItems(t *testing.T) {
	t.Run("nil or empty", func(t *testing.T) {
		resNil := trailingUserItems(nil)
		if resNil != nil {
			t.Errorf("expected nil, got %v", resNil)
		}

		resEmpty := trailingUserItems([]any{})
		if resEmpty != nil {
			t.Errorf("expected nil, got %v", resEmpty)
		}

		resNotSlice := trailingUserItems("not a slice")
		if resNotSlice != nil {
			t.Errorf("expected nil, got %v", resNotSlice)
		}
	})

	t.Run("filters after assistant or model role", func(t *testing.T) {
		items := []any{
			map[string]any{"role": "user", "content": "1"},
			map[string]any{"role": "assistant", "content": "2"},
			map[string]any{"role": "user", "content": "3"},
			map[string]any{"role": "user", "content": "4"},
		}

		res := trailingUserItems(items)
		if len(res) != 2 {
			t.Fatalf("expected 2 items, got %d", len(res))
		}

		m0, ok0 := res[0].(map[string]any)
		m1, ok1 := res[1].(map[string]any)

		if !ok0 || !ok1 || m0["content"] != "3" || m1["content"] != "4" {
			t.Errorf("unexpected trailing items: %v", res)
		}
	})

	t.Run("stops on model role", func(t *testing.T) {
		items := []any{
			map[string]any{"role": "user", "content": "1"},
			map[string]any{"role": "model", "content": "2"},
			map[string]any{"role": "user", "content": "3"},
		}

		res := trailingUserItems(items)
		if len(res) != 1 {
			t.Fatalf("expected 1 item, got %d", len(res))
		}

		m0, ok0 := res[0].(map[string]any)
		if !ok0 || m0["content"] != "3" {
			t.Errorf("unexpected trailing items for model role: %v", res)
		}
	})

	t.Run("stops on non-map item", func(t *testing.T) {
		items := []any{
			map[string]any{"role": "user", "content": "1"},
			"invalid-item",
			map[string]any{"role": "user", "content": "3"},
		}

		res := trailingUserItems(items)
		if len(res) != 1 {
			t.Fatalf("expected 1 item, got %d", len(res))
		}

		m0, ok0 := res[0].(map[string]any)
		if !ok0 || m0["content"] != "3" {
			t.Errorf("unexpected trailing items on non-map item: %v", res)
		}
	})
}

func TestDetectRequiredCapabilities(t *testing.T) {
	if got := DetectRequiredCapabilities(nil); len(got) != 0 {
		t.Errorf("expected empty map for nil body")
	}

	t.Run("detects from input array", func(t *testing.T) {
		body := map[string]any{
			"input": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_audio"},
					},
				},
			},
		}

		req := DetectRequiredCapabilities(body)
		if !req["audioInput"] {
			t.Errorf("expected audioInput detected from 'input'")
		}
	})

	t.Run("detects from contents and request.contents parts", func(t *testing.T) {
		body := map[string]any{
			"request": map[string]any{
				"contents": []any{
					map[string]any{
						"role": "user",
						"parts": []any{
							map[string]any{"type": "file"},
						},
					},
				},
			},
		}

		req := DetectRequiredCapabilities(body)
		if !req["pdf"] {
			t.Errorf("expected pdf detected from contents parts")
		}
	})
}

func TestReorderForCapabilities(t *testing.T) {
	t.Run("short models slice", func(t *testing.T) {
		models := []string{"gpt-4o"}

		res := ReorderForCapabilities(models, []byte(`{}`))
		if !reflect.DeepEqual(res, models) {
			t.Errorf("expected unchanged models for len <= 1")
		}
	})

	t.Run("invalid json body", func(t *testing.T) {
		models := []string{"gpt-4o", "deepseek-v3"}

		res := ReorderForCapabilities(models, []byte(`invalid json`))
		if !reflect.DeepEqual(res, models) {
			t.Errorf("expected unchanged models on json error")
		}
	})

	t.Run("no capabilities required", func(t *testing.T) {
		models := []string{"deepseek-v3", "gpt-4o"}

		res := ReorderForCapabilities(models, []byte(`{"messages":[{"role":"user","content":"hello"}]}`))
		if !reflect.DeepEqual(res, models) {
			t.Errorf("expected unchanged models when no modalities detected")
		}
	})

	t.Run("hard capability reordering (audio input)", func(t *testing.T) {
		models := []string{"openai/gpt-4o", "openai/whisper-1"}
		body := []byte(`{"messages":[{"role":"user","content":[{"type":"input_audio"}]}]}`)

		res := ReorderForCapabilities(models, body)
		if res[0] != "openai/whisper-1" {
			t.Errorf("expected whisper-1 first for audio input, got %v", res)
		}
	})
}

func TestModelHasCapability(t *testing.T) {
	caps := provider.Capabilities{
		ThinkingFormat: "",
		ContextWindow:  100000,
		MaxOutput:      4000,
		Vision:         true,
		PDF:            true,
		AudioInput:     true,
		VideoInput:     true,
		ImageOutput:    false,
		AudioOutput:    false,
		Search:         true,
		Tools:          true,
		Reasoning:      false,
	}

	if !modelHasCapability(caps, "vision") {
		t.Error("expected vision true")
	}

	if !modelHasCapability(caps, "pdf") {
		t.Error("expected pdf true")
	}

	if !modelHasCapability(caps, "audioInput") {
		t.Error("expected audioInput true")
	}

	if !modelHasCapability(caps, "videoInput") {
		t.Error("expected videoInput true")
	}

	if !modelHasCapability(caps, "search") {
		t.Error("expected search true")
	}

	if modelHasCapability(caps, "unknown_capability") {
		t.Error("expected unknown_capability false")
	}
}

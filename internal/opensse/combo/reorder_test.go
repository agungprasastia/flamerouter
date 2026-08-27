package combo

import (
	"reflect"
	"testing"
)

func TestReorderForCapabilities_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		models []string
		body   []byte
		want   []string
	}{
		{
			name:   "nil models",
			models: nil,
			body:   []byte(`{"messages":[{"role":"user","content":[{"type":"image_url"}]}]}`),
			want:   nil,
		},
		{
			name:   "empty models",
			models: []string{},
			body:   []byte(`{"messages":[{"role":"user","content":[{"type":"image_url"}]}]}`),
			want:   []string{},
		},
		{
			name:   "single model",
			models: []string{"openai/gpt-4o"},
			body:   []byte(`{"messages":[{"role":"user","content":[{"type":"image_url"}]}]}`),
			want:   []string{"openai/gpt-4o"},
		},
		{
			name:   "invalid json body",
			models: []string{"openai/deepseek-v3", "openai/gpt-4o"},
			body:   []byte(`{invalid json`),
			want:   []string{"openai/deepseek-v3", "openai/gpt-4o"},
		},
		{
			name:   "empty json body",
			models: []string{"openai/deepseek-v3", "openai/gpt-4o"},
			body:   []byte(`{}`),
			want:   []string{"openai/deepseek-v3", "openai/gpt-4o"},
		},
		{
			name:   "no required capabilities in payload",
			models: []string{"openai/deepseek-v3", "openai/gpt-4o"},
			body:   []byte(`{"messages":[{"role":"user","content":"just plain text"}]}`),
			want:   []string{"openai/deepseek-v3", "openai/gpt-4o"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReorderForCapabilities(tt.models, tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReorderForCapabilities() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReorderForCapabilities_PayloadStructures(t *testing.T) {
	tests := []struct {
		name   string
		models []string
		body   []byte
		want   []string
	}{
		{
			name:   "messages field format with image_url",
			models: []string{"openai/deepseek-v3", "openai/gpt-4o"},
			body:   []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"http://example.com/img.png"}}]}]}`),
			want:   []string{"openai/gpt-4o", "openai/deepseek-v3"},
		},
		{
			name:   "input field format with audio",
			models: []string{"openai/deepseek-v3", "openai/whisper-1"},
			body:   []byte(`{"input":[{"role":"user","content":[{"type":"input_audio"}]}]}`),
			want:   []string{"openai/whisper-1", "openai/deepseek-v3"},
		},
		{
			name:   "contents field format with inlineData image",
			models: []string{"openai/deepseek-v3", "openai/gpt-4o"},
			body:   []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png"}}]}]}`),
			want:   []string{"openai/gpt-4o", "openai/deepseek-v3"},
		},
		{
			name:   "request.contents field format with fileData pdf",
			models: []string{"openai/deepseek-v3", "openai/gpt-4o"},
			body:   []byte(`{"request":{"contents":[{"role":"user","parts":[{"fileData":{"mimeType":"application/pdf"}}]}]}}`),
			want:   []string{"openai/deepseek-v3", "openai/gpt-4o"}, // neither model has PDF in capabilities table, order unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReorderForCapabilities(tt.models, tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReorderForCapabilities() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReorderForCapabilities_BlockTypes(t *testing.T) {
	blockTypeTests := []struct {
		blockJSON string
		wantFirst string
	}{
		{`{"type":"image"}`, "openai/gpt-4o"},
		{`{"type":"input_image"}`, "openai/gpt-4o"},
		{`{"type":"image_url"}`, "openai/gpt-4o"},
		{`{"type":"audio"}`, "openai/whisper-1"},
		{`{"type":"input_audio"}`, "openai/whisper-1"},
		{`{"inlineData":{"mimeType":"image/jpeg"}}`, "openai/gpt-4o"},
		{`{"fileData":{"mimeType":"image/png"}}`, "openai/gpt-4o"},
	}

	for _, tt := range blockTypeTests {
		t.Run(tt.blockJSON, func(t *testing.T) {
			models := []string{"openai/deepseek-v3", "openai/gpt-4o", "openai/whisper-1"}
			body := []byte(`{"messages":[{"role":"user","content":[` + tt.blockJSON + `]}]}`)

			got := ReorderForCapabilities(models, body)

			if len(got) == 0 {
				t.Fatalf("ReorderForCapabilities(%s) returned empty result, want length 3 with first model %s", tt.blockJSON, tt.wantFirst)
			}
			if len(got) != 3 || got[0] != tt.wantFirst {
				t.Errorf("ReorderForCapabilities(%s) first model = %s, want %s (full: %v)", tt.blockJSON, got[0], tt.wantFirst, got)
			}
		})
	}
}

func TestReorderForCapabilities_TrailingUserTurn(t *testing.T) {
	// Past turn had an image, but assistant replied, and current turn has no image -> vision requirement should be ignored.
	bodyPastImage := []byte(`{
		"messages":[
			{"role":"user","content":[{"type":"image_url"}]},
			{"role":"assistant","content":"I see the image"},
			{"role":"user","content":"Now answer this text question"}
		]
	}`)

	models := []string{"openai/deepseek-v3", "openai/gpt-4o"}
	got := ReorderForCapabilities(models, bodyPastImage)
	want := []string{"openai/deepseek-v3", "openai/gpt-4o"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("TrailingUserTurn ignored past turn: got %v, want %v", got, want)
	}

	// Trailing turn HAS an image after assistant response -> vision requirement should promote gpt-4o.
	bodyCurrentTurnImage := []byte(`{
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"model","content":"hi"},
			{"role":"user","content":[{"type":"image_url"}]}
		]
	}`)

	got = ReorderForCapabilities(models, bodyCurrentTurnImage)
	want = []string{"openai/gpt-4o", "openai/deepseek-v3"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("TrailingUserTurn active current turn: got %v, want %v", got, want)
	}
}

func TestReorderForCapabilities_StabilityAndTiering(t *testing.T) {
	// gpt-4o and gpt-5 both support vision; deepseek-v3 does not.
	// Original order: deepseek-v3, gpt-4o, gpt-5
	models := []string{"openai/deepseek-v3", "openai/gpt-4o", "openai/gpt-5"}
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url"}]}]}`)

	got := ReorderForCapabilities(models, body)
	want := []string{"openai/gpt-4o", "openai/gpt-5", "openai/deepseek-v3"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("StabilityAndTiering: got %v, want %v", got, want)
	}
}

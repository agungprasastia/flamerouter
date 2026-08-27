package combo

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name          string
		comboStrategy string
		perCombo      map[string]string
		comboName     string
		wantType      string
	}{
		{
			name:          "empty comboStrategy defaults to FallbackStrategy",
			comboStrategy: "",
			perCombo:      nil,
			comboName:     "test",
			wantType:      "*combo.FallbackStrategy",
		},
		{
			name:          "unknown strategy defaults to FallbackStrategy",
			comboStrategy: "unknown-strategy",
			perCombo:      nil,
			comboName:     "test",
			wantType:      "*combo.FallbackStrategy",
		},
		{
			name:          "explicit fallback strategy returns FallbackStrategy",
			comboStrategy: "fallback",
			perCombo:      nil,
			comboName:     "test",
			wantType:      "*combo.FallbackStrategy",
		},
		{
			name:          "round-robin strategy returns RoundRobin",
			comboStrategy: "round-robin",
			perCombo:      nil,
			comboName:     "test",
			wantType:      "*combo.RoundRobin",
		},
		{
			name:          "fusion strategy returns Fusion",
			comboStrategy: "fusion",
			perCombo:      nil,
			comboName:     "test",
			wantType:      "*combo.Fusion",
		},
		{
			name:          "perCombo override matches comboName to fusion",
			comboStrategy: "fallback",
			perCombo:      map[string]string{"mycombo": "fusion"},
			comboName:     "mycombo",
			wantType:      "*combo.Fusion",
		},
		{
			name:          "perCombo override matches comboName to round-robin",
			comboStrategy: "fallback",
			perCombo:      map[string]string{"mycombo": "round-robin"},
			comboName:     "mycombo",
			wantType:      "*combo.RoundRobin",
		},
		{
			name:          "perCombo override matches comboName to fallback",
			comboStrategy: "round-robin",
			perCombo:      map[string]string{"mycombo": "fallback"},
			comboName:     "mycombo",
			wantType:      "*combo.FallbackStrategy",
		},
		{
			name:          "perCombo non-matching comboName retains comboStrategy",
			comboStrategy: "round-robin",
			perCombo:      map[string]string{"othercombo": "fusion"},
			comboName:     "mycombo",
			wantType:      "*combo.RoundRobin",
		},
		{
			name:          "empty perCombo map retains comboStrategy",
			comboStrategy: "fusion",
			perCombo:      map[string]string{},
			comboName:     "mycombo",
			wantType:      "*combo.Fusion",
		},
		{
			name:          "perCombo override to unknown strategy defaults to FallbackStrategy",
			comboStrategy: "round-robin",
			perCombo:      map[string]string{"mycombo": "invalid-strategy"},
			comboName:     "mycombo",
			wantType:      "*combo.FallbackStrategy",
		},
		{
			name:          "empty comboName matching empty key in perCombo",
			comboStrategy: "fallback",
			perCombo:      map[string]string{"": "fusion"},
			comboName:     "",
			wantType:      "*combo.Fusion",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Resolve(tt.comboStrategy, tt.perCombo, tt.comboName)
			if s == nil {
				t.Fatalf("Resolve(%q, %v, %q) returned nil", tt.comboStrategy, tt.perCombo, tt.comboName)
			}

			var gotType string
			switch s.(type) {
			case *FallbackStrategy:
				gotType = "*combo.FallbackStrategy"
			case *RoundRobin:
				gotType = "*combo.RoundRobin"
			case *Fusion:
				gotType = "*combo.Fusion"
			default:
				gotType = "unknown"
			}

			if gotType != tt.wantType {
				t.Errorf("Resolve(%q, %v, %q) = %T (%s), want %s",
					tt.comboStrategy, tt.perCombo, tt.comboName, s, gotType, tt.wantType)
			}
		})
	}
}

func TestReorderForCapabilities_NoImages(t *testing.T) {
	models := []string{"gpt-4", "claude-3-opus"}

	result := ReorderForCapabilities(models, []byte(`{"messages":[{"role":"user","content":"hello"}]}`))
	if len(result) != 2 {
		t.Fatal("expected same length")
	}

	if result[0] != models[0] || result[1] != models[1] {
		t.Fatalf("expected order preserved, got %v", result)
	}
}

func TestReorderForCapabilities_VisionPromotes(t *testing.T) {
	// deepseek-v3 has no vision; gpt-4o does
	models := []string{"openai/deepseek-v3", "openai/gpt-4o"}
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,xx"}}]}]}`)

	result := ReorderForCapabilities(models, body)
	if result[0] != "openai/gpt-4o" {
		t.Fatalf("expected gpt-4o first, got %v", result)
	}
}

func TestGetRotatedModels_Sticky(t *testing.T) {
	ResetRotation("")

	models := []string{"provider/model-a", "provider/model-b"}
	got := make([]string, 0, 6)

	for i := 0; i < 6; i++ {
		rotated := GetRotatedModels(models, "code-xhigh", "round-robin", 2)
		got = append(got, rotated[0])
	}

	want := []string{
		"provider/model-a", "provider/model-a",
		"provider/model-b", "provider/model-b",
		"provider/model-a", "provider/model-a",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("i=%d want %s got %s full=%v", i, want[i], got[i], got)
		}
	}
}

func TestGetRotatedModels_FallbackNoRotate(t *testing.T) {
	ResetRotation("")

	models := []string{"provider/model-a", "provider/model-b"}
	a := GetRotatedModels(models, "c", "fallback", 2)
	b := GetRotatedModels(models, "c", "fallback", 2)

	if a[0] != models[0] || b[0] != models[0] {
		t.Fatalf("fallback must not rotate: %v %v", a, b)
	}
}

func TestDetectRequiredCapabilities_Image(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "x"}},
				},
			},
		},
	}

	req := DetectRequiredCapabilities(body)
	if !req["vision"] {
		t.Fatal("expected vision")
	}
}

func TestFusion_NoJudgeReturnsFirstPanel(t *testing.T) {
	var judgeCalls atomic.Int32

	f := &Fusion{}
	rr := httptest.NewRecorder()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"stream":false}`)
	opts := Options{
		Stream:         false,
		JudgeModel:     "",
		ClientHeaders:  nil,
		SourceFormat:   "",
		TargetFormat:   "",
		TokenSaverJSON: "",
		ComboName:      "",
		StickyLimit:    0,
		SingleModel: func(_ context.Context, w http.ResponseWriter, _ []byte, modelStr string, _ bool) error {
			// Panel calls write into captureWriter; judge path would hit real w (httptest).
			if _, ok := w.(*captureWriter); !ok {
				judgeCalls.Add(1)
			}

			resp, err := json.Marshal(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": "answer-from-" + modelStr},
				}},
			})
			if err != nil {
				return err
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			_, err = w.Write(resp)

			return err
		},
	}

	err := f.Execute(context.Background(), rr, body, []string{"p/a", "p/b"}, nil, nil, nil, opts)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if judgeCalls.Load() != 0 {
		t.Fatalf("expected no judge SingleModel call, got %d", judgeCalls.Load())
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "answer-from-") {
		t.Fatalf("expected panel brief body, got %s", rr.Body.String())
	}
}

func TestFusion_WithJudgeCallsJudge(t *testing.T) {
	var (
		mu     sync.Mutex
		models []string
	)

	f := &Fusion{}
	rr := httptest.NewRecorder()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"stream":false}`)
	opts := Options{
		Stream:         false,
		JudgeModel:     "p/judge",
		ClientHeaders:  nil,
		SourceFormat:   "",
		TargetFormat:   "",
		TokenSaverJSON: "",
		ComboName:      "",
		StickyLimit:    0,
		SingleModel: func(_ context.Context, w http.ResponseWriter, _ []byte, modelStr string, _ bool) error {
			mu.Lock()
			models = append(models, modelStr)
			mu.Unlock()

			resp, err := json.Marshal(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": "text-" + modelStr},
				}},
			})
			if err != nil {
				return err
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)

			_, err = w.Write(resp)

			return err
		},
	}

	if err := f.Execute(context.Background(), rr, body, []string{"p/a", "p/b"}, nil, nil, nil, opts); err != nil {
		t.Fatalf("execute: %v", err)
	}

	foundJudge := false

	mu.Lock()
	for _, m := range models {
		if m == "p/judge" {
			foundJudge = true
		}
	}

	recordedModels := append([]string(nil), models...)
	mu.Unlock()

	if !foundJudge {
		t.Fatalf("expected judge model call, models=%v", recordedModels)
	}
}

func TestWritePanelBrief_JSON(t *testing.T) {
	rr := httptest.NewRecorder()
	if err := writePanelBrief(rr, "hello brief", false); err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if json.Unmarshal(rr.Body.Bytes(), &m) != nil {
		t.Fatalf("invalid json: %s", rr.Body.String())
	}

	if !bytes.Contains(rr.Body.Bytes(), []byte("hello brief")) {
		t.Fatal("missing content")
	}
}

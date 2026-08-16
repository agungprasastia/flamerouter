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

func TestResolve_Default(t *testing.T) {
	s := Resolve("", nil, "test")
	if _, ok := s.(*FallbackStrategy); !ok {
		t.Fatal("expected FallbackStrategy for empty string")
	}
}

func TestResolve_RoundRobin(t *testing.T) {
	s := Resolve("round-robin", nil, "test")
	if _, ok := s.(*RoundRobin); !ok {
		t.Fatal("expected RoundRobin")
	}
}

func TestResolve_Fusion(t *testing.T) {
	s := Resolve("fusion", nil, "test")
	if _, ok := s.(*Fusion); !ok {
		t.Fatal("expected Fusion")
	}
}

func TestResolve_PerComboOverride(t *testing.T) {
	s := Resolve("fallback", map[string]string{"mycombo": "fusion"}, "mycombo")
	if _, ok := s.(*Fusion); !ok {
		t.Fatal("expected Fusion override for mycombo")
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
		Stream:     false,
		JudgeModel: "",
		SingleModel: func(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, stream bool) error {
			// Panel calls write into captureWriter; judge path would hit real w (httptest).
			if _, ok := w.(*captureWriter); !ok {
				judgeCalls.Add(1)
			}
			resp, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": "answer-from-" + modelStr},
				}},
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp)
			return nil
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
		Stream:     false,
		JudgeModel: "p/judge",
		SingleModel: func(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, stream bool) error {
			mu.Lock()
			models = append(models, modelStr)
			mu.Unlock()

			resp, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{
					"message": map[string]any{"role": "assistant", "content": "text-" + modelStr},
				}},
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp)
			return nil
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

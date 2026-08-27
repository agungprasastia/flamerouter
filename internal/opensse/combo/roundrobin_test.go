package combo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func TestGetRotatedModels_EdgeCases(t *testing.T) {
	ResetRotation("")

	// Case 1: models slice length <= 1
	singleModel := []string{"model-a"}
	gotSingle := GetRotatedModels(singleModel, "combo1", "round-robin", 1)
	if !reflect.DeepEqual(gotSingle, singleModel) {
		t.Fatalf("expected untouched single model, got %v", gotSingle)
	}

	emptyModels := []string{}
	gotEmpty := GetRotatedModels(emptyModels, "combo1", "round-robin", 1)
	if !reflect.DeepEqual(gotEmpty, emptyModels) {
		t.Fatalf("expected untouched empty slice, got %v", gotEmpty)
	}

	// Case 2: strategy != "round-robin"
	models := []string{"model-a", "model-b"}
	gotFallback := GetRotatedModels(models, "combo1", "fallback", 1)
	if !reflect.DeepEqual(gotFallback, models) {
		t.Fatalf("expected untouched models when strategy is fallback, got %v", gotFallback)
	}

	// Case 3: stickyLimit <= 0 defaults limit to 1 (rotates on every call)
	ResetRotation("zero-limit")
	res1 := GetRotatedModels(models, "zero-limit", "round-robin", 0)
	res2 := GetRotatedModels(models, "zero-limit", "round-robin", -5)

	if res1[0] != "model-a" || res2[0] != "model-b" {
		t.Fatalf("expected rotation on every call with stickyLimit <= 0, got res1=%v res2=%v", res1, res2)
	}
}

func TestGetRotatedModels_DefaultKeyAndIsolation(t *testing.T) {
	ResetRotation("")

	models := []string{"m1", "m2", "m3"}

	// Empty comboName uses __default__ key
	default1 := GetRotatedModels(models, "", "round-robin", 1)
	default2 := GetRotatedModels(models, "", "round-robin", 1)
	if default1[0] != "m1" || default2[0] != "m2" {
		t.Fatalf("expected default combo key rotation m1 -> m2, got %v then %v", default1, default2)
	}

	// Distinct combo names maintain independent rotation states
	c1Call1 := GetRotatedModels(models, "combo-1", "round-robin", 2)
	c2Call1 := GetRotatedModels(models, "combo-2", "round-robin", 1)
	c1Call2 := GetRotatedModels(models, "combo-1", "round-robin", 2)
	c2Call2 := GetRotatedModels(models, "combo-2", "round-robin", 1)

	if c1Call1[0] != "m1" || c1Call2[0] != "m1" {
		t.Fatalf("combo-1 sticky limit 2 failed: got %v and %v", c1Call1, c1Call2)
	}
	if c2Call1[0] != "m1" || c2Call2[0] != "m2" {
		t.Fatalf("combo-2 sticky limit 1 failed: got %v and %v", c2Call1, c2Call2)
	}
}

func TestGetRotatedModels_Concurrency(t *testing.T) {
	ResetRotation("")

	models := []string{"model-1", "model-2", "model-3", "model-4"}
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			comboName := fmt.Sprintf("combo-%d", id%5)
			_ = GetRotatedModels(models, comboName, "round-robin", 2)
			if id%10 == 0 {
				ResetRotation(comboName)
			}
		}(i)
	}

	wg.Wait()
}

func TestResetRotation(t *testing.T) {
	models := []string{"m1", "m2"}

	// Advance state for combo-A and combo-B
	GetRotatedModels(models, "combo-A", "round-robin", 1) // index moves to 1
	GetRotatedModels(models, "combo-B", "round-robin", 1) // index moves to 1

	// Reset combo-A specifically
	ResetRotation("combo-A")

	resA := GetRotatedModels(models, "combo-A", "round-robin", 1)
	resB := GetRotatedModels(models, "combo-B", "round-robin", 1)

	if resA[0] != "m1" {
		t.Fatalf("expected combo-A to reset back to m1, got %s", resA[0])
	}
	if resB[0] != "m2" {
		t.Fatalf("expected combo-B state preserved at m2, got %s", resB[0])
	}

	// Reset all keys
	ResetRotation("")
	resB2 := GetRotatedModels(models, "combo-B", "round-robin", 1)
	if resB2[0] != "m1" {
		t.Fatalf("expected global reset to clear combo-B back to m1, got %s", resB2[0])
	}
}

func TestRotateFromIndex(t *testing.T) {
	models := []string{"a", "b", "c", "d"}

	tests := []struct {
		name         string
		currentIndex int
		want         []string
	}{
		{"index zero", 0, []string{"a", "b", "c", "d"}},
		{"negative index", -1, []string{"a", "b", "c", "d"}},
		{"out of bound high", 4, []string{"a", "b", "c", "d"}},
		{"middle index 1", 1, []string{"b", "c", "d", "a"}},
		{"middle index 2", 2, []string{"c", "d", "a", "b"}},
		{"last index 3", 3, []string{"d", "a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rotateFromIndex(models, tt.currentIndex)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("rotateFromIndex(%v, %d) = %v; want %v", models, tt.currentIndex, got, tt.want)
			}
		})
	}
}

func TestRoundRobin_Execute(t *testing.T) {
	ResetRotation("execute-combo")

	models := []string{"provider/model-1", "provider/model-2"}
	r := &RoundRobin{}

	var attempted []string
	opts := Options{
		ComboName:   "execute-combo",
		StickyLimit: 1,
		Stream:      false,
		SingleModel: func(_ context.Context, _ http.ResponseWriter, _ []byte, modelStr string, _ bool) error {
			attempted = append(attempted, modelStr)
			if modelStr == "provider/model-1" {
				return errors.New("model 1 error")
			}
			return nil
		},
	}

	w := httptest.NewRecorder()
	ctx := context.Background()

	err := r.Execute(ctx, w, []byte(`{"messages":[{"role":"user","content":"hello"}]}`), models, nil, nil, nil, opts)
	if err != nil {
		t.Fatalf("expected execution success on model-2 fallback, got err: %v", err)
	}

	if len(attempted) != 2 || attempted[0] != "provider/model-1" || attempted[1] != "provider/model-2" {
		t.Fatalf("expected sequential fallback attempted [provider/model-1, provider/model-2], got %v", attempted)
	}
}

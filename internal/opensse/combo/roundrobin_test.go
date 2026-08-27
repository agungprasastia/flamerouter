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

	t.Run("empty or single model slice", func(t *testing.T) {
		nilModels := GetRotatedModels(nil, "c1", "round-robin", 1)
		if nilModels != nil {
			t.Fatalf("expected nil for nil input, got %v", nilModels)
		}

		emptyModels := GetRotatedModels([]string{}, "c1", "round-robin", 1)
		if len(emptyModels) != 0 {
			t.Fatalf("expected empty slice, got %v", emptyModels)
		}

		singleModel := []string{"model-1"}
		gotSingle := GetRotatedModels(singleModel, "c1", "round-robin", 1)

		if !reflect.DeepEqual(gotSingle, singleModel) {
			t.Fatalf("expected %v, got %v", singleModel, gotSingle)
		}
	})

	t.Run("non round-robin strategy", func(t *testing.T) {
		models := []string{"m1", "m2", "m3"}

		for _, strategy := range []string{"fallback", "fusion", "", "random"} {
			got := GetRotatedModels(models, "c1", strategy, 1)
			if !reflect.DeepEqual(got, models) {
				t.Fatalf("strategy %s expected %v, got %v", strategy, models, got)
			}
		}
	})

	t.Run("negative or zero sticky limit defaults to 1", func(t *testing.T) {
		ResetRotation("")

		models := []string{"m1", "m2", "m3"}

		for _, limit := range []int{0, -1, -5} {
			ResetRotation("")

			got1 := GetRotatedModels(models, "c_neg", "round-robin", limit)
			if got1[0] != "m1" {
				t.Fatalf("limit %d call 1 expected m1, got %v", limit, got1)
			}

			got2 := GetRotatedModels(models, "c_neg", "round-robin", limit)
			if got2[0] != "m2" {
				t.Fatalf("limit %d call 2 expected m2, got %v", limit, got2)
			}
		}
	})

	t.Run("empty combo name uses default key", func(t *testing.T) {
		ResetRotation("")

		models := []string{"m1", "m2"}

		got1 := GetRotatedModels(models, "", "round-robin", 1)
		if got1[0] != "m1" {
			t.Fatalf("expected m1, got %s", got1[0])
		}

		rotMu.Lock()
		st, exists := rotState["__default__"]
		rotMu.Unlock()

		if !exists || st == nil {
			t.Fatalf("expected rotationState under __default__ key")
		}

		got2 := GetRotatedModels(models, "", "round-robin", 1)
		if got2[0] != "m2" {
			t.Fatalf("expected m2, got %s", got2[0])
		}
	})

	t.Run("independent rotation state per combo name", func(t *testing.T) {
		ResetRotation("")

		models := []string{"m1", "m2"}

		// combo1: 1 call -> index 1 for next call
		c1a := GetRotatedModels(models, "combo1", "round-robin", 1)
		if c1a[0] != "m1" {
			t.Fatalf("combo1 call 1 expected m1, got %s", c1a[0])
		}

		// combo2: should start at index 0 independently
		c2a := GetRotatedModels(models, "combo2", "round-robin", 1)
		if c2a[0] != "m1" {
			t.Fatalf("combo2 call 1 expected m1, got %s", c2a[0])
		}

		// combo1 next call should be m2
		c1b := GetRotatedModels(models, "combo1", "round-robin", 1)
		if c1b[0] != "m2" {
			t.Fatalf("combo1 call 2 expected m2, got %s", c1b[0])
		}
	})
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

func TestGetRotatedModels_Concurrency(_ *testing.T) {
	ResetRotation("")

	models := []string{"model-1", "model-2", "model-3", "model-4"}

	var wg sync.WaitGroup

	worker := func(id int) {
		defer wg.Done()

		comboName := fmt.Sprintf("combo-%d", id%5)
		_ = GetRotatedModels(models, comboName, "round-robin", 2)

		if id%10 == 0 {
			ResetRotation(comboName)
		}
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)

		go worker(i)
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

func TestResetRotation_Structured(funcT *testing.T) {
	funcT.Run("selective reset", func(t *testing.T) {
		ResetRotation("")

		models := []string{"m1", "m2"}

		GetRotatedModels(models, "combo-a", "round-robin", 1)
		GetRotatedModels(models, "combo-b", "round-robin", 1)

		ResetRotation("combo-a")

		rotMu.Lock()
		_, hasA := rotState["combo-a"]
		_, hasB := rotState["combo-b"]
		rotMu.Unlock()

		if hasA {
			t.Fatalf("combo-a should have been reset")
		}

		if !hasB {
			t.Fatalf("combo-b should still exist in rotation state")
		}

		// Next call for combo-a starts fresh at index 0
		gotA := GetRotatedModels(models, "combo-a", "round-robin", 1)
		if gotA[0] != "m1" {
			t.Fatalf("combo-a expected m1 after reset, got %s", gotA[0])
		}

		// Next call for combo-b continues to index 1 ("m2")
		gotB := GetRotatedModels(models, "combo-b", "round-robin", 1)
		if gotB[0] != "m2" {
			t.Fatalf("combo-b expected m2, got %s", gotB[0])
		}
	})

	funcT.Run("global reset with empty string", func(t *testing.T) {
		ResetRotation("")

		models := []string{"m1", "m2"}

		GetRotatedModels(models, "combo-x", "round-robin", 1)
		GetRotatedModels(models, "combo-y", "round-robin", 1)

		ResetRotation("")

		rotMu.Lock()
		stateLen := len(rotState)
		rotMu.Unlock()

		if stateLen != 0 {
			t.Fatalf("expected rotState map to be empty after ResetRotation(\"\"), got len=%d", stateLen)
		}
	})
}

func TestRotateFromIndex(t *testing.T) {
	models := []string{"a", "b", "c", "d"}

	tests := []struct {
		name     string
		models   []string
		expected []string
		index    int
	}{
		{
			name:     "negative index returns slice copy",
			models:   models,
			expected: []string{"a", "b", "c", "d"},
			index:    -1,
		},
		{
			name:     "zero index returns slice copy",
			models:   models,
			expected: []string{"a", "b", "c", "d"},
			index:    0,
		},
		{
			name:     "index out of bounds upper returns slice copy",
			models:   models,
			expected: []string{"a", "b", "c", "d"},
			index:    4,
		},
		{
			name:     "valid rotation at index 1",
			models:   models,
			expected: []string{"b", "c", "d", "a"},
			index:    1,
		},
		{
			name:     "valid rotation at index 2",
			models:   models,
			expected: []string{"c", "d", "a", "b"},
			index:    2,
		},
		{
			name:     "valid rotation at index 3",
			models:   models,
			expected: []string{"d", "a", "b", "c"},
			index:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rotateFromIndex(tt.models, tt.index)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Fatalf("rotateFromIndex(%v, %d) = %v, want %v", tt.models, tt.index, got, tt.expected)
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
		ClientHeaders:  nil,
		SourceFormat:   "",
		TargetFormat:   "",
		TokenSaverJSON: "",
		JudgeModel:     "",
		ComboName:      "execute-combo",
		StickyLimit:    1,
		Stream:         false,
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

	err := r.Execute(ctx, w, []byte(`{"messages":[{"role":"user stream":false}]}`), models, nil, nil, nil, opts)
	if err != nil {
		t.Fatalf("expected execution success on model-2 fallback, got err: %v", err)
	}

	if len(attempted) != 2 || attempted[0] != "provider/model-1" || attempted[1] != "provider/model-2" {
		t.Fatalf("expected sequential fallback attempted [provider/model-1, provider/model-2], got %v", attempted)
	}
}

func TestGetRotatedModels_ConcurrencyExtensive(t *testing.T) {
	ResetRotation("")

	const (
		numGoroutines          = 50
		iterationsPerGoroutine = 100
	)

	models := []string{"m1", "m2", "m3", "m4"}
	comboNames := []string{"c1", "c2", "c3", ""}

	var wg sync.WaitGroup

	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			combo := comboNames[id%len(comboNames)]

			for j := 0; j < iterationsPerGoroutine; j++ {
				rotated := GetRotatedModels(models, combo, "round-robin", (j%3)+1)
				if len(rotated) != len(models) {
					t.Errorf("goroutine %d iter %d: expected length %d, got %d", id, j, len(models), len(rotated))
				}

				if j%25 == 0 {
					ResetRotation(combo)
				}
			}
		}(i)
	}

	wg.Wait()
}

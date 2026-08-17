package combo

import (
	"context"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
	"sync"
)

// RoundRobin rotates through models with a sticky limit (rotation via rotState).
type RoundRobin struct{}

// rotationState is process-global per combo name (parity with 9router Map).
type rotationState struct {
	index               int
	consecutiveUseCount int
}

var (
	rotMu    sync.Mutex
	rotState = map[string]*rotationState{}
)

// GetRotatedModels returns models rotated for round-robin; fallback returns as-is.
func GetRotatedModels(models []string, comboName, strategy string, stickyLimit int) []string {
	if len(models) <= 1 || strategy != "round-robin" {
		return models
	}

	limit := stickyLimit
	if limit <= 0 {
		limit = 1
	}

	key := comboName
	if key == "" {
		key = "__default__"
	}

	rotMu.Lock()
	defer rotMu.Unlock()

	st := rotState[key]
	if st == nil {
		st = &rotationState{index: 0, consecutiveUseCount: 0}
		rotState[key] = st
	}

	currentIndex := st.index % len(models)
	rotated := rotateFromIndex(models, currentIndex)

	next := st.consecutiveUseCount + 1
	if next >= limit {
		st.index = (currentIndex + 1) % len(models)
		st.consecutiveUseCount = 0
	} else {
		st.consecutiveUseCount = next
	}

	return rotated
}

// ResetRotation clears rotation state for comboName, or all if empty.
func ResetRotation(comboName string) {
	rotMu.Lock()
	defer rotMu.Unlock()

	if comboName == "" {
		rotState = map[string]*rotationState{}
		return
	}

	delete(rotState, comboName)
}

func rotateFromIndex(models []string, currentIndex int) []string {
	if currentIndex <= 0 || currentIndex >= len(models) {
		out := make([]string, len(models))
		copy(out, models)

		return out
	}

	out := make([]string, 0, len(models))
	out = append(out, models[currentIndex:]...)
	out = append(out, models[:currentIndex]...)

	return out
}

// Execute runs the combo using round-robin strategy.
func (r *RoundRobin) Execute(ctx context.Context, w http.ResponseWriter, body []byte,
	models []string, st *store.Store, _ executor.Executor,
	_ *fallback.Fallback, opts Options,
) error {
	models = PrepareModelsWithCapacityAdapter(models, opts.ComboName, "round-robin", opts.StickyLimit, body, st)
	return runSequential(ctx, w, body, models, opts)
}

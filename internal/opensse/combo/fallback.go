package combo

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
	"strings"
)

// FallbackStrategy tries models sequentially, skipping to next on error.
type FallbackStrategy struct{}

// ponytail: simplest strategy - just iterates. Add circuit-breaker per model if needed.

// Execute runs the combo using fallback strategy.
func (f *FallbackStrategy) Execute(ctx context.Context, w http.ResponseWriter, body []byte,
	models []string, st *store.Store, _ executor.Executor,
	_ *fallback.Fallback, opts Options,
) error {
	models = PrepareModelsWithCapacityAdapter(models, opts.ComboName, "fallback", opts.StickyLimit, body, st)
	return runSequential(ctx, w, body, models, opts)
}

func runSequential(ctx context.Context, w http.ResponseWriter, body []byte, models []string, opts Options) error {
	if opts.SingleModel == nil {
		http.Error(w, `{"error":"combo single-model runner not configured"}`, http.StatusInternalServerError)
		return nil
	}

	var lastErr error

	for _, m := range models {
		if m == "" {
			continue
		}

		err := opts.SingleModel(ctx, w, body, m, opts.Stream)
		if err == nil {
			return nil
		}

		lastErr = err
	}
	// If SSE already started, do not http.Error (corrupts stream); emit SSE error event.
	ct := w.Header().Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		if _, err := w.Write([]byte(`data: {"error":"all combo models failed"}` + "\n\n")); err != nil {
			_ = err
		}

		if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
			_ = err
		}

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		return lastErr
	}

	http.Error(w, `{"error":"all combo models failed"}`, http.StatusBadGateway)

	return lastErr
}

// PrepareModelsWithCapacityAdapter prepares models slice augmented with capacity adapter and rotated/reordered.
func PrepareModelsWithCapacityAdapter(models []string, comboName, strategy string, sticky int, body []byte, st *store.Store) []string {
	if len(models) == 0 {
		return models
	}

	var m map[string]any
	if body != nil {
		if err := json.Unmarshal(body, &m); err != nil {
			_ = err
		}
	}

	reqCaps := DetectRequiredCapabilities(m)
	cfg := LoadCapacityAdapterConfig(st)
	augmented := AugmentModelsWithCapacityAdapter(models, reqCaps, cfg)

	rotated := GetRotatedModels(augmented, comboName, strategy, sticky)

	return ReorderForCapabilities(rotated, body)
}

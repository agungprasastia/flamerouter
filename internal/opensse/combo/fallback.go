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

func (f *FallbackStrategy) Execute(ctx context.Context, w http.ResponseWriter, body []byte,
	models []string, st *store.Store, exec executor.Executor,
	fb *fallback.Fallback, opts Options,
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
		_, _ = w.Write([]byte(`data: {"error":"all combo models failed"}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		return lastErr
	}

	http.Error(w, `{"error":"all combo models failed"}`, http.StatusBadGateway)

	return lastErr
}

func PrepareModelsWithCapacityAdapter(models []string, comboName, strategy string, sticky int, body []byte, st *store.Store) []string {
	if len(models) == 0 {
		return models
	}

	var m map[string]any
	if body != nil {
		_ = json.Unmarshal(body, &m)
	}

	reqCaps := DetectRequiredCapabilities(m)
	cfg := LoadCapacityAdapterConfig(st)
	augmented := AugmentModelsWithCapacityAdapter(models, reqCaps, cfg)

	rotated := GetRotatedModels(augmented, comboName, strategy, sticky)

	return ReorderForCapabilities(rotated, body)
}

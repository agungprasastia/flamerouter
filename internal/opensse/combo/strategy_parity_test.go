package combo_test

import (
	"context"
	"errors"
	"flamerouter/internal/opensse/combo"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComboFallbackSequentialExecution(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	fb := fallback.New(st)
	start := &combo.FallbackStrategy{}

	var attempted []string

	singleRunner := func(ctx context.Context, w *httptest.ResponseRecorder, body []byte, modelStr string, stream bool) error {
		attempted = append(attempted, modelStr)

		if modelStr == "openai/gpt-4o" {
			return errors.New("provider rate limited")
		}
		// Second model succeeds
		return nil
	}

	rec := httptest.NewRecorder()
	opts := combo.Options{
		ComboName: "test-combo",
		SingleModel: func(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, stream bool) error {
			return singleRunner(ctx, rec, body, modelStr, stream)
		},
	}

	err = start.Execute(context.Background(), rec, []byte(`{}`), []string{"openai/gpt-4o", "anthropic/claude-3-5-sonnet"}, st, nil, fb, opts)
	if err != nil {
		t.Fatalf("unexpected combo error: %v", err)
	}

	if len(attempted) != 2 {
		t.Fatalf("expected 2 attempts, got %v", attempted)
	}

	if attempted[0] != "openai/gpt-4o" || attempted[1] != "anthropic/claude-3-5-sonnet" {
		t.Fatalf("wrong order of attempts: %v", attempted)
	}
}

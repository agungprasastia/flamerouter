package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flamerouter/internal/opensse/models"
	"flamerouter/internal/store"
)

type dummyResolverForHandlersTest struct {
	fn func(ctx context.Context, conn *store.Connection) ([]models.DynamicModel, error)
}

func (d *dummyResolverForHandlersTest) Resolve(ctx context.Context, conn *store.Connection) ([]models.DynamicModel, error) {
	return d.fn(ctx, conn)
}

func (d *dummyResolverForHandlersTest) TTL() time.Duration {
	return 5 * time.Minute
}

func TestDynamicModelsResolutionInHandlers(t *testing.T) {
	st := newTestStore(t)
	_, err := st.CreateOAuthConnection("kiro", "oauth", "Kiro Dev", "dummy-access-token", "dummy-refresh-token", "", map[string]any{
		"clientId": "test-client",
	})
	if err != nil {
		t.Fatalf("CreateOAuthConnection: %v", err)
	}

	// Register test dynamic resolver
	models.DefaultEngine.Register("kiro", &dummyResolverForHandlersTest{
		fn: func(ctx context.Context, conn *store.Connection) ([]models.DynamicModel, error) {
			return []models.DynamicModel{
				{
					ID:            "claude-opus-4.8",
					Name:          "Kiro Claude Opus 4.8",
					ContextLength: 200000,
					Capabilities:  map[string]any{"thinking": false, "agentic": false},
				},
				{
					ID:            "claude-opus-4.8-thinking",
					Name:          "Kiro Claude Opus 4.8 (Thinking)",
					ContextLength: 200000,
					Capabilities:  map[string]any{"thinking": true, "agentic": false},
				},
			}, nil
		},
	})
	models.DefaultEngine.ClearCache()

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	Models(rec, req, st)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal /v1/models: %v", err)
	}

	foundDynamic := false
	for _, m := range resp.Data {
		if id, ok := m["id"].(string); ok && id == "kr/claude-opus-4.8" {
			foundDynamic = true
			break
		}
	}

	if !foundDynamic {
		t.Fatalf("expected dynamic model kr/claude-opus-4.8 in /v1/models response, got %+v", resp.Data)
	}
}

func TestDynamicModelsFallbackToStaticOnNetworkError(t *testing.T) {
	st := newTestStore(t)
	_, err := st.CreateOAuthConnection("kiro", "oauth", "Kiro Dev", "dummy-access-token", "dummy-refresh-token", "", map[string]any{
		"clientId": "test-client",
	})
	if err != nil {
		t.Fatalf("CreateOAuthConnection: %v", err)
	}

	// Register failing dynamic resolver to verify fallback to static registry
	models.DefaultEngine.Register("kiro", &dummyResolverForHandlersTest{
		fn: func(ctx context.Context, conn *store.Connection) ([]models.DynamicModel, error) {
			return nil, context.DeadlineExceeded
		},
	})
	models.DefaultEngine.ClearCache()

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	Models(rec, req, st)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal /v1/models: %v", err)
	}

	// Should fallback to static registry for kiro (which contains e.g. kr/claude-opus-5 or kr/claude-opus-4.8)
	foundStatic := false
	for _, m := range resp.Data {
		if id, ok := m["id"].(string); ok && (id == "kr/claude-opus-5" || id == "kr/claude-opus-4.8") {
			foundStatic = true
			break
		}
	}

	if !foundStatic {
		t.Fatalf("expected static kiro models on error fallback, got %+v", resp.Data)
	}
}

package models

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flamerouter/internal/store"
)

func TestEngineCachingAndDeduplication(t *testing.T) {
	callCount := 0
	dummyResolver := &testDummyResolver{
		ttl: 5 * time.Minute,
		fn: func(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
			callCount++
			return []DynamicModel{
				{ID: "test-model-1", Name: "Test Model 1"},
				{ID: "test-model-2", Name: "Test Model 2"},
			}, nil
		},
	}

	engine := NewEngine()
	engine.Register("dummy", dummyResolver)

	conn := &store.Connection{
		ID:          "conn-1",
		Provider:    "dummy",
		AccessToken: "dummy-token-123",
	}

	ctx := context.Background()

	// 1. Initial resolution
	models1, err := engine.ResolveModels(ctx, conn)
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	if len(models1) != 2 || callCount != 1 {
		t.Fatalf("expected 2 models and 1 fetch call, got %d models, %d calls", len(models1), callCount)
	}

	// 2. Cached resolution (should not increment callCount)
	models2, err := engine.ResolveModels(ctx, conn)
	if err != nil {
		t.Fatalf("cached resolve failed: %v", err)
	}
	if len(models2) != 2 || callCount != 1 {
		t.Fatalf("expected 2 models from cache without new fetch call, got %d calls", callCount)
	}

	// 3. Invalidation
	engine.InvalidateCache(conn)
	models3, err := engine.ResolveModels(ctx, conn)
	if err != nil {
		t.Fatalf("post-invalidation resolve failed: %v", err)
	}
	if len(models3) != 2 || callCount != 2 {
		t.Fatalf("expected 2 models and 2 fetch calls after invalidation, got %d calls", callCount)
	}
}

func TestCopilotResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-copilot-token" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [
				{"id":"gpt-4o","name":"GPT-4o","capabilities":{"type":"chat"},"policy":{"state":"enabled"}},
				{"id":"text-embedding-3-small","name":"Text Embed","capabilities":{"type":"embedding"},"policy":{"state":"enabled"}},
				{"id":"claude-3-7-sonnet","name":"Claude 3.7 Sonnet","capabilities":{"type":"chat"},"policy":{"state":"enabled"}},
				{"id":"disabled-model","name":"Disabled","capabilities":{"type":"chat"},"policy":{"state":"disabled"}}
			]
		}`)
	}))
	defer server.Close()

	resolver := &CopilotResolver{
		Client: server.Client(),
	}

	// Override URL with test server client custom transport
	oldTransport := server.Client().Transport
	server.Client().Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return oldTransport.RoundTrip(req)
	})

	conn := &store.Connection{
		ID:          "gh-conn",
		Provider:    "github",
		AccessToken: "valid-copilot-token",
	}

	res, err := resolver.Resolve(context.Background(), conn)
	if err != nil {
		t.Fatalf("copilot resolve failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 enabled chat models, got %d", len(res))
	}
	if res[0].ID != "gpt-4o" || res[1].ID != "claude-3-7-sonnet" {
		t.Fatalf("unexpected model IDs: %+v", res)
	}
}

func TestKiroResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"models": [
				{
					"modelId": "claude-opus-4.8",
					"modelName": "Claude Opus 4.8",
					"description": "Opus model",
					"rateMultiplier": 1.5,
					"tokenLimits": {"maxInputTokens": 200000}
				},
				{
					"modelId": "auto",
					"modelName": "Auto",
					"description": "Auto model",
					"rateMultiplier": 1.0,
					"tokenLimits": {"maxInputTokens": 200000}
				}
			]
		}`)
	}))
	defer server.Close()

	resolver := &KiroResolver{
		Client: server.Client(),
	}

	oldTransport := server.Client().Transport
	server.Client().Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return oldTransport.RoundTrip(req)
	})

	conn := &store.Connection{
		ID:          "kiro-conn",
		Provider:    "kiro",
		AccessToken: "kiro-token",
	}

	res, err := resolver.Resolve(context.Background(), conn)
	if err != nil {
		t.Fatalf("kiro resolve failed: %v", err)
	}
	// claude-opus-4.8 produces 4 variants (base, -thinking, -agentic, -thinking-agentic)
	// auto produces 2 variants (base, -thinking)
	if len(res) != 6 {
		t.Fatalf("expected 6 variants, got %d", len(res))
	}
	if res[0].ID != "claude-opus-4.8" || res[0].Name != "Kiro Claude Opus 4.8 (1.5x credit)" {
		t.Fatalf("unexpected first model: %+v", res[0])
	}
	if res[1].ID != "claude-opus-4.8-thinking" {
		t.Fatalf("unexpected second model: %+v", res[1])
	}
	if res[4].ID != "auto" || res[4].Name != "Kiro Auto" {
		t.Fatalf("unexpected auto model: %+v", res[4])
	}
}

func TestGrokCliResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [
				{"id":"grok-build","display_name":"Grok Build","context_length":500000,"max_output_tokens":64000},
				{"id":"grok-4.5","display_name":"Grok 4.5","context_length":131072,"max_output_tokens":16384}
			]
		}`)
	}))
	defer server.Close()

	resolver := &GrokCliResolver{
		Client: server.Client(),
	}

	oldTransport := server.Client().Transport
	server.Client().Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return oldTransport.RoundTrip(req)
	})

	conn := &store.Connection{
		ID:          "gcli-conn",
		Provider:    "grok-cli",
		AccessToken: "grok-token",
	}

	res, err := resolver.Resolve(context.Background(), conn)
	if err != nil {
		t.Fatalf("grok-cli resolve failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 models, got %d", len(res))
	}
	if res[0].ID != "grok-build" || res[0].ContextLength != 500000 {
		t.Fatalf("unexpected grok-build model: %+v", res[0])
	}
}

func TestKimchiResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"models": [
				{
					"slug": "kimi-k2.7",
					"display_name": "Kimi K2.7",
					"provider": "moonshot",
					"reasoning": true,
					"input_modalities": ["text", "image"],
					"limits": {"context_window": 200000, "max_output_tokens": 16384}
				},
				{
					"slug": "minimax-m3",
					"display_name": "MiniMax M3",
					"provider": "minimax",
					"reasoning": false,
					"input_modalities": ["text"],
					"limits": {"context_window": 1000000, "max_output_tokens": 8192}
				}
			]
		}`)
	}))
	defer server.Close()

	resolver := &KimchiResolver{
		Client: server.Client(),
	}

	conn := &store.Connection{
		ID:          "kimchi-conn",
		Provider:    "kimchi",
		AccessToken: "kimchi-token",
		BaseURL:     server.URL,
	}

	res, err := resolver.Resolve(context.Background(), conn)
	if err != nil {
		t.Fatalf("kimchi resolve failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 models, got %d", len(res))
	}
	if res[0].ID != "kimi-k2.7" || !res[0].IsVL || !res[0].IsReasoning {
		t.Fatalf("unexpected kimchi model: %+v", res[0])
	}
}

func TestClinePassResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"data": [
				{"id":"cline-pass/glm-5.2","name":"GLM 5.2 (ClinePass)"},
				{"id":"cline-pass/kimi-k2.7-code","name":"Kimi K2.7 Code (ClinePass)"},
				{"id":"other/model","name":"Other Model"}
			]
		}`)
	}))
	defer server.Close()

	resolver := &ClinePassResolver{
		Client: server.Client(),
	}

	oldTransport := server.Client().Transport
	server.Client().Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return oldTransport.RoundTrip(req)
	})

	conn := &store.Connection{
		ID:       "clinepass-conn",
		Provider: "clinepass",
		APIKey:   "cp-test-key",
	}

	res, err := resolver.Resolve(context.Background(), conn)
	if err != nil {
		t.Fatalf("clinepass resolve failed: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 cline-pass models, got %d", len(res))
	}
	if res[0].ID != "cline-pass/glm-5.2" || res[1].ID != "cline-pass/kimi-k2.7-code" {
		t.Fatalf("unexpected clinepass models: %+v", res)
	}
}

func TestQoderResolver(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"chat": [
				{
					"key": "qmodel",
					"display_name": "QModel",
					"is_reasoning": true,
					"is_vl": true,
					"max_input_tokens": 131072,
					"max_output_tokens": 32768,
					"enable": true
				},
				{
					"key": "lite",
					"display_name": "Lite",
					"is_reasoning": false,
					"max_input_tokens": 131072,
					"max_output_tokens": 16384,
					"enable": false
				}
			]
		}`)
	}))
	defer server.Close()

	resolver := &QoderResolver{
		Client: server.Client(),
	}

	oldTransport := server.Client().Transport
	server.Client().Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = server.Listener.Addr().String()
		return oldTransport.RoundTrip(req)
	})

	conn := &store.Connection{
		ID:          "qoder-conn",
		Provider:    "qoder",
		AccessToken: "dt-test-token",
		ProviderSpecificData: map[string]any{
			"userId": "user-123",
		},
	}

	res, err := resolver.Resolve(context.Background(), conn)
	if err != nil {
		t.Fatalf("qoder resolve failed: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 enabled model, got %d", len(res))
	}
	if res[0].ID != "qmodel" || !res[0].IsReasoning || !res[0].IsVL {
		t.Fatalf("unexpected qoder model: %+v", res[0])
	}
}

type testDummyResolver struct {
	ttl time.Duration
	fn  func(ctx context.Context, conn *store.Connection) ([]DynamicModel, error)
}

func (r *testDummyResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	return r.fn(ctx, conn)
}

func (r *testDummyResolver) TTL() time.Duration {
	return r.ttl
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"flamerouter/internal/store"
)

// DynamicModel represents a discovered model from a provider.
type DynamicModel struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ContextLength   int            `json:"contextLength,omitempty"`
	MaxOutputTokens int            `json:"maxOutputTokens,omitempty"`
	IsReasoning     bool           `json:"isReasoning,omitempty"`
	IsVL            bool           `json:"isVL,omitempty"`
	RateMultiplier  float64        `json:"rateMultiplier,omitempty"`
	UpstreamModelID string         `json:"upstreamModelId,omitempty"`
	Description     string         `json:"description,omitempty"`
	Capabilities    map[string]any `json:"capabilities,omitempty"`
	RawConfig       map[string]any `json:"rawConfig,omitempty"`
}

// ProviderResolver resolves live dynamic models for a connection.
type ProviderResolver interface {
	Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error)
	TTL() time.Duration
}

type cacheEntry struct {
	expiresAt time.Time
	models    []DynamicModel
}

// Engine coordinates dynamic model resolution across providers with in-memory TTL caching.
type Engine struct {
	mu        sync.RWMutex
	resolvers map[string]ProviderResolver
	cache     map[string]cacheEntry
	inflight  map[string]chan struct{}
}

// DefaultEngine is the global default dynamic models resolver engine.
var DefaultEngine = NewEngine()

// NewEngine creates a new dynamic model resolver engine and registers default provider fetchers.
func NewEngine() *Engine {
	e := &Engine{
		resolvers: make(map[string]ProviderResolver),
		cache:     make(map[string]cacheEntry),
		inflight:  make(map[string]chan struct{}),
	}
	e.RegisterDefaultResolvers()
	return e
}

// Register registers a provider resolver.
func (e *Engine) Register(provider string, r ProviderResolver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resolvers[provider] = r
}

// ClearCache clears all cached model catalogs.
func (e *Engine) ClearCache() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = make(map[string]cacheEntry)
}

// InvalidateCache invalidates the cache for a specific connection.
func (e *Engine) InvalidateCache(conn *store.Connection) {
	if conn == nil {
		return
	}
	key := e.cacheKey(conn)
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.cache, key)
}

func (e *Engine) cacheKey(conn *store.Connection) string {
	seed := conn.AccessToken
	if seed == "" {
		seed = conn.APIKey
	}
	if seed == "" {
		seed = conn.RefreshToken
	}
	if seed == "" && conn.ProviderSpecificData != nil {
		for _, k := range []string{"clientId", "profileArn", "userId", "username", "copilotToken"} {
			if v, ok := conn.ProviderSpecificData[k].(string); ok && v != "" {
				seed = v
				break
			}
		}
	}
	if seed == "" {
		seed = conn.ID
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s", conn.Provider, conn.BaseURL, seed)))
	return hex.EncodeToString(sum[:])
}

// ResolveModels attempts to fetch or retrieve cached dynamic models for a connection.
// Returns (models, nil) on success, or error on failure.
func (e *Engine) ResolveModels(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	if conn == nil {
		return nil, nil
	}
	e.mu.RLock()
	resolver, ok := e.resolvers[conn.Provider]
	e.mu.RUnlock()
	if !ok || resolver == nil {
		return nil, nil
	}

	key := e.cacheKey(conn)
	now := time.Now()

	e.mu.RLock()
	entry, found := e.cache[key]
	e.mu.RUnlock()

	if found && now.Before(entry.expiresAt) {
		return entry.models, nil
	}

	// Singleflight deduplication
	e.mu.Lock()
	if entry, found = e.cache[key]; found && now.Before(entry.expiresAt) {
		e.mu.Unlock()
		return entry.models, nil
	}
	waitChan, inProgress := e.inflight[key]
	if inProgress {
		e.mu.Unlock()
		select {
		case <-waitChan:
			e.mu.RLock()
			entry = e.cache[key]
			e.mu.RUnlock()
			return entry.models, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	ch := make(chan struct{})
	e.inflight[key] = ch
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.inflight, key)
		close(ch)
		e.mu.Unlock()
	}()

	models, err := resolver.Resolve(ctx, conn)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}

	ttl := resolver.TTL()
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	e.mu.Lock()
	e.cache[key] = cacheEntry{
		expiresAt: time.Now().Add(ttl),
		models:    models,
	}
	e.mu.Unlock()

	return models, nil
}

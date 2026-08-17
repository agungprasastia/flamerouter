// Package tokenrefresh provides token refresh management, deduplication, and caching.
package tokenrefresh

import (
	"context"
	"sync"
	"time"
)

// DefaultRefreshResultTTL is the default cache expiration for refreshed tokens.
const DefaultRefreshResultTTL = 10 * time.Second

type call struct {
	err error
	val *RefreshResult
	wg  sync.WaitGroup
}

type cacheEntry struct {
	result    *RefreshResult
	call      *call
	expiresAt time.Time
}

// DedupGroup handles in-flight deduplication and short-lived caching for token refreshes.
type DedupGroup struct {
	cache   map[string]*cacheEntry
	timeNow func() time.Time
	ttl     time.Duration
	mu      sync.Mutex
}

// NewDedupGroup creates a new DedupGroup with given TTL.
func NewDedupGroup(ttl time.Duration) *DedupGroup {
	if ttl <= 0 {
		ttl = DefaultRefreshResultTTL
	}

	return &DedupGroup{
		cache:   make(map[string]*cacheEntry),
		ttl:     ttl,
		timeNow: time.Now,
		mu:      sync.Mutex{},
	}
}

// Do executes or reuses an in-flight refresh or cached result for the given key.
func (g *DedupGroup) Do(_ context.Context, key string, fn func() (*RefreshResult, error)) (*RefreshResult, error) {
	if key == "" {
		return fn()
	}

	g.mu.Lock()
	now := g.timeNow()

	if entry, ok := g.cache[key]; ok {
		// If there is an in-flight call
		if entry.call != nil {
			c := entry.call
			g.mu.Unlock()
			c.wg.Wait()

			return c.val, c.err
		}
		// If cached result is still valid
		if now.Before(entry.expiresAt) {
			val := entry.result
			g.mu.Unlock()

			return val, nil
		}
		// Expired
		delete(g.cache, key)
	}

	c := new(call)
	c.wg.Add(1)
	g.cache[key] = &cacheEntry{
		result:    nil,
		call:      c,
		expiresAt: time.Time{},
	}
	g.mu.Unlock()

	res, err := fn()

	g.mu.Lock()
	c.val = res
	c.err = err
	c.wg.Done()

	if err == nil && res != nil && res.Error == "" {
		g.cache[key] = &cacheEntry{
			result:    res,
			call:      nil,
			expiresAt: g.timeNow().Add(g.ttl),
		}
	} else {
		delete(g.cache, key)
	}
	g.mu.Unlock()

	return res, err
}

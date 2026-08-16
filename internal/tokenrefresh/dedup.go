package tokenrefresh

import (
	"context"
	"sync"
	"time"
)

const DefaultRefreshResultTTL = 10 * time.Second

type call struct {
	wg     sync.WaitGroup
	val    *RefreshResult
	err    error
	shared bool
}

type cacheEntry struct {
	result    *RefreshResult
	expiresAt time.Time
	call      *call
}

// DedupGroup handles in-flight deduplication and short-lived caching for token refreshes.
type DedupGroup struct {
	mu      sync.Mutex
	cache   map[string]*cacheEntry
	ttl     time.Duration
	timeNow func() time.Time
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
	}
}

// Do executes or reuses an in-flight refresh or cached result for the given key.
func (g *DedupGroup) Do(ctx context.Context, key string, fn func() (*RefreshResult, error)) (*RefreshResult, error) {
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
		call: c,
	}
	g.mu.Unlock()

	res, err := fn()

	g.mu.Lock()
	c.val = res
	c.err = err
	if err == nil && res != nil && res.Error == "" {
		// Cache success result
		g.cache[key] = &cacheEntry{
			result:    res,
			expiresAt: g.timeNow().Add(g.ttl),
		}
	} else {
		// Do not cache errors
		delete(g.cache, key)
	}
	c.wg.Done()
	g.mu.Unlock()

	return res, err
}

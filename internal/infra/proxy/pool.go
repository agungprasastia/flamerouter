// Package proxy manages outbound proxy rotation and transport configuration.
package proxy

import (
	"flamerouter/internal/store"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// Pool manages a rotating pool of HTTP/SOCKS proxies.
type Pool struct {
	st      *store.Store
	mu      sync.Mutex
	current int
}

// New creates a new proxy Pool instance.
func New(st *store.Store) *Pool {
	return &Pool{st: st, mu: sync.Mutex{}, current: 0}
}

// Next returns the next active proxy URL from the pool, or nil for direct.
func (p *Pool) Next() *url.URL {
	if p.st == nil {
		return nil
	}

	pools, err := p.st.ListProxyPools()
	if err != nil || len(pools) == 0 {
		return nil
	}

	active := make([]store.ProxyPool, 0, len(pools))

	for _, pl := range pools {
		if pl.IsActive {
			active = append(active, pl)
		}
	}

	if len(active) == 0 {
		// fall back to all if none marked active
		active = pools
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	pl := active[p.current%len(active)]
	p.current++

	return poolToURL(pl)
}

func poolToURL(pl store.ProxyPool) *url.URL {
	scheme := pl.Type
	if scheme == "" {
		scheme = "http"
	}

	host := fmt.Sprintf("%s:%d", pl.Host, pl.Port)
	u := &url.URL{
		Scheme:      scheme,
		Host:        host,
		Opaque:      "",
		User:        nil,
		Path:        "",
		RawPath:     "",
		OmitHost:    false,
		ForceQuery:  false,
		RawQuery:    "",
		Fragment:    "",
		RawFragment: "",
	}

	if pl.Username != "" {
		if pl.Password != "" {
			u.User = url.UserPassword(pl.Username, pl.Password)
		} else {
			u.User = url.User(pl.Username)
		}
	}

	return u
}

// Transport returns an http.Transport using the pool's next proxy.
func (p *Pool) Transport() *http.Transport {
	proxy := p.Next()

	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}

	tr := t.Clone()
	if proxy != nil {
		tr.Proxy = http.ProxyURL(proxy)
	}

	return tr
}

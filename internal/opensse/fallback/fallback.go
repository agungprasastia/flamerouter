package fallback

import (
	"flamerouter/internal/opensse/config"
	"flamerouter/internal/store"
	"log"
	"sort"
	"sync"
	"time"
)

// AccountState holds circuit breaking state for a connection.
type AccountState struct {
	ConnectionID     string
	UnavailableUntil string
	BackoffLevel     int
}

// rrConnState tracks sticky round-robin use in memory.
type rrConnState struct {
	lastUsedAt          string
	consecutiveUseCount int
	lastUsedSeq         int64
}

// Fallback manages multi-account failover and rate-limit backoff.
type Fallback struct {
	stores   *store.Store
	states   map[string]*AccountState
	rr       map[string]*rrConnState
	rrSeq    int64
	statesMu sync.RWMutex
	rrMu     sync.Mutex
}

// New creates a new Fallback manager.
func New(st *store.Store) *Fallback {
	return &Fallback{
		stores:   st,
		states:   make(map[string]*AccountState),
		rr:       make(map[string]*rrConnState),
		rrSeq:    0,
		statesMu: sync.RWMutex{},
		rrMu:     sync.Mutex{},
	}
}

// GetState returns the current AccountState for a connection.
func (f *Fallback) GetState(connID string) *AccountState {
	f.statesMu.RLock()
	defer f.statesMu.RUnlock()

	if s, ok := f.states[connID]; ok {
		return s
	}

	return &AccountState{
		ConnectionID:     connID,
		UnavailableUntil: "",
		BackoffLevel:     0,
	}
}

// MarkUnavailable records an error and calculates the cooldown period.
func (f *Fallback) MarkUnavailable(connID string, status int, errorText string, resetsAtMs int64) (shouldFallback bool, cooldownMs int64) {
	f.statesMu.Lock()
	defer f.statesMu.Unlock()

	state := f.states[connID]
	if state == nil {
		state = &AccountState{
			ConnectionID:     connID,
			UnavailableUntil: "",
			BackoffLevel:     0,
		}
		f.states[connID] = state
	}

	if resetsAtMs > 0 {
		cooldownMs = resetsAtMs
	} else {
		var newLevel int
		shouldFallback, cooldownMs, newLevel = config.CheckFallbackError(status, errorText, state.BackoffLevel)
		state.BackoffLevel = newLevel

		if !shouldFallback {
			return false, 0
		}
	}

	state.UnavailableUntil = config.GetUnavailableUntil(cooldownMs)
	//nolint:errcheck // best effort DB update
	_ = f.stores.MarkConnectionUnavailable(connID, cooldownMs)
	log.Printf("[FALLBACK] conn=%s status=%d cooldown=%dms until=%s", connID, status, cooldownMs, state.UnavailableUntil)

	return true, cooldownMs
}

// ClearError resets backoff state on successful request.
func (f *Fallback) ClearError(connID string) {
	f.statesMu.Lock()
	defer f.statesMu.Unlock()

	if s, ok := f.states[connID]; ok {
		s.BackoffLevel = 0
		s.UnavailableUntil = ""
	}

	//nolint:errcheck // best effort DB update
	_ = f.stores.ClearConnectionError(connID)
}

// SelectAccount returns the first available connection for provider.
//
//nolint:nilnil // returning (nil, nil) when no connection found is by design
func (f *Fallback) SelectAccount(provider string) (*store.Connection, error) {
	conns, err := f.stores.ListActiveByProvider(provider)
	if err != nil {
		return nil, err
	}

	if len(conns) == 0 {
		return nil, nil
	}

	f.statesMu.RLock()
	defer f.statesMu.RUnlock()

	for _, c := range conns {
		if config.IsAccountUnavailable(c.RateLimitedUntil) {
			continue
		}

		return &c, nil
	}

	return nil, nil
}

// SelectAccountExcluding returns the first available connection excluding given IDs.
//
//nolint:nilnil // returning (nil, nil) when no connection found is by design
func (f *Fallback) SelectAccountExcluding(provider string, excludeIDs map[string]bool) (*store.Connection, error) {
	conns, err := f.stores.ListActiveByProvider(provider)
	if err != nil {
		return nil, err
	}

	if len(conns) == 0 {
		return nil, nil
	}

	f.statesMu.RLock()
	defer f.statesMu.RUnlock()

	for _, c := range conns {
		if excludeIDs[c.ID] {
			continue
		}

		if config.IsAccountUnavailable(c.RateLimitedUntil) {
			continue
		}

		return &c, nil
	}

	return nil, nil
}

// SelectAccountWithStrategy selects an account using either round-robin or fill-first.
//
//nolint:nilnil // returning (nil, nil) when no connection found is by design
func (f *Fallback) SelectAccountWithStrategy(provider, strategy string, stickyLimit int, excludeIDs map[string]bool) (*store.Connection, error) {
	switch strategy {
	case "round-robin":
		return f.selectRoundRobin(provider, stickyLimit, excludeIDs)
	default:
		return f.SelectAccountExcluding(provider, excludeIDs)
	}
}

type rrView struct {
	conn  store.Connection
	count int
	seq   int64 // 0 = never used
}

func filterAvailableConns(conns []store.Connection, excludeIDs map[string]bool) []store.Connection {
	base := make([]store.Connection, 0, len(conns))

	for _, c := range conns {
		if excludeIDs[c.ID] {
			continue
		}

		if config.IsAccountUnavailable(c.RateLimitedUntil) {
			continue
		}

		base = append(base, c)
	}

	return base
}

func (f *Fallback) buildRRViews(base []store.Connection) []rrView {
	available := make([]rrView, 0, len(base))

	for _, c := range base {
		v := rrView{
			conn:  c,
			count: 0,
			seq:   0,
		}

		if st := f.rr[c.ID]; st != nil {
			v.count = st.consecutiveUseCount
			v.seq = st.lastUsedSeq
			c.ConsecutiveUseCount = st.consecutiveUseCount
			c.LastUsedAt = st.lastUsedAt
			v.conn = c
		}

		available = append(available, v)
	}

	return available
}

func sortRRViews(views []rrView, desc bool) []rrView {
	sorted := append([]rrView(nil), views...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ai, aj := sorted[i].seq, sorted[j].seq
		if ai == 0 && aj == 0 {
			return sorted[i].conn.Priority < sorted[j].conn.Priority
		}

		if ai == 0 {
			return false
		}

		if aj == 0 {
			return true
		}

		if ai != aj {
			if desc {
				return ai > aj
			}

			return ai < aj
		}

		return sorted[i].conn.Priority < sorted[j].conn.Priority
	})

	return sorted
}

//nolint:nilnil // returning (nil, nil) when no connection found is by design
func (f *Fallback) selectRoundRobin(provider string, stickyLimit int, excludeIDs map[string]bool) (*store.Connection, error) {
	conns, err := f.stores.ListActiveByProvider(provider)
	if err != nil {
		return nil, err
	}

	base := filterAvailableConns(conns, excludeIDs)
	if len(base) == 0 {
		return nil, nil
	}

	limit := stickyLimit
	if limit <= 0 {
		limit = 3 // parity with 9router stickyRoundRobinLimit
	}

	f.rrMu.Lock()
	defer f.rrMu.Unlock()

	available := f.buildRRViews(base)
	byRecency := sortRRViews(available, true)
	current := byRecency[0]

	var chosen rrView
	if current.seq > 0 && current.count < limit {
		chosen = current
		f.touchRR(chosen.conn.ID, current.count+1)
	} else {
		byOldest := sortRRViews(available, false)
		chosen = byOldest[0]
		f.touchRR(chosen.conn.ID, 1)
	}

	if st := f.rr[chosen.conn.ID]; st != nil {
		chosen.conn.ConsecutiveUseCount = st.consecutiveUseCount
		chosen.conn.LastUsedAt = st.lastUsedAt
	}

	out := chosen.conn

	return &out, nil
}

func (f *Fallback) touchRR(connID string, count int) {
	// caller holds rrMu
	st := f.rr[connID]
	if st == nil {
		st = &rrConnState{
			lastUsedAt:          "",
			consecutiveUseCount: 0,
			lastUsedSeq:         0,
		}
		f.rr[connID] = st
	}

	st.consecutiveUseCount = count
	f.rrSeq++
	st.lastUsedSeq = f.rrSeq
	st.lastUsedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

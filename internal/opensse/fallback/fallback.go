package fallback

import (
	"flamerouter/internal/opensse/config"
	"flamerouter/internal/store"
	"log"
	"sort"
	"sync"
	"time"
)

type AccountState struct {
	ConnectionID     string
	UnavailableUntil string
	BackoffLevel     int
}

// rrConnState tracks sticky round-robin use in memory (DB columns deferred to Task 8).
type rrConnState struct {
	lastUsedAt          string
	consecutiveUseCount int
	lastUsedSeq         int64
}

type Fallback struct {
	stores   *store.Store
	states   map[string]*AccountState
	rr       map[string]*rrConnState
	rrSeq    int64
	statesMu sync.RWMutex
	rrMu     sync.Mutex
}

func New(st *store.Store) *Fallback {
	return &Fallback{
		stores: st,
		states: make(map[string]*AccountState),
		rr:     make(map[string]*rrConnState),
	}
}

func (f *Fallback) GetState(connID string) *AccountState {
	f.statesMu.RLock()
	defer f.statesMu.RUnlock()

	if s, ok := f.states[connID]; ok {
		return s
	}

	return &AccountState{ConnectionID: connID}
}

func (f *Fallback) MarkUnavailable(connID string, status int, errorText string, resetsAtMs int64) (shouldFallback bool, cooldownMs int64) {
	f.statesMu.Lock()
	defer f.statesMu.Unlock()

	state := f.states[connID]
	if state == nil {
		state = &AccountState{ConnectionID: connID}
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
	_ = f.stores.MarkConnectionUnavailable(connID, cooldownMs)
	log.Printf("[FALLBACK] conn=%s status=%d cooldown=%dms until=%s", connID, status, cooldownMs, state.UnavailableUntil)

	return true, cooldownMs
}

func (f *Fallback) ClearError(connID string) {
	f.statesMu.Lock()
	defer f.statesMu.Unlock()

	if s, ok := f.states[connID]; ok {
		s.BackoffLevel = 0
		s.UnavailableUntil = ""
	}

	_ = f.stores.ClearConnectionError(connID)
}

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

// stickyLimit: for round-robin, how many requests before rotation.
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

func (f *Fallback) selectRoundRobin(provider string, stickyLimit int, excludeIDs map[string]bool) (*store.Connection, error) {
	conns, err := f.stores.ListActiveByProvider(provider)
	if err != nil {
		return nil, err
	}

	var base []store.Connection

	for _, c := range conns {
		if excludeIDs[c.ID] {
			continue
		}

		if config.IsAccountUnavailable(c.RateLimitedUntil) {
			continue
		}

		base = append(base, c)
	}

	if len(base) == 0 {
		return nil, nil
	}

	limit := stickyLimit
	if limit <= 0 {
		limit = 3 // parity with 9router stickyRoundRobinLimit
	}

	f.rrMu.Lock()
	defer f.rrMu.Unlock()

	available := make([]rrView, 0, len(base))

	for _, c := range base {
		v := rrView{conn: c}
		if st := f.rr[c.ID]; st != nil {
			v.count = st.consecutiveUseCount
			v.seq = st.lastUsedSeq
			c.ConsecutiveUseCount = st.consecutiveUseCount
			c.LastUsedAt = st.lastUsedAt
			v.conn = c
		}

		available = append(available, v)
	}

	// Most recent first (9router byRecency).
	byRecency := append([]rrView(nil), available...)
	sort.SliceStable(byRecency, func(i, j int) bool {
		ai, aj := byRecency[i].seq, byRecency[j].seq
		if ai == 0 && aj == 0 {
			return byRecency[i].conn.Priority < byRecency[j].conn.Priority
		}

		if ai == 0 {
			return false
		}

		if aj == 0 {
			return true
		}

		if ai != aj {
			return ai > aj
		}

		return byRecency[i].conn.Priority < byRecency[j].conn.Priority
	})

	current := byRecency[0]

	var chosen rrView

	if current.seq > 0 && current.count < limit {
		chosen = current
		f.touchRR(chosen.conn.ID, current.count+1)
	} else {
		// Least recently used (9router byOldest).
		byOldest := append([]rrView(nil), available...)
		sort.SliceStable(byOldest, func(i, j int) bool {
			ai, aj := byOldest[i].seq, byOldest[j].seq
			if ai == 0 && aj == 0 {
				return byOldest[i].conn.Priority < byOldest[j].conn.Priority
			}

			if ai == 0 {
				return true
			}

			if aj == 0 {
				return false
			}

			if ai != aj {
				return ai < aj
			}

			return byOldest[i].conn.Priority < byOldest[j].conn.Priority
		})

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
		st = &rrConnState{}
		f.rr[connID] = st
	}

	st.consecutiveUseCount = count
	f.rrSeq++
	st.lastUsedSeq = f.rrSeq
	st.lastUsedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

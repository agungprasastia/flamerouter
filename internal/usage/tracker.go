package usage

import (
	"flamerouter/internal/store"
	"sync/atomic"
	"time"
)

// Record captures a completed request's usage data.
type Record struct {
	RequestBody      string
	Model            string
	ConnectionID     string
	Client           string
	SourceFormat     string
	TargetFormat     string
	Provider         string
	ErrorText        string
	ResponsePreview  string
	Cost             float64
	StatusCode       int
	CompletionTokens int
	PromptTokens     int
	CachedTokens     int
	DurationMs       int64
}

// Tracker records usage asynchronously via a buffered channel.
type Tracker struct {
	st      *store.Store
	hub     *StreamHub
	ch      chan Record
	done    chan struct{}
	dropped atomic.Uint64
}

// NewTracker creates and starts a new usage Tracker.
func NewTracker(st *store.Store, hub *StreamHub) *Tracker {
	t := &Tracker{
		st:      st,
		hub:     hub,
		ch:      make(chan Record, 4096),
		done:    make(chan struct{}),
		dropped: atomic.Uint64{},
	}
	go t.loop()

	return t
}

// Track queues a Record for persistence and streaming broadcast.
func (t *Tracker) Track(r Record) {
	select {
	case t.ch <- r:
	default:
		t.dropped.Add(1)
	}
}

// Dropped returns the number of records dropped when the queue was full.
func (t *Tracker) Dropped() uint64 {
	return t.dropped.Load()
}

func (t *Tracker) loop() {
	for r := range t.ch {
		cost := r.Cost
		if cost == 0 && (r.PromptTokens > 0 || r.CompletionTokens > 0) {
			cost = CalculateCost(r.Provider, r.Model, r.PromptTokens, r.CachedTokens, r.CompletionTokens)
		}

		r.Cost = cost

		if err := t.st.InsertRequestDetail(store.RequestDetail{
			ID:               "",
			Timestamp:        "",
			Provider:         r.Provider,
			Model:            r.Model,
			ConnectionID:     r.ConnectionID,
			StatusCode:       r.StatusCode,
			DurationMs:       int(r.DurationMs),
			PromptTokens:     r.PromptTokens,
			CompletionTokens: r.CompletionTokens,
			CachedTokens:     r.CachedTokens,
			Cost:             cost,
			RequestBody:      r.RequestBody,
			ResponsePreview:  r.ResponsePreview,
			ErrorText:        r.ErrorText,
			Client:           r.Client,
			SourceFormat:     r.SourceFormat,
			TargetFormat:     r.TargetFormat,
		}); err != nil {
			_ = err
		}

		date := time.Now().UTC().Format("2006-01-02")
		if err := t.st.InsertUsageDaily(date, r.Provider, r.Model, 1, r.PromptTokens, r.CompletionTokens, r.CachedTokens, cost); err != nil {
			_ = err
		}

		if err := t.st.InsertUsage(r.Provider, r.Model, r.PromptTokens, r.CompletionTokens, r.ConnectionID); err != nil {
			_ = err
		}

		if t.hub != nil {
			t.hub.Broadcast(r)
		}
	}

	close(t.done)
}

// Close stops the tracker channel and waits for pending writes to finish.
func (t *Tracker) Close() {
	close(t.ch)
	<-t.done
}

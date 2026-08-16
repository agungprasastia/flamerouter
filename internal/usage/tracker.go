package usage

import (
	"flamerouter/internal/store"
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
	StatusCode       int
	CompletionTokens int
	PromptTokens     int
	DurationMs       int64
}

// Tracker records usage asynchronously via a buffered channel.
type Tracker struct {
	st   *store.Store
	hub  *StreamHub
	ch   chan Record
	done chan struct{}
}

func NewTracker(st *store.Store, hub *StreamHub) *Tracker {
	t := &Tracker{st: st, hub: hub, ch: make(chan Record, 256), done: make(chan struct{})}
	go t.loop()

	return t
}

func (t *Tracker) Track(r Record) {
	select {
	case t.ch <- r:
	default:
	}
}

func (t *Tracker) loop() {
	for r := range t.ch {
		_ = t.st.InsertRequestDetail(store.RequestDetail{
			Provider: r.Provider, Model: r.Model, ConnectionID: r.ConnectionID,
			StatusCode: r.StatusCode, DurationMs: int(r.DurationMs),
			PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
			RequestBody: r.RequestBody, ResponsePreview: r.ResponsePreview, ErrorText: r.ErrorText,
			Client: r.Client, SourceFormat: r.SourceFormat, TargetFormat: r.TargetFormat,
		})
		date := time.Now().UTC().Format("2006-01-02")
		_ = t.st.InsertUsageDaily(date, r.Provider, r.Model, 1, r.PromptTokens, r.CompletionTokens)
		_ = t.st.InsertUsage(r.Provider, r.Model, r.PromptTokens, r.CompletionTokens, r.ConnectionID)

		if t.hub != nil {
			t.hub.Broadcast(r)
		}
	}

	close(t.done)
}

func (t *Tracker) Close() {
	close(t.ch)
	<-t.done
}

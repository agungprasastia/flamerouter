package usage

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// RecentRequestItem matches the frontend RecentRequests table item shape.
type RecentRequestItem struct {
	Timestamp        string `json:"timestamp"`
	Model            string `json:"model"`
	Provider         string `json:"provider"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	DurationMs       int64  `json:"durationMs,omitempty"`
	Status           string `json:"status"`
}

// ActiveRequestItem matches the frontend active requests schema.
type ActiveRequestItem struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Account  string `json:"account"`
	Count    int    `json:"count"`
}

// StreamPayload matches the frontend SSE event payload in UsageStats.tsx.
type StreamPayload struct {
	ActiveRequests []ActiveRequestItem `json:"activeRequests"`
	RecentRequests []RecentRequestItem `json:"recentRequests"`
	ErrorProvider  string              `json:"errorProvider,omitempty"`
	Pending        map[string]any      `json:"pending,omitempty"`
}

// StreamHub broadcasts real-time usage events to connected SSE clients.
type StreamHub struct {
	mu            sync.RWMutex
	clients       map[chan []byte]struct{}
	recentRing    []RecentRequestItem
	recentCap     int
	lastErrProv   string
	lastErrTs     time.Time
}

func NewStreamHub() *StreamHub {
	return &StreamHub{
		clients:    make(map[chan []byte]struct{}),
		recentRing: make([]RecentRequestItem, 0, 50),
		recentCap:  50,
	}
}

func (h *StreamHub) PushRecent(r Record) {
	h.mu.Lock()
	defer h.mu.Unlock()

	status := "ok"
	if r.StatusCode >= 400 || r.ErrorText != "" {
		status = "error"
		if r.Provider != "" {
			h.lastErrProv = r.Provider
			h.lastErrTs = time.Now()
		}
	}

	item := RecentRequestItem{
		Timestamp:        time.Now().UTC().Format(time.RFC3339Nano),
		Model:            r.Model,
		Provider:         r.Provider,
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		DurationMs:       r.DurationMs,
		Status:           status,
	}

	h.recentRing = append(h.recentRing, item)
	if len(h.recentRing) > h.recentCap {
		h.recentRing = h.recentRing[len(h.recentRing)-h.recentCap:]
	}

	payload := h.buildPayloadLocked()
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for ch := range h.clients {
		select {
		case ch <- data:
		default:
		}
	}
}

func (h *StreamHub) GetRecent() []RecentRequestItem {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]RecentRequestItem, len(h.recentRing))
	for i, item := range h.recentRing {
		out[len(h.recentRing)-1-i] = item
	}
	return out
}

func (h *StreamHub) buildPayloadLocked() StreamPayload {
	// Reverse ring to have newest first
	n := len(h.recentRing)
	recent := make([]RecentRequestItem, n)
	for i, item := range h.recentRing {
		recent[n-1-i] = item
	}
	if len(recent) > 20 {
		recent = recent[:20]
	}

	errProv := ""
	if time.Since(h.lastErrTs) < 10*time.Second {
		errProv = h.lastErrProv
	}

	return StreamPayload{
		ActiveRequests: []ActiveRequestItem{},
		RecentRequests: recent,
		ErrorProvider:  errProv,
		Pending: map[string]any{
			"byModel":   map[string]int{},
			"byAccount": map[string]map[string]int{},
		},
	}
}

func (h *StreamHub) Broadcast(event Record) {
	h.PushRecent(event)
}

func (h *StreamHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan []byte, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	initPayload := h.buildPayloadLocked()
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	// Send initial snapshot
	if initData, err := json.Marshal(initPayload); err == nil {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(initData)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case data := <-ch:
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

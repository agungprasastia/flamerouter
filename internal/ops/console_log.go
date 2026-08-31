// Package ops provides operational utilities such as logging, updater, and shutdown handlers.
package ops

import (
	"sync"
)

const defaultConsoleMaxLines = 500

// ConsoleLog is a fixed-capacity ring buffer of log lines (9router consoleLogBuffer).
type ConsoleLog struct {
	subs  map[chan string]struct{}
	lines []string
	max   int
	mu    sync.Mutex
}

// DefaultConsole is the process-wide translator/dashboard console buffer.
var DefaultConsole = NewConsoleLog(defaultConsoleMaxLines)

// NewConsoleLog creates a new ConsoleLog ring buffer.
func NewConsoleLog(max int) *ConsoleLog {
	if max <= 0 {
		max = defaultConsoleMaxLines
	}

	return &ConsoleLog{
		max:   max,
		lines: make([]string, 0, max),
		subs:  make(map[chan string]struct{}),
		mu:    sync.Mutex{},
	}
}

// Append adds a new log line to the buffer and broadcasts to subscribers.
func (c *ConsoleLog) Append(line string) {
	if c == nil {
		return
	}

	c.mu.Lock()

	c.lines = append(c.lines, line)
	if len(c.lines) > c.max {
		c.lines = append([]string(nil), c.lines[len(c.lines)-c.max:]...)
	}
	// fan-out non-blocking
	for ch := range c.subs {
		select {
		case ch <- line:
		default:
		}
	}
	c.mu.Unlock()
}

// Get returns a copy of all buffered log lines.
func (c *ConsoleLog) Get() []string {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.lines))
	copy(out, c.lines)

	return out
}

// Clear removes all log lines from the buffer and signals clear to subscribers.
func (c *ConsoleLog) Clear() {
	if c == nil {
		return
	}

	c.mu.Lock()

	c.lines = c.lines[:0]
	for ch := range c.subs {
		select {
		case ch <- "": // empty sentinel = clear; stream maps to type:clear
		default:
		}
	}
	c.mu.Unlock()
}

// Subscribe returns a buffered channel of new lines. Empty string means clear.
// Caller must Unsubscribe.
func (c *ConsoleLog) Subscribe() chan string {
	ch := make(chan string, 64)
	if c == nil {
		return ch
	}

	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()

	return ch
}

// Unsubscribe removes a subscription channel and drains any remaining messages.
func (c *ConsoleLog) Unsubscribe(ch chan string) {
	if c == nil || ch == nil {
		return
	}

	c.mu.Lock()
	delete(c.subs, ch)
	c.mu.Unlock()
	// drain
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// Writer adapts ConsoleLog as an io.Writer for optional log sink hooks.
type Writer struct {
	Log *ConsoleLog
}

// Write implements io.Writer by appending log entries to the ConsoleLog.
func (w Writer) Write(p []byte) (int, error) {
	if w.Log == nil {
		w.Log = DefaultConsole
	}
	// one line per Write; strip trailing newline
	s := string(p)
	if n := len(s); n > 0 && s[n-1] == '\n' {
		s = s[:n-1]
	}

	if s != "" {
		w.Log.Append(s)
	}

	return len(p), nil
}

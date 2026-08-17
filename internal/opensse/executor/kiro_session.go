package executor

import "sync"

// KiroSessionManager tracks session IDs for Kiro requests.
type KiroSessionManager struct {
	sessions map[string]string
	mu       sync.RWMutex
}

// NewKiroSessionManager constructs a KiroSessionManager.
func NewKiroSessionManager() *KiroSessionManager {
	return &KiroSessionManager{
		sessions: make(map[string]string),
		mu:       sync.RWMutex{},
	}
}

// Get returns the session ID for a connection.
func (m *KiroSessionManager) Get(connID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[connID]
}

// Set associates a session ID with a connection.
func (m *KiroSessionManager) Set(connID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[connID] = sessionID
}

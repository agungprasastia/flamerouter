package executor

import "sync"

// KiroSessionManager tracks session IDs for Kiro requests.
type KiroSessionManager struct {
	sessions map[string]string
	mu       sync.RWMutex
}

func NewKiroSessionManager() *KiroSessionManager {
	return &KiroSessionManager{sessions: make(map[string]string)}
}

func (m *KiroSessionManager) Get(connID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[connID]
}

func (m *KiroSessionManager) Set(connID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[connID] = sessionID
}

// Package clitools provides management and registry for supported CLI tools.
package clitools

import (
	"encoding/json"
	"flamerouter/internal/store"
	"fmt"
	"strings"
	"sync"
)

// Manager manages CLI tool configurations and provides real OS & filesystem detection/application.
type Manager struct {
	st       *store.Store
	handlers map[string]ToolHandler
	mu       sync.RWMutex
}

// New creates a new CLI tools manager.
func New(st *store.Store) *Manager {
	return &Manager{
		st: st,
		handlers: map[string]ToolHandler{
			"claude":       &ClaudeHandler{},
			"codex":        &CodexHandler{},
			"opencode":     &OpenCodeHandler{},
			"droid":        &DroidHandler{},
			"openclaw":     &OpenClawHandler{},
			"cline":        &ClineHandler{},
			"kilo":         &KiloHandler{},
			"copilot":      &CopilotHandler{},
			"hermes":       &HermesHandler{},
			"jcode":        &JCodeHandler{},
			"deepseek-tui": &DeepSeekTuiHandler{},
			"grok-build":   &GrokBuildHandler{},
			"devin":        &DevinHandler{},
			"cowork":       &CoworkHandler{},
		},
		mu: sync.RWMutex{},
	}
}

func (m *Manager) normalizeToolID(toolID string) string {
	tid := strings.ToLower(strings.TrimSpace(toolID))
	tid = strings.TrimSuffix(tid, "-settings")
	tid = strings.ReplaceAll(tid, "_", "-")

	return tid
}

// GetStatus returns the real filesystem status for a tool.
func (m *Manager) GetStatus(toolID, baseURL string) (map[string]any, error) {
	tid := m.normalizeToolID(toolID)
	m.mu.RLock()
	h, ok := m.handlers[tid]
	m.mu.RUnlock()

	if !ok {
		// Fallback to KV if not in handlers
		return m.GetSettings(toolID)
	}

	return h.GetStatus(baseURL)
}

// ApplySettings applies settings to the real tool config on disk.
func (m *Manager) ApplySettings(toolID string, body map[string]any) (map[string]any, error) {
	tid := m.normalizeToolID(toolID)
	m.mu.RLock()
	h, ok := m.handlers[tid]
	m.mu.RUnlock()

	if !ok {
		// Fallback to storing in KV store
		if err := m.PatchSettings(toolID, body); err != nil {
			return nil, err
		}

		return map[string]any{"success": true, "message": "Settings saved to store"}, nil
	}

	res, err := h.ApplySettings(body)
	if err == nil && m.st != nil {
		if errPatch := m.PatchSettings(toolID, body); errPatch != nil {
			return res, errPatch
		}
	}

	return res, err
}

// ResetSettings removes 9Router/Flamerouter configuration from the tool.
func (m *Manager) ResetSettings(toolID string) (map[string]any, error) {
	tid := m.normalizeToolID(toolID)
	m.mu.RLock()
	h, ok := m.handlers[tid]
	m.mu.RUnlock()

	if !ok {
		if m.st != nil {
			if errKV := m.st.KVSet("cli-tools", toolID, ""); errKV != nil {
				return nil, errKV
			}
		}

		return map[string]any{"success": true, "message": "Settings reset"}, nil
	}

	res, err := h.ResetSettings()
	if err == nil && m.st != nil {
		if errKV := m.st.KVSet("cli-tools", toolID, ""); errKV != nil {
			return res, errKV
		}
	}

	return res, err
}

// GetSettings returns the stored settings for a CLI tool from KV.
func (m *Manager) GetSettings(toolID string) (map[string]any, error) {
	if m.st == nil {
		return map[string]any{}, nil
	}

	val, err := m.st.KVGet("cli-tools", toolID)
	if err != nil {
		return nil, err
	}

	if val == "" {
		return map[string]any{}, nil
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(val), &out); err != nil {
		return nil, err
	}

	if out == nil {
		out = map[string]any{}
	}

	return out, nil
}

// PatchSettings merges partial settings for a CLI tool in KV.
func (m *Manager) PatchSettings(toolID string, patch map[string]any) error {
	if m.st == nil {
		return nil
	}

	cur, err := m.GetSettings(toolID)
	if err != nil {
		cur = make(map[string]any)
	}

	for k, v := range patch {
		cur[k] = v
	}

	b, err := json.Marshal(cur)
	if err != nil {
		return err
	}

	return m.st.KVSet("cli-tools", toolID, string(b))
}

// AllStatuses returns real status for all known CLI tools.
func (m *Manager) AllStatuses(baseURL string) map[string]any {
	out := make(map[string]any, len(KnownTools))

	for _, id := range KnownTools {
		st, err := m.GetStatus(id, baseURL)
		if err != nil {
			out[id] = map[string]any{"installed": false, "configured": false, "error": err.Error()}
			continue
		}

		out[id] = st
	}

	return out
}

// Known reports whether toolID is in KnownTools.
func Known(toolID string) bool {
	tid := strings.ToLower(strings.TrimSpace(toolID))
	tid = strings.TrimSuffix(tid, "-settings")
	tid = strings.ReplaceAll(tid, "_", "-")

	for _, id := range KnownTools {
		if id == tid || id == toolID {
			return true
		}
	}

	return false
}

// ErrUnknownTool is returned for unknown tool IDs when strict checks apply.
var ErrUnknownTool = fmt.Errorf("unknown cli tool")

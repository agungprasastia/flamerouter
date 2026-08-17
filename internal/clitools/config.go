// Package clitools provides management and registry for supported CLI tools.
package clitools

import (
	"encoding/json"
	"flamerouter/internal/store"
	"fmt"
)

// Manager manages CLI tool configurations stored in the kv table.
type Manager struct {
	st *store.Store
}

// New creates a new CLI tools manager.
func New(st *store.Store) *Manager {
	return &Manager{st: st}
}

// GetSettings returns the stored settings for a CLI tool.
func (m *Manager) GetSettings(toolID string) (map[string]any, error) {
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

// PatchSettings merges partial settings for a CLI tool.
func (m *Manager) PatchSettings(toolID string, patch map[string]any) error {
	cur, err := m.GetSettings(toolID)
	if err != nil {
		return err
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

// AllStatuses returns status for all known CLI tools.
func (m *Manager) AllStatuses() map[string]any {
	out := make(map[string]any, len(KnownTools))

	for _, id := range KnownTools {
		settings, err := m.GetSettings(id)
		if err != nil {
			out[id] = map[string]any{"configured": false, "error": err.Error()}
			continue
		}

		configured := len(settings) > 0

		if v, ok := settings["enabled"]; ok {
			switch t := v.(type) {
			case bool:
				configured = t || configured
			case string:
				configured = t == "true" || t == "1" || configured
			}
		}

		out[id] = map[string]any{
			"configured": configured,
			"settings":   settings,
		}
	}

	return out
}

// Known reports whether toolID is in KnownTools.
func Known(toolID string) bool {
	for _, id := range KnownTools {
		if id == toolID {
			return true
		}
	}

	return false
}

// ErrUnknownTool is returned for unknown tool IDs when strict checks apply.
var ErrUnknownTool = fmt.Errorf("unknown cli tool")

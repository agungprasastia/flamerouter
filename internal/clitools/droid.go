package clitools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// --- Droid ---
type DroidHandler struct{}

func (h *DroidHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".factory", "settings.json")
}

func (h *DroidHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("droid", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Factory Droid CLI is not installed",
		}, nil
	}

	settings, _ := readJSONFile(p)
	customModels, _ := settings["customModels"].([]any)
	has9Router := false
	for _, mRaw := range customModels {
		if m, ok := mRaw.(map[string]any); ok {
			if id, okID := m["id"].(string); okID && strings.HasPrefix(id, "custom:9Router") {
				has9Router = true
				break
			}
		}
	}

	return map[string]any{
		"installed":    true,
		"settings":     settings,
		"has9Router":   has9Router,
		"settingsPath": p,
	}, nil
}

func (h *DroidHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	activeModel, _ := body["activeModel"].(string)

	var modelsArray []string
	if mList, ok := body["models"].([]any); ok {
		for _, m := range mList {
			if s, ok := m.(string); ok && s != "" {
				modelsArray = append(modelsArray, s)
			}
		}
	} else if singleM, ok := body["model"].(string); ok && singleM != "" {
		modelsArray = append(modelsArray, singleM)
	}

	if baseUrl == "" || len(modelsArray) == 0 {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}

	p := h.getPath()
	settings, _ := readJSONFile(p)
	if settings == nil {
		settings = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseUrl)
	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "your_api_key"
	}

	var existingModels []any
	if cm, ok := settings["customModels"].([]any); ok {
		for _, item := range cm {
			if m, okMap := item.(map[string]any); okMap {
				if id, okID := m["id"].(string); okID && !strings.HasPrefix(id, "custom:9Router") {
					existingModels = append(existingModels, m)
				}
			}
		}
	}

	for _, m := range modelsArray {
		cleanName := strings.ReplaceAll(m, "/", "-")
		customID := fmt.Sprintf("custom:9Router-%s", cleanName)
		existingModels = append(existingModels, map[string]any{
			"id":          customID,
			"name":        fmt.Sprintf("9Router / %s", m),
			"provider":    "openai",
			"baseUrl":     normBase,
			"apiKey":      keyToUse,
			"model":       m,
			"maxTokens":   16384,
			"temperature": 0.7,
		})
	}

	settings["customModels"] = existingModels
	if activeModel != "" {
		cleanName := strings.ReplaceAll(activeModel, "/", "-")
		settings["model"] = fmt.Sprintf("custom:9Router-%s", cleanName)
	}

	if err := writeJSONFile(p, settings); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":      true,
		"message":      "Factory Droid settings applied successfully!",
		"settingsPath": p,
	}, nil
}

func (h *DroidHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	settings, _ := readJSONFile(p)
	if settings == nil {
		return map[string]any{"success": true, "message": "No settings file to reset"}, nil
	}

	if cm, ok := settings["customModels"].([]any); ok {
		var filtered []any
		for _, item := range cm {
			if m, okMap := item.(map[string]any); okMap {
				if id, okID := m["id"].(string); okID && !strings.HasPrefix(id, "custom:9Router") {
					filtered = append(filtered, m)
				}
			}
		}
		settings["customModels"] = filtered
	}

	if m, ok := settings["model"].(string); ok && strings.HasPrefix(m, "custom:9Router") {
		delete(settings, "model")
	}

	if err := writeJSONFile(p, settings); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "9Router models removed from Factory Droid",
	}, nil
}

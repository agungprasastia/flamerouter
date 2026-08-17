package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DroidHandler manages Factory Droid CLI configurations.
type DroidHandler struct{}

func (h *DroidHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".factory", "settings.json")
}

// GetStatus returns status of Factory Droid CLI.
func (h *DroidHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("droid", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Factory Droid CLI is not installed",
		}, nil
	}

	settings, errS := readJSONFile(p)
	if errS != nil {
		settings = make(map[string]any)
	}

	customModels, okCM := settings["customModels"].([]any)
	if !okCM {
		customModels = nil
	}

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

// ApplySettings applies configuration for Factory Droid CLI.
func (h *DroidHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	activeModel, okAct := body["activeModel"].(string)

	if !okBase {
		baseURL = ""
	}

	if !okKey {
		apiKey = ""
	}

	if !okAct {
		activeModel = ""
	}

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

	if baseURL == "" || len(modelsArray) == 0 {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}

	p := h.getPath()

	settings, errS := readJSONFile(p)
	if errS != nil || settings == nil {
		settings = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseURL)

	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "your_api_key"
	}

	existingModels := make([]any, 0, len(modelsArray))

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

// ResetSettings resets Factory Droid CLI configurations.
func (h *DroidHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()

	settings, errS := readJSONFile(p)
	if errS != nil {
		if os.IsNotExist(errS) {
			return map[string]any{"success": true, "message": "No settings file to reset"}, nil
		}

		return nil, errS
	}

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

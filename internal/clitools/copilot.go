package clitools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CopilotHandler manages VS Code Copilot model configurations.
type CopilotHandler struct{}

func (h *CopilotHandler) getPath() string {
	home := userHomeDir()

	var configDir string

	switch runtime.GOOS {
	case "darwin":
		configDir = filepath.Join(home, "Library", "Application Support", "Code", "User")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}

		configDir = filepath.Join(appData, "Code", "User")
	default: // linux
		configDir = filepath.Join(home, ".config", "Code", "User")
	}

	return filepath.Join(configDir, "chatLanguageModels.json")
}

// GetStatus returns status of VS Code Copilot models.
func (h *CopilotHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()
	// Copilot check: config file exists or parent dir exists
	parentDir := filepath.Dir(p)
	installed := false

	if _, err := os.Stat(parentDir); err == nil {
		installed = true
	}

	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "VS Code configuration directory not found",
		}, nil
	}

	data, err := os.ReadFile(filepath.Clean(p))
	has9Router := false

	var models []any
	if err == nil {
		if errU := json.Unmarshal(data, &models); errU == nil {
			for _, mRaw := range models {
				if m, ok := mRaw.(map[string]any); ok {
					if id, okID := m["id"].(string); okID && (strings.HasPrefix(id, "9router-") || strings.HasPrefix(id, "flamerouter-")) {
						has9Router = true
						break
					}
				}
			}
		}
	}

	return map[string]any{
		"installed":  true,
		"settings":   models,
		"has9Router": has9Router,
		"configPath": p,
	}, nil
}

// ApplySettings applies configuration for VS Code Copilot models.
func (h *CopilotHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)

	if !okBase {
		baseURL = ""
	}

	if !okKey {
		apiKey = ""
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
	normBase := normalizeBaseURLV1(baseURL)

	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "sk_9router"
	}

	var existingModels []any
	if data, err := os.ReadFile(filepath.Clean(p)); err == nil {
		if errU := json.Unmarshal(data, &existingModels); errU != nil {
			existingModels = nil
		}
	}

	// Filter out existing 9router models
	filtered := make([]any, 0, len(existingModels)+len(modelsArray))

	for _, mRaw := range existingModels {
		if m, ok := mRaw.(map[string]any); ok {
			if id, okID := m["id"].(string); !okID || (!strings.HasPrefix(id, "9router-") && !strings.HasPrefix(id, "flamerouter-")) {
				filtered = append(filtered, m)
			}
		}
	}

	for _, m := range modelsArray {
		cleanName := strings.ReplaceAll(m, "/", "-")
		filtered = append(filtered, map[string]any{
			"id":        fmt.Sprintf("9router-%s", cleanName),
			"name":      fmt.Sprintf("9Router / %s", m),
			"model":     m,
			"provider":  "openai",
			"url":       normBase,
			"apiKey":    keyToUse,
			"maxTokens": 16384,
		})
	}

	cleanP := filepath.Clean(p)
	if err := os.MkdirAll(filepath.Dir(cleanP), 0o750); err != nil {
		return nil, err
	}

	out, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(cleanP, out, 0o600); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "VS Code Copilot models applied successfully!",
		"configPath": p,
	}, nil
}

// ResetSettings resets VS Code Copilot model configurations.
func (h *CopilotHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	cleanP := filepath.Clean(p)

	data, err := os.ReadFile(cleanP)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"success": true, "message": "No config file to reset"}, nil
		}

		return nil, err
	}

	var existingModels []any
	if errU := json.Unmarshal(data, &existingModels); errU != nil {
		return nil, errU
	}

	filtered := make([]any, 0, len(existingModels))

	for _, mRaw := range existingModels {
		if m, ok := mRaw.(map[string]any); ok {
			if id, okID := m["id"].(string); !okID || (!strings.HasPrefix(id, "9router-") && !strings.HasPrefix(id, "flamerouter-")) {
				filtered = append(filtered, m)
			}
		}
	}

	out, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(cleanP, out, 0o600); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "9Router models removed from Copilot config",
	}, nil
}

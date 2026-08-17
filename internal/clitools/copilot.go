package clitools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// --- Copilot ---
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

func (h *CopilotHandler) GetStatus(baseUrl string) (map[string]any, error) {
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

	data, err := os.ReadFile(p)
	has9Router := false
	var models []any
	if err == nil {
		_ = json.Unmarshal(data, &models)
		for _, mRaw := range models {
			if m, ok := mRaw.(map[string]any); ok {
				if id, _ := m["id"].(string); strings.HasPrefix(id, "9router-") || strings.HasPrefix(id, "flamerouter-") {
					has9Router = true
					break
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

func (h *CopilotHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)

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
	normBase := normalizeBaseURLV1(baseUrl)
	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "sk_9router"
	}

	var existingModels []any
	if data, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(data, &existingModels)
	}

	// Filter out existing 9router models
	var filtered []any
	for _, mRaw := range existingModels {
		if m, ok := mRaw.(map[string]any); ok {
			if id, _ := m["id"].(string); !strings.HasPrefix(id, "9router-") && !strings.HasPrefix(id, "flamerouter-") {
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

	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, out, 0644); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "VS Code Copilot models applied successfully!",
		"configPath": p,
	}, nil
}

func (h *CopilotHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]any{"success": true, "message": "No config file to reset"}, nil
	}

	var existingModels []any
	_ = json.Unmarshal(data, &existingModels)

	var filtered []any
	for _, mRaw := range existingModels {
		if m, ok := mRaw.(map[string]any); ok {
			if id, _ := m["id"].(string); !strings.HasPrefix(id, "9router-") && !strings.HasPrefix(id, "flamerouter-") {
				filtered = append(filtered, m)
			}
		}
	}

	out, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, out, 0644); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "9Router models removed from Copilot config",
	}, nil
}

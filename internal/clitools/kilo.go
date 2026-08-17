package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KiloHandler manages Kilo CLI tool configurations.
type KiloHandler struct{}

func (h *KiloHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".local", "share", "kilo", "auth.json")
}

// GetStatus returns status of Kilo CLI.
func (h *KiloHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("kilo", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Kilo CLI is not installed",
		}, nil
	}

	auth, errA := readJSONFile(p)
	if errA != nil {
		auth = make(map[string]any)
	}

	openAI, okOpenAI := auth["openai"].(map[string]any)
	if !okOpenAI {
		openAI = nil
	}

	has9Router := false

	if openAI != nil {
		if u, okU := openAI["base_url"].(string); okU && u != "" {
			if strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") || strings.Contains(u, "9router") || strings.Contains(u, "flamerouter") {
				has9Router = true
			}
		}
	}

	return map[string]any{
		"installed":  true,
		"settings":   auth,
		"has9Router": has9Router,
		"authPath":   p,
	}, nil
}

// ApplySettings applies configuration for Kilo CLI.
func (h *KiloHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)

	if !okBase || !okKey || baseURL == "" || apiKey == "" {
		return nil, fmt.Errorf("baseUrl and apiKey are required")
	}

	p := h.getPath()

	auth, errA := readJSONFile(p)
	if errA != nil || auth == nil {
		auth = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseURL)
	auth["openai"] = map[string]any{
		"base_url": normBase,
		"api_key":  apiKey,
	}

	if err := writeJSONFile(p, auth); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":  true,
		"message":  "Kilo settings applied successfully!",
		"authPath": p,
	}, nil
}

// ResetSettings resets Kilo CLI configurations.
func (h *KiloHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()

	auth, errA := readJSONFile(p)
	if errA != nil {
		if os.IsNotExist(errA) {
			return map[string]any{"success": true, "message": "No auth file to reset"}, nil
		}

		return nil, errA
	}

	if auth == nil {
		return map[string]any{"success": true, "message": "No auth file to reset"}, nil
	}

	delete(auth, "openai")

	if err := writeJSONFile(p, auth); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "9Router settings removed from Kilo",
	}, nil
}

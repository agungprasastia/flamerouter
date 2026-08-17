package clitools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// --- Kilo ---
type KiloHandler struct{}

func (h *KiloHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".local", "share", "kilo", "auth.json")
}

func (h *KiloHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("kilo", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Kilo CLI is not installed",
		}, nil
	}

	auth, _ := readJSONFile(p)
	openAi, _ := auth["openai"].(map[string]any)
	has9Router := false
	if openAi != nil {
		if u, _ := openAi["base_url"].(string); u != "" {
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

func (h *KiloHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)

	if baseUrl == "" || apiKey == "" {
		return nil, fmt.Errorf("baseUrl and apiKey are required")
	}

	p := h.getPath()
	auth, _ := readJSONFile(p)
	if auth == nil {
		auth = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseUrl)
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

func (h *KiloHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	auth, _ := readJSONFile(p)
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

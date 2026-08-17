package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- Hermes ---
type HermesHandler struct{}

func (h *HermesHandler) getPaths() (configPath, envPath string) {
	home := userHomeDir()
	hermesDir := filepath.Join(home, ".hermes")
	return filepath.Join(hermesDir, "config.yaml"), filepath.Join(hermesDir, ".env")
}

func (h *HermesHandler) GetStatus(baseUrl string) (map[string]any, error) {
	cPath, ePath := h.getPaths()
	installed := checkCommandInstalled("hermes", cPath)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Hermes CLI is not installed",
		}, nil
	}

	configBytes, _ := os.ReadFile(cPath)
	envBytes, _ := os.ReadFile(ePath)

	configStr := string(configBytes)
	has9Router := strings.Contains(configStr, "custom_endpoints") &&
		(strings.Contains(configStr, "localhost") || strings.Contains(configStr, "127.0.0.1") || strings.Contains(configStr, "9router") || strings.Contains(configStr, "flamerouter"))

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     configStr,
		"env":        string(envBytes),
		"configPath": cPath,
		"envPath":    ePath,
	}, nil
}

func (h *HermesHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	cPath, ePath := h.getPaths()
	normBase := normalizeBaseURLV1(baseUrl)

	if err := os.MkdirAll(filepath.Dir(cPath), 0755); err != nil {
		return nil, err
	}

	// Simple YAML configuration for Hermes
	yamlContent := fmt.Sprintf(`model: "%s"
custom_endpoints:
  - name: "9router"
    base_url: "%s"
    api_key_env: "HERMES_9ROUTER_KEY"
`, model, normBase)

	if err := os.WriteFile(cPath, []byte(yamlContent), 0644); err != nil {
		return nil, err
	}

	envContent := fmt.Sprintf("HERMES_9ROUTER_KEY=%s\n", apiKey)
	if err := os.WriteFile(ePath, []byte(envContent), 0644); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "Hermes settings applied successfully!",
		"configPath": cPath,
		"envPath":    ePath,
	}, nil
}

func (h *HermesHandler) ResetSettings() (map[string]any, error) {
	cPath, ePath := h.getPaths()
	_ = os.Remove(cPath)
	_ = os.Remove(ePath)
	return map[string]any{
		"success": true,
		"message": "Hermes settings reset successfully",
	}, nil
}

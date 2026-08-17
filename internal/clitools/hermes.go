package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HermesHandler manages Hermes CLI tool configuration.
type HermesHandler struct{}

func (h *HermesHandler) getPaths() (configPath, envPath string) {
	home := userHomeDir()
	hermesDir := filepath.Join(home, ".hermes")

	return filepath.Join(hermesDir, "config.yaml"), filepath.Join(hermesDir, ".env")
}

// GetStatus returns status of Hermes CLI.
func (h *HermesHandler) GetStatus(_ string) (map[string]any, error) {
	cPath, ePath := h.getPaths()

	installed := checkCommandInstalled("hermes", cPath)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Hermes CLI is not installed",
		}, nil
	}

	var configBytes []byte
	if b, err := os.ReadFile(filepath.Clean(cPath)); err == nil {
		configBytes = b
	}

	var envBytes []byte
	if b, err := os.ReadFile(filepath.Clean(ePath)); err == nil {
		envBytes = b
	}

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

// ApplySettings applies configuration for Hermes CLI.
func (h *HermesHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	cPath, ePath := h.getPaths()
	cleanC := filepath.Clean(cPath)
	cleanE := filepath.Clean(ePath)
	normBase := normalizeBaseURLV1(baseURL)

	if err := os.MkdirAll(filepath.Dir(cleanC), 0o750); err != nil {
		return nil, err
	}

	// Simple YAML configuration for Hermes
	yamlContent := fmt.Sprintf(`model: "%s"
custom_endpoints:
  - name: "9router"
    base_url: "%s"
    api_key_env: "HERMES_9ROUTER_KEY"
`, model, normBase)

	if err := os.WriteFile(cleanC, []byte(yamlContent), 0o600); err != nil {
		return nil, err
	}

	envContent := fmt.Sprintf("HERMES_9ROUTER_KEY=%s\n", apiKey)
	if err := os.WriteFile(cleanE, []byte(envContent), 0o600); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "Hermes settings applied successfully!",
		"configPath": cPath,
		"envPath":    ePath,
	}, nil
}

// ResetSettings resets Hermes CLI settings.
func (h *HermesHandler) ResetSettings() (map[string]any, error) {
	cPath, ePath := h.getPaths()

	if err := os.Remove(filepath.Clean(cPath)); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.Remove(filepath.Clean(ePath)); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "Hermes settings reset successfully",
	}, nil
}

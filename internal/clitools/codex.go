package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- Codex ---
type CodexHandler struct{}

func (h *CodexHandler) getPaths() (configPath, authPath string) {
	home := userHomeDir()
	dir := filepath.Join(home, ".codex")
	return filepath.Join(dir, "config.toml"), filepath.Join(dir, "auth.json")
}

func (h *CodexHandler) GetStatus(baseUrl string) (map[string]any, error) {
	cfgFile, _ := h.getPaths()
	installed := checkCommandInstalled("codex", cfgFile)
	if !installed {
		return map[string]any{
			"installed": false,
			"config":    nil,
			"message":   "Codex CLI is not installed",
		}, nil
	}

	content := ""
	if data, err := os.ReadFile(cfgFile); err == nil {
		content = string(data)
	}

	has9Router := strings.Contains(content, "model_provider = \"9router\"") ||
		strings.Contains(content, "[model_providers.9router]")

	return map[string]any{
		"installed":  true,
		"config":     content,
		"has9Router": has9Router,
		"configPath": cfgFile,
	}, nil
}

func (h *CodexHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)
	subagentModel, _ := body["subagentModel"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	cfgFile, authFile := h.getPaths()
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0755); err != nil {
		return nil, err
	}

	normBase := normalizeBaseURLV1(baseUrl)

	// Read existing config or start empty
	existing := ""
	if data, err := os.ReadFile(cfgFile); err == nil {
		existing = string(data)
	}

	// Simple TOML upsert / generator for 9router provider
	var b strings.Builder
	b.WriteString(fmt.Sprintf("model = %q\n", model))
	b.WriteString("model_provider = \"9router\"\n")
	if subagentModel != "" {
		b.WriteString(fmt.Sprintf("subagent_model = %q\n", subagentModel))
	}
	b.WriteString("\n[model_providers.9router]\n")
	b.WriteString(fmt.Sprintf("base_url = %q\n", normBase))
	b.WriteString("wire_specification = \"openai\"\n")
	b.WriteString("supports_websockets = false\n")

	if err := os.WriteFile(cfgFile, []byte(b.String()), 0644); err != nil {
		return nil, err
	}

	authMap, _ := readJSONFile(authFile)
	if authMap == nil {
		authMap = make(map[string]any)
	}
	authMap["9router"] = apiKey
	if err := writeJSONFile(authFile, authMap); err != nil {
		return nil, err
	}

	_ = existing
	return map[string]any{
		"success": true,
		"message": "Codex settings applied successfully!",
	}, nil
}

func (h *CodexHandler) ResetSettings() (map[string]any, error) {
	cfgFile, authFile := h.getPaths()
	if _, err := os.Stat(cfgFile); err == nil {
		// remove 9router config
		_ = os.WriteFile(cfgFile, []byte(""), 0644)
	}
	authMap, _ := readJSONFile(authFile)
	if authMap != nil {
		delete(authMap, "9router")
		_ = writeJSONFile(authFile, authMap)
	}
	return map[string]any{
		"success": true,
		"message": "9Router settings removed from Codex",
	}, nil
}

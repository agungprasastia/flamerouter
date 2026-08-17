package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodexHandler manages Codex CLI tool configuration.
type CodexHandler struct{}

func (h *CodexHandler) getPaths() (configPath, authPath string) {
	home := userHomeDir()
	dir := filepath.Join(home, ".codex")

	return filepath.Join(dir, "config.toml"), filepath.Join(dir, "auth.json")
}

// GetStatus returns status of Codex CLI.
func (h *CodexHandler) GetStatus(_ string) (map[string]any, error) {
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
	if data, err := os.ReadFile(filepath.Clean(cfgFile)); err == nil {
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

// ApplySettings applies configuration for Codex CLI.
func (h *CodexHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)
	subagentModel, okSub := body["subagentModel"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	cfgFile, authFile := h.getPaths()
	if err := os.MkdirAll(filepath.Dir(filepath.Clean(cfgFile)), 0o750); err != nil {
		return nil, err
	}

	normBase := normalizeBaseURLV1(baseURL)

	// Simple TOML upsert / generator for 9router provider
	var b strings.Builder

	b.WriteString(fmt.Sprintf("model = %q\n", model))
	b.WriteString("model_provider = \"9router\"\n")

	if okSub && subagentModel != "" {
		b.WriteString(fmt.Sprintf("subagent_model = %q\n", subagentModel))
	}

	b.WriteString("\n[model_providers.9router]\n")
	b.WriteString(fmt.Sprintf("base_url = %q\n", normBase))
	b.WriteString("wire_specification = \"openai\"\n")
	b.WriteString("supports_websockets = false\n")

	if err := os.WriteFile(filepath.Clean(cfgFile), []byte(b.String()), 0o600); err != nil {
		return nil, err
	}

	authMap, errA := readJSONFile(authFile)
	if errA != nil || authMap == nil {
		authMap = make(map[string]any)
	}

	authMap["9router"] = apiKey
	if err := writeJSONFile(authFile, authMap); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "Codex settings applied successfully!",
	}, nil
}

// ResetSettings resets Codex CLI settings.
func (h *CodexHandler) ResetSettings() (map[string]any, error) {
	cfgFile, authFile := h.getPaths()
	cleanCfg := filepath.Clean(cfgFile)

	if _, err := os.Stat(cleanCfg); err == nil {
		// remove 9router config
		if errW := os.WriteFile(cleanCfg, []byte(""), 0o600); errW != nil {
			return nil, errW
		}
	}

	authMap, errA := readJSONFile(authFile)
	if errA == nil && authMap != nil {
		delete(authMap, "9router")

		if errW := writeJSONFile(authFile, authMap); errW != nil {
			return nil, errW
		}
	}

	return map[string]any{
		"success": true,
		"message": "9Router settings removed from Codex",
	}, nil
}

package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JCodeHandler manages JCode CLI tool configurations.
type JCodeHandler struct{}

func (h *JCodeHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".jcode", "config.toml")
}

// GetStatus returns status of JCode CLI.
func (h *JCodeHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("jcode", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "JCode CLI is not installed",
		}, nil
	}

	var content []byte
	if b, err := os.ReadFile(filepath.Clean(p)); err == nil {
		content = b
	}

	str := string(content)
	has9Router := strings.Contains(str, "9router") || strings.Contains(str, "flamerouter")

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     str,
		"configPath": p,
	}, nil
}

// ApplySettings applies configuration for JCode CLI.
func (h *JCodeHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := filepath.Clean(h.getPath())
	normBase := normalizeBaseURLV1(baseURL)

	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return nil, err
	}

	tomlContent := fmt.Sprintf(`[model]
name = "%s"

[provider]
name = "openai"
base_url = "%s"
api_key = "%s"
`, model, normBase, apiKey)

	if err := os.WriteFile(p, []byte(tomlContent), 0o600); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "JCode settings applied successfully!",
		"configPath": p,
	}, nil
}

// ResetSettings resets JCode CLI configurations.
func (h *JCodeHandler) ResetSettings() (map[string]any, error) {
	p := filepath.Clean(h.getPath())
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "JCode settings reset successfully",
	}, nil
}

// DeepSeekTuiHandler manages DeepSeek-TUI CLI tool configurations.
type DeepSeekTuiHandler struct{}

func (h *DeepSeekTuiHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".deepseek", "config.toml")
}

// GetStatus returns status of DeepSeek-TUI CLI.
func (h *DeepSeekTuiHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("deepseek", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "DeepSeek-TUI is not installed",
		}, nil
	}

	var content []byte
	if b, err := os.ReadFile(filepath.Clean(p)); err == nil {
		content = b
	}

	str := string(content)
	has9Router := strings.Contains(str, "9router") || strings.Contains(str, "flamerouter")

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     str,
		"configPath": p,
	}, nil
}

// ApplySettings applies configuration for DeepSeek-TUI CLI.
func (h *DeepSeekTuiHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := filepath.Clean(h.getPath())
	normBase := normalizeBaseURLV1(baseURL)

	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return nil, err
	}

	tomlContent := fmt.Sprintf(`model = "%s"
api_base = "%s"
api_key = "%s"
`, model, normBase, apiKey)

	if err := os.WriteFile(p, []byte(tomlContent), 0o600); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "DeepSeek-TUI settings applied successfully!",
		"configPath": p,
	}, nil
}

// ResetSettings resets DeepSeek-TUI CLI configurations.
func (h *DeepSeekTuiHandler) ResetSettings() (map[string]any, error) {
	p := filepath.Clean(h.getPath())
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "DeepSeek-TUI settings reset successfully",
	}, nil
}

// GrokBuildHandler manages Grok Build CLI tool configurations.
type GrokBuildHandler struct{}

func (h *GrokBuildHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".grok", "config.toml")
}

// GetStatus returns status of Grok Build CLI.
func (h *GrokBuildHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("grok", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Grok Build is not installed",
		}, nil
	}

	var content []byte
	if b, err := os.ReadFile(filepath.Clean(p)); err == nil {
		content = b
	}

	str := string(content)
	has9Router := strings.Contains(str, "9router") || strings.Contains(str, "flamerouter")

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     str,
		"configPath": p,
	}, nil
}

// ApplySettings applies configuration for Grok Build CLI.
func (h *GrokBuildHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := filepath.Clean(h.getPath())
	normBase := normalizeBaseURLV1(baseURL)

	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return nil, err
	}

	tomlContent := fmt.Sprintf(`model = "%s"
api_base = "%s"
api_key = "%s"
`, model, normBase, apiKey)

	if err := os.WriteFile(p, []byte(tomlContent), 0o600); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "Grok Build settings applied successfully!",
		"configPath": p,
	}, nil
}

// ResetSettings resets Grok Build CLI configurations.
func (h *GrokBuildHandler) ResetSettings() (map[string]any, error) {
	p := filepath.Clean(h.getPath())
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "Grok Build settings reset successfully",
	}, nil
}

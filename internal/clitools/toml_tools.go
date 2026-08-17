package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- JCode ---
type JCodeHandler struct{}

func (h *JCodeHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".jcode", "config.toml")
}

func (h *JCodeHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("jcode", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "JCode CLI is not installed",
		}, nil
	}

	content, _ := os.ReadFile(p)
	str := string(content)
	has9Router := strings.Contains(str, "9router") || strings.Contains(str, "flamerouter")

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     str,
		"configPath": p,
	}, nil
}

func (h *JCodeHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := h.getPath()
	normBase := normalizeBaseURLV1(baseUrl)

	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}

	tomlContent := fmt.Sprintf(`[model]
name = "%s"

[provider]
name = "openai"
base_url = "%s"
api_key = "%s"
`, model, normBase, apiKey)

	if err := os.WriteFile(p, []byte(tomlContent), 0644); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "JCode settings applied successfully!",
		"configPath": p,
	}, nil
}

func (h *JCodeHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	_ = os.Remove(p)
	return map[string]any{
		"success": true,
		"message": "JCode settings reset successfully",
	}, nil
}

// --- DeepSeek-TUI ---
type DeepSeekTuiHandler struct{}

func (h *DeepSeekTuiHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".deepseek", "config.toml")
}

func (h *DeepSeekTuiHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("deepseek", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "DeepSeek-TUI is not installed",
		}, nil
	}

	content, _ := os.ReadFile(p)
	str := string(content)
	has9Router := strings.Contains(str, "9router") || strings.Contains(str, "flamerouter")

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     str,
		"configPath": p,
	}, nil
}

func (h *DeepSeekTuiHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := h.getPath()
	normBase := normalizeBaseURLV1(baseUrl)

	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}

	tomlContent := fmt.Sprintf(`model = "%s"
api_base = "%s"
api_key = "%s"
`, model, normBase, apiKey)

	if err := os.WriteFile(p, []byte(tomlContent), 0644); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "DeepSeek-TUI settings applied successfully!",
		"configPath": p,
	}, nil
}

func (h *DeepSeekTuiHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	_ = os.Remove(p)
	return map[string]any{
		"success": true,
		"message": "DeepSeek-TUI settings reset successfully",
	}, nil
}

// --- Grok Build ---
type GrokBuildHandler struct{}

func (h *GrokBuildHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".grok", "config.toml")
}

func (h *GrokBuildHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("grok", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Grok Build is not installed",
		}, nil
	}

	content, _ := os.ReadFile(p)
	str := string(content)
	has9Router := strings.Contains(str, "9router") || strings.Contains(str, "flamerouter")

	return map[string]any{
		"installed":  true,
		"has9Router": has9Router,
		"config":     str,
		"configPath": p,
	}, nil
}

func (h *GrokBuildHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := h.getPath()
	normBase := normalizeBaseURLV1(baseUrl)

	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return nil, err
	}

	tomlContent := fmt.Sprintf(`model = "%s"
api_base = "%s"
api_key = "%s"
`, model, normBase, apiKey)

	if err := os.WriteFile(p, []byte(tomlContent), 0644); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "Grok Build settings applied successfully!",
		"configPath": p,
	}, nil
}

func (h *GrokBuildHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	_ = os.Remove(p)
	return map[string]any{
		"success": true,
		"message": "Grok Build settings reset successfully",
	}, nil
}

package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// --- Devin ---
type DevinHandler struct{}


func (h *DevinHandler) GetStatus(baseUrl string) (map[string]any, error) {
	installed := checkCommandInstalled("devin", "")
	return map[string]any{
		"installed":  installed,
		"has9Router": false,
		"message":    "Devin CLI configuration via environment variables",
	}, nil
}

func (h *DevinHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	return map[string]any{
		"success": true,
		"message": "Devin CLI uses OPENAI_BASE_URL and OPENAI_API_KEY environment variables.",
	}, nil
}

func (h *DevinHandler) ResetSettings() (map[string]any, error) {
	return map[string]any{
		"success": true,
		"message": "Devin CLI reset",
	}, nil
}

// --- Cowork ---
type CoworkHandler struct{}

func (h *CoworkHandler) getMetaPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".configLibrary", "_meta.json")
}

func (h *CoworkHandler) getClaudeDesktopPath() string {
	home := userHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	} else if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json")
	}
	return filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")
}

func (h *CoworkHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getClaudeDesktopPath()
	metaPath := h.getMetaPath()
	installed := false
	if _, err := os.Stat(p); err == nil {
		installed = true
	} else if _, err := os.Stat(metaPath); err == nil {
		installed = true
	}

	config, _ := readJSONFile(p)
	mcpServers, _ := config["mcpServers"].(map[string]any)
	_, has9Router := mcpServers["9router"]

	return map[string]any{
		"installed":  installed,
		"config":     config,
		"has9Router": has9Router,
		"configPath": p,
	}, nil
}

func (h *CoworkHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)

	if baseUrl == "" {
		return nil, fmt.Errorf("baseUrl is required")
	}

	p := h.getClaudeDesktopPath()
	config, _ := readJSONFile(p)
	if config == nil {
		config = make(map[string]any)
	}

	mcpServers, _ := config["mcpServers"].(map[string]any)
	if mcpServers == nil {
		mcpServers = make(map[string]any)
	}

	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "sk_9router"
	}

	mcpServers["9router"] = map[string]any{
		"command": "npx",
		"args": []any{
			"-y",
			"@modelcontextprotocol/server-everything",
		},
		"env": map[string]any{
			"BASE_URL": baseUrl,
			"API_KEY":  keyToUse,
		},
	}
	config["mcpServers"] = mcpServers

	if err := writeJSONFile(p, config); err != nil {
		return nil, err
	}

	// Also update _meta.json if present
	metaPath := h.getMetaPath()
	if meta, err := readJSONFile(metaPath); err == nil && meta != nil {
		meta["activeRouter"] = "9router"
		meta["baseUrl"] = baseUrl
		_ = writeJSONFile(metaPath, meta)
	}

	return map[string]any{
		"success":    true,
		"message":    "Cowork / Claude Desktop settings applied successfully!",
		"configPath": p,
	}, nil
}

func (h *CoworkHandler) ResetSettings() (map[string]any, error) {
	p := h.getClaudeDesktopPath()
	config, _ := readJSONFile(p)
	if config != nil {
		if mcpServers, ok := config["mcpServers"].(map[string]any); ok {
			delete(mcpServers, "9router")
			_ = writeJSONFile(p, config)
		}
	}

	metaPath := h.getMetaPath()
	if meta, err := readJSONFile(metaPath); err == nil && meta != nil {
		delete(meta, "activeRouter")
		_ = writeJSONFile(metaPath, meta)
	}

	return map[string]any{
		"success": true,
		"message": "9Router removed from Cowork / Claude Desktop",
	}, nil
}

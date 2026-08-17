package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DevinHandler manages Devin CLI configurations.
type DevinHandler struct{}

// GetStatus returns status of Devin CLI.
func (h *DevinHandler) GetStatus(_ string) (map[string]any, error) {
	installed := checkCommandInstalled("devin", "")

	return map[string]any{
		"installed":  installed,
		"has9Router": false,
		"message":    "Devin CLI configuration via environment variables",
	}, nil
}

// ApplySettings applies configuration for Devin CLI.
func (h *DevinHandler) ApplySettings(_ map[string]any) (map[string]any, error) {
	return map[string]any{
		"success": true,
		"message": "Devin CLI uses OPENAI_BASE_URL and OPENAI_API_KEY environment variables.",
	}, nil
}

// ResetSettings resets Devin CLI configurations.
func (h *DevinHandler) ResetSettings() (map[string]any, error) {
	return map[string]any{
		"success": true,
		"message": "Devin CLI reset",
	}, nil
}

// CoworkHandler manages Claude Desktop / Cowork configurations.
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

// GetStatus returns status of Claude Desktop / Cowork configuration.
func (h *CoworkHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getClaudeDesktopPath()
	metaPath := h.getMetaPath()

	installed := false
	if _, err := os.Stat(p); err == nil {
		installed = true
	} else if _, err := os.Stat(metaPath); err == nil {
		installed = true
	}

	config, errC := readJSONFile(p)
	if errC != nil {
		config = make(map[string]any)
	}

	mcpServers, okMcp := config["mcpServers"].(map[string]any)
	has9Router := false

	if okMcp && mcpServers != nil {
		_, has9Router = mcpServers["9router"]
	}

	return map[string]any{
		"installed":  installed,
		"config":     config,
		"has9Router": has9Router,
		"configPath": p,
	}, nil
}

// ApplySettings applies configuration for Claude Desktop / Cowork.
func (h *CoworkHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)

	if !okKey {
		apiKey = ""
	}

	if !okBase || baseURL == "" {
		return nil, fmt.Errorf("baseUrl is required")
	}

	p := h.getClaudeDesktopPath()

	config, errC := readJSONFile(p)
	if errC != nil || config == nil {
		config = make(map[string]any)
	}

	mcpServers, okMcp := config["mcpServers"].(map[string]any)
	if !okMcp || mcpServers == nil {
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
			"BASE_URL": baseURL,
			"API_KEY":  keyToUse,
		},
	}
	config["mcpServers"] = mcpServers

	if err := writeJSONFile(p, config); err != nil {
		return nil, err
	}

	// Also update _meta.json if present
	metaPath := h.getMetaPath()
	if meta, errMeta := readJSONFile(metaPath); errMeta == nil && meta != nil {
		meta["activeRouter"] = "9router"
		meta["baseUrl"] = baseURL

		if errW := writeJSONFile(metaPath, meta); errW != nil {
			return nil, errW
		}
	}

	return map[string]any{
		"success":    true,
		"message":    "Cowork / Claude Desktop settings applied successfully!",
		"configPath": p,
	}, nil
}

// ResetSettings resets Claude Desktop / Cowork configuration.
func (h *CoworkHandler) ResetSettings() (map[string]any, error) {
	p := h.getClaudeDesktopPath()

	config, errC := readJSONFile(p)
	if errC == nil && config != nil {
		if mcpServers, ok := config["mcpServers"].(map[string]any); ok {
			delete(mcpServers, "9router")

			if errW := writeJSONFile(p, config); errW != nil {
				return nil, errW
			}
		}
	}

	metaPath := h.getMetaPath()
	if meta, errMeta := readJSONFile(metaPath); errMeta == nil && meta != nil {
		delete(meta, "activeRouter")

		if errW := writeJSONFile(metaPath, meta); errW != nil {
			return nil, errW
		}
	}

	return map[string]any{
		"success": true,
		"message": "9Router removed from Cowork / Claude Desktop",
	}, nil
}

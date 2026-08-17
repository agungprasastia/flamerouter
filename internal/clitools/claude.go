package clitools

import (
	"fmt"
	"path/filepath"
)

// ClaudeHandler manages Claude CLI tool configuration.
type ClaudeHandler struct{}

func (h *ClaudeHandler) getPaths() (settingsPath, claudeJSONPath string) {
	home := userHomeDir()
	return filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".claude.json")
}

// GetStatus returns status of Claude CLI.
func (h *ClaudeHandler) GetStatus(_ string) (map[string]any, error) {
	sFile, _ := h.getPaths()

	installed := checkCommandInstalled("claude", sFile)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Claude CLI is not installed",
		}, nil
	}

	settings, errS := readJSONFile(sFile)
	if errS != nil {
		settings = make(map[string]any)
	}

	env, okEnv := settings["env"].(map[string]any)
	has9Router := false

	if okEnv && env != nil {
		if u, ok := env["ANTHROPIC_BASE_URL"].(string); ok && u != "" {
			has9Router = true
		}
	}

	_, cFile := h.getPaths()

	cJSON, errC := readJSONFile(cFile)
	if errC != nil {
		cJSON = make(map[string]any)
	}

	mcpServers, okMcp := cJSON["mcpServers"].(map[string]any)
	hasExa := false

	if okMcp && mcpServers != nil {
		_, hasExa = mcpServers["exa"]
	}

	return map[string]any{
		"installed":     true,
		"settings":      settings,
		"has9Router":    has9Router,
		"exaMcpEnabled": hasExa,
		"settingsPath":  sFile,
	}, nil
}

// ApplySettings applies configuration for Claude CLI.
func (h *ClaudeHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	env, okEnv := body["env"].(map[string]any)
	if !okEnv || env == nil {
		return nil, fmt.Errorf("invalid env object")
	}

	sFile, cFile := h.getPaths()

	curSettings, errS := readJSONFile(sFile)
	if errS != nil || curSettings == nil {
		curSettings = make(map[string]any)
	}

	curEnv, okCurEnv := curSettings["env"].(map[string]any)
	if !okCurEnv || curEnv == nil {
		curEnv = make(map[string]any)
	}

	for k, v := range env {
		if k == "ANTHROPIC_BASE_URL" {
			if strVal, ok := v.(string); ok {
				v = normalizeBaseURLV1(strVal)
			}
		}

		curEnv[k] = v
	}

	if maxTok, ok := body["maxContextTokens"].(string); ok && maxTok != "" {
		curEnv["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = maxTok
	} else if body["maxContextTokens"] == nil || body["maxContextTokens"] == "" {
		delete(curEnv, "CLAUDE_CODE_MAX_CONTEXT_TOKENS")
	}

	curSettings["hasCompletedOnboarding"] = true
	curSettings["env"] = curEnv

	if err := writeJSONFile(sFile, curSettings); err != nil {
		return nil, err
	}

	// Exa MCP toggle
	if exaEnabled, ok := body["exaMcpEnabled"].(bool); ok {
		cJSON, errC := readJSONFile(cFile)
		if errC != nil || cJSON == nil {
			cJSON = make(map[string]any)
		}

		mcp, okMcp := cJSON["mcpServers"].(map[string]any)
		if !okMcp || mcp == nil {
			mcp = make(map[string]any)
		}

		if exaEnabled {
			mcp["exa"] = map[string]any{
				"type": "sse",
				"url":  "https://mcp.exa.ai/sse",
			}
		} else {
			delete(mcp, "exa")
		}

		if len(mcp) > 0 {
			cJSON["mcpServers"] = mcp
		} else {
			delete(cJSON, "mcpServers")
		}

		if err := writeJSONFile(cFile, cJSON); err != nil {
			return nil, err
		}
	}

	return map[string]any{
		"success": true,
		"message": "Settings updated successfully",
	}, nil
}

// ResetSettings resets Claude CLI settings.
func (h *ClaudeHandler) ResetSettings() (map[string]any, error) {
	sFile, cFile := h.getPaths()

	curSettings, err := readJSONFile(sFile)
	if err == nil && curSettings != nil {
		if curEnv, ok := curSettings["env"].(map[string]any); ok {
			keys := []string{
				"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
				"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL",
				"API_TIMEOUT_MS", "CLAUDE_CODE_MAX_CONTEXT_TOKENS",
			}
			for _, k := range keys {
				delete(curEnv, k)
			}

			curSettings["env"] = curEnv
			if errW := writeJSONFile(sFile, curSettings); errW != nil {
				return nil, errW
			}
		}
	}

	cJSON, err := readJSONFile(cFile)
	if err == nil && cJSON != nil {
		if mcp, ok := cJSON["mcpServers"].(map[string]any); ok {
			delete(mcp, "exa")

			if len(mcp) > 0 {
				cJSON["mcpServers"] = mcp
			} else {
				delete(cJSON, "mcpServers")
			}

			if errW := writeJSONFile(cFile, cJSON); errW != nil {
				return nil, errW
			}
		}
	}

	return map[string]any{
		"success": true,
		"message": "Settings reset successfully",
	}, nil
}

package clitools

import (
	"fmt"
	"path/filepath"
)

// --- Claude ---
type ClaudeHandler struct{}

func (h *ClaudeHandler) getPaths() (settingsPath, claudeJsonPath string) {
	home := userHomeDir()
	return filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".claude.json")
}

func (h *ClaudeHandler) GetStatus(baseUrl string) (map[string]any, error) {
	sFile, _ := h.getPaths()

	installed := checkCommandInstalled("claude", sFile)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Claude CLI is not installed",
		}, nil
	}

	settings, _ := readJSONFile(sFile)
	env, _ := settings["env"].(map[string]any)
	has9Router := false
	if env != nil {
		if u, ok := env["ANTHROPIC_BASE_URL"].(string); ok && u != "" {
			has9Router = true
		}
	}

	_, cFile := h.getPaths()
	cJson, _ := readJSONFile(cFile)
	mcpServers, _ := cJson["mcpServers"].(map[string]any)
	_, hasExa := mcpServers["exa"]

	return map[string]any{
		"installed":     true,
		"settings":      settings,
		"has9Router":    has9Router,
		"exaMcpEnabled": hasExa,
		"settingsPath":  sFile,
	}, nil
}

func (h *ClaudeHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	env, _ := body["env"].(map[string]any)
	if env == nil {
		return nil, fmt.Errorf("invalid env object")
	}

	sFile, cFile := h.getPaths()
	curSettings, _ := readJSONFile(sFile)
	if curSettings == nil {
		curSettings = make(map[string]any)
	}

	curEnv, _ := curSettings["env"].(map[string]any)
	if curEnv == nil {
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
		cJson, _ := readJSONFile(cFile)
		if cJson == nil {
			cJson = make(map[string]any)
		}
		mcp, _ := cJson["mcpServers"].(map[string]any)
		if mcp == nil {
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
			cJson["mcpServers"] = mcp
		} else {
			delete(cJson, "mcpServers")
		}
		_ = writeJSONFile(cFile, cJson)
	}

	return map[string]any{
		"success": true,
		"message": "Settings updated successfully",
	}, nil
}

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
			_ = writeJSONFile(sFile, curSettings)
		}
	}

	cJson, err := readJSONFile(cFile)
	if err == nil && cJson != nil {
		if mcp, ok := cJson["mcpServers"].(map[string]any); ok {
			delete(mcp, "exa")
			if len(mcp) > 0 {
				cJson["mcpServers"] = mcp
			} else {
				delete(cJson, "mcpServers")
			}
			_ = writeJSONFile(cFile, cJson)
		}
	}

	return map[string]any{
		"success": true,
		"message": "Settings reset successfully",
	}, nil
}

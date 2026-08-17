package clitools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// --- Cline ---
type ClineHandler struct{}

func (h *ClineHandler) getPaths() (globalStatePath, secretsPath string) {
	home := userHomeDir()
	dataDir := filepath.Join(home, ".cline", "data")
	return filepath.Join(dataDir, "globalState.json"), filepath.Join(dataDir, "secrets.json")
}

func (h *ClineHandler) GetStatus(baseUrl string) (map[string]any, error) {
	gPath, _ := h.getPaths()
	installed := checkCommandInstalled("cline", gPath)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Cline CLI is not installed",
		}, nil
	}

	globalState, _ := readJSONFile(gPath)
	isOpenAi := false
	if act, _ := globalState["actModeApiProvider"].(string); act == "openai" {
		isOpenAi = true
	} else if plan, _ := globalState["planModeApiProvider"].(string); plan == "openai" {
		isOpenAi = true
	}

	openAiBaseUrl, _ := globalState["openAiBaseUrl"].(string)
	has9Router := isOpenAi && (strings.Contains(openAiBaseUrl, "localhost") ||
		strings.Contains(openAiBaseUrl, "127.0.0.1") ||
		strings.Contains(openAiBaseUrl, "9router") ||
		strings.Contains(openAiBaseUrl, "flamerouter"))

	var settings map[string]any
	if globalState != nil {
		settings = map[string]any{
			"actModeApiProvider":  globalState["actModeApiProvider"],
			"planModeApiProvider": globalState["planModeApiProvider"],
			"openAiBaseUrl":       globalState["openAiBaseUrl"],
			"openAiModelId":       globalState["openAiModelId"],
		}
	}

	return map[string]any{
		"installed":       true,
		"settings":        settings,
		"has9Router":      has9Router,
		"globalStatePath": gPath,
	}, nil
}

func (h *ClineHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	gPath, sPath := h.getPaths()
	normBase := strings.TrimSuffix(baseUrl, "/v1")

	globalState, _ := readJSONFile(gPath)
	if globalState == nil {
		globalState = make(map[string]any)
	}
	globalState["actModeApiProvider"] = "openai"
	globalState["planModeApiProvider"] = "openai"
	globalState["openAiBaseUrl"] = normBase
	globalState["openAiModelId"] = model
	globalState["planModeOpenAiModelId"] = model

	if err := writeJSONFile(gPath, globalState); err != nil {
		return nil, err
	}

	secrets, _ := readJSONFile(sPath)
	if secrets == nil {
		secrets = make(map[string]any)
	}
	secrets["openAiApiKey"] = apiKey
	if err := writeJSONFile(sPath, secrets); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":         true,
		"message":         "Cline settings applied successfully!",
		"globalStatePath": gPath,
	}, nil
}

func (h *ClineHandler) ResetSettings() (map[string]any, error) {
	gPath, sPath := h.getPaths()
	globalState, _ := readJSONFile(gPath)
	if globalState != nil {
		if act, _ := globalState["actModeApiProvider"].(string); act == "openai" {
			delete(globalState, "openAiBaseUrl")
			delete(globalState, "openAiModelId")
			delete(globalState, "planModeOpenAiModelId")
			_ = writeJSONFile(gPath, globalState)
		}
	}
	secrets, _ := readJSONFile(sPath)
	if secrets != nil {
		delete(secrets, "openAiApiKey")
		_ = writeJSONFile(sPath, secrets)
	}

	return map[string]any{
		"success": true,
		"message": "Cline settings reset successfully",
	}, nil
}

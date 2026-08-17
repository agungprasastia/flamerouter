package clitools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ClineHandler manages Cline CLI configuration.
type ClineHandler struct{}

func (h *ClineHandler) getPaths() (globalStatePath, secretsPath string) {
	home := userHomeDir()
	dataDir := filepath.Join(home, ".cline", "data")

	return filepath.Join(dataDir, "globalState.json"), filepath.Join(dataDir, "secrets.json")
}

// GetStatus returns status of Cline CLI.
func (h *ClineHandler) GetStatus(_ string) (map[string]any, error) {
	gPath, _ := h.getPaths()

	installed := checkCommandInstalled("cline", gPath)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Cline CLI is not installed",
		}, nil
	}

	globalState, errG := readJSONFile(gPath)
	if errG != nil {
		globalState = make(map[string]any)
	}

	isOpenAi := false

	act, okAct := globalState["actModeApiProvider"].(string)
	plan, okPlan := globalState["planModeApiProvider"].(string)

	if okAct && act == "openai" {
		isOpenAi = true
	} else if okPlan && plan == "openai" {
		isOpenAi = true
	}

	openAiBaseURL, okBaseURL := globalState["openAiBaseUrl"].(string)
	has9Router := isOpenAi && okBaseURL && (strings.Contains(openAiBaseURL, "localhost") ||
		strings.Contains(openAiBaseURL, "127.0.0.1") ||
		strings.Contains(openAiBaseURL, "9router") ||
		strings.Contains(openAiBaseURL, "flamerouter"))

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

// ApplySettings applies configuration for Cline CLI.
func (h *ClineHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	gPath, sPath := h.getPaths()
	normBase := strings.TrimSuffix(baseURL, "/v1")

	globalState, errG := readJSONFile(gPath)
	if errG != nil || globalState == nil {
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

	secrets, errS := readJSONFile(sPath)
	if errS != nil || secrets == nil {
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

// ResetSettings resets Cline CLI settings.
func (h *ClineHandler) ResetSettings() (map[string]any, error) {
	gPath, sPath := h.getPaths()

	globalState, errG := readJSONFile(gPath)
	if errG == nil && globalState != nil {
		if act, ok := globalState["actModeApiProvider"].(string); ok && act == "openai" {
			delete(globalState, "openAiBaseUrl")
			delete(globalState, "openAiModelId")
			delete(globalState, "planModeOpenAiModelId")

			if errW := writeJSONFile(gPath, globalState); errW != nil {
				return nil, errW
			}
		}
	}

	secrets, errS := readJSONFile(sPath)
	if errS == nil && secrets != nil {
		delete(secrets, "openAiApiKey")

		if errW := writeJSONFile(sPath, secrets); errW != nil {
			return nil, errW
		}
	}

	return map[string]any{
		"success": true,
		"message": "Cline settings reset successfully",
	}, nil
}

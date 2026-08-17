package clitools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenCodeHandler manages OpenCode CLI configurations.
type OpenCodeHandler struct{}

func (h *OpenCodeHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}

// GetStatus returns status of OpenCode CLI.
func (h *OpenCodeHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("opencode", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"config":    nil,
			"message":   "OpenCode CLI is not installed",
		}, nil
	}

	config, errC := readJSONFile(p)
	if errC != nil {
		config = make(map[string]any)
	}

	prov, okProv := config["provider"].(map[string]any)
	if !okProv {
		prov = nil
	}

	var prov9R map[string]any

	if prov != nil {
		if r, okR := prov["9router"].(map[string]any); okR {
			prov9R = r
		}
	}

	has9Router := prov9R != nil

	var models []string

	if prov9R != nil {
		if mmap, ok := prov9R["models"].(map[string]any); ok {
			for k := range mmap {
				models = append(models, k)
			}
		}
	}

	activeModel := ""
	if m, ok := config["model"].(string); ok && strings.HasPrefix(m, "9router/") {
		activeModel = strings.TrimPrefix(m, "9router/")
	}

	optBase := ""

	if prov9R != nil {
		if opts, ok := prov9R["options"].(map[string]any); ok {
			if b, okB := opts["baseURL"].(string); okB {
				optBase = b
			}
		}
	}

	return map[string]any{
		"installed":  true,
		"config":     config,
		"has9Router": has9Router,
		"configPath": p,
		"opencode": map[string]any{
			"models":      models,
			"activeModel": activeModel,
			"baseURL":     optBase,
		},
	}, nil
}

// ApplySettings applies configuration for OpenCode CLI.
func (h *OpenCodeHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	activeModel, okAct := body["activeModel"].(string)

	if !okBase {
		baseURL = ""
	}

	if !okKey {
		apiKey = ""
	}

	if !okAct {
		activeModel = ""
	}

	var modelsArray []string

	if mList, ok := body["models"].([]any); ok {
		for _, m := range mList {
			if s, ok := m.(string); ok && s != "" {
				modelsArray = append(modelsArray, s)
			}
		}
	} else if singleM, ok := body["model"].(string); ok && singleM != "" {
		modelsArray = append(modelsArray, singleM)
	}

	if baseURL == "" || len(modelsArray) == 0 {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}

	p := h.getPath()

	config, errC := readJSONFile(p)
	if errC != nil || config == nil {
		config = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseURL)

	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "sk_9router"
	}

	prov, okProv := config["provider"].(map[string]any)
	if !okProv || prov == nil {
		prov = make(map[string]any)
	}

	modelMap := make(map[string]any)
	for _, m := range modelsArray {
		modelMap[m] = map[string]any{
			"name": m,
		}
	}

	prov["9router"] = map[string]any{
		"name": "9Router",
		"options": map[string]any{
			"baseURL": normBase,
			"apiKey":  keyToUse,
		},
		"models": modelMap,
	}
	config["provider"] = prov

	if activeModel == "" {
		activeModel = modelsArray[0]
	}

	config["model"] = "9router/" + activeModel

	if err := writeJSONFile(p, config); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":    true,
		"message":    "OpenCode settings applied successfully!",
		"configPath": p,
	}, nil
}

// ResetSettings resets OpenCode CLI configurations.
func (h *OpenCodeHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()

	config, errC := readJSONFile(p)
	if errC != nil {
		if os.IsNotExist(errC) {
			return map[string]any{"success": true, "message": "No settings file to reset"}, nil
		}

		return nil, errC
	}

	if config == nil {
		return map[string]any{"success": true, "message": "No settings file to reset"}, nil
	}

	if prov, ok := config["provider"].(map[string]any); ok {
		delete(prov, "9router")
	}

	if m, ok := config["model"].(string); ok && strings.HasPrefix(m, "9router/") {
		delete(config, "model")
	}

	if err := writeJSONFile(p, config); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "9Router settings removed from OpenCode",
	}, nil
}

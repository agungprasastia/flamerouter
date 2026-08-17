package clitools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// --- OpenCode ---
type OpenCodeHandler struct{}

func (h *OpenCodeHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}

func (h *OpenCodeHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("opencode", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"config":    nil,
			"message":   "OpenCode CLI is not installed",
		}, nil
	}

	config, _ := readJSONFile(p)
	prov, _ := config["provider"].(map[string]any)
	prov9R, _ := prov["9router"].(map[string]any)
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
			optBase, _ = opts["baseURL"].(string)
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

func (h *OpenCodeHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	activeModel, _ := body["activeModel"].(string)

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

	if baseUrl == "" || len(modelsArray) == 0 {
		return nil, fmt.Errorf("baseUrl and at least one model are required")
	}

	p := h.getPath()
	config, _ := readJSONFile(p)
	if config == nil {
		config = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseUrl)
	keyToUse := apiKey
	if keyToUse == "" {
		keyToUse = "sk_9router"
	}

	prov, _ := config["provider"].(map[string]any)
	if prov == nil {
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

func (h *OpenCodeHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	config, _ := readJSONFile(p)
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

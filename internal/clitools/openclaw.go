package clitools

import (
	"fmt"
	"path/filepath"
)

// --- OpenClaw ---
type OpenClawHandler struct{}


func (h *OpenClawHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

func (h *OpenClawHandler) GetStatus(baseUrl string) (map[string]any, error) {
	p := h.getPath()
	installed := checkCommandInstalled("openclaw", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Open Claw CLI is not installed",
		}, nil
	}

	settings, _ := readJSONFile(p)
	modelsSection, _ := settings["models"].(map[string]any)
	providersSection, _ := modelsSection["providers"].(map[string]any)
	_, has9Router := providersSection["9router"]

	var enrichedAgents []any
	agentsObj, _ := settings["agents"].(map[string]any)
	agentList, _ := agentsObj["list"].([]any)
	for _, aRaw := range agentList {
		if a, ok := aRaw.(map[string]any); ok {
			agentCopy := make(map[string]any)
			for k, v := range a {
				agentCopy[k] = v
			}
			// normalize model
			if mStr, okStr := a["model"].(string); okStr {
				agentCopy["model"] = mStr
			} else if mObj, okObj := a["model"].(map[string]any); okObj {
				if prim, okPrim := mObj["primary"].(string); okPrim {
					agentCopy["model"] = prim
				}
			}
			// read agentDir/models.json
			if aDir, okDir := a["agentDir"].(string); okDir && aDir != "" {
				mFile := filepath.Join(aDir, "models.json")
				if mData, err := readJSONFile(mFile); err == nil {
					if pData, okP := mData["providers"].(map[string]any); okP {
						if rData, okR := pData["9router"].(map[string]any); okR {
							if mList, okM := rData["models"].([]any); okM && len(mList) > 0 {
								if firstM, okFM := mList[0].(map[string]any); okFM {
									agentCopy["currentModel"] = firstM["id"]
								}
							}
						}
					}
				}
			}
			enrichedAgents = append(enrichedAgents, agentCopy)
		}
	}

	return map[string]any{
		"installed":    true,
		"settings":     settings,
		"agents":       enrichedAgents,
		"has9Router":   has9Router,
		"settingsPath": p,
	}, nil
}

func (h *OpenClawHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseUrl, _ := body["baseUrl"].(string)
	apiKey, _ := body["apiKey"].(string)
	model, _ := body["model"].(string)
	agentId, _ := body["agentId"].(string)

	if baseUrl == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	p := h.getPath()
	settings, _ := readJSONFile(p)
	if settings == nil {
		settings = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseUrl)
	modelsSec, _ := settings["models"].(map[string]any)
	if modelsSec == nil {
		modelsSec = make(map[string]any)
	}
	provSec, _ := modelsSec["providers"].(map[string]any)
	if provSec == nil {
		provSec = make(map[string]any)
	}

	provSec["9router"] = map[string]any{
		"baseUrl": normBase,
		"apiKey":  apiKey,
		"api":     "openai-completions",
		"models": []any{
			map[string]any{
				"id":            model,
				"name":          model,
				"contextWindow": 128000,
				"maxTokens":     16384,
			},
		},
	}
	modelsSec["providers"] = provSec
	settings["models"] = modelsSec

	// If agentId provided, configure that agent
	if agentId != "" {
		if agentsObj, ok := settings["agents"].(map[string]any); ok {
			if list, okList := agentsObj["list"].([]any); okList {
				for _, aRaw := range list {
					if a, okMap := aRaw.(map[string]any); okMap {
						if id, _ := a["id"].(string); id == agentId {
							a["model"] = "9router/" + model
							if aDir, okDir := a["agentDir"].(string); okDir && aDir != "" {
								mPath := filepath.Join(aDir, "models.json")
								_ = writeJSONFile(mPath, map[string]any{
									"providers": map[string]any{
										"9router": provSec["9router"],
									},
								})
							}
						}
					}
				}
			}
		}
	}

	if err := writeJSONFile(p, settings); err != nil {
		return nil, err
	}

	return map[string]any{
		"success":      true,
		"message":      "Open Claw settings applied successfully!",
		"settingsPath": p,
	}, nil
}

func (h *OpenClawHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()
	settings, _ := readJSONFile(p)
	if settings == nil {
		return map[string]any{"success": true, "message": "No settings file to reset"}, nil
	}

	if modelsSec, ok := settings["models"].(map[string]any); ok {
		if provSec, okP := modelsSec["providers"].(map[string]any); okP {
			delete(provSec, "9router")
		}
	}

	if err := writeJSONFile(p, settings); err != nil {
		return nil, err
	}

	return map[string]any{
		"success": true,
		"message": "9Router settings removed from Open Claw",
	}, nil
}

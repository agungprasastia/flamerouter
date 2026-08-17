package clitools

import (
	"fmt"
	"os"
	"path/filepath"
)

// OpenClawHandler manages OpenClaw CLI configurations.
type OpenClawHandler struct{}

func (h *OpenClawHandler) getPath() string {
	home := userHomeDir()
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

// GetStatus returns status of OpenClaw CLI.
func (h *OpenClawHandler) GetStatus(_ string) (map[string]any, error) {
	p := h.getPath()

	installed := checkCommandInstalled("openclaw", p)
	if !installed {
		return map[string]any{
			"installed": false,
			"settings":  nil,
			"message":   "Open Claw CLI is not installed",
		}, nil
	}

	settings, errS := readJSONFile(p)
	if errS != nil {
		settings = make(map[string]any)
	}

	modelsSection, okModels := settings["models"].(map[string]any)
	if !okModels {
		modelsSection = nil
	}

	var providersSection map[string]any

	if modelsSection != nil {
		if prov, okProv := modelsSection["providers"].(map[string]any); okProv {
			providersSection = prov
		}
	}

	_, has9Router := providersSection["9router"]

	var enrichedAgents []any

	agentsObj, okAgents := settings["agents"].(map[string]any)
	if !okAgents {
		agentsObj = nil
	}

	var agentList []any

	if agentsObj != nil {
		if l, okL := agentsObj["list"].([]any); okL {
			agentList = l
		}
	}

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

// ApplySettings applies configuration for OpenClaw CLI.
func (h *OpenClawHandler) ApplySettings(body map[string]any) (map[string]any, error) {
	baseURL, okBase := body["baseUrl"].(string)
	apiKey, okKey := body["apiKey"].(string)
	model, okModel := body["model"].(string)
	agentID, okAgent := body["agentId"].(string)

	if !okBase || !okKey || !okModel || baseURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("baseUrl, apiKey and model are required")
	}

	if !okAgent {
		agentID = ""
	}

	p := h.getPath()

	settings, errS := readJSONFile(p)
	if errS != nil || settings == nil {
		settings = make(map[string]any)
	}

	normBase := normalizeBaseURLV1(baseURL)

	modelsSec, okMS := settings["models"].(map[string]any)
	if !okMS || modelsSec == nil {
		modelsSec = make(map[string]any)
	}

	provSec, okPS := modelsSec["providers"].(map[string]any)
	if !okPS || provSec == nil {
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

	// If agentID provided, configure that agent
	if agentID != "" {
		if agentsObj, ok := settings["agents"].(map[string]any); ok {
			if list, okList := agentsObj["list"].([]any); okList {
				for _, aRaw := range list {
					if a, okMap := aRaw.(map[string]any); okMap {
						if id, okID := a["id"].(string); okID && id == agentID {
							a["model"] = "9router/" + model
							if aDir, okDir := a["agentDir"].(string); okDir && aDir != "" {
								mPath := filepath.Join(aDir, "models.json")
								if errW := writeJSONFile(mPath, map[string]any{
									"providers": map[string]any{
										"9router": provSec["9router"],
									},
								}); errW != nil {
									return nil, errW
								}
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

// ResetSettings resets OpenClaw CLI configurations.
func (h *OpenClawHandler) ResetSettings() (map[string]any, error) {
	p := h.getPath()

	settings, errS := readJSONFile(p)
	if errS != nil {
		if os.IsNotExist(errS) {
			return map[string]any{"success": true, "message": "No settings file to reset"}, nil
		}

		return nil, errS
	}

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

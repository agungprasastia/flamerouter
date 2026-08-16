package combo

import (
	"context"
	"encoding/json"
	"errors"
	"flamerouter/internal/provider"
	"flamerouter/internal/store"
)

// DefaultFallbackModel is used when enabled modality pool has no models specified.
const DefaultFallbackModel = "oc/mimo-v2.5-free"

// ModalityPoolConfig defines configuration for a single capability pool.
type ModalityPoolConfig struct {
	Models     []string `json:"models"`
	Enabled    bool     `json:"enabled"`
	RoundRobin bool     `json:"roundRobin"`
}

// CapacityAdapterConfig defines full configuration for all capability pools.
type CapacityAdapterConfig struct {
	Vision     ModalityPoolConfig `json:"vision"`
	PDF        ModalityPoolConfig `json:"pdf"`
	AudioInput ModalityPoolConfig `json:"audioInput"`
	VideoInput ModalityPoolConfig `json:"videoInput"`
}

var capabilityKeys = []string{"vision", "pdf", "audioInput", "videoInput"}

// DefaultCapacityAdapterConfig returns default configuration matching upstream.
func DefaultCapacityAdapterConfig() CapacityAdapterConfig {
	return CapacityAdapterConfig{
		Vision:     ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: nil},
		PDF:        ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		AudioInput: ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: nil},
		VideoInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
	}
}

// NormalizePoolConfig normalizes an arbitrary JSON value/map/slice into ModalityPoolConfig.
func NormalizePoolConfig(raw any) ModalityPoolConfig {
	if raw == nil {
		return ModalityPoolConfig{}
	}

	switch v := raw.(type) {
	case []any:
		var models []string

		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				models = append(models, s)
			} else if m, ok := item.(map[string]any); ok {
				if ms, ok := m["model"].(string); ok && ms != "" {
					models = append(models, ms)
				}
			}
		}

		return ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: models}
	case []string:
		return ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: v}
	case map[string]any:
		cfg := ModalityPoolConfig{Enabled: true}
		if en, ok := v["enabled"].(bool); ok {
			cfg.Enabled = en
		}

		if rr, ok := v["roundRobin"].(bool); ok {
			cfg.RoundRobin = rr
		}

		if arr, ok := v["models"].([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && s != "" {
					cfg.Models = append(cfg.Models, s)
				}
			}
		} else if arr, ok := v["models"].([]string); ok {
			cfg.Models = arr
		}

		return cfg
	case ModalityPoolConfig:
		return v
	default:
		return ModalityPoolConfig{}
	}
}

// LoadCapacityAdapterConfig loads CapacityAdapterConfig from store or JSON string.
func LoadCapacityAdapterConfig(st *store.Store) CapacityAdapterConfig {
	cfg := DefaultCapacityAdapterConfig()
	if st == nil {
		return cfg
	}

	raw, err := st.GetSetting("capacityAdapter")
	if err != nil || raw == "" {
		return cfg
	}

	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return cfg
	}

	if v, ok := m["vision"]; ok {
		cfg.Vision = NormalizePoolConfig(v)
	}

	if v, ok := m["pdf"]; ok {
		cfg.PDF = NormalizePoolConfig(v)
	}

	if v, ok := m["audioInput"]; ok {
		cfg.AudioInput = NormalizePoolConfig(v)
	}

	if v, ok := m["videoInput"]; ok {
		cfg.VideoInput = NormalizePoolConfig(v)
	}

	return cfg
}

// GetPoolConfig gets the normalized pool configuration for a given capability,
// falling back to DefaultFallbackModel if enabled but empty.
func GetPoolConfig(capName string, cfg CapacityAdapterConfig) ModalityPoolConfig {
	pool := poolByCapName(capName, cfg)
	if pool.Enabled && len(pool.Models) == 0 {
		pool.Models = []string{DefaultFallbackModel}
	}

	return pool
}

func poolByCapName(capName string, cfg CapacityAdapterConfig) ModalityPoolConfig {
	switch capName {
	case "vision":
		return cfg.Vision
	case "pdf":
		return cfg.PDF
	case "audioInput":
		return cfg.AudioInput
	case "videoInput":
		return cfg.VideoInput
	default:
		return ModalityPoolConfig{}
	}
}

// GetCapacityAdapterModels flattens enabled models across all capability pools in priority order, deduped.
func GetCapacityAdapterModels(cfg CapacityAdapterConfig) []string {
	seen := make(map[string]bool)

	var models []string

	for _, capName := range capabilityKeys {
		pool := GetPoolConfig(capName, cfg)
		if !pool.Enabled {
			continue
		}

		for _, m := range pool.Models {
			if m != "" && !seen[m] {
				seen[m] = true

				models = append(models, m)
			}
		}
	}

	return models
}

// GetCapacityAdapterStrategy returns strategy for capability: "round-robin" or "fallback".
func GetCapacityAdapterStrategy(capName string, cfg CapacityAdapterConfig) string {
	pool := GetPoolConfig(capName, cfg)
	if pool.Enabled && pool.RoundRobin {
		return "round-robin"
	}

	return "fallback"
}

// GetActiveAdapterStrategy returns strategy for required capabilities (first enabled matching hard cap).
func GetActiveAdapterStrategy(requiredCaps map[string]bool, cfg CapacityAdapterConfig) string {
	for _, capName := range capabilityKeys {
		if !requiredCaps[capName] || !hardCaps[capName] {
			continue
		}

		pool := GetPoolConfig(capName, cfg)
		if !pool.Enabled || len(pool.Models) == 0 {
			continue
		}

		return GetCapacityAdapterStrategy(capName, cfg)
	}

	return "fallback"
}

// ModelSatisfiesCapabilities checks whether a model satisfies all required hard capabilities.
func ModelSatisfiesCapabilities(modelStr string, requiredHard []string) bool {
	_, modelName := splitProviderModel(modelStr)
	caps := provider.GetCapabilities(modelName)

	has := func(name string) bool {
		switch name {
		case "vision":
			return caps.Vision
		case "pdf":
			return caps.PDF
		case "audioInput":
			return caps.AudioInput
		case "videoInput":
			return caps.VideoInput
		case "search":
			return caps.Search
		default:
			return false
		}
	}
	for _, req := range requiredHard {
		if !has(req) {
			return false
		}
	}

	return true
}

// AugmentModelsWithCapacityAdapter prepends capacity-adapter models when none of the original models satisfy requirements.
func AugmentModelsWithCapacityAdapter(models []string, requiredCaps map[string]bool, cfg CapacityAdapterConfig) []string {
	var hard []string

	for _, k := range capabilityKeys {
		if requiredCaps[k] && hardCaps[k] {
			hard = append(hard, k)
		}
	}

	if len(hard) == 0 || len(models) == 0 {
		return models
	}

	for _, m := range models {
		if ModelSatisfiesCapabilities(m, hard) {
			return models
		}
	}

	origSet := make(map[string]bool, len(models))
	for _, m := range models {
		origSet[m] = true
	}

	var pool []string

	for _, m := range GetCapacityAdapterModels(cfg) {
		if !origSet[m] && ModelSatisfiesCapabilities(m, hard) {
			pool = append(pool, m)
		}
	}

	if len(pool) == 0 {
		return models
	}

	return append(pool, models...)
}

// AdaptModelForCapabilities selects the best model for the requested capabilities given a configuration.
// Returns the requested model if it already satisfies capabilities, otherwise selects from adapter pools.
func AdaptModelForCapabilities(ctx context.Context, requestedModel string, requiredCapabilities []string, config CapacityAdapterConfig) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}

	reqMap := make(map[string]bool, len(requiredCapabilities))

	var hard []string

	for _, c := range requiredCapabilities {
		reqMap[c] = true

		if hardCaps[c] {
			hard = append(hard, c)
		}
	}

	if len(hard) == 0 || requestedModel == "" || ModelSatisfiesCapabilities(requestedModel, hard) {
		return requestedModel, nil
	}

	augmented := AugmentModelsWithCapacityAdapter([]string{requestedModel}, reqMap, config)
	if len(augmented) > 0 && augmented[0] != requestedModel {
		start := GetActiveAdapterStrategy(reqMap, config)
		if start == "round-robin" {
			// ponytail: single sticky rotation for adapter pool when round-robin enabled
			rotated := GetRotatedModels(augmented[:len(augmented)-1], "capacity_adapter", "round-robin", 1)
			if len(rotated) > 0 {
				return rotated[0], nil
			}
		}

		return augmented[0], nil
	}

	if len(augmented) > 0 && augmented[0] != "" {
		return augmented[0], nil
	}

	return requestedModel, errors.New("no capable model found")
}

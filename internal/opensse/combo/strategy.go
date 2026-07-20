package combo

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
)

// Strategy defines how a combo group dispatches across models.
type Strategy interface {
	// Execute runs the combo strategy, writing response to w.
	Execute(ctx context.Context, w http.ResponseWriter, body []byte,
		models []string, st *store.Store, exec executor.Executor,
		fb *fallback.Fallback, opts Options) error
}

// Options carries per-request config for combo execution.
type Options struct {
	SourceFormat   string
	TargetFormat   string
	Stream         bool
	ClientHeaders  http.Header
	TokenSaverJSON string // raw JSON from settings
	ComboName      string
	StickyLimit    int
	JudgeModel     string
	// SingleModel runs one provider/model and writes to w. Returns nil on success.
	SingleModel func(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, stream bool) error
}

// Resolve picks the strategy for a combo based on settings.
// comboStrategy: "fallback" (default), "round-robin", "fusion"
// perCombo overrides per combo name (optional; LoadStrategySettings already folds
// comboStrategies[name].fallbackStrategy into comboStrategy before chat calls Resolve).
func Resolve(comboStrategy string, perCombo map[string]string, comboName string) Strategy {
	s := comboStrategy
	if perCombo != nil {
		if override, ok := perCombo[comboName]; ok {
			s = override
		}
	}
	switch s {
	case "round-robin":
		return &RoundRobin{}
	case "fusion":
		return &Fusion{}
	default:
		return &FallbackStrategy{}
	}
}

// LoadStrategySettings reads comboStrategy + comboStrategies from store settings.
func LoadStrategySettings(st *store.Store, comboName string) (strategy string, sticky int, judge string) {
	strategy = "fallback"
	sticky = 1
	if st == nil {
		return
	}
	if v, err := st.GetSetting("comboStrategy"); err == nil && v != "" {
		strategy = v
	}
	if v, err := st.GetSetting("comboStickyRoundRobinLimit"); err == nil && v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			sticky = n
		}
	}
	raw, err := st.GetSetting("comboStrategies")
	if err != nil || raw == "" {
		return
	}
	var m map[string]map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	entry, ok := m[comboName]
	if !ok {
		return
	}
	if fs, ok := entry["fallbackStrategy"].(string); ok && fs != "" {
		strategy = fs
	}
	if j, ok := entry["judgeModel"].(string); ok {
		judge = j
	}
	return
}

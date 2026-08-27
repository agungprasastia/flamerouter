package combo

import (
	"flamerouter/internal/store"
	"testing"
)

func TestResolve_StrategySelection(t *testing.T) {
	tests := []struct {
		wantType      Strategy
		perCombo      map[string]string
		name          string
		comboStrategy string
		comboName     string
	}{
		{
			wantType:      &FallbackStrategy{},
			perCombo:      nil,
			name:          "default fallback when strategy is empty",
			comboStrategy: "",
			comboName:     "combo1",
		},
		{
			wantType:      &FallbackStrategy{},
			perCombo:      nil,
			name:          "fallback strategy explicitly set",
			comboStrategy: "fallback",
			comboName:     "combo1",
		},
		{
			wantType:      &RoundRobin{},
			perCombo:      nil,
			name:          "round-robin strategy",
			comboStrategy: "round-robin",
			comboName:     "combo1",
		},
		{
			wantType:      &Fusion{},
			perCombo:      nil,
			name:          "fusion strategy",
			comboStrategy: "fusion",
			comboName:     "combo1",
		},
		{
			wantType:      &Fusion{},
			perCombo:      map[string]string{"combo1": "fusion", "combo2": "round-robin"},
			name:          "per-combo override matches comboName",
			comboStrategy: "fallback",
			comboName:     "combo1",
		},
		{
			wantType:      &FallbackStrategy{},
			perCombo:      map[string]string{"combo2": "fusion"},
			name:          "per-combo override does not match comboName",
			comboStrategy: "fallback",
			comboName:     "combo1",
		},
		{
			wantType:      &FallbackStrategy{},
			perCombo:      nil,
			name:          "unknown strategy falls back to FallbackStrategy",
			comboStrategy: "unknown-strategy",
			comboName:     "combo1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Resolve(tt.comboStrategy, tt.perCombo, tt.comboName)
			switch tt.wantType.(type) {
			case *FallbackStrategy:
				if _, ok := got.(*FallbackStrategy); !ok {
					t.Errorf("Resolve() = %T, want *FallbackStrategy", got)
				}
			case *RoundRobin:
				if _, ok := got.(*RoundRobin); !ok {
					t.Errorf("Resolve() = %T, want *RoundRobin", got)
				}
			case *Fusion:
				if _, ok := got.(*Fusion); !ok {
					t.Errorf("Resolve() = %T, want *Fusion", got)
				}
			}
		})
	}
}

func TestLoadStrategySettings_NilStore(t *testing.T) {
	strategy, sticky, judge := LoadStrategySettings(nil, "my-combo")
	if strategy != "fallback" {
		t.Errorf("expected strategy 'fallback', got %q", strategy)
	}

	if sticky != 1 {
		t.Errorf("expected sticky 1, got %d", sticky)
	}

	if judge != "" {
		t.Errorf("expected judge '', got %q", judge)
	}
}

func TestLoadStrategySettings_WithStore(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	defer func() {
		if errClose := st.Close(); errClose != nil {
			_ = errClose
		}
	}()

	t.Run("defaults when store is empty", func(t *testing.T) {
		strategy, sticky, judge := LoadStrategySettings(st, "my-combo")
		if strategy != "fallback" || sticky != 1 || judge != "" {
			t.Errorf("unexpected defaults: strategy=%q, sticky=%d, judge=%q", strategy, sticky, judge)
		}
	})

	t.Run("loads global settings", func(t *testing.T) {
		err1 := st.SetSetting("comboStrategy", "round-robin")
		if err1 != nil {
			t.Fatal(err1)
		}

		err2 := st.SetSetting("comboStickyRoundRobinLimit", "5")
		if err2 != nil {
			t.Fatal(err2)
		}

		strategy, sticky, judge := LoadStrategySettings(st, "my-combo")
		if strategy != "round-robin" {
			t.Errorf("expected strategy 'round-robin', got %q", strategy)
		}

		if sticky != 5 {
			t.Errorf("expected sticky 5, got %d", sticky)
		}

		if judge != "" {
			t.Errorf("expected judge '', got %q", judge)
		}
	})

	t.Run("invalid sticky limit falls back to default 1", func(t *testing.T) {
		err1 := st.SetSetting("comboStickyRoundRobinLimit", "invalid")
		if err1 != nil {
			t.Fatal(err1)
		}

		_, sticky1, _ := LoadStrategySettings(st, "my-combo")
		if sticky1 != 1 {
			t.Errorf("expected sticky 1 for invalid int, got %d", sticky1)
		}

		err2 := st.SetSetting("comboStickyRoundRobinLimit", "0")
		if err2 != nil {
			t.Fatal(err2)
		}

		_, sticky2, _ := LoadStrategySettings(st, "my-combo")
		if sticky2 != 1 {
			t.Errorf("expected sticky 1 for 0 limit, got %d", sticky2)
		}
	})

	t.Run("loads combo override from comboStrategies JSON", func(t *testing.T) {
		jsonConfig := `{
			"combo-alpha": {
				"fallbackStrategy": "fusion",
				"judgeModel": "gpt-4o"
			},
			"combo-beta": {
				"fallbackStrategy": "round-robin"
			}
		}`

		errSet := st.SetSetting("comboStrategies", jsonConfig)
		if errSet != nil {
			t.Fatal(errSet)
		}

		// Check combo-alpha
		strategy1, _, judge1 := LoadStrategySettings(st, "combo-alpha")
		if strategy1 != "fusion" {
			t.Errorf("expected strategy 'fusion', got %q", strategy1)
		}

		if judge1 != "gpt-4o" {
			t.Errorf("expected judge 'gpt-4o', got %q", judge1)
		}

		// Check combo-beta
		strategy2, _, judge2 := LoadStrategySettings(st, "combo-beta")
		if strategy2 != "round-robin" {
			t.Errorf("expected strategy 'round-robin', got %q", strategy2)
		}

		if judge2 != "" {
			t.Errorf("expected judge '', got %q", judge2)
		}

		// Check unlisted combo
		strategy3, _, _ := LoadStrategySettings(st, "combo-other")
		if strategy3 != "round-robin" { // should remain global comboStrategy ("round-robin" set earlier)
			t.Errorf("expected global strategy 'round-robin', got %q", strategy3)
		}
	})
}

func TestLoadComboOverride_EdgeCases(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}

	defer func() {
		if errClose := st.Close(); errClose != nil {
			_ = errClose
		}
	}()

	t.Run("invalid JSON in comboStrategies setting", func(t *testing.T) {
		errSet := st.SetSetting("comboStrategies", "not-valid-json")
		if errSet != nil {
			t.Fatal(errSet)
		}

		gotStrategy, judge := loadComboOverride(st, "my-combo", "fallback", "default-judge")
		if gotStrategy != "fallback" || judge != "default-judge" {
			t.Errorf("expected unchanged strategy and judge, gotStrategy=%q judge=%q", gotStrategy, judge)
		}
	})

	t.Run("empty string in fallbackStrategy does not override", func(t *testing.T) {
		jsonConfig := `{
			"my-combo": {
				"fallbackStrategy": "",
				"judgeModel": "new-judge"
			}
		}`

		errSet := st.SetSetting("comboStrategies", jsonConfig)
		if errSet != nil {
			t.Fatal(errSet)
		}

		gotStrategy, judge := loadComboOverride(st, "my-combo", "fallback", "default-judge")
		if gotStrategy != "fallback" {
			t.Errorf("expected fallback strategy unchanged, got %q", gotStrategy)
		}

		if judge != "new-judge" {
			t.Errorf("expected judge model updated to 'new-judge', got %q", judge)
		}
	})
}

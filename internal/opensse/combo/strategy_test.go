package combo

import (
	"testing"

	"flamerouter/internal/store"
)

func TestResolve_StrategySelection(t *testing.T) {
	tests := []struct {
		name          string
		comboStrategy string
		perCombo      map[string]string
		comboName     string
		wantType      Strategy
	}{
		{
			name:          "default fallback when strategy is empty",
			comboStrategy: "",
			perCombo:      nil,
			comboName:     "combo1",
			wantType:      &FallbackStrategy{},
		},
		{
			name:          "fallback strategy explicitly set",
			comboStrategy: "fallback",
			perCombo:      nil,
			comboName:     "combo1",
			wantType:      &FallbackStrategy{},
		},
		{
			name:          "round-robin strategy",
			comboStrategy: "round-robin",
			perCombo:      nil,
			comboName:     "combo1",
			wantType:      &RoundRobin{},
		},
		{
			name:          "fusion strategy",
			comboStrategy: "fusion",
			perCombo:      nil,
			comboName:     "combo1",
			wantType:      &Fusion{},
		},
		{
			name:          "per-combo override matches comboName",
			comboStrategy: "fallback",
			perCombo:      map[string]string{"combo1": "fusion", "combo2": "round-robin"},
			comboName:     "combo1",
			wantType:      &Fusion{},
		},
		{
			name:          "per-combo override does not match comboName",
			comboStrategy: "fallback",
			perCombo:      map[string]string{"combo2": "fusion"},
			comboName:     "combo1",
			wantType:      &FallbackStrategy{},
		},
		{
			name:          "unknown strategy falls back to FallbackStrategy",
			comboStrategy: "unknown-strategy",
			perCombo:      nil,
			comboName:     "combo1",
			wantType:      &FallbackStrategy{},
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
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	t.Run("defaults when store is empty", func(t *testing.T) {
		strategy, sticky, judge := LoadStrategySettings(st, "my-combo")
		if strategy != "fallback" || sticky != 1 || judge != "" {
			t.Errorf("unexpected defaults: strategy=%q, sticky=%d, judge=%q", strategy, sticky, judge)
		}
	})

	t.Run("loads global settings", func(t *testing.T) {
		if err := st.SetSetting("comboStrategy", "round-robin"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSetting("comboStickyRoundRobinLimit", "5"); err != nil {
			t.Fatal(err)
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
		if err := st.SetSetting("comboStickyRoundRobinLimit", "invalid"); err != nil {
			t.Fatal(err)
		}
		_, sticky, _ := LoadStrategySettings(st, "my-combo")
		if sticky != 1 {
			t.Errorf("expected sticky 1 for invalid int, got %d", sticky)
		}

		if err := st.SetSetting("comboStickyRoundRobinLimit", "0"); err != nil {
			t.Fatal(err)
		}
		_, sticky, _ = LoadStrategySettings(st, "my-combo")
		if sticky != 1 {
			t.Errorf("expected sticky 1 for 0 limit, got %d", sticky)
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
		if err := st.SetSetting("comboStrategies", jsonConfig); err != nil {
			t.Fatal(err)
		}

		// Check combo-alpha
		strategy, _, judge := LoadStrategySettings(st, "combo-alpha")
		if strategy != "fusion" {
			t.Errorf("expected strategy 'fusion', got %q", strategy)
		}
		if judge != "gpt-4o" {
			t.Errorf("expected judge 'gpt-4o', got %q", judge)
		}

		// Check combo-beta
		strategy, _, judge = LoadStrategySettings(st, "combo-beta")
		if strategy != "round-robin" {
			t.Errorf("expected strategy 'round-robin', got %q", strategy)
		}
		if judge != "" {
			t.Errorf("expected judge '', got %q", judge)
		}

		// Check unlisted combo
		strategy, _, judge = LoadStrategySettings(st, "combo-other")
		if strategy != "round-robin" { // should remain global comboStrategy ("round-robin" set earlier)
			t.Errorf("expected global strategy 'round-robin', got %q", strategy)
		}
	})
}

func TestLoadComboOverride_EdgeCases(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	t.Run("invalid JSON in comboStrategies setting", func(t *testing.T) {
		if err := st.SetSetting("comboStrategies", "not-valid-json"); err != nil {
			t.Fatal(err)
		}
		strat, judge := loadComboOverride(st, "my-combo", "fallback", "default-judge")
		if strat != "fallback" || judge != "default-judge" {
			t.Errorf("expected unchanged strategy and judge, got strat=%q judge=%q", strat, judge)
		}
	})

	t.Run("empty string in fallbackStrategy does not override", func(t *testing.T) {
		jsonConfig := `{
			"my-combo": {
				"fallbackStrategy": "",
				"judgeModel": "new-judge"
			}
		}`
		if err := st.SetSetting("comboStrategies", jsonConfig); err != nil {
			t.Fatal(err)
		}
		strat, judge := loadComboOverride(st, "my-combo", "fallback", "default-judge")
		if strat != "fallback" {
			t.Errorf("expected fallback strategy unchanged, got %q", strat)
		}
		if judge != "new-judge" {
			t.Errorf("expected judge model updated to 'new-judge', got %q", judge)
		}
	})
}

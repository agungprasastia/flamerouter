package combo

import (
	"context"
	"flamerouter/internal/store"
	"reflect"
	"testing"
)

func TestNormalizePoolConfig(t *testing.T) {
	// legacy slice form
	legacy := []any{"openai/gpt-4o", "gemini/gemini-2.5-flash"}

	cfg := NormalizePoolConfig(legacy)
	if !cfg.Enabled || cfg.RoundRobin || len(cfg.Models) != 2 {
		t.Fatalf("unexpected legacy parse: %+v", cfg)
	}

	// object with models and roundRobin
	obj := map[string]any{
		"enabled":    true,
		"roundRobin": true,
		"models":     []any{"openai/gpt-4o"},
	}

	cfg2 := NormalizePoolConfig(obj)
	if !cfg2.Enabled || !cfg2.RoundRobin || len(cfg2.Models) != 1 || cfg2.Models[0] != "openai/gpt-4o" {
		t.Fatalf("unexpected obj parse: %+v", cfg2)
	}

	// disabled object
	objDisabled := map[string]any{
		"enabled": false,
		"models":  []any{"openai/gpt-4o"},
	}

	cfg3 := NormalizePoolConfig(objDisabled)
	if cfg3.Enabled {
		t.Fatalf("expected disabled: %+v", cfg3)
	}
}

func TestGetPoolConfig_EmptyEnabledFallsBack(t *testing.T) {
	cfg := CapacityAdapterConfig{
		Vision:     ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: nil},
		PDF:        ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		AudioInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		VideoInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
	}

	pool := GetPoolConfig("vision", cfg)
	if len(pool.Models) != 1 || pool.Models[0] != DefaultFallbackModel {
		t.Fatalf("expected fallback model, got %v", pool.Models)
	}
}

func TestGetCapacityAdapterModels_Deduplication(t *testing.T) {
	cfg := CapacityAdapterConfig{
		Vision:     ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: []string{"openai/gpt-4o", "gemini/gemini-2.5-flash"}},
		AudioInput: ModalityPoolConfig{Enabled: true, RoundRobin: false, Models: []string{"openai/gpt-4o", "whisper-large"}},
		PDF:        ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: []string{"ignored/pdf"}},
		VideoInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
	}
	models := GetCapacityAdapterModels(cfg)
	want := []string{"openai/gpt-4o", "gemini/gemini-2.5-flash", "whisper-large"}

	if !reflect.DeepEqual(models, want) {
		t.Fatalf("want %v, got %v", want, models)
	}
}

func TestAugmentModelsWithCapacityAdapter_PrependWhenIncapable(t *testing.T) {
	cfg := CapacityAdapterConfig{
		Vision: ModalityPoolConfig{
			Enabled:    true,
			RoundRobin: false,
			Models:     []string{"openai/gpt-4o"},
		},
		PDF:        ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		AudioInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		VideoInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
	}
	models := []string{"deepseek/deepseek-v3"}
	reqCaps := map[string]bool{"vision": true}

	augmented := AugmentModelsWithCapacityAdapter(models, reqCaps, cfg)
	want := []string{"openai/gpt-4o", "deepseek/deepseek-v3"}

	if !reflect.DeepEqual(augmented, want) {
		t.Fatalf("want %v, got %v", want, augmented)
	}
}

func TestAugmentModelsWithCapacityAdapter_UntouchedWhenCapable(t *testing.T) {
	cfg := CapacityAdapterConfig{
		Vision: ModalityPoolConfig{
			Enabled:    true,
			RoundRobin: false,
			Models:     []string{"openai/gpt-4o"},
		},
		PDF:        ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		AudioInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		VideoInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
	}
	models := []string{"openai/gpt-4o", "deepseek/deepseek-v3"}
	reqCaps := map[string]bool{"vision": true}

	augmented := AugmentModelsWithCapacityAdapter(models, reqCaps, cfg)
	if !reflect.DeepEqual(augmented, models) {
		t.Fatalf("expected untouched models, got %v", augmented)
	}
}

func TestAdaptModelForCapabilities(t *testing.T) {
	cfg := CapacityAdapterConfig{
		Vision: ModalityPoolConfig{
			Enabled:    true,
			RoundRobin: false,
			Models:     []string{"openai/gpt-4o"},
		},
		PDF:        ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		AudioInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
		VideoInput: ModalityPoolConfig{Enabled: false, RoundRobin: false, Models: nil},
	}

	ctx := context.Background()

	// 1. Incapable model gets adapted to vision capable model
	selected, err := AdaptModelForCapabilities(ctx, "deepseek/deepseek-v3", []string{"vision"}, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if selected != "openai/gpt-4o" {
		t.Fatalf("expected openai/gpt-4o, got %s", selected)
	}

	// 2. Already capable model stays as is
	selected2, err := AdaptModelForCapabilities(ctx, "openai/gpt-4o", []string{"vision"}, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if selected2 != "openai/gpt-4o" {
		t.Fatalf("expected openai/gpt-4o, got %s", selected2)
	}

	// 3. No capabilities required stays as is
	selected3, err := AdaptModelForCapabilities(ctx, "deepseek/deepseek-v3", nil, cfg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if selected3 != "deepseek/deepseek-v3" {
		t.Fatalf("expected deepseek/deepseek-v3, got %s", selected3)
	}
}

func TestLoadCapacityAdapterConfig(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := st.Close(); err != nil {
			_ = err
		}
	}()

	if err := st.SetSetting("capacityAdapter", `{"vision":{"enabled":true,"roundRobin":true,"models":["m1","m2"]}}`); err != nil {
		_ = err
	}

	cfg := LoadCapacityAdapterConfig(st)
	if !cfg.Vision.Enabled || !cfg.Vision.RoundRobin || len(cfg.Vision.Models) != 2 {
		t.Fatalf("failed loading settings: %+v", cfg)
	}
}

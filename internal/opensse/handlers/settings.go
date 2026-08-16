package handlers

import (
	"encoding/json"
	"flamerouter/internal/opensse/rtk"
	"flamerouter/internal/store"
	"strconv"
)

// LoadTokenSaverFromStore reads settings table for token-saver flags.
// Fail-open: defaults if missing.
func LoadTokenSaverFromStore(st *store.Store) rtk.TokenSaverOptions {
	opts := rtk.DefaultTokenSaver()
	if st == nil {
		return opts
	}
	// settings key/value JSON blob if present
	raw, err := st.GetSetting("token_saver")
	if err != nil || raw == "" {
		return opts
	}

	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return opts
	}

	if v, ok := m["rtkEnabled"].(bool); ok {
		opts.RTK = v
	}

	if v, ok := m["headroomEnabled"].(bool); ok {
		opts.Headroom = v
	}

	if v, ok := m["headroomUrl"].(string); ok && v != "" {
		opts.HeadroomURL = v
	}

	if v, ok := m["headroomCompressUserMessages"].(bool); ok {
		opts.HeadroomCompressUserMessages = v
	}

	if v, ok := m["cavemanEnabled"].(bool); ok {
		opts.Caveman = v
	}

	if v, ok := m["cavemanLevel"].(string); ok && v != "" {
		opts.CavemanLevel = v
	}

	if v, ok := m["ponytailEnabled"].(bool); ok {
		opts.Ponytail = v
	}

	if v, ok := m["ponytailLevel"].(string); ok && v != "" {
		opts.PonytailLevel = v
	}

	if v, ok := m["pxpipeEnabled"].(bool); ok {
		opts.Pxpipe = v
	}

	if v, ok := m["pxpipeMinChars"].(float64); ok && v > 0 {
		opts.PxpipeMinChars = int(v)
	}

	return opts
}

// LoadAccountStrategyFromStore reads fallbackStrategy + stickyRoundRobinLimit
// (parity 9router settingsRepo / auth.js getProviderCredentials).
// Default strategy: fill-first. Sticky default: 3.
func LoadAccountStrategyFromStore(st *store.Store) (strategy string, sticky int) {
	strategy = "fill-first"
	sticky = 3

	if st == nil {
		return
	}

	if v, err := st.GetSetting("fallbackStrategy"); err == nil && v != "" {
		strategy = v
	}

	if v, err := st.GetSetting("stickyRoundRobinLimit"); err == nil && v != "" {
		if n, e := strconv.Atoi(v); e == nil && n > 0 {
			sticky = n
		}
	}

	return
}

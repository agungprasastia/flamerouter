package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// pricing stored in kv scope "pricing", key=provider, value=JSON models map

func (s *Server) handleGetPricing(w http.ResponseWriter, _ *http.Request) {
	raw, err := s.st.KVList("pricing")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	out := map[string]any{}

	for provider, val := range raw {
		var models map[string]any
		if err := json.Unmarshal([]byte(val), &models); err != nil {
			out[provider] = val
			continue
		}

		out[provider] = models
	}

	writeJSONOK(w, out)
}

func validatePricingMap(provider string, models map[string]map[string]float64) error {
	for model, pricing := range models {
		for k, v := range pricing {
			if v < 0 {
				return fmt.Errorf("invalid pricing value for %s in %s/%s", k, provider, model)
			}
		}
	}

	return nil
}

func (s *Server) updateProviderPricing(provider string, models map[string]map[string]float64) error {
	if err := validatePricingMap(provider, models); err != nil {
		return err
	}

	existing := map[string]map[string]float64{}
	if raw, err := s.st.KVGet("pricing", provider); err == nil && raw != "" {
		if err := json.Unmarshal([]byte(raw), &existing); err != nil {
			existing = map[string]map[string]float64{}
		}
	}

	if existing == nil {
		existing = map[string]map[string]float64{}
	}

	for model, pricing := range models {
		existing[model] = pricing
	}

	b, err := json.Marshal(existing)
	if err != nil {
		return err
	}

	return s.st.KVSet("pricing", provider, string(b))
}

func (s *Server) handleSetPricing(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	var data map[string]map[string]map[string]float64
	if err := json.Unmarshal(body, &data); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid pricing data format")
		return
	}

	for provider, models := range data {
		if err := s.updateProviderPricing(provider, models); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	s.handleGetPricing(w, r)
}

func (s *Server) deleteSingleModelPricing(provider, model string) error {
	raw, err := s.st.KVGet("pricing", provider)
	if err != nil {
		return err
	}

	if raw == "" {
		return nil
	}

	existing := map[string]map[string]float64{}
	if uErr := json.Unmarshal([]byte(raw), &existing); uErr != nil {
		return uErr
	}

	delete(existing, model)

	if len(existing) == 0 {
		return s.st.KVDelete("pricing", provider)
	}

	b, mErr := json.Marshal(existing)
	if mErr != nil {
		return mErr
	}

	return s.st.KVSet("pricing", provider, string(b))
}

func (s *Server) handleDeletePricing(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")

	switch {
	case provider != "" && model != "":
		if err := s.deleteSingleModelPricing(provider, model); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}
	case provider != "":
		if err := s.st.KVDelete("pricing", provider); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}
	default:
		all, err := s.st.KVList("pricing")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		for k := range all {
			if err := s.st.KVDelete("pricing", k); err != nil {
				_ = err
			}
		}
	}

	s.handleGetPricing(w, r)
}

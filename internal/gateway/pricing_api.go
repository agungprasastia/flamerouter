package gateway

import (
	"encoding/json"
	"io"
	"net/http"
)

// pricing stored in kv scope "pricing", key=provider, value=JSON models map

func (s *Server) handleGetPricing(w http.ResponseWriter, r *http.Request) {
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
		existing := map[string]map[string]float64{}
		if raw, err := s.st.KVGet("pricing", provider); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &existing)
		}

		if existing == nil {
			existing = map[string]map[string]float64{}
		}

		for model, pricing := range models {
			for k, v := range pricing {
				if v < 0 {
					writeErr(w, http.StatusBadRequest, "Invalid pricing value for "+k+" in "+provider+"/"+model)
					return
				}
			}

			existing[model] = pricing
		}

		b, _ := json.Marshal(existing)
		if err := s.st.KVSet("pricing", provider, string(b)); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}
	}

	s.handleGetPricing(w, r)
}

func (s *Server) handleDeletePricing(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	model := r.URL.Query().Get("model")

	if provider != "" && model != "" {
		raw, err := s.st.KVGet("pricing", provider)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		if raw != "" {
			existing := map[string]map[string]float64{}
			_ = json.Unmarshal([]byte(raw), &existing)
			delete(existing, model)

			if len(existing) == 0 {
				_ = s.st.KVDelete("pricing", provider)
			} else {
				b, _ := json.Marshal(existing)
				_ = s.st.KVSet("pricing", provider, string(b))
			}
		}
	} else if provider != "" {
		_ = s.st.KVDelete("pricing", provider)
	} else {
		all, err := s.st.KVList("pricing")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		for k := range all {
			_ = s.st.KVDelete("pricing", k)
		}
	}

	s.handleGetPricing(w, r)
}

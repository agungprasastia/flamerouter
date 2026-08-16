package gateway

import (
	"context"
	"encoding/json"
	"flamerouter/internal/netutil"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Static TTS catalogs (used when no API key / offline).
var staticEdgeVoices = []map[string]any{
	{"id": "en-US-JennyNeural", "name": "Jenny (en-US)", "locale": "en-US", "lang": "en", "country": "US", "gender": "Female", "provider": "edge-tts"},
	{"id": "en-US-GuyNeural", "name": "Guy (en-US)", "locale": "en-US", "lang": "en", "country": "US", "gender": "Male", "provider": "edge-tts"},
	{"id": "en-GB-SoniaNeural", "name": "Sonia (en-GB)", "locale": "en-GB", "lang": "en", "country": "GB", "gender": "Female", "provider": "edge-tts"},
	{"id": "vi-VN-HoaiMyNeural", "name": "HoaiMy (vi-VN)", "locale": "vi-VN", "lang": "vi", "country": "VN", "gender": "Female", "provider": "edge-tts"},
	{"id": "vi-VN-NamMinhNeural", "name": "NamMinh (vi-VN)", "locale": "vi-VN", "lang": "vi", "country": "VN", "gender": "Male", "provider": "edge-tts"},
	{"id": "ja-JP-NanamiNeural", "name": "Nanami (ja-JP)", "locale": "ja-JP", "lang": "ja", "country": "JP", "gender": "Female", "provider": "edge-tts"},
	{"id": "zh-CN-XiaoxiaoNeural", "name": "Xiaoxiao (zh-CN)", "locale": "zh-CN", "lang": "zh", "country": "CN", "gender": "Female", "provider": "edge-tts"},
}

var staticElevenVoices = []map[string]any{
	{"id": "21m00Tcm4TlvDq8ikWAM", "name": "Rachel", "lang": "en", "gender": "female", "provider": "elevenlabs"},
	{"id": "AZnzlk1XvdvUeBnXmlld", "name": "Domi", "lang": "en", "gender": "female", "provider": "elevenlabs"},
	{"id": "EXAVITQu4vr4xnSDxMaL", "name": "Bella", "lang": "en", "gender": "female", "provider": "elevenlabs"},
	{"id": "ErXwobaYiN019PkySvjV", "name": "Antoni", "lang": "en", "gender": "male", "provider": "elevenlabs"},
}

var staticOpenAIVoices = []map[string]any{
	{"id": "alloy", "name": "Alloy", "lang": "en", "provider": "openai"},
	{"id": "echo", "name": "Echo", "lang": "en", "provider": "openai"},
	{"id": "fable", "name": "Fable", "lang": "en", "provider": "openai"},
	{"id": "onyx", "name": "Onyx", "lang": "en", "provider": "openai"},
	{"id": "nova", "name": "Nova", "lang": "en", "provider": "openai"},
	{"id": "shimmer", "name": "Shimmer", "lang": "en", "provider": "openai"},
}

func groupVoicesByLang(voices []map[string]any) (languages []map[string]any, byLang map[string]any) {
	byLang = map[string]any{}

	for _, v := range voices {
		code, _ := v["lang"].(string)
		if code == "" {
			code = "en"
		}

		name := code
		if n, ok := v["langName"].(string); ok && n != "" {
			name = n
		}

		entry, ok := byLang[code].(map[string]any)
		if !ok {
			entry = map[string]any{"code": code, "name": name, "voices": []any{}}
			byLang[code] = entry
		}

		vs := entry["voices"].([]any)
		entry["voices"] = append(vs, v)
	}

	for _, v := range byLang {
		languages = append(languages, v.(map[string]any))
	}

	sort.Slice(languages, func(i, j int) bool {
		a, _ := languages[i]["name"].(string)
		b, _ := languages[j]["name"].(string)

		return a < b
	})

	return languages, byLang
}

func filterVoicesLang(voices []map[string]any, lang string) []map[string]any {
	if lang == "" {
		return voices
	}

	out := make([]map[string]any, 0)

	for _, v := range voices {
		if v["lang"] == lang {
			out = append(out, v)
		}
	}

	return out
}

func (s *Server) activeAPIKey(provider string) string {
	conns, err := s.st.ListActiveByProvider(provider)
	if err != nil || len(conns) == 0 {
		return ""
	}

	if conns[0].APIKey != "" {
		return conns[0].APIKey
	}

	return conns[0].AccessToken
}

// GET /api/media-providers/tts/voices?provider=&lang=&apiKey=.
func (s *Server) handleMediaTTSVoices(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "edge-tts"
	}

	lang := r.URL.Query().Get("lang")
	apiKey := r.URL.Query().Get("apiKey")

	var voices []map[string]any

	switch provider {
	case "elevenlabs":
		if apiKey == "" {
			apiKey = s.activeAPIKey("elevenlabs")
		}

		if apiKey != "" {
			if live, err := fetchElevenLabsVoices(apiKey); err == nil && len(live) > 0 {
				voices = live
			}
		}

		if voices == nil {
			voices = append([]map[string]any{}, staticElevenVoices...)
		}
	case "openai", "local-device":
		voices = append([]map[string]any{}, staticOpenAIVoices...)
	default: // edge-tts
		if live, err := fetchEdgeTTSVoices(); err == nil && len(live) > 0 {
			voices = live
		} else {
			voices = append([]map[string]any{}, staticEdgeVoices...)
		}
	}

	voices = filterVoicesLang(voices, lang)
	languages, byLang := groupVoicesByLang(voices)
	writeJSONOK(w, map[string]any{"voices": voices, "languages": languages, "byLang": byLang})
}

// GET /api/media-providers/tts/elevenlabs/voices.
func (s *Server) handleMediaElevenLabsVoices(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	apiKey := s.activeAPIKey("elevenlabs")

	var voices []map[string]any

	if apiKey != "" {
		if live, err := fetchElevenLabsVoices(apiKey); err == nil {
			voices = live
		}
	}

	if voices == nil {
		voices = append([]map[string]any{}, staticElevenVoices...)
	}

	languages, byLang := groupVoicesByLang(voices)

	if lang != "" {
		writeJSONOK(w, map[string]any{"voices": filterVoicesLang(voices, lang)})
		return
	}

	writeJSONOK(w, map[string]any{"languages": languages, "byLang": byLang})
}

// GET /api/media-providers/tts/minimax/voices.
func (s *Server) handleMediaMinimaxVoices(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider != "minimax-cn" {
		provider = "minimax"
	}

	lang := r.URL.Query().Get("lang")
	apiKey := s.activeAPIKey(provider)

	endpoint := "https://api.minimax.io/v1/get_voice"
	if provider == "minimax-cn" {
		endpoint = "https://api.minimaxi.com/v1/get_voice"
	}

	voices := []map[string]any{
		{"id": "English_expressive_narrator", "name": "Expressive Narrator", "lang": "English", "category": "system_voice", "provider": provider},
		{"id": "English_calm_woman", "name": "Calm Woman", "lang": "English", "category": "system_voice", "provider": provider},
	}

	if apiKey != "" {
		if live, err := fetchMinimaxVoices(endpoint, apiKey, r.URL.Query().Get("voice_type")); err == nil && len(live) > 0 {
			voices = live
		}
	}

	languages, byLang := groupVoicesByLang(voices)

	if lang != "" {
		writeJSONOK(w, map[string]any{"voices": filterVoicesLang(voices, lang)})
		return
	}

	writeJSONOK(w, map[string]any{"languages": languages, "byLang": byLang})
}

// GET /api/media-providers/tts/deepgram/voices.
func (s *Server) handleMediaDeepgramVoices(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	apiKey := s.activeAPIKey("deepgram")
	voices := []map[string]any{
		{"id": "aura-2-thalia-en", "name": "Thalia", "lang": "en", "gender": "feminine", "provider": "deepgram"},
		{"id": "aura-2-orion-en", "name": "Orion", "lang": "en", "gender": "masculine", "provider": "deepgram"},
		{"id": "aura-2-luna-en", "name": "Luna", "lang": "en", "gender": "feminine", "provider": "deepgram"},
	}

	if apiKey != "" {
		if live, err := fetchDeepgramVoices(apiKey); err == nil && len(live) > 0 {
			voices = live
		}
	}

	languages, byLang := groupVoicesByLang(voices)

	if lang != "" {
		writeJSONOK(w, map[string]any{"voices": filterVoicesLang(voices, lang)})
		return
	}

	writeJSONOK(w, map[string]any{"languages": languages, "byLang": byLang})
}

// GET /api/media-providers/tts/inworld/voices.
func (s *Server) handleMediaInworldVoices(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	apiKey := s.activeAPIKey("inworld")
	voices := []map[string]any{
		{"id": "Ashley", "name": "Ashley", "lang": "en", "gender": "FEMALE", "provider": "inworld"},
		{"id": "Craig", "name": "Craig", "lang": "en", "gender": "MALE", "provider": "inworld"},
	}

	if apiKey != "" {
		if live, err := fetchInworldVoices(apiKey); err == nil && len(live) > 0 {
			voices = live
		}
	}

	languages, byLang := groupVoicesByLang(voices)

	if lang != "" {
		writeJSONOK(w, map[string]any{"voices": filterVoicesLang(voices, lang)})
		return
	}

	writeJSONOK(w, map[string]any{"languages": languages, "byLang": byLang})
}

func httpGetJSON(url, authHeader, authVal string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if authHeader != "" {
		req.Header.Set(authHeader, authVal)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := netutil.DoHTTP(client, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, errHTTPStatus(resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type httpStatusError int

func (e httpStatusError) Error() string { return "http status" }
func errHTTPStatus(code int) error      { return httpStatusError(code) }

func fetchEdgeTTSVoices() ([]map[string]any, error) {
	body, err := httpGetJSON(
		"https://speech.platform.bing.com/consumer/speech/synthesize/readaloud/voices/list?trustedclienttoken=6A5AA1D4EAFF4E9FB37E23D68491D6F4",
		"", "",
	)
	if err != nil {
		return nil, err
	}

	var raw []map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(raw))

	for _, v := range raw {
		locale, _ := v["Locale"].(string)
		short, _ := v["ShortName"].(string)
		friendly, _ := v["FriendlyName"].(string)
		gender, _ := v["Gender"].(string)

		if short == "" {
			continue
		}

		parts := strings.SplitN(locale, "-", 2)
		lang := locale
		country := ""

		if len(parts) > 0 {
			lang = parts[0]
		}

		if len(parts) > 1 {
			country = parts[1]
		}

		name := friendly
		if name == "" {
			name = short
		}

		name = strings.ReplaceAll(name, "Microsoft ", "")
		name = strings.ReplaceAll(name, " Online (Natural) - ", " (")
		out = append(out, map[string]any{
			"id": short, "name": name, "locale": locale, "lang": lang, "country": country,
			"gender": gender, "provider": "edge-tts",
		})
	}

	return out, nil
}

func fetchElevenLabsVoices(apiKey string) ([]map[string]any, error) {
	body, err := httpGetJSON("https://api.elevenlabs.io/v1/voices", "xi-api-key", apiKey)
	if err != nil {
		return nil, err
	}

	var data struct {
		Voices []map[string]any `json:"voices"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(data.Voices))

	for _, v := range data.Voices {
		id, _ := v["voice_id"].(string)
		name, _ := v["name"].(string)
		labels, _ := v["labels"].(map[string]any)
		lang := "en"
		gender := ""

		if labels != nil {
			if l, ok := labels["language"].(string); ok && l != "" {
				lang = strings.Split(l, "-")[0]
			}

			if g, ok := labels["gender"].(string); ok {
				gender = g
			}
		}

		out = append(out, map[string]any{
			"id": id, "name": name, "lang": lang, "gender": gender, "provider": "elevenlabs",
		})
	}

	return out, nil
}

func fetchMinimaxVoices(endpoint, apiKey, voiceType string) ([]map[string]any, error) {
	if voiceType == "" {
		voiceType = "all"
	}

	payload, _ := json.Marshal(map[string]string{"voice_type": voiceType})
	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := netutil.DoHTTP(client, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, errHTTPStatus(resp.StatusCode)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	groups := []struct{ key, label string }{
		{"system_voice", "System"},
		{"voice_cloning", "Cloned"},
		{"voice_generation", "Generated"},
		{"music_generation", "Music"},
	}

	var out []map[string]any

	for _, g := range groups {
		arr, _ := data[g.key].([]any)
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}

			id, _ := m["voice_id"].(string)
			if id == "" {
				id, _ = m["voiceId"].(string)
			}

			if id == "" {
				continue
			}

			name, _ := m["voice_name"].(string)
			if name == "" {
				name, _ = m["voiceName"].(string)
			}

			if name == "" {
				name = id
			}

			lang := "Custom"

			if g.key == "system_voice" {
				if i := strings.Index(id, "_"); i > 0 {
					lang = id[:i]
				}
			} else {
				name = name + " · " + g.label
			}

			out = append(out, map[string]any{"id": id, "name": name, "lang": lang, "category": g.key, "provider": "minimax"})
		}
	}

	return out, nil
}

func fetchDeepgramVoices(apiKey string) ([]map[string]any, error) {
	body, err := httpGetJSON("https://api.deepgram.com/v1/models", "Authorization", "Token "+apiKey)
	if err != nil {
		return nil, err
	}

	var data struct {
		TTS []map[string]any `json:"tts"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var out []map[string]any

	for _, m := range data.TTS {
		id, _ := m["canonical_name"].(string)
		if id == "" {
			id, _ = m["name"].(string)
		}

		name, _ := m["name"].(string)
		if name == "" {
			name = id
		}

		lang := "en"

		if langs, ok := m["languages"].([]any); ok && len(langs) > 0 {
			if s, ok := langs[0].(string); ok {
				lang = s
			}
		}

		out = append(out, map[string]any{"id": id, "name": name, "lang": lang, "provider": "deepgram"})
	}

	return out, nil
}

func fetchInworldVoices(apiKey string) ([]map[string]any, error) {
	body, err := httpGetJSON("https://api.inworld.ai/tts/v1/voices", "Authorization", "Basic "+apiKey)
	if err != nil {
		return nil, err
	}

	var data struct {
		Voices []map[string]any `json:"voices"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var out []map[string]any

	for _, v := range data.Voices {
		id, _ := v["voiceId"].(string)
		name, _ := v["displayName"].(string)

		if name == "" {
			name = id
		}

		gender, _ := v["gender"].(string)
		lang := "en"

		if langs, ok := v["languages"].([]any); ok && len(langs) > 0 {
			if s, ok := langs[0].(string); ok {
				lang = s
			}
		}

		out = append(out, map[string]any{"id": id, "name": name, "lang": lang, "gender": gender, "provider": "inworld"})
	}

	return out, nil
}

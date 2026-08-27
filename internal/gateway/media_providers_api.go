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
		code, name := extractVoiceLangAndName(v)

		entry, okEntry := byLang[code].(map[string]any)
		if !okEntry {
			entry = map[string]any{"code": code, "name": name, "voices": []any{}}
			byLang[code] = entry
		}

		vs, okVs := entry["voices"].([]any)
		if okVs {
			entry["voices"] = append(vs, v)
		}
	}

	languages = sortVoiceLanguages(byLang)

	return languages, byLang
}

func extractVoiceLangAndName(v map[string]any) (string, string) {
	code, okCode := v["lang"].(string)
	if !okCode || code == "" {
		code = "en"
	}

	name := code
	if n, okN := v["langName"].(string); okN && n != "" {
		name = n
	}

	return code, name
}

func sortVoiceLanguages(byLang map[string]any) []map[string]any {
	var languages []map[string]any

	for _, v := range byLang {
		m, okM := v.(map[string]any)
		if okM {
			languages = append(languages, m)
		}
	}

	sort.Slice(languages, func(i, j int) bool {
		a, okA := languages[i]["name"].(string)
		if !okA {
			return false
		}

		b, okB := languages[j]["name"].(string)
		if !okB {
			return false
		}

		return a < b
	})

	return languages
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

	voices := s.resolveTTSVoices(provider, apiKey)

	voices = filterVoicesLang(voices, lang)
	languages, byLang := groupVoicesByLang(voices)
	writeJSONOK(w, map[string]any{"voices": voices, "languages": languages, "byLang": byLang})
}

func (s *Server) resolveTTSVoices(provider, apiKey string) []map[string]any {
	switch provider {
	case "elevenlabs":
		if apiKey == "" {
			apiKey = s.activeAPIKey("elevenlabs")
		}

		if apiKey != "" {
			if live, err := fetchElevenLabsVoices(apiKey); err == nil && len(live) > 0 {
				return live
			}
		}

		return append([]map[string]any{}, staticElevenVoices...)
	case "openai", "local-device":
		return append([]map[string]any{}, staticOpenAIVoices...)
	default: // edge-tts
		if live, err := fetchEdgeTTSVoices(); err == nil && len(live) > 0 {
			return live
		}

		return append([]map[string]any{}, staticEdgeVoices...)
	}
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
	client := &http.Client{Timeout: 15 * time.Second, Transport: nil, CheckRedirect: nil, Jar: nil}

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

	defer func() {
		if resp != nil && resp.Body != nil {
			if err := resp.Body.Close(); err != nil {
				_ = err
			}
		}
	}()

	if resp.StatusCode >= 400 {
		return nil, errHTTPStatus(resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

type httpStatusError int

// Error implements the error interface for httpStatusError.
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
		entry, ok := parseEdgeTTSVoiceItem(v)
		if ok {
			out = append(out, entry)
		}
	}

	return out, nil
}

func parseEdgeTTSVoiceItem(v map[string]any) (map[string]any, bool) {
	short, okShort := v["ShortName"].(string)
	if !okShort || short == "" {
		return nil, false
	}

	locale, okLocale := v["Locale"].(string)
	if !okLocale {
		return nil, false
	}

	friendly, okFriendly := v["FriendlyName"].(string)
	if !okFriendly {
		friendly = ""
	}

	gender, okGender := v["Gender"].(string)
	if !okGender {
		gender = ""
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

	return map[string]any{
		"id": short, "name": name, "locale": locale, "lang": lang, "country": country,
		"gender": gender, "provider": "edge-tts",
	}, true
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
		entry, ok := parseElevenLabsVoiceItem(v)
		if ok {
			out = append(out, entry)
		}
	}

	return out, nil
}

func parseElevenLabsVoiceItem(v map[string]any) (map[string]any, bool) {
	id, okID := v["voice_id"].(string)
	if !okID {
		return nil, false
	}

	name, okName := v["name"].(string)
	if !okName {
		return nil, false
	}

	labels, okLabels := v["labels"].(map[string]any)
	lang := "en"
	gender := ""

	if okLabels && labels != nil {
		if l, ok := labels["language"].(string); ok && l != "" {
			lang = strings.Split(l, "-")[0]
		}

		if g, ok := labels["gender"].(string); ok {
			gender = g
		}
	}

	return map[string]any{
		"id": id, "name": name, "lang": lang, "gender": gender, "provider": "elevenlabs",
	}, true
}

func fetchMinimaxVoices(endpoint, apiKey, voiceType string) ([]map[string]any, error) {
	body, err := executeMinimaxVoicesRequest(endpoint, apiKey, voiceType)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	return parseMinimaxVoiceGroups(data), nil
}

func executeMinimaxVoicesRequest(endpoint, apiKey, voiceType string) ([]byte, error) {
	if voiceType == "" {
		voiceType = "all"
	}

	payload, err := json.Marshal(map[string]string{"voice_type": voiceType})
	if err != nil {
		return nil, err
	}

	req, errReq := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if errReq != nil {
		return nil, errReq
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second, Transport: nil, CheckRedirect: nil, Jar: nil}

	resp, errDo := netutil.DoHTTP(client, req)
	if errDo != nil {
		return nil, errDo
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			if err := resp.Body.Close(); err != nil {
				_ = err
			}
		}
	}()

	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, errRead
	}

	if resp.StatusCode >= 400 {
		return nil, errHTTPStatus(resp.StatusCode)
	}

	return body, nil
}

func resolveMinimaxField(m map[string]any, primaryKey, fallbackKey string) string {
	if val, ok := m[primaryKey].(string); ok && val != "" {
		return val
	}

	if val, ok := m[fallbackKey].(string); ok {
		return val
	}

	return ""
}

func parseMinimaxVoiceItem(item any, gKey, gLabel string) (map[string]any, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return nil, false
	}

	id := resolveMinimaxField(m, "voice_id", "voiceId")
	if id == "" {
		return nil, false
	}

	name := resolveMinimaxField(m, "voice_name", "voiceName")
	if name == "" {
		name = id
	}

	lang := "Custom"

	if gKey == "system_voice" {
		if i := strings.Index(id, "_"); i > 0 {
			lang = id[:i]
		}
	} else {
		name = name + " · " + gLabel
	}

	return map[string]any{"id": id, "name": name, "lang": lang, "category": gKey, "provider": "minimax"}, true
}

func parseMinimaxVoiceGroups(data map[string]any) []map[string]any {
	groups := []struct{ key, label string }{
		{"system_voice", "System"},
		{"voice_cloning", "Cloned"},
		{"voice_generation", "Generated"},
		{"music_generation", "Music"},
	}

	var out []map[string]any

	for _, g := range groups {
		arr, okArr := data[g.key].([]any)
		if !okArr {
			continue
		}

		for _, item := range arr {
			if v, ok := parseMinimaxVoiceItem(item, g.key, g.label); ok {
				out = append(out, v)
			}
		}
	}

	return out
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

	out := make([]map[string]any, 0, len(data.TTS))

	for _, m := range data.TTS {
		entry := parseDeepgramVoiceItem(m)
		out = append(out, entry)
	}

	return out, nil
}

func parseDeepgramVoiceItem(m map[string]any) map[string]any {
	id, okID := m["canonical_name"].(string)
	if !okID || id == "" {
		if vName, okVName := m["name"].(string); okVName {
			id = vName
		}
	}

	name, okName := m["name"].(string)
	if !okName || name == "" {
		name = id
	}

	lang := "en"

	if langs, ok := m["languages"].([]any); ok && len(langs) > 0 {
		if s, okStr := langs[0].(string); okStr {
			lang = s
		}
	}

	return map[string]any{"id": id, "name": name, "lang": lang, "provider": "deepgram"}
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

	out := make([]map[string]any, 0, len(data.Voices))

	for _, v := range data.Voices {
		entry, ok := parseInworldVoiceItem(v)
		if ok {
			out = append(out, entry)
		}
	}

	return out, nil
}

func parseInworldVoiceItem(v map[string]any) (map[string]any, bool) {
	id, okID := v["voiceId"].(string)
	if !okID {
		return nil, false
	}

	name, okName := v["displayName"].(string)
	if !okName || name == "" {
		name = id
	}

	gender, okGender := v["gender"].(string)
	if !okGender {
		gender = ""
	}

	lang := "en"

	if langs, ok := v["languages"].([]any); ok && len(langs) > 0 {
		if s, okStr := langs[0].(string); okStr {
			lang = s
		}
	}

	return map[string]any{"id": id, "name": name, "lang": lang, "gender": gender, "provider": "inworld"}, true
}

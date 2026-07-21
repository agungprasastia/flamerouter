package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/oauth"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/handlers"
	"flamerouter/internal/store"
	"flamerouter/internal/tokenrefresh"
	"flamerouter/internal/usage"
)

type Server struct {
	cfg      *config.Config
	st       *store.Store
	keys     *auth.APIKeys
	jwt      *auth.JWTManager
	session  *auth.SessionHandler
	oidc     *auth.OIDCHandler
	exec     executor.Executor
	fb       *fallback.Fallback
	oauth    *oauth.Handler
	refresh  *tokenrefresh.RefreshManager
	usageHub *usage.StreamHub
	tracker  *usage.Tracker
	mux      *http.ServeMux
}

func New(cfg *config.Config, st *store.Store, keys *auth.APIKeys, exec executor.Executor) http.Handler {
	fb := fallback.New(st)
	jwt := auth.NewJWTManager(cfg.JWTSecret)
	hub := usage.NewStreamHub()
	s := &Server{
		cfg:      cfg,
		st:       st,
		keys:     keys,
		jwt:      jwt,
		session:  auth.NewSessionHandler(jwt, st, cfg.InitialPassword),
		oidc:     auth.NewOIDCHandler(jwt, st),
		exec:     exec,
		fb:       fb,
		oauth:    oauth.NewHandler(),
		refresh:  tokenrefresh.NewRefreshManager(),
		usageHub: hub,
		tracker:  usage.NewTracker(st, hub),
		mux:      http.NewServeMux(),
	}
	s.routes()
	return auth.DashboardGuard(jwt, st, s)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/auth/login", s.session.Login)
	s.mux.HandleFunc("/api/auth/logout", s.session.Logout)
	s.mux.HandleFunc("/api/auth/status", s.session.Status)
	s.mux.HandleFunc("/api/auth/reset-password", s.session.ResetPassword)
	s.mux.HandleFunc("/api/auth/oidc/start", s.oidc.Start)
	s.mux.HandleFunc("/api/auth/oidc/callback", s.oidc.Callback)
	s.mux.HandleFunc("/api/auth/oidc/test", s.oidc.Test)

	// Settings
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("PATCH /api/settings", s.handlePatchSettings)
	s.mux.HandleFunc("GET /api/settings/require-login", s.handleRequireLogin)
	s.mux.HandleFunc("PATCH /api/settings/require-login", s.handleRequireLogin)
	s.mux.HandleFunc("POST /api/settings/proxy-test", s.handleProxyTest)
	s.mux.HandleFunc("/api/settings/database", s.handleDatabase)

	// Keys / combos / aliases (existing + extended)
	s.mux.HandleFunc("/api/keys", s.handleKeys)
	s.mux.HandleFunc("PUT /api/keys/{id}", s.handleUpdateKey)
	s.mux.HandleFunc("DELETE /api/keys/{id}", s.handleDeleteKey)
	s.mux.HandleFunc("/api/combos", s.handleCombos)
	s.mux.HandleFunc("PUT /api/combos/{id}", s.handleUpdateCombo)
	s.mux.HandleFunc("DELETE /api/combos/{id}", s.handleDeleteCombo)
	s.mux.HandleFunc("/api/aliases", s.handleAliases)

	// Providers — static paths BEFORE {id}
	s.mux.HandleFunc("GET /api/providers", s.handleListProviders)
	s.mux.HandleFunc("POST /api/providers/connections", s.handleConnections)
	s.mux.HandleFunc("POST /api/providers/validate", s.handleProviderValidate)
	s.mux.HandleFunc("GET /api/providers/client", s.handleProvidersClient)
	s.mux.HandleFunc("GET /api/providers/suggested-models", s.handleSuggestedModels)
	s.mux.HandleFunc("POST /api/providers/test-batch", s.handleProviderTestBatch)
	s.mux.HandleFunc("GET /api/providers/kilo/free-models", s.handleKiloFreeModels)
	s.mux.HandleFunc("GET /api/providers/{id}/models", s.handleProviderModels)
	s.mux.HandleFunc("POST /api/providers/{id}/test", s.handleProviderTest)
	s.mux.HandleFunc("POST /api/providers/{id}/test-models", s.handleProviderTestModels)
	s.mux.HandleFunc("GET /api/providers/{id}", s.handleGetProvider)
	s.mux.HandleFunc("POST /api/providers/{id}", s.handleCreateProviderConnection)

	// Media providers TTS voices
	s.mux.HandleFunc("GET /api/media-providers/tts/voices", s.handleMediaTTSVoices)
	s.mux.HandleFunc("GET /api/media-providers/tts/elevenlabs/voices", s.handleMediaElevenLabsVoices)
	s.mux.HandleFunc("GET /api/media-providers/tts/minimax/voices", s.handleMediaMinimaxVoices)
	s.mux.HandleFunc("GET /api/media-providers/tts/deepgram/voices", s.handleMediaDeepgramVoices)
	s.mux.HandleFunc("GET /api/media-providers/tts/inworld/voices", s.handleMediaInworldVoices)

	// Provider nodes
	s.mux.HandleFunc("GET /api/provider-nodes", s.handleListProviderNodes)
	s.mux.HandleFunc("POST /api/provider-nodes", s.handleCreateProviderNode)
	s.mux.HandleFunc("PUT /api/provider-nodes/{id}", s.handleUpdateProviderNode)
	s.mux.HandleFunc("DELETE /api/provider-nodes/{id}", s.handleDeleteProviderNode)
	s.mux.HandleFunc("POST /api/provider-nodes/validate", s.handleValidateProviderNode)

	// Models management (more specific paths first)
	s.mux.HandleFunc("GET /api/models/custom", s.handleListCustomModels)
	s.mux.HandleFunc("POST /api/models/custom", s.handleCreateCustomModel)
	s.mux.HandleFunc("GET /api/models/disabled", s.handleListDisabledModels)
	s.mux.HandleFunc("PATCH /api/models/disabled", s.handleToggleDisabledModel)
	s.mux.HandleFunc("GET /api/models/alias", s.handleListAliases)
	s.mux.HandleFunc("GET /api/models/availability", s.handleModelAvailability)
	s.mux.HandleFunc("POST /api/models/test", s.handleTestModel)
	s.mux.HandleFunc("GET /api/models", s.handleAllModels)

	// Pricing + proxy pools
	s.mux.HandleFunc("GET /api/pricing", s.handleGetPricing)
	s.mux.HandleFunc("POST /api/pricing", s.handleSetPricing)
	s.mux.HandleFunc("PATCH /api/pricing", s.handleSetPricing)
	s.mux.HandleFunc("DELETE /api/pricing", s.handleDeletePricing)
	s.mux.HandleFunc("GET /api/proxy-pools", s.handleListProxyPools)
	s.mux.HandleFunc("POST /api/proxy-pools", s.handleCreateProxyPool)
	// deploy endpoints BEFORE {id}
	s.mux.HandleFunc("POST /api/proxy-pools/cloudflare-deploy", s.handleCloudflareDeploy)
	s.mux.HandleFunc("POST /api/proxy-pools/deno-deploy", s.handleDenoDeploy)
	s.mux.HandleFunc("POST /api/proxy-pools/vercel-deploy", s.handleVercelDeploy)
	s.mux.HandleFunc("POST /api/proxy-pools/{id}/test", s.handleTestProxyPool)
	s.mux.HandleFunc("PUT /api/proxy-pools/{id}", s.handleUpdateProxyPool)
	s.mux.HandleFunc("DELETE /api/proxy-pools/{id}", s.handleDeleteProxyPool)

	// Translator playground + console logs (static before nothing dynamic)
	s.mux.HandleFunc("GET /api/translator/load", s.handleTranslatorLoad)
	s.mux.HandleFunc("POST /api/translator/save", s.handleTranslatorSave)
	s.mux.HandleFunc("GET /api/translator/console-logs/stream", s.handleTranslatorConsoleLogsStream)
	s.mux.HandleFunc("POST /api/translator/translate", s.handleTranslatorTranslate)
	s.mux.HandleFunc("POST /api/translator/send", s.handleTranslatorSend)
	s.mux.HandleFunc("GET /api/translator/console-logs", s.handleTranslatorConsoleLogs)
	s.mux.HandleFunc("DELETE /api/translator/console-logs", s.handleTranslatorConsoleLogs)

	// Tags, locale, init
	s.mux.HandleFunc("GET /api/tags", s.handleTags)
	s.mux.HandleFunc("GET /api/locale", s.handleLocale)
	s.mux.HandleFunc("POST /api/locale", s.handleLocale)
	s.mux.HandleFunc("/api/init", s.handleInit)

	// CLI tools (static path before {toolSettings})
	s.mux.HandleFunc("GET /api/cli-tools/all-statuses", s.handleAllCLIStatuses)
	s.mux.HandleFunc("GET /api/cli-tools/antigravity-mitm", s.handleAntigravityMitm)
	s.mux.HandleFunc("PATCH /api/cli-tools/antigravity-mitm", s.handleAntigravityMitm)
	s.mux.HandleFunc("GET /api/cli-tools/antigravity-mitm/alias", s.handleAntigravityMitmAlias)
	s.mux.HandleFunc("PATCH /api/cli-tools/antigravity-mitm/alias", s.handleAntigravityMitmAlias)
	s.mux.HandleFunc("PUT /api/cli-tools/antigravity-mitm/alias", s.handleAntigravityMitmAlias)
	s.mux.HandleFunc("GET /api/cli-tools/cowork-mcp-tools", s.handleCoworkMCPTools)
	s.mux.HandleFunc("PATCH /api/cli-tools/cowork-mcp-tools", s.handleCoworkMCPTools)
	s.mux.HandleFunc("POST /api/cli-tools/cowork-mcp-tools", s.handleCoworkMCPTools)
	s.mux.HandleFunc("GET /api/cli-tools/cowork-mcp-registry", s.handleCoworkMCPRegistry)
	s.mux.HandleFunc("PATCH /api/cli-tools/cowork-mcp-registry", s.handleCoworkMCPRegistry)
	s.mux.HandleFunc("GET /api/cli-tools/{toolSettings}", s.handleCLIToolSettings)
	s.mux.HandleFunc("PATCH /api/cli-tools/{toolSettings}", s.handleCLIToolSettings)

	// MCP stdio↔SSE bridge
	s.mux.HandleFunc("GET /api/mcp/{plugin}/sse", s.handleMCPSSE)
	s.mux.HandleFunc("POST /api/mcp/{plugin}/message", s.handleMCPMessage)

	// Ops: version / update / shutdown (/api/init already registered)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)
	s.mux.HandleFunc("POST /api/version/update", s.handleUpdate)
	s.mux.HandleFunc("POST /api/version/shutdown", s.handleShutdown)
	s.mux.HandleFunc("POST /api/shutdown", s.handleShutdown)

	// Tunnel (cloudflare + tailscale)
	s.mux.HandleFunc("POST /api/tunnel/enable", s.handleTunnelEnable)
	s.mux.HandleFunc("POST /api/tunnel/disable", s.handleTunnelDisable)
	s.mux.HandleFunc("GET /api/tunnel/status", s.handleTunnelStatus)
	s.mux.HandleFunc("POST /api/tunnel/tailscale-install", s.handleTailscaleInstall)
	s.mux.HandleFunc("POST /api/tunnel/tailscale-enable", s.handleTailscaleEnable)
	s.mux.HandleFunc("POST /api/tunnel/tailscale-disable", s.handleTailscaleDisable)
	s.mux.HandleFunc("GET /api/tunnel/tailscale-check", s.handleTailscaleCheck)

	// MITM proxy API (start/stop/status/cert/trust/hosts)
	s.mux.HandleFunc("POST /api/mitm/start", s.handleMITMStart)
	s.mux.HandleFunc("POST /api/mitm/stop", s.handleMITMStop)
	s.mux.HandleFunc("GET /api/mitm/status", s.handleMITMStatus)
	s.mux.HandleFunc("GET /api/mitm/cert", s.handleMITMCert)
	s.mux.HandleFunc("POST /api/mitm/trust", s.handleMITMTrust)
	s.mux.HandleFunc("POST /api/mitm/hosts", s.handleMITMHosts)

	// Headroom process manager + reverse proxy
	s.mux.HandleFunc("POST /api/headroom/start", s.handleHeadroomStart)
	s.mux.HandleFunc("POST /api/headroom/stop", s.handleHeadroomStop)
	s.mux.HandleFunc("POST /api/headroom/restart", s.handleHeadroomRestart)
	s.mux.HandleFunc("GET /api/headroom/status", s.handleHeadroomStatus)
	s.mux.HandleFunc("GET /api/headroom/extras", s.handleHeadroomExtras)
	s.mux.HandleFunc("POST /api/headroom/extras", s.handleHeadroomExtras)
	s.mux.HandleFunc("DELETE /api/headroom/extras", s.handleHeadroomExtras)
	s.mux.HandleFunc("/api/headroom/proxy/{path...}", s.handleHeadroomProxy)

	// Pxpipe process manager
	s.mux.HandleFunc("POST /api/pxpipe/install", s.handlePxpipeInstall)
	s.mux.HandleFunc("POST /api/pxpipe/start", s.handlePxpipeStart)
	s.mux.HandleFunc("POST /api/pxpipe/stop", s.handlePxpipeStop)
	s.mux.HandleFunc("POST /api/pxpipe/restart", s.handlePxpipeRestart)
	s.mux.HandleFunc("GET /api/pxpipe/status", s.handlePxpipeStatus)
	s.mux.HandleFunc("GET /api/pxpipe/health", s.handlePxpipeHealth)
	s.mux.HandleFunc("GET /api/pxpipe/stats", s.handlePxpipeStats)
	s.mux.HandleFunc("GET /api/pxpipe/logs", s.handlePxpipeLogs)

	// Usage / analytics (static paths before {connectionId})
	s.mux.HandleFunc("GET /api/usage/stream", s.handleUsageStream)
	s.mux.HandleFunc("GET /api/usage/stats", s.handleUsageStats)
	s.mux.HandleFunc("GET /api/usage/logs", s.handleUsageLogs)
	s.mux.HandleFunc("GET /api/usage/request-logs", s.handleRequestLogs)
	s.mux.HandleFunc("GET /api/usage/request-details", s.handleRequestDetails)
	s.mux.HandleFunc("GET /api/usage/providers", s.handleUsageProviders)
	s.mux.HandleFunc("GET /api/usage/history", s.handleUsageHistory)
	s.mux.HandleFunc("GET /api/usage/chart", s.handleUsageChart)
	s.mux.HandleFunc("GET /api/usage/{connectionId}/codex-reset-credits", s.handleCodexResetCredits)
	s.mux.HandleFunc("POST /api/usage/{connectionId}/codex-reset-credits", s.handleCodexResetCredits)
	s.mux.HandleFunc("GET /api/usage/{connectionId}", s.handleUsageByConnection)

	s.mux.HandleFunc("/api/oauth/", s.handleOAuth)
	// more specific /v1/models paths before bare /v1/models
	s.mux.HandleFunc("GET /v1/models/info", s.handleModelsInfo)
	s.mux.HandleFunc("GET /v1/models/{kind}", s.handleModelsByKind)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	s.mux.HandleFunc("POST /v1/api/chat", s.handleVercelAI)
	s.mux.HandleFunc("POST /v1/responses/compact", s.handleCompactResponses)
	s.mux.HandleFunc("/v1/chat/completions", s.handleChat)
	s.mux.HandleFunc("/v1/messages", s.handleMessages)
	s.mux.HandleFunc("/v1/responses", s.handleResponses)
	s.mux.HandleFunc("/v1/embeddings", s.handleEmbeddings)
	s.mux.HandleFunc("/v1/images/generations", s.handleImageGeneration)
	s.mux.HandleFunc("/v1/audio/speech", s.handleTTS)
	s.mux.HandleFunc("/v1/audio/transcriptions", s.handleSTT)
	s.mux.HandleFunc("/v1/audio/voices", s.handleVoices)
	s.mux.HandleFunc("POST /v1/videos/generations", s.handleVideo)
	s.mux.HandleFunc("POST /v1/videos/edits", s.handleVideo)
	s.mux.HandleFunc("POST /v1/videos/extensions", s.handleVideo)
	s.mux.HandleFunc("GET /v1/videos/{id}", s.handleVideoPoll)
	s.mux.HandleFunc("/v1/search", s.handleSearch)
	s.mux.HandleFunc("/v1/web/fetch", s.handleFetch)
	s.mux.HandleFunc("/v1beta/", s.handleGeminiV1Beta)
	s.mux.HandleFunc("/codex/", s.handleCodexRewrite)
	s.mux.HandleFunc("/v1/", s.handleCORS)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.writeCORS(w)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) writeCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request) {
	s.writeCORS(w)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	mid := machineID(s.cfg.MachineIDSalt)
	key, keyID := s.keys.Generate(mid)
	hash := auth.HashKey(key)
	_, err := s.st.CreateAPIKey(req.Name, keyID, hash, mid)
	if err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"key":"` + key + `"}`))
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Name     string `json:"name"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" {
		http.Error(w, `{"error":"provider required"}`, http.StatusBadRequest)
		return
	}
	id, err := s.st.CreateConnection(req.Provider, "api_key", req.Name, req.APIKey, req.BaseURL)
	if err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
}

func (s *Server) handleCombos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		combos, err := s.st.ListCombos()
		if err != nil {
			http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(combos)
	case http.MethodPost:
		var req struct {
			Name   string   `json:"name"`
			Models []string `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			http.Error(w, `{"error":"name and models required"}`, http.StatusBadRequest)
			return
		}
		id, err := s.st.CreateCombo(req.Name, req.Models)
		if err != nil {
			http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAliases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		aliases, err := s.st.ListAliases()
		if err != nil {
			http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(aliases)
	case http.MethodPost:
		var req struct {
			Alias       string `json:"alias"`
			TargetModel string `json:"target_model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Alias == "" {
			http.Error(w, `{"error":"alias and target_model required"}`, http.StatusBadRequest)
			return
		}
		err := s.st.SetAlias(req.Alias, req.TargetModel)
		if err != nil {
			http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	handlers.Models(w, r, s.st)
}

func (s *Server) handleModelsInfo(w http.ResponseWriter, r *http.Request) {
	handlers.ModelsInfo(w, r, s.st)
}

func (s *Server) handleModelsByKind(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if kind == "info" {
		// defensive: method pattern should prefer /info route
		handlers.ModelsInfo(w, r, s.st)
		return
	}
	handlers.ModelsByKind(w, r, s.st, kind)
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	handlers.CountTokens(w, r)
}

func (s *Server) handleVercelAI(w http.ResponseWriter, r *http.Request) {
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.VercelAIChat(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleCompactResponses(w http.ResponseWriter, r *http.Request) {
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.CompactResponses(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleGeminiV1Beta(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.writeCORS(w)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	_ = handlers.GeminiV1Beta(r.Context(), w, r, s.st, s.exec, s.fb)
}

func (s *Server) handleCodexRewrite(w http.ResponseWriter, r *http.Request) {
	// strip /codex prefix → re-dispatch as /v1/responses
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/v1/responses"
	r2.RequestURI = ""
	s.handleResponses(w, r2)
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	ts := handlers.LoadTokenSaverFromStore(s.st)
	strategy, sticky := handlers.LoadAccountStrategyFromStore(s.st)
	_ = handlers.ChatWithOptions(r.Context(), w, body, s.st, s.exec, s.fb, handlers.ChatOptions{
		TokenSaver:      ts,
		ClientHeaders:   r.Header,
		Usage:           usageBridge{s.tracker},
		AccountStrategy: strategy,
		StickyLimit:     sticky,
	})
}

// usageBridge adapts usage.Tracker to handlers.UsageSink.
type usageBridge struct{ t *usage.Tracker }

func (u usageBridge) OnUsage(provider, model, connectionID string, prompt, completion, statusCode int) {
	if u.t == nil {
		return
	}
	u.t.Track(usage.Record{
		Provider: provider, Model: model, ConnectionID: connectionID,
		PromptTokens: prompt, CompletionTokens: completion, StatusCode: statusCode,
	})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	ts := handlers.LoadTokenSaverFromStore(s.st)
	// Anthropic Messages API — force Claude source format
	_ = handlers.ChatWithOptions(r.Context(), w, body, s.st, s.exec, s.fb, handlers.ChatOptions{
		TokenSaver:    ts,
		SourceFormat:  "claude",
		ClientHeaders: r.Header,
		Usage:         usageBridge{s.tracker},
	})
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.Responses(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.Embeddings(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleImageGeneration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.ImageGeneration(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleTTS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.TTS(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleSTT(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	// parity 9router maxDuration: 300s
	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		_ = handlers.STTMultipart(ctx, w, r, s.st, s.fb)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.STT(ctx, w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleVoices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	_ = handlers.Voices(r.Context(), w, s.st)
}

func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.Video(r.Context(), w, r, body, s.st, s.exec, s.fb)
}

func (s *Server) handleVideoPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	_ = handlers.VideoPoll(r.Context(), w, r, r.PathValue("id"), s.st, s.fb)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.Search(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.RequireAPIKey && !s.authOK(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}
	_ = handlers.Fetch(r.Context(), w, body, s.st, s.exec, s.fb)
}

func (s *Server) handleOAuth(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/oauth/")
	path = strings.Trim(path, "/")
	if path == "" {
		http.Error(w, `{"error":"invalid oauth path"}`, http.StatusBadRequest)
		return
	}
	// Specialized import/social routes (cursor/kiro/codex/iflow/gitlab)
	if s.oauth.SpecializedImport(w, r, path, s.st) {
		return
	}

	parts := strings.Split(path, "/")
	provider := parts[0]
	action := "authorize"
	if len(parts) > 1 {
		action = parts[1]
	}

switch action {
	case "authorize":
		cfg, ok := oauth.ProviderConfigs[provider]
		if ok && cfg.AuthStyle == "device" {
			dc, err := oauth.StartDeviceFlowForProvider(r.Context(), provider)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(dc)
			return
		}
		s.oauth.StartAuth(w, r, provider)
	case "device-code":
		// GET parity with 9router device-code start
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		cfg, ok := oauth.ProviderConfigs[provider]
		if !ok || cfg.AuthStyle != "device" {
			http.Error(w, `{"error":"Provider does not support device code flow"}`, http.StatusBadRequest)
			return
		}
		dc, err := oauth.StartDeviceFlowForProvider(r.Context(), provider)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(dc)
	case "start-proxy":
		if provider != "codex" && provider != "xai" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Proxy only supported for codex/xai"})
			return
		}
		appPortStr := r.URL.Query().Get("app_port")
		if appPortStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing app_port"})
			return
		}
		appPort, _ := strconv.Atoi(appPortStr)
		result, err := oauth.StartOAuthProxy(provider, appPort, s.oauth, s.st)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		serverSide := false
		state := r.URL.Query().Get("state")
		cv := r.URL.Query().Get("code_verifier")
		ru := r.URL.Query().Get("redirect_uri")
		if ok, _ := result["success"].(bool); ok && state != "" && cv != "" && ru != "" {
			serverSide = oauth.RegisterProxySession(provider, state, cv, ru)
		}
		result["serverSide"] = serverSide
		writeJSONOK(w, result)
	case "poll-status":
		if provider != "codex" && provider != "xai" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Poll only supported for codex/xai"})
			return
		}
		state := r.URL.Query().Get("state")
		if state == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Missing state"})
			return
		}
		session := oauth.GetProxySessionStatus(provider, state)
		if session == nil {
			writeJSONOK(w, map[string]any{"status": "unknown"})
			return
		}
		st, _ := session["status"].(string)
		if st == "done" || st == "error" {
			oauth.ClearProxySession(provider, state)
			writeJSONOK(w, session)
			return
		}
		writeJSONOK(w, map[string]any{"status": st})
	case "stop-proxy":
		if provider != "codex" && provider != "xai" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Proxy only supported for codex/xai"})
			return
		}
		_ = oauth.StopOAuthProxy(provider)
		writeJSONOK(w, map[string]any{"success": true})
	case "device", "poll":
		// POST {device_code|deviceCode} → poll once / return token
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			DeviceCode  string         `json:"device_code"`
			DeviceCode2 string         `json:"deviceCode"`
			Interval    int            `json:"interval"`
			CodeVerifier string        `json:"codeVerifier"`
			ExtraData   map[string]any `json:"extraData"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
			return
		}
		dc := req.DeviceCode
		if dc == "" {
			dc = req.DeviceCode2
		}
		if dc == "" {
			http.Error(w, `{"error":"device_code required"}`, http.StatusBadRequest)
			return
		}
		if req.Interval <= 0 {
			req.Interval = 5
		}
		cfg, ok := oauth.ProviderConfigs[provider]
		if !ok {
			http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
			return
		}
		if provider == "github" || provider == "copilot" {
			tok, extra, err := oauth.ExchangeGithubDeviceToken(r.Context(), dc, provider == "copilot" || provider == "github")
			if err != nil {
				// pending-friendly shape for poll
				writeJSONOK(w, map[string]any{"success": false, "error": err.Error(), "pending": true})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  tok.AccessToken,
				"refresh_token": tok.RefreshToken,
				"expires_at":    tok.ExpiresAt,
				"extra":         extra,
				"success":       true,
			})
			return
		}
		tok, err := oauth.PollDeviceToken(r.Context(), cfg, dc, req.Interval)
		if err != nil {
			msg := err.Error()
			pending := strings.Contains(msg, "authorization_pending") || strings.Contains(msg, "slow_down") || strings.Contains(msg, "pending")
			writeJSONOK(w, map[string]any{"success": false, "error": msg, "pending": pending})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  tok.AccessToken,
			"refresh_token": tok.RefreshToken,
			"expires_at":    tok.ExpiresAt,
			"success":       true,
		})
	case "exchange":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Code         string         `json:"code"`
			RedirectURI  string         `json:"redirectUri"`
			CodeVerifier string         `json:"codeVerifier"`
			State        string         `json:"state"`
			Meta         map[string]any `json:"meta"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid or empty request body"})
			return
		}
		conn, err := s.oauth.ExchangeAndSave(r.Context(), s.st, provider, body.Code, body.RedirectURI, body.CodeVerifier, body.State, body.Meta)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSONOK(w, map[string]any{"success": true, "connection": conn})
	case "manual-code":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if provider != "xai" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Manual code only supported for xai"})
			return
		}
		var body struct {
			Code  string `json:"code"`
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
			return
		}
		code := strings.TrimSpace(body.Code)
		state := strings.TrimSpace(body.State)
		sess := oauth.GetProxySessionStatus("xai", state)
		if sess == nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "xAI OAuth session not found; restart the login flow and paste the code again"})
			return
		}
		// Need full session with verifier — re-register path stores only status view.
		// Exchange via stored session by reading internal map through Register again is wrong;
		// use ExchangeAndSave with redirect from query fallback.
		// Manual code: session must still be pending with verifier — pull via poll-status shape is incomplete.
		// Complete by calling ExchangeAndSave with redirect_uri from registered session internals:
		conn, err := s.completeXaiManualCode(r.Context(), code, state)
		if err != nil {
			oauth.ClearProxySession("xai", state)
			_ = oauth.StopOAuthProxy("xai")
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		oauth.ClearProxySession("xai", state)
		_ = oauth.StopOAuthProxy("xai")
		writeJSONOK(w, map[string]any{"success": true, "connection": conn})
	case "callback":
		s.oauth.HandleCallback(w, r, provider)
	case "refresh":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			RefreshToken string         `json:"refresh_token"`
			PSD          map[string]any `json:"provider_specific_data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
			http.Error(w, `{"error":"refresh_token required"}`, http.StatusBadRequest)
			return
		}
		if provider == "kiro" {
			tok, err := oauth.RefreshKiroToken(r.Context(), req.RefreshToken, req.PSD)
			if err != nil {
				token, err2 := s.refresh.Refresh(r.Context(), provider, req.RefreshToken)
				if err2 != nil {
					http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(token)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  tok.AccessToken,
				"refresh_token": tok.RefreshToken,
				"expires_at":    tok.ExpiresAt,
			})
			return
		}
		token, err := s.refresh.Refresh(r.Context(), provider, req.RefreshToken)
		if err != nil {
			tok, err2 := s.oauth.RefreshToken(r.Context(), provider, req.RefreshToken)
			if err2 != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  tok.AccessToken,
				"refresh_token": tok.RefreshToken,
				"expires_at":    tok.ExpiresAt,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(token)
	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
	}
}

func (s *Server) completeXaiManualCode(ctx context.Context, code, state string) (map[string]any, error) {
	// Access full session via GetProxySessionStatus is status-only; use ExchangeAndSave with
	// redirect_uri from xAI fixed proxy default when session still holds verifier internally.
	return oauth.CompleteXaiManualCode(ctx, s.oauth, s.st, code, state)
}

func (s *Server) authOK(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	if h == "" {
		return !s.cfg.RequireAPIKey
	}
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return false
	}
	raw := strings.TrimSpace(h[len(p):])
	if !s.keys.VerifyCRC(raw) {
		return false
	}
	_, keyID, ok := s.keys.Parse(raw)
	if !ok {
		return false
	}
	hash, _, found, err := s.st.LookupActiveByKeyID(keyID)
	if err != nil || !found || hash != auth.HashKey(raw) {
		return false
	}
	return true
}

func machineID(salt string) string {
	h := auth.HashKey(salt)
	if len(h) > 16 {
		return h[:16]
	}
	return h
}

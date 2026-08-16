package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"flamerouter/internal/oauth"
	"flamerouter/internal/opensse/combo"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/opensse/rtk"
	"flamerouter/internal/opensse/stream"
	"flamerouter/internal/provider"
	"flamerouter/internal/store"
	"flamerouter/internal/tokenrefresh"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"

	// Ensure translators register when handlers package loads (tests + gateway)
	_ "flamerouter/internal/translator/request"
	_ "flamerouter/internal/translator/response"
)

type refreshAdapter struct {
	rm *tokenrefresh.RefreshManager
}

func (a *refreshAdapter) Refresh(ctx context.Context, provider, refreshToken string) (string, string, time.Time, error) {
	res, err := a.rm.Refresh(ctx, provider, refreshToken)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return res.AccessToken, res.RefreshToken, res.ExpiresAt, nil
}

var (
	credOnce sync.Once
	credMgr  *oauth.CredManager
)

func getCredMgr() *oauth.CredManager {
	credOnce.Do(func() {
		credMgr = oauth.NewCredManager(&refreshAdapter{rm: tokenrefresh.NewRefreshManager()})
	})
	return credMgr
}

// ensureFreshConn refreshes OAuth tokens before use when needed. Fail-open on error.
func ensureFreshConn(ctx context.Context, st *store.Store, conn *store.Connection) *store.Connection {
	if conn == nil || st == nil {
		return conn
	}
	if conn.RefreshToken == "" && conn.AuthType != "oauth" {
		return conn
	}
	out, err := getCredMgr().RefreshIfNeeded(ctx, st, conn)
	if err != nil || out == nil {
		return conn
	}
	return out
}

var (
	errInvalidModel   = errors.New("invalid model: provider required")
	errNoAccount      = errors.New("no account available for provider")
	errUpstreamFailed = errors.New("upstream request failed")
)

// ChatOptions optional chat flags (token savers, source override).
// UsageSink optional async usage (gateway wires usage.Tracker).
type UsageSink interface {
	OnUsage(provider, model, connectionID string, prompt, completion, statusCode int)
}

type ChatOptions struct {
	TokenSaver    rtk.TokenSaverOptions
	SourceFormat  string // force source format (e.g. claude for /v1/messages)
	ClientHeaders http.Header
	Usage         UsageSink
	// AccountStrategy: ""|"fill-first"|"round-robin" — SelectAccountWithStrategy
	AccountStrategy string
	// StickyLimit for round-robin (0 → fallback default 3)
	StickyLimit int
}

func Chat(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	return ChatWithOptions(ctx, w, body, st, exec, fb, ChatOptions{})
}

func ChatWithOptions(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback, opts ChatOptions) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	sourceFormat := opts.SourceFormat
	if sourceFormat == "" {
		sourceFormat = translator.DetectSourceFormat(m)
	}
	modelStr, _ := m["model"].(string)
	streamReq, _ := m["stream"].(bool)

	// Token saver master switch from header (x-9router-token-saver / x-flamerouter-token-saver)
	ts := opts.TokenSaver
	if ts.Enabled == false && opts.TokenSaver == (rtk.TokenSaverOptions{}) {
		ts = rtk.DefaultTokenSaver()
	}
	if opts.ClientHeaders != nil {
		h := opts.ClientHeaders.Get("x-flamerouter-token-saver")
		if h == "" {
			h = opts.ClientHeaders.Get("x-9router-token-saver")
		}
		if !rtk.ParseTokenSaverHeader(h) {
			ts.Enabled = false
		}
	}

	combo, _ := st.GetComboByName(modelStr)
	if combo != nil && len(combo.Models) > 0 {
		return handleCombo(ctx, w, body, combo, st, exec, fb, streamReq, sourceFormat, ts)
	}

	aliases, _ := st.ListAliases()
	mref := model.ParseModel(modelStr)

	if mref.IsAlias {
		if resolved, ok := model.ResolveModelAlias(mref.Model, aliases); ok {
			mref = resolved
		}
	}

	if mref.Provider == "" {
		http.Error(w, `{"error":"model must be provider/model format"}`, http.StatusBadRequest)
		return nil
	}
	providerID := model.ResolveProviderAlias(mref.Provider, provider.ProviderAliases())

	return handleWithFallback(ctx, w, body, providerID, mref.Model, st, exec, fb, streamReq, sourceFormat, ts, opts.AccountStrategy, opts.StickyLimit, opts.Usage)
}

func handleCombo(ctx context.Context, w http.ResponseWriter, body []byte, c *store.Combo, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions) error {
	// LoadStrategySettings already applies comboStrategies[name].fallbackStrategy + judgeModel.
	// Resolve(nil) is intentional: per-combo map not needed when strategy name is pre-resolved.
	strategyName, sticky, judge := combo.LoadStrategySettings(st, c.Name)
	strat := combo.Resolve(strategyName, nil, c.Name)

	opts := combo.Options{
		SourceFormat: sourceFormat,
		Stream:       streamReq,
		ComboName:    c.Name,
		StickyLimit:  sticky,
		JudgeModel:   judge,
		SingleModel: func(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, stream bool) error {
			return runComboModel(ctx, w, body, modelStr, st, exec, fb, stream, sourceFormat, ts)
		},
	}
	return strat.Execute(ctx, w, body, c.Models, st, exec, fb, opts)
}

func runComboModel(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions) error {
	mref := model.ParseModel(modelStr)
	aliases, _ := st.ListAliases()
	if mref.IsAlias {
		if resolved, ok := model.ResolveModelAlias(mref.Model, aliases); ok {
			mref = resolved
		}
	}
	if mref.Provider == "" {
		return fmt.Errorf("%w: %q", errInvalidModel, modelStr)
	}
	providerID := model.ResolveProviderAlias(mref.Provider, provider.ProviderAliases())
	if streamReq {
		// SSE headers only after first successful upstream stream (see streamModel).
		// Avoid WriteHeader(200) before success so total-fail can still http.Error cleanly.
		if flusher, ok := w.(http.Flusher); ok {
			err := streamModel(ctx, w, flusher, body, providerID, mref.Model, st, exec, fb, sourceFormat, ts)
			if err == nil {
				w.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
			}
			return err
		}
	}
	return handleWithFallback(ctx, w, body, providerID, mref.Model, st, exec, fb, streamReq, sourceFormat, ts, "", 0, nil)
}

func handleWithFallback(ctx context.Context, w http.ResponseWriter, body []byte, providerID, modelName string, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions, strategy string, stickyLimit int, usageSink UsageSink) error {
	excludeIDs := make(map[string]bool)
	var lastErr error
	targetFormat := getTargetFormat(providerID)

	for {
		conn, err := fb.SelectAccountWithStrategy(providerID, strategy, stickyLimit, excludeIDs)
		if conn == nil {
			if len(excludeIDs) == 0 {
				http.Error(w, `{"error":"no active connection for provider `+providerID+`"}`, http.StatusBadRequest)
				return nil
			}
			http.Error(w, `{"error":"all accounts unavailable"}`, http.StatusServiceUnavailable)
			return lastErr
		}

		conn = ensureFreshConn(ctx, st, conn)
		cred := connCredentials(conn)

		var translatedBody map[string]any
		if translator.NeedsTranslation(sourceFormat, targetFormat) {
			var bodyMap map[string]any
			_ = json.Unmarshal(body, &bodyMap)
			// Pre-pass modality/prefetch also runs inside TranslateRequest
			translatedBody = translator.DefaultRegistry.TranslateRequest(sourceFormat, targetFormat, bodyMap, translator.TranslateOptions{
				Model:    modelName,
				Stream:   streamReq,
				Provider: providerID,
				Credentials: map[string]any{
					"apiKey":                 conn.APIKey,
					"accessToken":            firstNonEmpty(conn.AccessToken, conn.APIKey),
					"refreshToken":           conn.RefreshToken,
					"providerSpecificData":   conn.ProviderSpecificData,
				},
				ConnectionId: conn.ID,
			})
		} else {
			// Still strip modalities / prefetch even on passthrough
			var bodyMap map[string]any
			_ = json.Unmarshal(body, &bodyMap)
			caps := concerns.GetCapabilitiesForModel(providerID, modelName)
			if caps != nil {
				concerns.StripUnsupportedModalities(bodyMap, sourceFormat, caps)
			}
			concerns.PrefetchRemoteImages(bodyMap, sourceFormat, targetFormat)
			translatedBody = bodyMap
		}

		// Token savers on final body (matches 9router chatCore post-translate)
		if translatedBody != nil {
			ts.Model = modelName
			ts.Format = targetFormat
			translatedBody = rtk.ApplyTokenSavers(translatedBody, ts)
		}

		var payload []byte
		if translatedBody != nil {
			payload, _ = json.Marshal(translatedBody)
		} else {
			payload = body
		}

		ex := exec
		if ex == nil {
			ex = executor.GetExecutor(providerID)
		}
		res, err := ex.Execute(ctx, cred, modelName, payload, streamReq)
		if err != nil {
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, 502, err.Error(), 0)
			if !shouldFallback {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
				return err
			}
			excludeIDs[conn.ID] = true
			lastErr = err
			continue
		}
		defer res.Body.Close()

		if streamReq {
			stream.WriteSSEHeaders(w)
			if translator.NeedsTranslation(sourceFormat, targetFormat) {
				state := concerns.NewResponseState()
				scanner := bufio.NewScanner(res.Body)
				for scanner.Scan() {
					line := scanner.Text()
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					data := strings.TrimPrefix(line, "data: ")
					if data == "[DONE]" {
						break
					}
					var chunk map[string]any
					if json.Unmarshal([]byte(data), &chunk) != nil {
						continue
					}
					translated := translator.DefaultRegistry.TranslateResponse(targetFormat, sourceFormat, chunk, state)
					for _, t := range translated {
						j, _ := json.Marshal(t)
						w.Write([]byte("data: " + string(j) + "\n\n"))
					}
				}
			} else {
				_ = stream.Pipe(w, res.Body)
			}
			fb.ClearError(conn.ID)
			return nil
		}

		respBody, _ := io.ReadAll(res.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.StatusCode)

		if res.StatusCode >= 400 {
			errText := string(respBody)
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, res.StatusCode, errText, 0)
			if shouldFallback {
				excludeIDs[conn.ID] = true
				lastErr = fmt.Errorf("%w: status %d", errUpstreamFailed, res.StatusCode)
				continue
			}
		} else {
			fb.ClearError(conn.ID)
		}

		if translator.NeedsTranslation(sourceFormat, targetFormat) {
			var respMap map[string]any
			if json.Unmarshal(respBody, &respMap) == nil {
				translated := translator.DefaultRegistry.TranslateResponse(targetFormat, sourceFormat, respMap, concerns.NewResponseState())
				if len(translated) > 0 {
					j, _ := json.Marshal(translated[0])
					w.Write(j)
				} else {
					w.Write(respBody)
				}
			} else {
				w.Write(respBody)
			}
		} else {
			w.Write(respBody)
		}

		var rm map[string]any
		if json.Unmarshal(respBody, &rm) == nil {
			if usage, ok := rm["usage"].(map[string]any); ok {
				prompt, _ := usage["prompt_tokens"].(float64)
				completion, _ := usage["completion_tokens"].(float64)
				_ = st.InsertUsage(providerID, modelName, int(prompt), int(completion), conn.ID)
				if usageSink != nil {
					usageSink.OnUsage(providerID, modelName, conn.ID, int(prompt), int(completion), res.StatusCode)
				}
			}
		}
		return nil
	}
}

func streamModel(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, body []byte, providerID, modelName string, st *store.Store, exec executor.Executor, fb *fallback.Fallback, sourceFormat string, ts rtk.TokenSaverOptions) error {
	excludeIDs := make(map[string]bool)
	targetFormat := getTargetFormat(providerID)

	for {
		conn, err := fb.SelectAccountWithStrategy(providerID, "", 0, excludeIDs)
		if conn == nil {
			return errNoAccount
		}

		conn = ensureFreshConn(ctx, st, conn)
		cred := connCredentials(conn)

		var m map[string]any
		_ = json.Unmarshal(body, &m)
		m["model"] = providerID + "/" + modelName
		m["stream"] = true

		var translatedBody map[string]any
		if translator.NeedsTranslation(sourceFormat, targetFormat) {
			translatedBody = translator.DefaultRegistry.TranslateRequest(sourceFormat, targetFormat, m, translator.TranslateOptions{
				Model:    modelName,
				Stream:   true,
				Provider: providerID,
				Credentials: map[string]any{
					"apiKey":               conn.APIKey,
					"accessToken":          firstNonEmpty(conn.AccessToken, conn.APIKey),
					"refreshToken":         conn.RefreshToken,
					"providerSpecificData": conn.ProviderSpecificData,
				},
				ConnectionId: conn.ID,
			})
		} else {
			caps := concerns.GetCapabilitiesForModel(providerID, modelName)
			if caps != nil {
				concerns.StripUnsupportedModalities(m, sourceFormat, caps)
			}
			concerns.PrefetchRemoteImages(m, sourceFormat, targetFormat)
			translatedBody = m
		}
		if translatedBody != nil {
			ts.Model = modelName
			ts.Format = targetFormat
			translatedBody = rtk.ApplyTokenSavers(translatedBody, ts)
		}
		payload, _ := json.Marshal(translatedBody)

		ex := exec
		if ex == nil {
			ex = executor.GetExecutor(providerID)
		}
		res, err := ex.Execute(ctx, cred, modelName, payload, true)
		if err != nil {
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, 502, err.Error(), 0)
			if !shouldFallback {
				return err
			}
			excludeIDs[conn.ID] = true
			continue
		}
		defer res.Body.Close()

		if res.StatusCode >= 400 {
			respBody, _ := io.ReadAll(res.Body)
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, res.StatusCode, string(respBody), 0)
			if shouldFallback {
				excludeIDs[conn.ID] = true
				continue
			}
			return fmt.Errorf("%w: status %d", errUpstreamFailed, res.StatusCode)
		}

		// Headers only after confirmed successful upstream (status < 400).
		if w.Header().Get("Content-Type") == "" {
			stream.WriteSSEHeaders(w)
		}
		fb.ClearError(conn.ID)
		if translator.NeedsTranslation(sourceFormat, targetFormat) {
			state := concerns.NewResponseState()
			scanner := bufio.NewScanner(res.Body)
			for scanner.Scan() {
				line := scanner.Text()
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					break
				}
				var chunk map[string]any
				if json.Unmarshal([]byte(data), &chunk) != nil {
					continue
				}
				translated := translator.DefaultRegistry.TranslateResponse(targetFormat, sourceFormat, chunk, state)
				for _, t := range translated {
					j, _ := json.Marshal(t)
					w.Write([]byte("data: " + string(j) + "\n\n"))
				}
			}
		} else {
			buf := make([]byte, 32*1024)
			for {
				n, readErr := res.Body.Read(buf)
				if n > 0 {
					lines := strings.Split(string(buf[:n]), "\n")
					for _, line := range lines {
						line = strings.TrimSpace(line)
						if line == "" {
							continue
						}
						w.Write([]byte(line + "\n\n"))
						flusher.Flush()
					}
				}
				if readErr != nil {
					break
				}
			}
		}
		return nil
	}
}

func connCredentials(conn *store.Connection) executor.Credentials {
	return executor.Credentials{
		APIKey:               conn.APIKey,
		AccessToken:          firstNonEmpty(conn.AccessToken, conn.APIKey),
		RefreshToken:         conn.RefreshToken,
		BaseURL:              conn.BaseURL,
		ProviderSpecificData: conn.ProviderSpecificData,
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func getTargetFormat(providerID string) string {
	providerFormats := map[string]string{
		"claude":               translator.FormatClaude,
		"anthropic":            translator.FormatClaude,
		"anthropic-compatible": translator.FormatClaude,
		"gemini":               translator.FormatGemini,
		"gemini-cli":           translator.FormatGeminiCLI,
		"vertex":               translator.FormatVertex,
		"vertex-partner":       translator.FormatVertex,
		"antigravity":          translator.FormatAntigravity,
		"kiro":                 translator.FormatKiro,
		"cursor":               translator.FormatCursor,
		"cu":                   translator.FormatCursor,
		"ollama":               translator.FormatOllama,
		"ollama-local":         translator.FormatOllama,
		"commandcode":          translator.FormatCommandCode,
		"codex":                translator.FormatCodex,
	}
	if fmt, ok := providerFormats[providerID]; ok {
		return fmt
	}
	if strings.HasPrefix(providerID, "anthropic-compatible") {
		return translator.FormatClaude
	}
	if strings.HasPrefix(providerID, "openai-compatible") {
		return translator.FormatOpenAI
	}
	return translator.FormatOpenAI
}

package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flamerouter/internal/oauth"
	"flamerouter/internal/opensse/combo"
	"flamerouter/internal/opensse/config"
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
	"flamerouter/internal/usage"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	// Ensure translators register when handlers package loads (tests + gateway).
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

	if res == nil {
		return "", "", time.Time{}, fmt.Errorf("nil refresh result")
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

// UsageSink is an optional async usage interface (gateway wires usage.Tracker).
type UsageSink interface {
	OnUsage(provider, model, connectionID string, prompt, completion, statusCode int)
}

// ChatOptions specifies optional chat flags (token savers, source override).
type ChatOptions struct {
	Usage           UsageSink
	ClientHeaders   http.Header
	SourceFormat    string
	AccountStrategy string
	TokenSaver      rtk.TokenSaverOptions
	StickyLimit     int
}

// Chat handles standard chat completions.
func Chat(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	return ChatWithOptions(ctx, w, body, st, exec, fb, ChatOptions{
		Usage:           nil,
		ClientHeaders:   nil,
		SourceFormat:    "",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})
}

// ChatWithOptions handles chat completions with explicit options.
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

	modelStr, _ := m["model"].(string) //nolint:errcheck // optional type assertion
	streamReq, _ := m["stream"].(bool) //nolint:errcheck // optional type assertion
	ts := resolveTokenSaverOptions(opts)

	if handled, err := tryHandleComboOrSynth(ctx, w, body, modelStr, m, st, exec, fb, streamReq, sourceFormat, ts, opts.Usage); handled {
		return err
	}

	providerID, modelClean, err := resolveChatModel(modelStr, st)
	if err != nil {
		http.Error(w, `{"error":"model must be provider/model format"}`, http.StatusBadRequest)
		return nil //nolint:nilerr // HTTP 400 error already written to response
	}

	return handleWithFallback(ctx, w, body, providerID, modelClean, st, exec, fb, streamReq, sourceFormat, ts, opts.AccountStrategy, opts.StickyLimit, opts.Usage)
}

func resolveTokenSaverOptions(opts ChatOptions) rtk.TokenSaverOptions {
	ts := opts.TokenSaver
	if !ts.Enabled && opts.TokenSaver == rtk.EmptyTokenSaver() {
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

	return ts
}

func tryHandleComboOrSynth(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, m map[string]any, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions, usageSink UsageSink) (bool, error) {
	cDef, _ := st.GetComboByName(modelStr) //nolint:errcheck // optional combo
	if cDef != nil && len(cDef.Models) > 0 {
		return true, handleCombo(ctx, w, body, cDef, st, exec, fb, streamReq, sourceFormat, ts, usageSink)
	}

	capConfig := combo.LoadCapacityAdapterConfig(st)
	reqCaps := combo.DetectRequiredCapabilities(m)
	soloAugmented := combo.AugmentModelsWithCapacityAdapter([]string{modelStr}, reqCaps, capConfig)

	if len(soloAugmented) > 1 {
		synthCombo := &store.Combo{
			ID:     "",
			Name:   modelStr,
			Models: soloAugmented,
		}

		return true, handleCombo(ctx, w, body, synthCombo, st, exec, fb, streamReq, sourceFormat, ts, usageSink)
	}

	return false, nil
}

func resolveChatModel(modelStr string, st *store.Store) (string, string, error) {
	aliases, _ := st.ListAliases() //nolint:errcheck // optional alias list
	mref := model.ParseModel(modelStr)

	if mref.IsAlias {
		if resolved, ok := model.ResolveModelAlias(mref.Model, aliases); ok {
			mref = resolved
		}
	}

	if mref.Provider == "" {
		return "", "", errInvalidModel
	}

	providerID := model.ResolveProviderAlias(mref.Provider, provider.Aliases())

	return providerID, mref.Model, nil
}

func handleCombo(ctx context.Context, w http.ResponseWriter, body []byte, c *store.Combo, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions, usageSink UsageSink) error {
	// LoadStrategySettings already applies comboStrategies[name].fallbackStrategy + judgeModel.
	// Resolve(nil) is intentional: per-combo map not needed when strategy name is pre-resolved.
	strategyName, sticky, judge := combo.LoadStrategySettings(st, c.Name)
	start := combo.Resolve(strategyName, nil, c.Name)

	opts := combo.Options{
		SourceFormat:   sourceFormat,
		Stream:         streamReq,
		ComboName:      c.Name,
		StickyLimit:    sticky,
		JudgeModel:     judge,
		ClientHeaders:  nil,
		TargetFormat:   "",
		TokenSaverJSON: "",
		SingleModel: func(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, stream bool) error {
			return runComboModel(ctx, w, body, modelStr, st, exec, fb, stream, sourceFormat, ts, usageSink)
		},
	}

	return start.Execute(ctx, w, body, c.Models, st, exec, fb, opts)
}

func runComboModel(ctx context.Context, w http.ResponseWriter, body []byte, modelStr string, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions, usageSink UsageSink) error {
	mref := model.ParseModel(modelStr)
	aliases, _ := st.ListAliases() //nolint:errcheck // optional alias list

	if mref.IsAlias {
		if resolved, ok := model.ResolveModelAlias(mref.Model, aliases); ok {
			mref = resolved
		}
	}

	if mref.Provider == "" {
		return fmt.Errorf("%w: %q", errInvalidModel, modelStr)
	}

	providerID := model.ResolveProviderAlias(mref.Provider, provider.Aliases())

	if streamReq {
		if flusher, ok := w.(http.Flusher); ok {
			err := streamModel(ctx, w, flusher, body, providerID, mref.Model, st, exec, fb, sourceFormat, ts, usageSink)
			if err == nil {
				_, _ = w.Write([]byte("data: [DONE]\n\n")) //nolint:errcheck // stream write

				flusher.Flush()
			}

			return err
		}
	}

	return handleWithFallback(ctx, w, body, providerID, mref.Model, st, exec, fb, streamReq, sourceFormat, ts, "", 0, usageSink)
}

func stripContinuityFields(m map[string]any) {
	if m == nil {
		return
	}

	messages, ok := m["messages"].([]any)
	if !ok {
		return
	}

	for _, msgRaw := range messages {
		if msg, ok := msgRaw.(map[string]any); ok {
			delete(msg, "encrypted_content")
			delete(msg, "reasoning_encrypted_content")
		}
	}
}

func prepareChatPayload(body []byte, providerID, modelName string, streamReq bool, sourceFormat, targetFormat string, conn *store.Connection, ts rtk.TokenSaverOptions) []byte {
	var translatedBody map[string]any

	if translator.NeedsTranslation(sourceFormat, targetFormat) {
		var bodyMap map[string]any
		if err := json.Unmarshal(body, &bodyMap); err != nil || bodyMap == nil {
			bodyMap = make(map[string]any)
		}

		stripContinuityFields(bodyMap)

		translatedBody = translator.DefaultRegistry.TranslateRequest(sourceFormat, targetFormat, bodyMap, translator.TranslateOptions{
			Model:      modelName,
			Stream:     streamReq,
			Provider:   providerID,
			ClientTool: false,
			StripList:  nil,
			Credentials: map[string]any{
				"apiKey":               conn.APIKey,
				"accessToken":          firstNonEmpty(conn.AccessToken, conn.APIKey),
				"refreshToken":         conn.RefreshToken,
				"providerSpecificData": conn.ProviderSpecificData,
			},
			ConnectionID: conn.ID,
		})
	} else {
		var bodyMap map[string]any
		_ = json.Unmarshal(body, &bodyMap) //nolint:errcheck // best-effort body unmarshal

		stripContinuityFields(bodyMap)

		caps := concerns.GetCapabilitiesForModel(providerID, modelName)
		if caps != nil {
			concerns.StripUnsupportedModalities(bodyMap, sourceFormat, caps)
		}

		concerns.PrefetchRemoteImages(bodyMap, sourceFormat, targetFormat)
		translatedBody = bodyMap
	}

	if translatedBody != nil {
		ts.Model = modelName
		ts.Format = targetFormat
		translatedBody = rtk.ApplyTokenSavers(translatedBody, ts)

		payload, _ := json.Marshal(translatedBody) //nolint:errcheck // best-effort marshal

		return payload
	}

	return body
}

func writeTranslatedStream(w http.ResponseWriter, flusher http.Flusher, body io.Reader, sourceFormat, targetFormat string, reqBody []byte) usage.ExtractedUsage {
	state := concerns.NewResponseState()
	scanner := bufio.NewScanner(body)
	finalUsage := usage.ExtractedUsage{
		PromptTokens:     0,
		CompletionTokens: 0,
		HasUsage:         false,
	}
	contentLen := 0

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

		if u, ok := usage.ExtractUsageFromChunk(chunk); ok {
			if u.PromptTokens > finalUsage.PromptTokens {
				finalUsage.PromptTokens = u.PromptTokens
			}

			if u.CompletionTokens > finalUsage.CompletionTokens {
				finalUsage.CompletionTokens = u.CompletionTokens
			}

			finalUsage.HasUsage = true
		}

		contentLen += usage.ExtractContentLengthFromChunk(chunk)

		translated := translator.DefaultRegistry.TranslateResponse(targetFormat, sourceFormat, chunk, state)
		for _, t := range translated {
			j, _ := json.Marshal(t)                               //nolint:errcheck // best-effort marshal
			_, _ = w.Write([]byte("data: " + string(j) + "\n\n")) //nolint:errcheck // stream write

			if flusher != nil {
				flusher.Flush()
			}
		}
	}

	if state.Usage != nil {
		if state.Usage.PromptTokens > finalUsage.PromptTokens {
			finalUsage.PromptTokens = state.Usage.PromptTokens
		}

		if state.Usage.CompletionTokens > finalUsage.CompletionTokens {
			finalUsage.CompletionTokens = state.Usage.CompletionTokens
		}

		finalUsage.HasUsage = true
	}

	if !finalUsage.HasUsage || (finalUsage.PromptTokens == 0 && finalUsage.CompletionTokens == 0) {
		finalUsage.PromptTokens = usage.EstimateInputTokens(reqBody)
		finalUsage.CompletionTokens = usage.EstimateOutputTokens(contentLen)
		finalUsage.HasUsage = true
	}

	_, _ = w.Write([]byte("data: [DONE]\n\n")) //nolint:errcheck // stream write

	if flusher != nil {
		flusher.Flush()
	}

	return finalUsage
}

func writeTranslatedNonStream(w http.ResponseWriter, respBody []byte, sourceFormat, targetFormat string) {
	if !translator.NeedsTranslation(sourceFormat, targetFormat) {
		_, _ = w.Write(respBody) //nolint:errcheck // handler write
		return
	}

	var respMap map[string]any
	if json.Unmarshal(respBody, &respMap) == nil {
		translated := translator.DefaultRegistry.TranslateResponse(targetFormat, sourceFormat, respMap, concerns.NewResponseState())
		if len(translated) > 0 {
			j, _ := json.Marshal(translated[0]) //nolint:errcheck // best-effort marshal
			_, _ = w.Write(j)                   //nolint:errcheck // handler write

			return
		}
	}

	_, _ = w.Write(respBody) //nolint:errcheck // handler write
}

func recordUsageSinkDirect(st *store.Store, providerID, modelName, connID string, prompt, completion, statusCode int, usageSink UsageSink) {
	if prompt == 0 && completion == 0 {
		return
	}

	if st != nil {
		_ = st.InsertUsage(providerID, modelName, prompt, completion, connID) //nolint:errcheck // best-effort usage insert
	}

	if usageSink != nil {
		usageSink.OnUsage(providerID, modelName, connID, prompt, completion, statusCode)
	}
}

func recordUsageSink(st *store.Store, respBody []byte, providerID, modelName, connID string, statusCode int, usageSink UsageSink) {
	var rm map[string]any
	if json.Unmarshal(respBody, &rm) != nil {
		return
	}

	usage, ok := rm["usage"].(map[string]any)
	if !ok {
		return
	}

	prompt, _ := usage["prompt_tokens"].(float64)         //nolint:errcheck // optional type assertion
	completion, _ := usage["completion_tokens"].(float64) //nolint:errcheck // optional type assertion
	recordUsageSinkDirect(st, providerID, modelName, connID, int(prompt), int(completion), statusCode, usageSink)
}

func resolveChatExecutor(exec executor.Executor, providerID string) executor.Executor {
	if exec == nil || executor.HasSpecializedExecutor(providerID) {
		return executor.GetExecutor(providerID)
	}

	return exec
}

func getFallbackConn(fb *fallback.Fallback, providerID, strategy string, stickyLimit int, excludeIDs map[string]bool) *store.Connection {
	conn, _ := fb.SelectAccountWithStrategy(providerID, strategy, stickyLimit, excludeIDs) //nolint:errcheck // error handled via nil check
	if conn != nil {
		return conn
	}

	pDef := provider.GetProvider(providerID)
	if pDef == nil {
		pDef = provider.GetProviderByAlias(providerID)
	}

	if pDef != nil && (pDef.Category == "free" || pDef.HasFree || providerID == "opencode" || providerID == "mimo-free") && len(excludeIDs) == 0 {
		return &store.Connection{
			ID:                   "synthetic-" + providerID,
			Provider:             providerID,
			IsActive:             true,
			BaseURL:              pDef.Transport.BaseURL,
			AuthType:             "",
			Name:                 "",
			APIKey:               "",
			AccessToken:          "",
			RefreshToken:         "",
			ExpiresAt:            "",
			RateLimitedUntil:     "",
			LastError:            "",
			LastUsedAt:           "",
			ConsecutiveUseCount:  0,
			Priority:             0,
			TestStatus:           "",
			ProviderSpecificData: nil,
		}
	}

	return nil
}

func handleWithFallback(ctx context.Context, w http.ResponseWriter, body []byte, providerID, modelName string, st *store.Store, exec executor.Executor, fb *fallback.Fallback, streamReq bool, sourceFormat string, ts rtk.TokenSaverOptions, strategy string, stickyLimit int, usageSink UsageSink) error {
	excludeIDs := make(map[string]bool)

	var lastErr error

	var lastStatus int

	var lastBody []byte

	targetFormat := getTargetFormat(providerID)

	for {
		conn := getFallbackConn(fb, providerID, strategy, stickyLimit, excludeIDs)
		if conn == nil {
			return handleFallbackExhausted(w, providerID, excludeIDs, lastStatus, lastBody, lastErr)
		}

		conn = ensureFreshConn(ctx, st, conn)
		payload := prepareChatPayload(body, providerID, modelName, streamReq, sourceFormat, targetFormat, conn, ts)
		ex := resolveChatExecutor(exec, providerID)

		res, err := ex.Execute(ctx, connCredentials(conn), modelName, payload, streamReq)
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

		defer res.Body.Close() //nolint:errcheck // best-effort body close

		if res.StatusCode >= 400 {
			respBody, _ := io.ReadAll(res.Body) //nolint:errcheck // best-effort read
			lastStatus, lastBody = res.StatusCode, respBody

			resetsAtMs := config.ParseResetDelayFromError(res.Header, string(respBody))
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, res.StatusCode, string(respBody), resetsAtMs)

			if shouldFallback {
				excludeIDs[conn.ID] = true
				lastErr = fmt.Errorf("%w: status %d", errUpstreamFailed, res.StatusCode)

				continue
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(res.StatusCode)
			_, _ = w.Write(respBody) //nolint:errcheck // handler write

			return fmt.Errorf("%w: status %d", errUpstreamFailed, res.StatusCode)
		}

		completeChatResponse(ctx, w, res, conn.ID, providerID, modelName, st, fb, streamReq, sourceFormat, targetFormat, body, usageSink)

		return nil
	}
}

func handleFallbackExhausted(w http.ResponseWriter, providerID string, excludeIDs map[string]bool, lastStatus int, lastBody []byte, lastErr error) error {
	if len(excludeIDs) == 0 {
		http.Error(w, `{"error":"no active connection for provider `+providerID+`"}`, http.StatusBadRequest)
		return nil
	}

	if lastStatus >= 400 && len(lastBody) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(lastStatus)
		_, _ = w.Write(lastBody) //nolint:errcheck // handler write

		return lastErr
	}

	http.Error(w, `{"error":"all accounts unavailable"}`, http.StatusServiceUnavailable)

	return lastErr
}

func pipeRawStreamWithUsage(w http.ResponseWriter, flusher http.Flusher, body io.Reader, reqBody []byte) usage.ExtractedUsage {
	scanner := bufio.NewScanner(body)
	finalUsage := usage.ExtractedUsage{
		PromptTokens:     0,
		CompletionTokens: 0,
		HasUsage:         false,
	}
	contentLen := 0

	for scanner.Scan() {
		line := scanner.Text()

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if payloadBytes, ok := usage.ParseSSELinePayload(line); ok {
			var chunk map[string]any
			if json.Unmarshal(payloadBytes, &chunk) == nil {
				if u, ok := usage.ExtractUsageFromChunk(chunk); ok {
					if u.PromptTokens > finalUsage.PromptTokens {
						finalUsage.PromptTokens = u.PromptTokens
					}

					if u.CompletionTokens > finalUsage.CompletionTokens {
						finalUsage.CompletionTokens = u.CompletionTokens
					}

					finalUsage.HasUsage = true
				}

				contentLen += usage.ExtractContentLengthFromChunk(chunk)
			}
		}

		_, _ = w.Write([]byte(line + "\n\n")) //nolint:errcheck // stream write

		if flusher != nil {
			flusher.Flush()
		}
	}

	if !finalUsage.HasUsage || (finalUsage.PromptTokens == 0 && finalUsage.CompletionTokens == 0) {
		finalUsage.PromptTokens = usage.EstimateInputTokens(reqBody)
		finalUsage.CompletionTokens = usage.EstimateOutputTokens(contentLen)
		finalUsage.HasUsage = true
	}

	return finalUsage
}

func completeChatResponse(_ context.Context, w http.ResponseWriter, res *executor.Result, connID, providerID, modelName string, st *store.Store, fb *fallback.Fallback, streamReq bool, sourceFormat, targetFormat string, reqBody []byte, usageSink UsageSink) {
	if streamReq {
		stream.WriteSSEHeaders(w)
		flusher, _ := w.(http.Flusher) //nolint:errcheck // optional flusher assertion

		var extracted usage.ExtractedUsage
		if translator.NeedsTranslation(sourceFormat, targetFormat) {
			extracted = writeTranslatedStream(w, flusher, res.Body, sourceFormat, targetFormat, reqBody)
		} else {
			extracted = pipeRawStreamWithUsage(w, flusher, res.Body, reqBody)
		}

		fb.ClearError(connID)
		recordUsageSinkDirect(st, providerID, modelName, connID, extracted.PromptTokens, extracted.CompletionTokens, res.StatusCode, usageSink)

		return
	}

	respBody, _ := io.ReadAll(res.Body) //nolint:errcheck // best-effort read

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	fb.ClearError(connID)

	writeTranslatedNonStream(w, respBody, sourceFormat, targetFormat)
	recordUsageSink(st, respBody, providerID, modelName, connID, res.StatusCode, usageSink)
}

func prepareStreamModelPayload(body []byte, providerID, modelName, sourceFormat, targetFormat string, conn *store.Connection, ts rtk.TokenSaverOptions) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		m = make(map[string]any)
	}

	stripContinuityFields(m)

	m["model"] = providerID + "/" + modelName
	m["stream"] = true

	var translatedBody map[string]any
	if translator.NeedsTranslation(sourceFormat, targetFormat) {
		translatedBody = translator.DefaultRegistry.TranslateRequest(sourceFormat, targetFormat, m, translator.TranslateOptions{
			Model:      modelName,
			Stream:     true,
			Provider:   providerID,
			ClientTool: false,
			StripList:  nil,
			Credentials: map[string]any{
				"apiKey":               conn.APIKey,
				"accessToken":          firstNonEmpty(conn.AccessToken, conn.APIKey),
				"refreshToken":         conn.RefreshToken,
				"providerSpecificData": conn.ProviderSpecificData,
			},
			ConnectionID: conn.ID,
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

	payload, _ := json.Marshal(translatedBody) //nolint:errcheck // best-effort marshal

	return payload
}

func streamModel(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, body []byte, providerID, modelName string, st *store.Store, exec executor.Executor, fb *fallback.Fallback, sourceFormat string, ts rtk.TokenSaverOptions, usageSink UsageSink) error {
	excludeIDs := make(map[string]bool)
	targetFormat := getTargetFormat(providerID)

	for {
		conn, _ := fb.SelectAccountWithStrategy(providerID, "", 0, excludeIDs) //nolint:errcheck // error handled via nil check
		if conn == nil {
			return errNoAccount
		}

		conn = ensureFreshConn(ctx, st, conn)
		payload := prepareStreamModelPayload(body, providerID, modelName, sourceFormat, targetFormat, conn, ts)
		ex := resolveChatExecutor(exec, providerID)

		res, err := ex.Execute(ctx, connCredentials(conn), modelName, payload, true)
		if err != nil {
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, 502, err.Error(), 0)
			if !shouldFallback {
				return err
			}

			excludeIDs[conn.ID] = true

			continue
		}

		defer res.Body.Close() //nolint:errcheck // best-effort body close

		if res.StatusCode >= 400 {
			respBody, _ := io.ReadAll(res.Body) //nolint:errcheck // best-effort read

			resetsAtMs := config.ParseResetDelayFromError(res.Header, string(respBody))
			shouldFallback, _ := fb.MarkUnavailable(conn.ID, res.StatusCode, string(respBody), resetsAtMs)

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

		var extracted usage.ExtractedUsage
		if translator.NeedsTranslation(sourceFormat, targetFormat) {
			extracted = writeTranslatedStream(w, nil, res.Body, sourceFormat, targetFormat, body)
		} else {
			extracted = pipeRawStreamWithUsage(w, flusher, res.Body, body)
		}

		recordUsageSinkDirect(st, providerID, modelName, conn.ID, extracted.PromptTokens, extracted.CompletionTokens, res.StatusCode, usageSink)

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
		ProjectID:            "",
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

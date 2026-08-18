package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/provider"
	"flamerouter/internal/store"
	"flamerouter/internal/translator"
	"net/http"
)

// Responses handles POST /v1/responses requests.
func Responses(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	return ResponsesWithOptions(ctx, w, body, st, exec, fb, nil)
}

// ResponsesWithOptions handles POST /v1/responses requests with an optional UsageSink.
func ResponsesWithOptions(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback, usageSink UsageSink) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	converted := convertResponsesToChat(m)
	payload, _ := json.Marshal(converted) //nolint:errcheck // safe internal map marshal

	sourceFormat := translator.FormatOpenAIResponses

	return handleResponsesChat(ctx, w, payload, st, exec, fb, sourceFormat, usageSink)
}

// CompactResponses handles POST /v1/responses/compact.
// Parity: same pipeline as Responses with body._compact=true.
func CompactResponses(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	return CompactResponsesWithOptions(ctx, w, body, st, exec, fb, nil)
}

// CompactResponsesWithOptions handles POST /v1/responses/compact with an optional UsageSink.
func CompactResponsesWithOptions(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback, usageSink UsageSink) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	m["_compact"] = true
	payload, _ := json.Marshal(m) //nolint:errcheck // safe internal map marshal

	return ResponsesWithOptions(ctx, w, payload, st, exec, fb, usageSink)
}

func convertResponsesToChat(body map[string]any) map[string]any {
	result := make(map[string]any)

	if modelVal, ok := body["model"]; ok {
		result["model"] = modelVal
	}

	result["stream"] = false
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	}

	result["messages"] = extractResponsesMessages(body)

	applyResponsesOptionalFields(result, body)

	return result
}

func extractResponsesMessages(body map[string]any) []any {
	var messages []any

	if input, ok := body["input"]; ok {
		messages = parseInputMessages(input)
	}

	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages = append([]any{map[string]any{
			"role":    "system",
			"content": instructions,
		}}, messages...)
	}

	return messages
}

func parseInputMessages(input any) []any {
	var messages []any

	if inputArr, ok := input.([]any); ok {
		for _, item := range inputArr {
			if msg, ok := item.(map[string]any); ok {
				role, _ := msg["role"].(string) //nolint:errcheck // optional type assertion
				messages = append(messages, map[string]any{
					"role":    role,
					"content": msg["content"],
				})
			}
		}

		return messages
	}

	if inputStr, ok := input.(string); ok && inputStr != "" {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": inputStr,
		})
	}

	return messages
}

func applyResponsesOptionalFields(result, body map[string]any) {
	for _, key := range []string{"temperature", "top_p", "tools", "tool_choice", "response_format"} {
		if val, ok := body[key]; ok {
			result[key] = val
		}
	}

	if maxTokens, ok := body["max_output_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}

	if streamOpts, ok := body["stream_options"]; ok {
		result["stream_options"] = streamOpts
	}
}

func handleResponsesChat(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback, sourceFormat string, usageSink UsageSink) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	modelStr, _ := m["model"].(string) //nolint:errcheck // optional type assertion
	streamReq, _ := m["stream"].(bool) //nolint:errcheck // optional type assertion

	ts := LoadTokenSaverFromStore(st)
	combo, _ := st.GetComboByName(modelStr) //nolint:errcheck // optional combo

	if combo != nil && len(combo.Models) > 0 {
		return handleCombo(ctx, w, body, combo, st, exec, fb, streamReq, sourceFormat, ts)
	}

	aliases, _ := st.ListAliases() //nolint:errcheck // optional alias list
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

	providerID := model.ResolveProviderAlias(mref.Provider, provider.Aliases())

	return handleWithFallback(ctx, w, body, providerID, mref.Model, st, exec, fb, streamReq, sourceFormat, ts, "", 0, usageSink)
}

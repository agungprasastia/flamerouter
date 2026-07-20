package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/store"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
)

func Responses(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	converted := convertResponsesToChat(m)
	payload, _ := json.Marshal(converted)

	sourceFormat := translator.FormatOpenAIResponses

	return handleResponsesChat(ctx, w, payload, st, exec, fb, sourceFormat)
}

// CompactResponses handles POST /v1/responses/compact.
// Parity: same pipeline as Responses with body._compact=true.
func CompactResponses(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}
	m["_compact"] = true
	payload, _ := json.Marshal(m)
	return Responses(ctx, w, payload, st, exec, fb)
}

func convertResponsesToChat(body map[string]any) map[string]any {
	result := make(map[string]any)

	if model, ok := body["model"]; ok {
		result["model"] = model
	}

	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	} else {
		result["stream"] = false
	}

	if input, ok := body["input"]; ok {
		if inputArr, ok := input.([]any); ok {
			var messages []any
			for _, item := range inputArr {
				if msg, ok := item.(map[string]any); ok {
					role, _ := msg["role"].(string)
					content, _ := msg["content"]
					messages = append(messages, map[string]any{
						"role":    role,
						"content": content,
					})
				}
			}
			result["messages"] = messages
		}
	}

	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages, _ := result["messages"].([]any)
		messages = append([]any{map[string]any{
			"role":    "system",
			"content": instructions,
		}}, messages...)
		result["messages"] = messages
	}

	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}
	if maxTokens, ok := body["max_output_tokens"]; ok {
		result["max_tokens"] = maxTokens
	}

	return result
}

func handleResponsesChat(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback, sourceFormat string) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	modelStr, _ := m["model"].(string)
	streamReq, _ := m["stream"].(bool)

	ts := LoadTokenSaverFromStore(st)
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
	providerID := model.ResolveProviderAlias(mref.Provider, nil)

	return handleWithFallback(ctx, w, body, providerID, mref.Model, st, exec, fb, streamReq, sourceFormat, ts, "", 0, nil)
}

func translateResponsesChunk(chunk map[string]any, state *concerns.ResponseState) map[string]any {
	item := map[string]any{
		"type": "response.output_item.added",
		"output_index": 0,
		"item": map[string]any{
			"id":    state.MessageID,
			"type":  "message",
			"role":  "assistant",
			"status": "in_progress",
			"content": []any{},
		},
	}
	_ = chunk
	_ = item
	return nil
}

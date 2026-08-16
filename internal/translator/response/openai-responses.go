package response

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"strconv"
	"time"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatResponses, nil, openaiToResponsesChunk)
	translator.Register(translator.FormatOpenAI, translator.FormatOpenAIResponses, nil, openaiToResponsesChunk)
}

func openaiToResponsesChunk(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	ensureResponsesState(state)

	if !state.ResponsesStarted {
		state.ResponsesStarted = true
		return []map[string]any{buildResponsesEvent("response.created", map[string]any{
			"type":       "response.created",
			"response": map[string]any{
				"id":         state.ResponseId,
				"object":     "response",
				"created_at": state.Created,
				"status":     "in_progress",
			},
		})}
	}

	choice, _ := extractFirstChoiceFromChunk(chunk)
	if choice == nil {
		return nil
	}
	delta, _ := choice["delta"].(map[string]any)
	finishReason, _ := choice["finish_reason"].(string)

	var events []map[string]any

	if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
		if !state.ReasoningStarted {
			state.ReasoningStarted = true
			state.ReasoningId = "rs_" + state.ResponseId + "_" + strconv.Itoa(state.OutputIndex)
			events = append(events, buildResponsesEvent("response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": state.OutputIndex,
				"item": map[string]any{
					"id":      state.ReasoningId,
					"type":    "reasoning",
					"summary": []any{},
				},
			}))
			events = append(events, buildResponsesEvent("response.reasoning_summary_part.added", map[string]any{
				"type":           "response.reasoning_summary_part.added",
				"item_id":        state.ReasoningId,
				"output_index":   state.OutputIndex,
				"summary_index":  0,
				"part": map[string]any{
					"type": "summary_text",
					"text": "",
				},
			}))
		}
		events = append(events, buildResponsesEvent("response.reasoning_summary_text.delta", map[string]any{
			"type":           "response.reasoning_summary_text.delta",
			"item_id":        state.ReasoningId,
			"output_index":   state.OutputIndex,
			"summary_index":  0,
			"delta":          reasoning,
		}))
	}

	if content, ok := delta["content"].(string); ok && content != "" {
		if state.ReasoningStarted && !state.ReasoningDone {
			events = append(events, closeReasoningEvents(state)...)
		}
		if !state.MessageStarted {
			state.MessageStarted = true
			state.MessageId = "msg_" + state.ResponseId + "_" + strconv.Itoa(state.OutputIndex)
			events = append(events, buildResponsesEvent("response.output_item.added", map[string]any{
				"type":         "response.output_item.added",
				"output_index": state.OutputIndex,
				"item": map[string]any{
					"id":      state.MessageId,
					"type":    "message",
					"role":    "assistant",
					"content": []any{},
				},
			}))
			events = append(events, buildResponsesEvent("response.content_part.added", map[string]any{
				"type":           "response.content_part.added",
				"item_id":        state.MessageId,
				"output_index":   state.OutputIndex,
				"content_index":  0,
				"part": map[string]any{
					"type": "output_text",
					"text": "",
				},
			}))
		}
		state.MessageTextBuf += content
		events = append(events, buildResponsesEvent("response.output_text.delta", map[string]any{
			"type":           "response.output_text.delta",
			"item_id":        state.MessageId,
			"output_index":   state.OutputIndex,
			"content_index":  0,
			"delta":          content,
		}))
	}

	if tc, ok := delta["tool_calls"].([]any); ok {
		for _, tcRaw := range tc {
			toolCall, ok := tcRaw.(map[string]any)
			if !ok {
				continue
			}
			idx := 0
			if i, ok := toolCall["index"].(float64); ok {
				idx = int(i)
			}
			id := ""
			if tid, ok := toolCall["id"].(string); ok {
				id = tid
			}
			if id != "" {
				callId := "fc_" + id
				events = append(events, buildResponsesEvent("response.output_item.added", map[string]any{
					"type":         "response.output_item.added",
					"output_index": state.FuncOutputIndex,
					"item": map[string]any{
						"id":      callId,
						"type":    "function_call",
						"name":    "",
						"call_id": id,
					},
				}))
				events = append(events, buildResponsesEvent("response.function_call_arguments.start", map[string]any{
					"type":         "response.function_call_arguments.start",
					"item_id":      callId,
					"output_index": state.FuncOutputIndex,
				}))
				if state.FuncNames == nil {
					state.FuncNames = make(map[int]string)
				}
				if fn, ok := toolCall["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						state.FuncNames[idx] = name
					}
				}
				if state.FuncCallIds == nil {
					state.FuncCallIds = make(map[int]string)
				}
				state.FuncCallIds[idx] = id
			}
			if fn, ok := toolCall["function"].(map[string]any); ok {
				if args, ok := fn["arguments"].(string); ok && args != "" {
					if state.FuncArgsBuf == nil {
						state.FuncArgsBuf = make(map[int]string)
					}
					state.FuncArgsBuf[idx] += args
					events = append(events, buildResponsesEvent("response.function_call_arguments.delta", map[string]any{
						"type":         "response.function_call_arguments.delta",
						"item_id":      "fc_" + state.FuncCallIds[idx],
						"output_index": state.FuncOutputIndex,
						"delta":        args,
					}))
				}
			}
		}
	}

	if finishReason != "" {
		if state.ReasoningStarted && !state.ReasoningDone {
			events = append(events, closeReasoningEvents(state)...)
		}
		if state.MessageStarted {
			events = append(events, closeMessageEvents(state)...)
		}
		for idx := range state.FuncCallIds {
			events = append(events, closeToolCallEvents(state, idx)...)
		}
		events = append(events, buildResponsesEvent("response.completed", map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"id":         state.ResponseId,
				"object":     "response",
				"created_at": state.Created,
				"status":     "completed",
			},
		}))
	}

	if len(events) == 0 {
		return nil
	}
	return events
}

func ensureResponsesState(state *concerns.ResponseState) {
	if state.ResponseCreated {
		return
	}
	state.ResponseCreated = true
	state.ResponseId = "resp_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	state.Created = time.Now().Unix()
	state.OutputIndex = 0
	state.FuncOutputIndex = 1000
	state.ResponsesStarted = false
	state.ReasoningStarted = false
	state.ReasoningDone = false
	state.MessageStarted = false
	state.MessageTextBuf = ""
	state.FuncNames = make(map[int]string)
	state.FuncCallIds = make(map[int]string)
	state.FuncArgsBuf = make(map[int]string)
}

func buildResponsesEvent(eventType string, data map[string]any) map[string]any {
	b, _ := json.Marshal(data)
	return map[string]any{
		"eventType": eventType,
		"data":      string(b),
	}
}

func closeReasoningEvents(state *concerns.ResponseState) []map[string]any {
	state.ReasoningDone = true
	var events []map[string]any
	events = append(events, buildResponsesEvent("response.reasoning_summary_text.done", map[string]any{
		"type":           "response.reasoning_summary_text.done",
		"item_id":        state.ReasoningId,
		"output_index":   state.OutputIndex,
		"summary_index":  0,
	}))
	events = append(events, buildResponsesEvent("response.reasoning_summary_part.done", map[string]any{
		"type":           "response.reasoning_summary_part.done",
		"item_id":        state.ReasoningId,
		"output_index":   state.OutputIndex,
		"summary_index":  0,
	}))
	events = append(events, buildResponsesEvent("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.OutputIndex,
		"item": map[string]any{
			"id":   state.ReasoningId,
			"type": "reasoning",
		},
	}))
	return events
}

func closeMessageEvents(state *concerns.ResponseState) []map[string]any {
	var events []map[string]any
	events = append(events, buildResponsesEvent("response.output_text.done", map[string]any{
		"type":           "response.output_text.done",
		"item_id":        state.MessageId,
		"output_index":   state.OutputIndex,
		"content_index":  0,
		"text":           state.MessageTextBuf,
	}))
	events = append(events, buildResponsesEvent("response.content_part.done", map[string]any{
		"type":           "response.content_part.done",
		"item_id":        state.MessageId,
		"output_index":   state.OutputIndex,
		"content_index":  0,
		"part": map[string]any{
			"type": "output_text",
			"text": state.MessageTextBuf,
		},
	}))
	events = append(events, buildResponsesEvent("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.OutputIndex,
		"item": map[string]any{
			"id":   state.MessageId,
			"type": "message",
			"role": "assistant",
			"content": []any{map[string]any{
				"type": "output_text",
				"text": state.MessageTextBuf,
			}},
		},
	}))
	return events
}

func closeToolCallEvents(state *concerns.ResponseState, idx int) []map[string]any {
	callId, exists := state.FuncCallIds[idx]
	if !exists {
		return nil
	}
	args := state.FuncArgsBuf[idx]
	if args == "" {
		args = "{}"
	}
	fcId := "fc_" + callId
	var events []map[string]any
	events = append(events, buildResponsesEvent("response.function_call_arguments.done", map[string]any{
		"type":         "response.function_call_arguments.done",
		"item_id":      fcId,
		"output_index": state.FuncOutputIndex,
		"arguments":    args,
	}))
	events = append(events, buildResponsesEvent("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.FuncOutputIndex,
		"item": map[string]any{
			"id":        fcId,
			"type":      "function_call",
			"call_id":   callId,
			"name":      state.FuncNames[idx],
			"arguments": args,
		},
	}))
	return events
}

func extractFirstChoiceFromChunk(chunk map[string]any) (map[string]any, bool) {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, false
	}
	choice, ok := choices[0].(map[string]any)
	return choice, ok
}

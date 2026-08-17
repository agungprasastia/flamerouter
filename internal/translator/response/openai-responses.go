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

func handleResponsesReasoning(delta map[string]any, state *concerns.ResponseState) []map[string]any {
	reasoning, ok := delta["reasoning_content"].(string)
	if !ok || reasoning == "" {
		return nil
	}

	var events []map[string]any

	if !state.ReasoningStarted {
		state.ReasoningStarted = true
		state.ReasoningID = "rs_" + state.ResponseID + "_" + strconv.Itoa(state.OutputIndex)
		events = append(events, buildResponsesEvent("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": state.OutputIndex,
			"item": map[string]any{
				"id":      state.ReasoningID,
				"type":    "reasoning",
				"summary": []any{},
			},
		}))
		events = append(events, buildResponsesEvent("response.reasoning_summary_part.added", map[string]any{
			"type":          "response.reasoning_summary_part.added",
			"item_id":       state.ReasoningID,
			"output_index":  state.OutputIndex,
			"summary_index": 0,
			"part": map[string]any{
				"type": "summary_text",
				"text": "",
			},
		}))
	}

	events = append(events, buildResponsesEvent("response.reasoning_summary_text.delta", map[string]any{
		"type":          "response.reasoning_summary_text.delta",
		"item_id":       state.ReasoningID,
		"output_index":  state.OutputIndex,
		"summary_index": 0,
		"delta":         reasoning,
	}))

	return events
}

func handleResponsesContent(delta map[string]any, state *concerns.ResponseState) []map[string]any {
	content, ok := delta["content"].(string)
	if !ok || content == "" {
		return nil
	}

	var events []map[string]any
	if state.ReasoningStarted && !state.ReasoningDone {
		events = append(events, closeReasoningEvents(state)...)
	}

	if !state.MessageStarted {
		state.MessageStarted = true
		state.MessageID = "msg_" + state.ResponseID + "_" + strconv.Itoa(state.OutputIndex)
		events = append(events, buildResponsesEvent("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": state.OutputIndex,
			"item": map[string]any{
				"id":      state.MessageID,
				"type":    "message",
				"role":    "assistant",
				"content": []any{},
			},
		}))
		events = append(events, buildResponsesEvent("response.content_part.added", map[string]any{
			"type":          "response.content_part.added",
			"item_id":       state.MessageID,
			"output_index":  state.OutputIndex,
			"content_index": 0,
			"part": map[string]any{
				"type": "output_text",
				"text": "",
			},
		}))
	}

	state.MessageTextBuf += content
	events = append(events, buildResponsesEvent("response.output_text.delta", map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       state.MessageID,
		"output_index":  state.OutputIndex,
		"content_index": 0,
		"delta":         content,
	}))

	return events
}

func handleResponsesToolCallStart(toolCall map[string]any, idx int, id string, state *concerns.ResponseState) []map[string]any {
	callID := "fc_" + id
	events := []map[string]any{
		buildResponsesEvent("response.output_item.added", map[string]any{
			"type":         "response.output_item.added",
			"output_index": state.FuncOutputIndex,
			"item": map[string]any{
				"id":      callID,
				"type":    "function_call",
				"name":    "",
				"call_id": id,
			},
		}),
		buildResponsesEvent("response.function_call_arguments.start", map[string]any{
			"type":         "response.function_call_arguments.start",
			"item_id":      callID,
			"output_index": state.FuncOutputIndex,
		}),
	}

	if state.FuncNames == nil {
		state.FuncNames = make(map[int]string)
	}

	if fn, ok := toolCall["function"].(map[string]any); ok {
		if name, nOk := fn["name"].(string); nOk {
			state.FuncNames[idx] = name
		}
	}

	if state.FuncCallIDs == nil {
		state.FuncCallIDs = make(map[int]string)
	}

	state.FuncCallIDs[idx] = id

	return events
}

func handleResponsesToolCalls(tc []any, state *concerns.ResponseState) []map[string]any {
	var events []map[string]any

	for _, tcRaw := range tc {
		toolCall, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}

		idx := 0
		if i, iOk := toolCall["index"].(float64); iOk {
			idx = int(i)
		}

		if id, idOk := toolCall["id"].(string); idOk && id != "" {
			events = append(events, handleResponsesToolCallStart(toolCall, idx, id, state)...)
		}

		if fn, fnOk := toolCall["function"].(map[string]any); fnOk {
			if args, argsOk := fn["arguments"].(string); argsOk && args != "" {
				if state.FuncArgsBuf == nil {
					state.FuncArgsBuf = make(map[int]string)
				}

				state.FuncArgsBuf[idx] += args
				events = append(events, buildResponsesEvent("response.function_call_arguments.delta", map[string]any{
					"type":         "response.function_call_arguments.delta",
					"item_id":      "fc_" + state.FuncCallIDs[idx],
					"output_index": state.FuncOutputIndex,
					"delta":        args,
				}))
			}
		}
	}

	return events
}

func handleResponsesFinishReason(_ string, state *concerns.ResponseState) []map[string]any {
	var events []map[string]any

	if state.ReasoningStarted && !state.ReasoningDone {
		events = append(events, closeReasoningEvents(state)...)
	}

	if state.MessageStarted {
		events = append(events, closeMessageEvents(state)...)
	}

	for idx := range state.FuncCallIDs {
		events = append(events, closeToolCallEvents(state, idx)...)
	}

	events = append(events, buildResponsesEvent("response.completed", map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id":         state.ResponseID,
			"object":     "response",
			"created_at": state.Created,
			"status":     "completed",
		},
	}))

	return events
}

func buildResponseCreatedEvent(state *concerns.ResponseState) map[string]any {
	return buildResponsesEvent("response.created", map[string]any{
		"type": "response.created",
		"response": map[string]any{
			"id":         state.ResponseID,
			"object":     "response",
			"created_at": state.Created,
			"status":     "in_progress",
		},
	})
}

func processResponsesChoice(choice map[string]any, state *concerns.ResponseState) []map[string]any {
	var delta map[string]any
	if d, dOk := choice["delta"].(map[string]any); dOk && d != nil {
		delta = d
	} else {
		delta = map[string]any{}
	}

	finishReason := ""
	if fr, frOk := choice["finish_reason"].(string); frOk {
		finishReason = fr
	}

	var events []map[string]any
	events = append(events, handleResponsesReasoning(delta, state)...)
	events = append(events, handleResponsesContent(delta, state)...)

	if tc, tcOk := delta["tool_calls"].([]any); tcOk {
		events = append(events, handleResponsesToolCalls(tc, state)...)
	}

	if finishReason != "" {
		events = append(events, handleResponsesFinishReason(finishReason, state)...)
	}

	return events
}

func openaiToResponsesChunk(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	ensureResponsesState(state)

	if !state.ResponsesStarted {
		state.ResponsesStarted = true

		return []map[string]any{buildResponseCreatedEvent(state)}
	}

	choice, ok := extractFirstChoiceFromChunk(chunk)
	if !ok || choice == nil {
		return nil
	}

	events := processResponsesChoice(choice, state)
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
	state.ResponseID = "resp_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	state.Created = time.Now().Unix()
	state.OutputIndex = 0
	state.FuncOutputIndex = 1000
	state.ResponsesStarted = false
	state.ReasoningStarted = false
	state.ReasoningDone = false
	state.MessageStarted = false
	state.MessageTextBuf = ""
	state.FuncNames = make(map[int]string)
	state.FuncCallIDs = make(map[int]string)
	state.FuncArgsBuf = make(map[int]string)
}

func buildResponsesEvent(eventType string, data map[string]any) map[string]any {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte("{}")
	}

	return map[string]any{
		"eventType": eventType,
		"data":      string(b),
	}
}

func closeReasoningEvents(state *concerns.ResponseState) []map[string]any {
	state.ReasoningDone = true

	var events []map[string]any
	events = append(events, buildResponsesEvent("response.reasoning_summary_text.done", map[string]any{
		"type":          "response.reasoning_summary_text.done",
		"item_id":       state.ReasoningID,
		"output_index":  state.OutputIndex,
		"summary_index": 0,
	}))
	events = append(events, buildResponsesEvent("response.reasoning_summary_part.done", map[string]any{
		"type":          "response.reasoning_summary_part.done",
		"item_id":       state.ReasoningID,
		"output_index":  state.OutputIndex,
		"summary_index": 0,
	}))
	events = append(events, buildResponsesEvent("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.OutputIndex,
		"item": map[string]any{
			"id":   state.ReasoningID,
			"type": "reasoning",
		},
	}))

	return events
}

func closeMessageEvents(state *concerns.ResponseState) []map[string]any {
	var events []map[string]any
	events = append(events, buildResponsesEvent("response.output_text.done", map[string]any{
		"type":          "response.output_text.done",
		"item_id":       state.MessageID,
		"output_index":  state.OutputIndex,
		"content_index": 0,
		"text":          state.MessageTextBuf,
	}))
	events = append(events, buildResponsesEvent("response.content_part.done", map[string]any{
		"type":          "response.content_part.done",
		"item_id":       state.MessageID,
		"output_index":  state.OutputIndex,
		"content_index": 0,
		"part": map[string]any{
			"type": "output_text",
			"text": state.MessageTextBuf,
		},
	}))
	events = append(events, buildResponsesEvent("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.OutputIndex,
		"item": map[string]any{
			"id":   state.MessageID,
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
	callID, exists := state.FuncCallIDs[idx]
	if !exists {
		return nil
	}

	args := state.FuncArgsBuf[idx]
	if args == "" {
		args = "{}"
	}

	fcID := "fc_" + callID

	var events []map[string]any
	events = append(events, buildResponsesEvent("response.function_call_arguments.done", map[string]any{
		"type":         "response.function_call_arguments.done",
		"item_id":      fcID,
		"output_index": state.FuncOutputIndex,
		"arguments":    args,
	}))
	events = append(events, buildResponsesEvent("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.FuncOutputIndex,
		"item": map[string]any{
			"id":        fcID,
			"type":      "function_call",
			"call_id":   callID,
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

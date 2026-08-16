package concerns

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

var toolIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func FallbackToolCallId(index *int) string {
	if index != nil {
		return fmt.Sprintf("call_%d_%d", *index, time.Now().UnixMilli())
	}

	return fmt.Sprintf("call_%d", time.Now().UnixMilli())
}

func GenerateToolCallId(msgIndex, tcIndex int, toolName string) string {
	name := ""

	if toolName != "" {
		safe := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(toolName, "")
		name = "_" + safe
	}

	return fmt.Sprintf("call_msg%d_tc%d%s", msgIndex, tcIndex, name)
}

func sanitizeToolId(id string) string {
	if id == "" {
		return ""
	}

	sanitized := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(id, "")
	if sanitized == "" {
		return ""
	}

	return sanitized
}

func EnsureToolCallIds(body map[string]any) {
	messages, _ := body["messages"].([]any)
	for i, msgRaw := range messages {
		msg, _ := msgRaw.(map[string]any)
		if msg == nil {
			continue
		}

		role, _ := msg["role"].(string)

		if role == "assistant" {
			if toolCalls, ok := msg["tool_calls"].([]any); ok {
				for j, tcRaw := range toolCalls {
					tc, _ := tcRaw.(map[string]any)
					if tc == nil {
						continue
					}

					id, _ := tc["id"].(string)
					if id == "" || !toolIDPattern.MatchString(id) {
						sanitized := sanitizeToolId(id)
						if sanitized != "" {
							tc["id"] = sanitized
						} else {
							name := ""
							if fn, ok := tc["function"].(map[string]any); ok {
								name, _ = fn["name"].(string)
							}

							tc["id"] = GenerateToolCallId(i, j, name)
						}
					}

					if _, ok := tc["type"]; !ok {
						tc["type"] = "function"
					}

					if fn, ok := tc["function"].(map[string]any); ok {
						if args, ok := fn["arguments"]; ok {
							if _, isStr := args.(string); !isStr {
								b, _ := json.Marshal(args)
								fn["arguments"] = string(b)
							}
						}
					}
				}
			}
		}

		if role == "tool" {
			if tcid, ok := msg["tool_call_id"].(string); ok && tcid != "" && !toolIDPattern.MatchString(tcid) {
				sanitized := sanitizeToolId(tcid)
				if sanitized != "" {
					msg["tool_call_id"] = sanitized
				} else {
					msg["tool_call_id"] = GenerateToolCallId(i, 0, "")
				}
			}
		}

		if contentArr, ok := msg["content"].([]any); ok {
			for k, blockRaw := range contentArr {
				block, _ := blockRaw.(map[string]any)
				if block == nil {
					continue
				}

				btype, _ := block["type"].(string)
				if btype == "tool_use" {
					if id, ok := block["id"].(string); ok && id != "" && !toolIDPattern.MatchString(id) {
						sanitized := sanitizeToolId(id)
						if sanitized != "" {
							block["id"] = sanitized
						} else {
							name, _ := block["name"].(string)
							block["id"] = GenerateToolCallId(i, k, name)
						}
					}
				}

				if btype == "tool_result" {
					if tuid, ok := block["tool_use_id"].(string); ok && tuid != "" && !toolIDPattern.MatchString(tuid) {
						sanitized := sanitizeToolId(tuid)
						if sanitized != "" {
							block["tool_use_id"] = sanitized
						} else {
							block["tool_use_id"] = GenerateToolCallId(i, k, "")
						}
					}
				}
			}
		}
	}
}

func GetToolCallIds(msg map[string]any) []string {
	role, _ := msg["role"].(string)
	if role != "assistant" {
		return nil
	}

	var ids []string

	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tcRaw := range toolCalls {
			tc, _ := tcRaw.(map[string]any)
			if tc == nil {
				continue
			}

			if id, ok := tc["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	if contentArr, ok := msg["content"].([]any); ok {
		for _, blockRaw := range contentArr {
			block, _ := blockRaw.(map[string]any)
			if block == nil {
				continue
			}

			if block["type"] == "tool_use" {
				if id, ok := block["id"].(string); ok && id != "" {
					ids = append(ids, id)
				}
			}
		}
	}

	return ids
}

func HasToolResults(msg map[string]any, toolCallIds []string) bool {
	if msg == nil || len(toolCallIds) == 0 {
		return false
	}

	idSet := make(map[string]bool, len(toolCallIds))
	for _, id := range toolCallIds {
		idSet[id] = true
	}

	role, _ := msg["role"].(string)
	if role == "tool" {
		if tcid, ok := msg["tool_call_id"].(string); ok {
			return idSet[tcid]
		}
	}

	if role == "user" {
		if contentArr, ok := msg["content"].([]any); ok {
			for _, blockRaw := range contentArr {
				block, _ := blockRaw.(map[string]any)
				if block == nil {
					continue
				}

				if block["type"] == "tool_result" {
					if tuid, ok := block["tool_use_id"].(string); ok && idSet[tuid] {
						return true
					}
				}
			}
		}
	}

	return false
}

func FixMissingToolResponses(body map[string]any) {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return
	}

	var newMessages []any

	for i := 0; i < len(messages); i++ {
		msg, _ := messages[i].(map[string]any)
		if msg == nil {
			newMessages = append(newMessages, messages[i])
			continue
		}

		newMessages = append(newMessages, messages[i])

		toolCallIds := GetToolCallIds(msg)
		if len(toolCallIds) == 0 {
			continue
		}

		var nextMsg map[string]any
		if i+1 < len(messages) {
			nextMsg, _ = messages[i+1].(map[string]any)
		}

		if nextMsg != nil && HasToolResults(nextMsg, toolCallIds) {
			continue
		}

		for _, id := range toolCallIds {
			newMessages = append(newMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      "",
			})
		}
	}

	body["messages"] = newMessages
}

func StripContentTypes(body map[string]any, stripList []string) {
	if len(stripList) == 0 {
		return
	}

	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return
	}

	imageTypes := map[string]bool{"image_url": true, "image": true}
	audioTypes := map[string]bool{"audio_url": true, "input_audio": true}

	for _, msgRaw := range messages {
		msg, _ := msgRaw.(map[string]any)
		if msg == nil {
			continue
		}

		contentArr, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		var filtered []any

		for _, partRaw := range contentArr {
			part, _ := partRaw.(map[string]any)
			if part == nil {
				filtered = append(filtered, partRaw)
				continue
			}

			ptype, _ := part["type"].(string)
			shouldStrip := false

			if imageTypes[ptype] {
				for _, s := range stripList {
					if s == "image" {
						shouldStrip = true
						break
					}
				}
			}

			if audioTypes[ptype] {
				for _, s := range stripList {
					if s == "audio" {
						shouldStrip = true
						break
					}
				}
			}

			if !shouldStrip {
				filtered = append(filtered, partRaw)
			}
		}

		if len(filtered) == 0 {
			msg["content"] = ""
		} else {
			msg["content"] = filtered
		}
	}
}

func NormalizeThinkingConfig(body map[string]any) {
	if body == nil {
		return
	}

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return
	}

	lastMsg, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		return
	}

	role, _ := lastMsg["role"].(string)
	if role != "user" {
		delete(body, "thinking")
	}
}

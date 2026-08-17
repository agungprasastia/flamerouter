package concerns

import (
	"fmt"
	"regexp"
	"time"
)

var (
	toolIDPattern         = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	nonToolIDCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
)

// FallbackToolCallID produces a fallback ID when one is absent.
func FallbackToolCallID(index *int) string {
	if index != nil {
		return fmt.Sprintf("call_%d_%d", *index, time.Now().UnixMilli())
	}

	return fmt.Sprintf("call_%d", time.Now().UnixMilli())
}

// GenerateToolCallID generates a sanitized unique tool call ID.
func GenerateToolCallID(msgIndex, tcIndex int, toolName string) string {
	name := ""

	if toolName != "" {
		safe := nonToolIDCharsPattern.ReplaceAllString(toolName, "")
		name = "_" + safe
	}

	return fmt.Sprintf("call_msg%d_tc%d%s", msgIndex, tcIndex, name)
}

func sanitizeToolID(id string) string {
	if id == "" {
		return ""
	}

	sanitized := nonToolIDCharsPattern.ReplaceAllString(id, "")
	if sanitized == "" {
		return ""
	}

	return sanitized
}

func sanitizeToolCallEntry(tc map[string]any, i, j int) {
	id, okID := tc["id"].(string)
	if !okID || id == "" || !toolIDPattern.MatchString(id) {
		applySanitizedOrGeneratedToolID(tc, id, i, j)
	}

	if _, ok := tc["type"]; !ok {
		tc["type"] = "function"
	}
}

func applySanitizedOrGeneratedToolID(tc map[string]any, id string, i, j int) {
	sanitized := sanitizeToolID(id)
	if sanitized != "" {
		tc["id"] = sanitized
		return
	}

	name := ""

	if fn, ok := tc["function"].(map[string]any); ok {
		if fnName, ok := fn["name"].(string); ok {
			name = fnName
		}
	}

	tc["id"] = GenerateToolCallID(i, j, name)
}

func sanitizeAssistantBlocks(blocks []any, i int) {
	for k, blockRaw := range blocks {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		if block["type"] != "tool_use" {
			continue
		}

		id, okID := block["id"].(string)
		if okID && id != "" && toolIDPattern.MatchString(id) {
			continue
		}

		sanitized := sanitizeToolID(id)
		if sanitized != "" {
			block["id"] = sanitized
			continue
		}

		name := ""
		if blockName, ok := block["name"].(string); ok {
			name = blockName
		}

		block["id"] = GenerateToolCallID(i, k, name)
	}
}

func sanitizeUserBlocks(blocks []any, i int) {
	for k, blockRaw := range blocks {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		if block["type"] != "tool_result" {
			continue
		}

		tuid, okTUID := block["tool_use_id"].(string)
		if okTUID && tuid != "" && toolIDPattern.MatchString(tuid) {
			continue
		}

		sanitized := sanitizeToolID(tuid)
		if sanitized != "" {
			block["tool_use_id"] = sanitized
		} else {
			block["tool_use_id"] = GenerateToolCallID(i, k, "")
		}
	}
}

// EnsureToolCallIDs ensures all tool calls have valid non-empty IDs.
func EnsureToolCallIDs(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		ensureSingleMsgToolCallIDs(msg, i)
	}
}

func sanitizeAssistantMsg(msg map[string]any, i int) {
	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for j, tcRaw := range toolCalls {
			if tc, ok := tcRaw.(map[string]any); ok && tc != nil {
				sanitizeToolCallEntry(tc, i, j)
			}
		}
	}

	if contentArr, ok := msg["content"].([]any); ok {
		sanitizeAssistantBlocks(contentArr, i)
	}
}

func sanitizeToolMsg(msg map[string]any, i int) {
	tcid, ok := msg["tool_call_id"].(string)
	if !ok || tcid == "" || !toolIDPattern.MatchString(tcid) {
		sanitized := sanitizeToolID(tcid)
		if sanitized != "" {
			msg["tool_call_id"] = sanitized
		} else {
			msg["tool_call_id"] = FallbackToolCallID(&i)
		}
	}
}

func ensureSingleMsgToolCallIDs(msg map[string]any, i int) {
	role, okRole := msg["role"].(string)
	if !okRole {
		return
	}

	switch role {
	case "assistant":
		sanitizeAssistantMsg(msg, i)
	case "tool":
		sanitizeToolMsg(msg, i)
	case "user":
		if contentArr, ok := msg["content"].([]any); ok {
			sanitizeUserBlocks(contentArr, i)
		}
	}
}

// GetToolCallIds retrieves all tool call IDs emitted in an assistant message.
func GetToolCallIds(msg map[string]any) []string {
	role, okRole := msg["role"].(string)
	if !okRole || role != "assistant" {
		return nil
	}

	var ids []string

	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		ids = append(ids, extractToolCallIDsFromCalls(toolCalls)...)
	}

	if contentArr, ok := msg["content"].([]any); ok {
		ids = append(ids, extractToolCallIDsFromContent(contentArr)...)
	}

	return ids
}

func extractToolCallIDsFromCalls(toolCalls []any) []string {
	var ids []string

	for _, tcRaw := range toolCalls {
		tc, ok := tcRaw.(map[string]any)
		if !ok || tc == nil {
			continue
		}

		if id, ok := tc["id"].(string); ok && id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

func extractToolCallIDsFromContent(contentArr []any) []string {
	var ids []string

	for _, blockRaw := range contentArr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		if block["type"] == "tool_use" {
			if id, ok := block["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	return ids
}

func checkUserBlockToolResults(contentArr []any, idSet map[string]bool) bool {
	for _, blockRaw := range contentArr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		if block["type"] == "tool_result" {
			if tuid, ok := block["tool_use_id"].(string); ok && idSet[tuid] {
				return true
			}
		}
	}

	return false
}

// HasToolResults checks if msg contains results answering the given toolCallIds.
func HasToolResults(msg map[string]any, toolCallIds []string) bool {
	if msg == nil || len(toolCallIds) == 0 {
		return false
	}

	idSet := make(map[string]bool, len(toolCallIds))
	for _, id := range toolCallIds {
		idSet[id] = true
	}

	role, okRole := msg["role"].(string)
	if !okRole {
		return false
	}

	if role == "tool" {
		if tcid, ok := msg["tool_call_id"].(string); ok {
			return idSet[tcid]
		}
	}

	if role == "user" {
		if contentArr, ok := msg["content"].([]any); ok {
			return checkUserBlockToolResults(contentArr, idSet)
		}
	}

	return false
}

func getMissingToolCallIDs(messages []any, i int, msg map[string]any) []string {
	toolCallIds := GetToolCallIds(msg)
	if len(toolCallIds) == 0 {
		return nil
	}

	var nextMsg map[string]any

	if i+1 < len(messages) {
		if nm, okNext := messages[i+1].(map[string]any); okNext {
			nextMsg = nm
		}
	}

	if nextMsg != nil && HasToolResults(nextMsg, toolCallIds) {
		return nil
	}

	return toolCallIds
}

// FixMissingToolResponses injects blank tool responses when required by protocol.
func FixMissingToolResponses(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return
	}

	var newMessages []any

	for i := 0; i < len(messages); i++ {
		msg, okMsg := messages[i].(map[string]any)
		if !okMsg || msg == nil {
			newMessages = append(newMessages, messages[i])
			continue
		}

		newMessages = append(newMessages, messages[i])

		missingIDs := getMissingToolCallIDs(messages, i, msg)
		for _, id := range missingIDs {
			newMessages = append(newMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      "",
			})
		}
	}

	body["messages"] = newMessages
}

func isPartStripped(ptype string, stripImage, stripAudio bool, imageTypes, audioTypes map[string]bool) bool {
	if stripImage && imageTypes[ptype] {
		return true
	}

	if stripAudio && audioTypes[ptype] {
		return true
	}

	return false
}

func filterContentParts(contentArr []any, stripImage, stripAudio bool, imageTypes, audioTypes map[string]bool) []any {
	var filtered []any

	for _, partRaw := range contentArr {
		part, ok := partRaw.(map[string]any)
		if !ok || part == nil {
			filtered = append(filtered, partRaw)
			continue
		}

		ptype, okType := part["type"].(string)
		if !okType {
			filtered = append(filtered, partRaw)
			continue
		}

		if !isPartStripped(ptype, stripImage, stripAudio, imageTypes, audioTypes) {
			filtered = append(filtered, partRaw)
		}
	}

	return filtered
}

// StripContentTypes strips unwanted content types (images/audio) from messages.
func StripContentTypes(body map[string]any, stripList []string) {
	if len(stripList) == 0 {
		return
	}

	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return
	}

	imageTypes := map[string]bool{"image_url": true, "image": true}
	audioTypes := map[string]bool{"audio_url": true, "input_audio": true}
	stripImage, stripAudio := resolveStripBooleans(stripList)

	stripMessageContents(messages, stripImage, stripAudio, imageTypes, audioTypes)
}

func resolveStripBooleans(stripList []string) (stripImage, stripAudio bool) {
	for _, s := range stripList {
		if s == "image" {
			stripImage = true
		}

		if s == "audio" {
			stripAudio = true
		}
	}

	return stripImage, stripAudio
}

func stripMessageContents(messages []any, stripImage, stripAudio bool, imageTypes, audioTypes map[string]bool) {
	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		contentArr, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		filtered := filterContentParts(contentArr, stripImage, stripAudio, imageTypes, audioTypes)
		if len(filtered) == 0 {
			msg["content"] = ""
		} else {
			msg["content"] = filtered
		}
	}
}

// NormalizeThinkingConfig cleans up thinking config on non-user ending messages.
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

	role, okRole := lastMsg["role"].(string)
	if !okRole || role != "user" {
		delete(body, "thinking")
	}
}

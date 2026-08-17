package rtk

import (
	"fmt"
	"strings"
)

// Hit represents a compression event with filter and saved byte count.
type Hit struct {
	Shape  string
	Filter string
	Saved  int
}

// Stats collects compression metrics across message items.
type Stats struct {
	Hits        []Hit
	BytesBefore int
	BytesAfter  int
}

func compressFieldStringOrParts(obj map[string]any, fieldKey string, isMatch func(string) bool, stats *Stats, strShape, arrShape string) {
	if s, ok := obj[fieldKey].(string); ok {
		obj[fieldKey] = compressText(s, stats, strShape)
		return
	}

	arr, ok := obj[fieldKey].([]any)
	if !ok {
		return
	}

	for _, p := range arr {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}

		if pt, ok := part["type"].(string); ok && isMatch(pt) {
			if text, ok := part["text"].(string); ok {
				part["text"] = compressText(text, stats, arrShape)
			}
		}
	}
}

func compressFunctionCallOutput(msg map[string]any, stats *Stats) {
	compressFieldStringOrParts(msg, "output", func(t string) bool { return t == "input_text" }, stats, "openai-responses-string", "openai-responses-array")
}

func compressToolRole(msg map[string]any, stats *Stats) bool {
	role, ok := msg["role"].(string)
	if !ok || role != "tool" {
		return false
	}

	if s, contentOk := msg["content"].(string); contentOk {
		msg["content"] = compressText(s, stats, "openai-tool")
		return true
	}

	compressFieldStringOrParts(msg, "content", func(t string) bool { return t == "text" }, stats, "openai-tool", "openai-tool-array")

	return true
}

func compressClaudeBlockContent(block map[string]any, stats *Stats) {
	compressFieldStringOrParts(block, "content", func(t string) bool { return t == "text" }, stats, "claude-string", "claude-array")
}

func compressClaudeContentBlocks(content []any, stats *Stats) {
	for _, blockRaw := range content {
		block, ok := blockRaw.(map[string]any)
		if !ok {
			continue
		}

		if t, ok := block["type"].(string); !ok || t != "tool_result" {
			continue
		}

		if isErr, ok := block["is_error"].(bool); ok && isErr {
			continue
		}

		compressClaudeBlockContent(block, stats)
	}
}

func compressMessageItem(msgRaw any, stats *Stats) {
	msg, ok := msgRaw.(map[string]any)
	if !ok {
		return
	}

	if t, ok := msg["type"].(string); ok && t == "function_call_output" {
		compressFunctionCallOutput(msg, stats)
		return
	}

	if compressToolRole(msg, stats) {
		return
	}

	if content, ok := msg["content"].([]any); ok {
		compressClaudeContentBlocks(content, stats)
	}
}

// CompressMessages compresses tool_result content in-place. Fail-open: returns nil on error.
func CompressMessages(body map[string]any, enabled bool) *Stats {
	if !enabled || body == nil {
		return nil
	}

	defer func() {
		//nolint:errcheck // recovery cleanup
		_ = recover()
	}()

	if body["conversationState"] != nil {
		return compressKiroFormat(body)
	}

	var items []any
	if m, ok := body["messages"].([]any); ok {
		items = m
	} else if m, ok := body["input"].([]any); ok {
		items = m
	} else {
		return nil
	}

	stats := &Stats{
		Hits:        nil,
		BytesBefore: 0,
		BytesAfter:  0,
	}

	for _, msgRaw := range items {
		compressMessageItem(msgRaw, stats)
	}

	return stats
}

func compressKiroToolResult(trRaw any, stats *Stats) {
	tr, ok := trRaw.(map[string]any)
	if !ok {
		return
	}

	if st, statusOk := tr["status"].(string); statusOk && st == "error" {
		return
	}

	content, ok := tr["content"].([]any)
	if !ok {
		return
	}

	for _, p := range content {
		if part, partOk := p.(map[string]any); partOk {
			if text, textOk := part["text"].(string); textOk {
				part["text"] = compressText(text, stats, "kiro-tool-result")
			}
		}
	}
}

func compressKiroMessage(msgRaw any, stats *Stats) {
	msg, ok := msgRaw.(map[string]any)
	if !ok {
		return
	}

	uim, ok := msg["userInputMessage"].(map[string]any)
	if !ok || uim == nil {
		return
	}

	ctx, ok := uim["userInputMessageContext"].(map[string]any)
	if !ok || ctx == nil {
		return
	}

	toolResults, ok := ctx["toolResults"].([]any)
	if !ok {
		return
	}

	for _, trRaw := range toolResults {
		compressKiroToolResult(trRaw, stats)
	}
}

func compressKiroFormat(body map[string]any) *Stats {
	stats := &Stats{
		Hits:        nil,
		BytesBefore: 0,
		BytesAfter:  0,
	}

	state, ok := body["conversationState"].(map[string]any)
	if !ok {
		return stats
	}

	var all []any
	if hist, ok := state["history"].([]any); ok {
		all = append(all, hist...)
	}

	if cm := state["currentMessage"]; cm != nil {
		all = append(all, cm)
	}

	for _, msgRaw := range all {
		compressKiroMessage(msgRaw, stats)
	}

	return stats
}

func compressText(text string, stats *Stats, shape string) string {
	bytesIn := len(text)
	stats.BytesBefore += bytesIn

	if bytesIn < MinCompressSize || bytesIn > RawCap {
		stats.BytesAfter += bytesIn
		return text
	}

	fn, name := AutoDetectFilter(text)
	if fn == nil {
		stats.BytesAfter += bytesIn
		return text
	}

	out := SafeApply(fn, text)
	if out == "" || len(out) >= bytesIn {
		stats.BytesAfter += bytesIn
		return text
	}

	stats.BytesAfter += len(out)
	stats.Hits = append(stats.Hits, Hit{Shape: shape, Filter: name, Saved: bytesIn - len(out)})

	return out
}

// FormatRtkLog formats stats for logging.
func FormatRtkLog(stats *Stats) string {
	if stats == nil || len(stats.Hits) == 0 {
		return ""
	}

	saved := stats.BytesBefore - stats.BytesAfter
	pct := 0.0

	if stats.BytesBefore > 0 {
		pct = float64(saved) / float64(stats.BytesBefore) * 100
	}

	seen := map[string]bool{}

	var filters []string

	for _, h := range stats.Hits {
		if !seen[h.Filter] {
			seen[h.Filter] = true

			filters = append(filters, h.Filter)
		}
	}

	return fmt.Sprintf("[RTK] saved %dB / %dB (%.1f%%) via [%s] hits=%d",
		saved, stats.BytesBefore, pct, strings.Join(filters, ","), len(stats.Hits))
}

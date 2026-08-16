package rtk

import (
	"fmt"
	"strings"
)

type Hit struct {
	Shape  string
	Filter string
	Saved  int
}

type Stats struct {
	Hits        []Hit
	BytesBefore int
	BytesAfter  int
}

// CompressMessages compresses tool_result content in-place. Fail-open: returns nil on error.
func CompressMessages(body map[string]any, enabled bool) *Stats {
	if !enabled || body == nil {
		return nil
	}

	defer func() { recover() }()

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

	stats := &Stats{}

	for _, msgRaw := range items {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		if t, _ := msg["type"].(string); t == "function_call_output" {
			if s, ok := msg["output"].(string); ok {
				msg["output"] = compressText(s, stats, "openai-responses-string")
			} else if arr, ok := msg["output"].([]any); ok {
				for _, p := range arr {
					if part, ok := p.(map[string]any); ok {
						if pt, _ := part["type"].(string); pt == "input_text" {
							if text, ok := part["text"].(string); ok {
								part["text"] = compressText(text, stats, "openai-responses-array")
							}
						}
					}
				}
			}

			continue
		}

		role, _ := msg["role"].(string)
		if role == "tool" {
			if s, ok := msg["content"].(string); ok {
				msg["content"] = compressText(s, stats, "openai-tool")
				continue
			}

			if arr, ok := msg["content"].([]any); ok {
				for _, p := range arr {
					if part, ok := p.(map[string]any); ok {
						if pt, _ := part["type"].(string); pt == "text" {
							if text, ok := part["text"].(string); ok {
								part["text"] = compressText(text, stats, "openai-tool-array")
							}
						}
					}
				}

				continue
			}
		}

		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		for _, blockRaw := range content {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}

			if t, _ := block["type"].(string); t != "tool_result" {
				continue
			}

			if err, _ := block["is_error"].(bool); err {
				continue
			}

			if s, ok := block["content"].(string); ok {
				block["content"] = compressText(s, stats, "claude-string")
			} else if arr, ok := block["content"].([]any); ok {
				for _, p := range arr {
					if part, ok := p.(map[string]any); ok {
						if pt, _ := part["type"].(string); pt == "text" {
							if text, ok := part["text"].(string); ok {
								part["text"] = compressText(text, stats, "claude-array")
							}
						}
					}
				}
			}
		}
	}

	return stats
}

func compressKiroFormat(body map[string]any) *Stats {
	stats := &Stats{}
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
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		uim, _ := msg["userInputMessage"].(map[string]any)
		if uim == nil {
			continue
		}

		ctx, _ := uim["userInputMessageContext"].(map[string]any)
		if ctx == nil {
			continue
		}

		toolResults, _ := ctx["toolResults"].([]any)
		for _, trRaw := range toolResults {
			tr, ok := trRaw.(map[string]any)
			if !ok {
				continue
			}

			if st, _ := tr["status"].(string); st == "error" {
				continue
			}

			content, _ := tr["content"].([]any)
			for _, p := range content {
				if part, ok := p.(map[string]any); ok {
					if text, ok := part["text"].(string); ok {
						part["text"] = compressText(text, stats, "kiro-tool-result")
					}
				}
			}
		}
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

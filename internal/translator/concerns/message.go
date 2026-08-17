package concerns

// CollapseTextParts collapses text parts into a single string when possible.
func CollapseTextParts(content any) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}

	if singleText, ok := extractSingleTextPart(parts); ok {
		return singleText
	}

	return collapseAllTextParts(parts, content)
}

func extractSingleTextPart(parts []any) (string, bool) {
	if len(parts) != 1 {
		return "", false
	}

	block, ok := parts[0].(map[string]any)
	if !ok {
		return "", false
	}

	t, ok := block["type"].(string)
	if !ok || t != "text" {
		return "", false
	}

	text, ok := block["text"].(string)
	if !ok {
		return "", false
	}

	return text, true
}

func collapseAllTextParts(parts []any, fallback any) any {
	texts := make([]string, 0, len(parts))

	for _, part := range parts {
		block, ok := part.(map[string]any)
		if !ok {
			return fallback
		}

		t, ok := block["type"].(string)
		if !ok || t != "text" {
			return fallback
		}

		text, ok := block["text"].(string)
		if !ok {
			return fallback
		}

		texts = append(texts, text)
	}

	if len(texts) == 0 {
		return fallback
	}

	result := ""

	for i, t := range texts {
		if i > 0 {
			result += "\n"
		}

		result += t
	}

	return result
}

// DedupeToolUseBlocks removes duplicate tool_use blocks with identical ids within messages.
func DedupeToolUseBlocks(messages []any) []any {
	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		content, ok := msg["content"]
		if !ok {
			continue
		}

		blocks, ok := content.([]any)
		if !ok {
			continue
		}

		msg["content"] = dedupeBlocks(blocks)
		messages[i] = msg
	}

	return messages
}

func dedupeBlocks(blocks []any) []any {
	seen := make(map[string]bool)
	deduped := make([]any, 0, len(blocks))

	for _, block := range blocks {
		b, ok := block.(map[string]any)
		if !ok {
			deduped = append(deduped, block)
			continue
		}

		btype, ok := b["type"].(string)
		if ok && btype == "tool_use" {
			if id, ok := b["id"].(string); ok && id != "" {
				if seen[id] {
					continue
				}

				seen[id] = true
			}
		}

		deduped = append(deduped, block)
	}

	return deduped
}

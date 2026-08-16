package concerns

func CollapseTextParts(content any) any {
	parts, ok := content.([]any)
	if !ok {
		return content
	}

	if len(parts) == 1 {
		if block, ok := parts[0].(map[string]any); ok {
			if t, ok := block["type"].(string); ok && t == "text" {
				if text, ok := block["text"].(string); ok {
					return text
				}
			}
		}
	}

	var texts []string

	for _, part := range parts {
		if block, ok := part.(map[string]any); ok {
			if t, ok := block["type"].(string); ok && t == "text" {
				if text, ok := block["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
	}

	if len(texts) == len(parts) && len(texts) > 0 {
		result := ""

		for i, t := range texts {
			if i > 0 {
				result += "\n"
			}

			result += t
		}

		return result
	}

	return content
}

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

		seen := make(map[string]bool)

		deduped := make([]any, 0, len(blocks))

		for _, block := range blocks {
			b, ok := block.(map[string]any)
			if !ok {
				deduped = append(deduped, block)
				continue
			}

			btype, _ := b["type"].(string)
			if btype == "tool_use" {
				id, _ := b["id"].(string)
				if id != "" {
					if seen[id] {
						continue
					}

					seen[id] = true
				}
			}

			deduped = append(deduped, block)
		}

		messages[i].(map[string]any)["content"] = deduped
	}

	return messages
}

package rtk

const sep = "\n\n"

// InjectSystemPrompt appends prompt into system message by format (fail-open).
func InjectSystemPrompt(body map[string]any, format, prompt string) {
	if body == nil || prompt == "" {
		return
	}
	switch format {
	case "claude":
		injectClaudeSystem(body, prompt)
	case "gemini", "gemini-cli", "vertex", "antigravity":
		injectGeminiSystem(body, prompt)
	default:
		injectMessagesSystem(body, prompt)
	}
}

func injectMessagesSystem(body map[string]any, prompt string) {
	if s, ok := body["instructions"].(string); ok {
		if s != "" {
			body["instructions"] = s + sep + prompt
		} else {
			body["instructions"] = prompt
		}
		return
	}
	var arr []any
	key := "messages"
	if m, ok := body["messages"].([]any); ok {
		arr = m
	} else if m, ok := body["input"].([]any); ok {
		arr = m
		key = "input"
	} else {
		return
	}
	for _, msgRaw := range arr {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "system" || role == "developer" {
			appendToOpenAIMessage(msg, prompt)
			body[key] = arr
			return
		}
	}
	arr = append([]any{map[string]any{"role": "system", "content": prompt}}, arr...)
	body[key] = arr
}

func appendToOpenAIMessage(msg map[string]any, prompt string) {
	switch c := msg["content"].(type) {
	case string:
		msg["content"] = c + sep + prompt
	case []any:
		msg["content"] = append(c, map[string]any{"type": "input_text", "text": prompt})
	default:
		msg["content"] = prompt
	}
}

func injectClaudeSystem(body map[string]any, prompt string) {
	if s, ok := body["system"].(string); ok && s != "" {
		body["system"] = s + sep + prompt
		return
	}
	if arr, ok := body["system"].([]any); ok {
		block := map[string]any{"type": "text", "text": prompt}
		lastCache := -1
		for i := len(arr) - 1; i >= 0; i-- {
			if m, ok := arr[i].(map[string]any); ok {
				if _, has := m["cache_control"]; has {
					lastCache = i
					break
				}
			}
		}
		if lastCache >= 0 {
			newArr := make([]any, 0, len(arr)+1)
			newArr = append(newArr, arr[:lastCache]...)
			newArr = append(newArr, block)
			newArr = append(newArr, arr[lastCache:]...)
			body["system"] = newArr
		} else {
			body["system"] = append(arr, block)
		}
		return
	}
	body["system"] = prompt
}

func injectGeminiSystem(body map[string]any, prompt string) {
	target := body
	if req, ok := body["request"].(map[string]any); ok {
		target = req
	}
	key := "systemInstruction"
	if _, has := target["system_instruction"]; has {
		key = "system_instruction"
	}
	if sys, ok := target[key].(map[string]any); ok {
		if parts, ok := sys["parts"].([]any); ok {
			sys["parts"] = append(parts, map[string]any{"text": prompt})
			return
		}
	}
	target[key] = map[string]any{"parts": []any{map[string]any{"text": prompt}}}
}

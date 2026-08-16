package combo

// StripHistoryForContext trims history by dropping middle turns if context window exceeds budget.
func StripHistoryForContext(body map[string]any, contextWindow int) map[string]any {
	if body == nil {
		return nil
	}

	key := ""
	if _, ok := body["messages"].([]any); ok {
		key = "messages"
	} else if _, ok := body["input"].([]any); ok {
		key = "input"
	} else if _, ok := body["contents"].([]any); ok {
		key = "contents"
	}

	if key == "" {
		return body
	}

	arr, _ := body[key].([]any)
	if len(arr) == 0 {
		return body
	}

	isSystem := func(r string) bool { return r == "system" || r == "developer" }
	isAssistant := func(r string) bool { return r == "assistant" || r == "model" }

	var systemMsgs []any

	var rest []any

	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		role, _ := m["role"].(string)
		if isSystem(role) {
			systemMsgs = append(systemMsgs, item)
		} else {
			rest = append(rest, item)
		}
	}

	if len(rest) == 0 {
		return body
	}

	i := len(rest) - 1
	for i >= 0 {
		m, ok := rest[i].(map[string]any)
		if ok {
			role, _ := m["role"].(string)
			if isAssistant(role) {
				break
			}
		}

		i--
	}

	tail := rest[i+1:]

	older := rest[:i+1]
	if len(older) == 0 {
		return body
	}

	contentOf := func(m any) any {
		if mp, ok := m.(map[string]any); ok {
			if c, ok := mp["content"]; ok {
				return c
			}

			if p, ok := mp["parts"]; ok {
				return p
			}
		}

		return nil
	}

	cw := contextWindow
	if cw <= 0 {
		cw = 200000
	}

	budgetChars := int(float64(cw) * 0.8 * float64(charsPerToken))

	headKeptCount := headKeep
	if len(older) < headKeptCount {
		headKeptCount = len(older)
	}

	headKept := make([]any, headKeptCount)
	copy(headKept, older[:headKeptCount])

	total := 0
	for _, m := range systemMsgs {
		total += blockLength(contentOf(m))
	}

	for _, m := range headKept {
		total += blockLength(contentOf(m))
	}

	for _, m := range tail {
		total += blockLength(contentOf(m))
	}

	head := headKept
	for total > budgetChars && len(head) > 0 {
		dropped := head[len(head)-1]
		head = head[:len(head)-1]
		total -= blockLength(contentOf(dropped))
	}

	if len(head) == len(older) {
		return body
	}

	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = v
	}

	newArr := make([]any, 0, len(systemMsgs)+len(head)+len(tail))
	newArr = append(newArr, systemMsgs...)
	newArr = append(newArr, head...)
	newArr = append(newArr, tail...)
	out[key] = newArr

	return out
}

const (
	charsPerToken = 4
	headKeep      = 6
)

func blockLength(content any) int {
	switch v := content.(type) {
	case string:
		return len(v)
	case []any:
		sum := 0

		for _, b := range v {
			if m, ok := b.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					sum += len(t)
				} else {
					sum += 50
				}
			}
		}

		return sum
	default:
		return 0
	}
}

package combo

const (
	charsPerToken = 4
	headKeep      = 6
)

func findMessagesKey(body map[string]any) string {
	keys := []string{"messages", "input", "contents"}
	for _, k := range keys {
		if _, ok := body[k].([]any); ok {
			return k
		}
	}

	return ""
}

func isSystemRole(role string) bool {
	return role == "system" || role == "developer"
}

func isAssistantRole(role string) bool {
	return role == "assistant" || role == "model"
}

func splitSystemAndRest(arr []any) ([]any, []any) {
	var (
		sys  []any
		rest []any
	)

	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		role, ok := m["role"].(string)
		if ok && isSystemRole(role) {
			sys = append(sys, item)
		} else {
			rest = append(rest, item)
		}
	}

	return sys, rest
}

func findLastAssistantIndex(rest []any) int {
	for i := len(rest) - 1; i >= 0; i-- {
		m, ok := rest[i].(map[string]any)
		if !ok {
			continue
		}

		role, ok := m["role"].(string)
		if ok && isAssistantRole(role) {
			return i
		}
	}

	return -1
}

func contentOf(m any) any {
	mp, ok := m.(map[string]any)
	if !ok {
		return nil
	}

	if c, ok := mp["content"]; ok {
		return c
	}

	if p, ok := mp["parts"]; ok {
		return p
	}

	return nil
}

func sumBlockLengths(items []any) int {
	total := 0
	for _, m := range items {
		total += blockLength(contentOf(m))
	}

	return total
}

func trimHeadToBudget(head []any, total, budgetChars int) []any {
	for total > budgetChars && len(head) > 0 {
		dropped := head[len(head)-1]
		head = head[:len(head)-1]
		total -= blockLength(contentOf(dropped))
	}

	return head
}

// StripHistoryForContext trims history by dropping middle turns if context window exceeds budget.
func StripHistoryForContext(body map[string]any, contextWindow int) map[string]any {
	if body == nil {
		return nil
	}

	key := findMessagesKey(body)
	if key == "" {
		return body
	}

	arr, ok := body[key].([]any)
	if !ok || len(arr) == 0 {
		return body
	}

	systemMsgs, rest := splitSystemAndRest(arr)
	if len(rest) == 0 {
		return body
	}

	lastIdx := findLastAssistantIndex(rest)
	tail := rest[lastIdx+1:]
	older := rest[:lastIdx+1]

	if len(older) == 0 {
		return body
	}

	cw := contextWindow
	if cw <= 0 {
		cw = 200000
	}

	budgetChars := int(float64(cw) * 0.8 * float64(charsPerToken))
	headKeptCount := min(len(older), headKeep)
	headKept := make([]any, headKeptCount)
	copy(headKept, older[:headKeptCount])

	total := sumBlockLengths(systemMsgs) + sumBlockLengths(headKept) + sumBlockLengths(tail)
	head := trimHeadToBudget(headKept, total, budgetChars)

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

package rtk

// SafeApply runs filter; on panic/error returns original text (fail-open).
func SafeApply(fn func(string) string, text string) (out string) {
	if fn == nil {
		return text
	}
	defer func() {
		if r := recover(); r != nil {
			out = text
		}
	}()
	result := fn(text)
	if result == "" && text != "" {
		// empty result is valid compression; keep it
	}
	return result
}

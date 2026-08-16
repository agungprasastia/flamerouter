package response

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
)

func init() {
	translator.Register(translator.FormatCursor, translator.FormatOpenAI, nil, cursorToOpenAIResponse)
}

func cursorToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	if obj, ok := chunk["object"].(string); ok {
		if obj == "chat.completion.chunk" || obj == "chat.completion" {
			if _, ok := chunk["choices"].([]any); ok {
				return []map[string]any{chunk}
			}
		}
	}

	return []map[string]any{chunk}
}

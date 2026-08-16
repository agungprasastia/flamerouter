package request

import (
	"flamerouter/internal/translator"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatVertex, openaiToVertexRequest, nil)
}

func openaiToVertexRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	gemini := openaiToGeminiRequest(model, body, stream, credentials)
	return postProcessForVertex(gemini)
}

func postProcessForVertex(body map[string]any) map[string]any {
	if body == nil {
		return body
	}

	contents, ok := body["contents"].([]any)
	if !ok {
		return body
	}

	for _, turnRaw := range contents {
		turn, ok := turnRaw.(map[string]any)
		if !ok {
			continue
		}

		parts, ok := turn["parts"].([]any)
		if !ok {
			continue
		}

		for _, partRaw := range parts {
			part, ok := partRaw.(map[string]any)
			if !ok {
				continue
			}

			if _, ok := part["thoughtSignature"]; ok {
				part["thoughtSignature"] = "AVhLS1AfXIFbELnIbBpHb2MqG1GnXz9pGxKmHxOmLzKmHxOmLzKmHxOmLzKmHxOmLzKm"
			}

			if fc, ok := part["functionCall"].(map[string]any); ok {
				delete(fc, "id")
			}

			if fr, ok := part["functionResponse"].(map[string]any); ok {
				delete(fr, "id")
			}
		}
	}

	return body
}

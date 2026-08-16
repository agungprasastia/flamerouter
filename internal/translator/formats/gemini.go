package formats

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"fmt"
	"strings"
	"time"
)

var UnsupportedSchemaConstraints = []string{
	"minLength", "maxLength", "exclusiveMinimum", "exclusiveMaximum",
	"minItems", "maxItems", "format",
	"default", "examples",
	"$schema", "$defs", "definitions", "const", "$ref", "$comment",
	"deprecated", "readOnly", "writeOnly",
	"additionalProperties", "propertyNames", "patternProperties", "enumDescriptions",
	"anyOf", "oneOf", "allOf", "not",
	"dependencies", "dependentSchemas", "dependentRequired",
	"title", "optional", "if", "then", "else", "contentMediaType", "contentEncoding",
	"cornerRadius", "fillColor", "fontFamily", "fontSize", "fontWeight",
	"gap", "padding", "strokeColor", "strokeThickness", "textColor",
}

var DefaultSafetySettings = []map[string]any{
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
}

func ConvertOpenAIContentToParts(content any) []any {
	var parts []any
	switch c := content.(type) {
	case string:
		parts = append(parts, map[string]any{"text": c})
	case []any:
		for _, itemRaw := range c {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}

			t, _ := item["type"].(string)
			switch t {
			case schema.OpenaiBlockText:
				if text, ok := item["text"].(string); ok {
					parts = append(parts, map[string]any{"text": text})
				}
			case schema.OpenaiBlockImageUrl:
				if iu, ok := item["image_url"].(map[string]any); ok {
					u, _ := iu["url"].(string)
					if strings.HasPrefix(u, "data:") {
						if mime, data, err := concerns.ParseDataUri(u); err == nil {
							parts = append(parts, map[string]any{
								"inlineData": map[string]any{"mime_type": mime, "data": encodeB64(data)},
							})
						}
					} else if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
						parts = append(parts, map[string]any{
							"fileData": map[string]any{"fileUri": u, "mimeType": "image/*"},
						})
					}
				}
			case schema.OpenaiBlockInputAudio:
				if ia, ok := item["input_audio"].(map[string]any); ok {
					data, _ := ia["data"].(string)

					format, _ := ia["format"].(string)
					if format == "" {
						format = "wav"
					}

					mime := "audio/" + format
					if format == "mp3" {
						mime = "audio/mpeg"
					}

					parts = append(parts, map[string]any{
						"inlineData": map[string]any{"mime_type": mime, "data": data},
					})
				}
			case schema.OpenaiBlockAudioUrl:
				if au, ok := item["audio_url"].(map[string]any); ok {
					u, _ := au["url"].(string)
					if strings.HasPrefix(u, "data:") {
						if mime, data, err := concerns.ParseDataUri(u); err == nil {
							parts = append(parts, map[string]any{
								"inlineData": map[string]any{"mime_type": mime, "data": encodeB64(data)},
							})
						}
					}
				}
			case schema.OpenaiBlockFile:
				if f, ok := item["file"].(map[string]any); ok {
					fd, _ := f["file_data"].(string)
					if strings.HasPrefix(fd, "data:") {
						if mime, data, err := concerns.ParseDataUri(fd); err == nil {
							parts = append(parts, map[string]any{
								"inlineData": map[string]any{"mime_type": mime, "data": encodeB64(data)},
							})
						}
					}
				}
			}
		}
	}

	return parts
}

func encodeB64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func ExtractTextContent(content any, separator string) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string

		for _, itemRaw := range c {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}

			if t, _ := item["type"].(string); t == schema.OpenaiBlockText {
				if text, ok := item["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}

		return strings.Join(parts, separator)
	}

	return ""
}

func GenerateRequestId() string {
	return "agent-" + randomUUID()
}

func GenerateSessionId() string {
	return randomUUID() + fmt.Sprintf("%d", time.Now().UnixMilli())
}

func GenerateProjectId() string {
	adjectives := []string{"useful", "bright", "swift", "calm", "bold"}
	nouns := []string{"fuze", "wave", "spark", "flow", "core"}
	b := make([]byte, 3)
	rand.Read(b)
	adj := adjectives[int(b[0])%len(adjectives)]
	noun := nouns[int(b[1])%len(nouns)]

	return fmt.Sprintf("%s-%s-%s", adj, noun, hex.EncodeToString(b)[:5])
}

func randomUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func removeUnsupportedKeywords(obj any, keywords map[string]bool) {
	switch v := obj.(type) {
	case map[string]any:
		for k := range v {
			if keywords[k] || strings.HasPrefix(k, "x-") {
				delete(v, k)
				continue
			}

			removeUnsupportedKeywords(v[k], keywords)
		}
	case []any:
		for _, item := range v {
			removeUnsupportedKeywords(item, keywords)
		}
	}
}

func convertConstToEnum(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if c, ok := v["const"]; ok {
			if _, has := v["enum"]; !has {
				v["enum"] = []any{c}
				delete(v, "const")
			}
		}

		for _, child := range v {
			convertConstToEnum(child)
		}
	case []any:
		for _, item := range v {
			convertConstToEnum(item)
		}
	}
}

func convertEnumValuesToStrings(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if enum, ok := v["enum"].([]any); ok {
			strs := make([]any, len(enum))
			for i, e := range enum {
				strs[i] = fmt.Sprint(e)
			}

			v["enum"] = strs
			if _, has := v["type"]; !has {
				v["type"] = "string"
			}
		}

		for _, child := range v {
			convertEnumValuesToStrings(child)
		}
	case []any:
		for _, item := range v {
			convertEnumValuesToStrings(item)
		}
	}
}

func mergeAllOf(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if allOf, ok := v["allOf"].([]any); ok {
			mergedProps := map[string]any{}

			var mergedReq []any

			for _, itemRaw := range allOf {
				item, ok := itemRaw.(map[string]any)
				if !ok {
					continue
				}

				if props, ok := item["properties"].(map[string]any); ok {
					for k, p := range props {
						mergedProps[k] = p
					}
				}

				if req, ok := item["required"].([]any); ok {
					mergedReq = append(mergedReq, req...)
				}
			}

			delete(v, "allOf")

			if len(mergedProps) > 0 {
				if existing, ok := v["properties"].(map[string]any); ok {
					for k, p := range mergedProps {
						existing[k] = p
					}
				} else {
					v["properties"] = mergedProps
				}
			}

			if len(mergedReq) > 0 {
				if existing, ok := v["required"].([]any); ok {
					v["required"] = append(existing, mergedReq...)
				} else {
					v["required"] = mergedReq
				}
			}
		}

		for _, child := range v {
			mergeAllOf(child)
		}
	case []any:
		for _, item := range v {
			mergeAllOf(item)
		}
	}
}

func selectBest(items []any) int {
	bestIdx, bestScore := 0, -1

	for i, itemRaw := range items {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}

		score := 0
		t, _ := item["type"].(string)

		if t == "object" || item["properties"] != nil {
			score = 3
		} else if t == "array" || item["items"] != nil {
			score = 2
		} else if t != "" && t != "null" {
			score = 1
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx
}

func flattenAnyOfOneOf(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		for _, key := range []string{"anyOf", "oneOf"} {
			if arr, ok := v[key].([]any); ok && len(arr) > 0 {
				var nonNull []any

				for _, s := range arr {
					m, ok := s.(map[string]any)
					if !ok {
						continue
					}

					if t, _ := m["type"].(string); t == "null" {
						continue
					}

					nonNull = append(nonNull, s)
				}

				if len(nonNull) > 0 {
					selected, _ := nonNull[selectBest(nonNull)].(map[string]any)

					delete(v, key)

					for k, val := range selected {
						v[k] = val
					}
				}
			}
		}

		for _, child := range v {
			flattenAnyOfOneOf(child)
		}
	case []any:
		for _, item := range v {
			flattenAnyOfOneOf(item)
		}
	}
}

func flattenTypeArrays(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if types, ok := v["type"].([]any); ok {
			var nonNull []string

			for _, t := range types {
				if s, ok := t.(string); ok && s != "null" {
					nonNull = append(nonNull, s)
				}
			}

			if len(nonNull) > 0 {
				v["type"] = nonNull[0]
			} else {
				v["type"] = "string"
			}
		}

		for _, child := range v {
			flattenTypeArrays(child)
		}
	case []any:
		for _, item := range v {
			flattenTypeArrays(item)
		}
	}
}

func ensureObjectType(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if v["properties"] != nil {
			if _, has := v["type"]; !has {
				v["type"] = "object"
			}
		}

		for _, child := range v {
			ensureObjectType(child)
		}
	case []any:
		for _, item := range v {
			ensureObjectType(item)
		}
	}
}

func cleanupRequired(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if req, ok := v["required"].([]any); ok {
			if props, ok := v["properties"].(map[string]any); ok {
				var valid []any

				for _, r := range req {
					if s, ok := r.(string); ok {
						if _, has := props[s]; has {
							valid = append(valid, s)
						}
					}
				}

				if len(valid) == 0 {
					delete(v, "required")
				} else {
					v["required"] = valid
				}
			}
		}

		for _, child := range v {
			cleanupRequired(child)
		}
	case []any:
		for _, item := range v {
			cleanupRequired(item)
		}
	}
}

func addPlaceholders(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if t, _ := v["type"].(string); t == "object" {
			props, _ := v["properties"].(map[string]any)
			if len(props) == 0 {
				v["properties"] = map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "Brief explanation of why you are calling this tool",
					},
				}
				v["required"] = []any{"reason"}
			}
		}

		for _, child := range v {
			addPlaceholders(child)
		}
	case []any:
		for _, item := range v {
			addPlaceholders(item)
		}
	}
}

// CleanJSONSchemaForAntigravity removes unsupported keywords recursively.
func CleanJSONSchemaForAntigravity(schema any) any {
	if schema == nil {
		return schema
	}

	m, ok := schema.(map[string]any)
	if !ok {
		return schema
	}

	convertConstToEnum(m)
	convertEnumValuesToStrings(m)
	mergeAllOf(m)
	flattenAnyOfOneOf(m)
	flattenTypeArrays(m)
	ensureObjectType(m)

	kw := map[string]bool{}
	for _, k := range UnsupportedSchemaConstraints {
		kw[k] = true
	}

	removeUnsupportedKeywords(m, kw)
	cleanupRequired(m)
	addPlaceholders(m)

	return m
}

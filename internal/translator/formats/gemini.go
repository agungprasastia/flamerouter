// Package formats provides message structure adapters and normalization for AI model protocols.
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

// UnsupportedSchemaConstraints list of JSON schema fields unsupported by Gemini/Antigravity.
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

// DefaultSafetySettings returns default lenient safety thresholds for Gemini.
var DefaultSafetySettings = []map[string]any{
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
}

func parseDataURIPart(dataURI string) (map[string]any, bool) {
	if mime, data, err := concerns.ParseDataURI(dataURI); err == nil {
		return map[string]any{
			"inlineData": map[string]any{"mime_type": mime, "data": encodeB64(data)},
		}, true
	}

	return nil, false
}

func convertImageBlock(item map[string]any) map[string]any {
	iu, ok := item["image_url"].(map[string]any)
	if !ok {
		return nil
	}

	u, ok := iu["url"].(string)
	if !ok {
		return nil
	}

	if strings.HasPrefix(u, "data:") {
		if part, ok := parseDataURIPart(u); ok {
			return part
		}
	} else if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return map[string]any{
			"fileData": map[string]any{"fileUri": u, "mimeType": "image/*"},
		}
	}

	return nil
}

func convertAudioBlock(item map[string]any) map[string]any {
	ia, ok := item["input_audio"].(map[string]any)
	if !ok {
		return nil
	}

	data, ok := ia["data"].(string)
	if !ok {
		return nil
	}

	format, okFormat := ia["format"].(string)
	if !okFormat || format == "" {
		format = "wav"
	}

	mime := "audio/" + format
	if format == "mp3" {
		mime = "audio/mpeg"
	}

	return map[string]any{
		"inlineData": map[string]any{"mime_type": mime, "data": data},
	}
}

func convertAudioURLBlock(item map[string]any) map[string]any {
	au, ok := item["audio_url"].(map[string]any)
	if !ok {
		return nil
	}

	u, ok := au["url"].(string)
	if !ok || !strings.HasPrefix(u, "data:") {
		return nil
	}

	if part, ok := parseDataURIPart(u); ok {
		return part
	}

	return nil
}

func convertFileBlock(item map[string]any) map[string]any {
	f, ok := item["file"].(map[string]any)
	if !ok {
		return nil
	}

	fd, ok := f["file_data"].(string)
	if !ok || !strings.HasPrefix(fd, "data:") {
		return nil
	}

	if part, ok := parseDataURIPart(fd); ok {
		return part
	}

	return nil
}

func convertSingleContentItem(item map[string]any) map[string]any {
	t, ok := item["type"].(string)
	if !ok {
		return nil
	}

	switch t {
	case schema.OpenaiBlockText:
		if text, ok := item["text"].(string); ok {
			return map[string]any{"text": text}
		}
	case schema.OpenaiBlockImageURL:
		return convertImageBlock(item)
	case schema.OpenaiBlockInputAudio:
		return convertAudioBlock(item)
	case schema.OpenaiBlockAudioURL:
		return convertAudioURLBlock(item)
	case schema.OpenaiBlockFile:
		return convertFileBlock(item)
	}

	return nil
}

// ConvertOpenAIContentToParts converts OpenAI message content to Gemini parts.
func ConvertOpenAIContentToParts(content any) []any {
	var parts []any
	switch c := content.(type) {
	case string:
		parts = append(parts, map[string]any{"text": c})
	case []any:
		for _, itemRaw := range c {
			if item, ok := itemRaw.(map[string]any); ok {
				if part := convertSingleContentItem(item); part != nil {
					parts = append(parts, part)
				}
			}
		}
	}

	return parts
}

func encodeB64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// ExtractTextContent extracts combined plain text from message content.
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

			if t, ok := item["type"].(string); ok && t == schema.OpenaiBlockText {
				if text, ok := item["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}

		return strings.Join(parts, separator)
	}

	return ""
}

// GenerateRequestID generates a request ID for agent traces.
func GenerateRequestID() string {
	return "agent-" + randomUUID()
}

// GenerateSessionID generates a unique session ID.
func GenerateSessionID() string {
	return randomUUID() + fmt.Sprintf("%d", time.Now().UnixMilli())
}

// GenerateProjectID generates a random readable project identifier.
func GenerateProjectID() string {
	adjectives := []string{"useful", "bright", "swift", "calm", "bold"}
	nouns := []string{"fuze", "wave", "spark", "flow", "core"}
	b := make([]byte, 3)

	if _, err := rand.Read(b); err != nil {
		b = []byte{0x01, 0x02, 0x03}
	}

	adj := adjectives[int(b[0])%len(adjectives)]
	noun := nouns[int(b[1])%len(nouns)]

	return fmt.Sprintf("%s-%s-%s", adj, noun, hex.EncodeToString(b)[:5])
}

func randomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b = make([]byte, 16)
	}

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

func mergeAllOfItems(allOf []any) (map[string]any, []any) {
	mergedProps := map[string]any{}
	mergedReq := make([]any, 0)

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

	return mergedProps, mergedReq
}

func applyMergedAllOf(v map[string]any, mergedProps map[string]any, mergedReq []any) {
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

func mergeAllOf(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		if allOf, ok := v["allOf"].([]any); ok {
			props, req := mergeAllOfItems(allOf)
			applyMergedAllOf(v, props, req)
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

func scoreSchemaType(item map[string]any) int {
	t, ok := item["type"].(string)
	if !ok {
		t = ""
	}

	switch {
	case t == "object" || item["properties"] != nil:
		return 3
	case t == "array" || item["items"] != nil:
		return 2
	case t != "" && t != "null":
		return 1
	default:
		return 0
	}
}

func selectBest(items []any) int {
	bestIdx, bestScore := 0, -1

	for i, itemRaw := range items {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}

		score := scoreSchemaType(item)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx
}

func extractNonNullBranches(arr []any) []any {
	nonNull := make([]any, 0, len(arr))

	for _, s := range arr {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}

		if t, ok := m["type"].(string); ok && t == "null" {
			continue
		}

		nonNull = append(nonNull, s)
	}

	return nonNull
}

func flattenSingleChoice(v map[string]any, key string) {
	arr, ok := v[key].([]any)
	if !ok || len(arr) == 0 {
		return
	}

	nonNull := extractNonNullBranches(arr)
	if len(nonNull) == 0 {
		return
	}

	selected, ok := nonNull[selectBest(nonNull)].(map[string]any)
	if !ok {
		return
	}

	delete(v, key)

	for k, val := range selected {
		v[k] = val
	}
}

func flattenAnyOfOneOf(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		flattenSingleChoice(v, "anyOf")
		flattenSingleChoice(v, "oneOf")

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

func filterValidRequired(req []any, props map[string]any) []any {
	var valid []any

	for _, r := range req {
		if s, ok := r.(string); ok {
			if _, has := props[s]; has {
				valid = append(valid, s)
			}
		}
	}

	return valid
}

func cleanObjectRequired(v map[string]any) {
	req, okReq := v["required"].([]any)
	if !okReq {
		return
	}

	props, okProps := v["properties"].(map[string]any)
	if !okProps {
		return
	}

	valid := filterValidRequired(req, props)
	if len(valid) == 0 {
		delete(v, "required")
	} else {
		v["required"] = valid
	}
}

func cleanupRequired(obj any) {
	switch v := obj.(type) {
	case map[string]any:
		cleanObjectRequired(v)

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
		if t, ok := v["type"].(string); ok && t == "object" {
			if props, ok := v["properties"].(map[string]any); ok && len(props) == 0 {
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

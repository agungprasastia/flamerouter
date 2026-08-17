package concerns

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxImageBytes and FetchTimeoutMS configure remote image prefetching limits.
const (
	MaxImageBytes  = 10 * 1024 * 1024
	FetchTimeoutMS = 10000
)

var blockedHosts = map[string]bool{
	"localhost":                true,
	"127.0.0.1":                true,
	"0.0.0.0":                  true,
	"::1":                      true,
	"169.254.169.254":          true,
	"metadata.google.internal": true,
}

var targetsNeedBase64 = map[string]bool{
	"gemini":      true,
	"gemini-cli":  true,
	"vertex":      true,
	"antigravity": true,
	"ollama":      true,
	"kiro":        true,
}

// PrefetchProviders kept for provider-based callers.
var PrefetchProviders = map[string]bool{
	"gemini":         true,
	"gemini-cli":     true,
	"vertex":         true,
	"vertex-partner": true,
	"antigravity":    true,
	"ollama":         true,
	"ollama-local":   true,
	"kiro":           true,
	"openrouter":     true,
}

// ShouldPrefetchImages checks if the given provider needs remote images pre-fetched.
func ShouldPrefetchImages(provider string) bool {
	return PrefetchProviders[provider]
}

func isRemoteURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

type imageRef struct {
	get         func() string
	set         func(string)
	part        map[string]any
	claudeBlock map[string]any
}

func collectOpenAIBlockImageRefs(block map[string]any) []imageRef {
	t, ok := block["type"].(string)
	if !ok || t != "image_url" {
		return nil
	}

	if s, isStr := block["image_url"].(string); isStr && isRemoteURL(s) {
		b := block

		return []imageRef{{
			get:         func() string { return s },
			set:         func(v string) { b["image_url"] = v },
			part:        nil,
			claudeBlock: nil,
		}}
	}

	return collectOpenAIMapImageRef(block)
}

func collectOpenAIMapImageRef(block map[string]any) []imageRef {
	iu, isMap := block["image_url"].(map[string]any)
	if !isMap {
		return nil
	}

	u, isURL := iu["url"].(string)
	if !isURL || !isRemoteURL(u) {
		return nil
	}

	return []imageRef{{
		get: func() string {
			if m, ok := block["image_url"].(map[string]any); ok {
				if s, ok := m["url"].(string); ok {
					return s
				}
			}

			return ""
		},
		set: func(v string) {
			if m, ok := block["image_url"].(map[string]any); ok {
				m["url"] = v
			}
		},
		part:        nil,
		claudeBlock: nil,
	}}
}

func collectOpenAIImageRefs(messages []any) []imageRef {
	var refs []imageRef

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		for _, blockRaw := range content {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}

			refs = append(refs, collectOpenAIBlockImageRefs(block)...)
		}
	}

	return refs
}

func collectGeminiImageRefs(contents []any) []imageRef {
	var refs []imageRef

	for _, cRaw := range contents {
		c, ok := cRaw.(map[string]any)
		if !ok {
			continue
		}

		parts, ok := c["parts"].([]any)
		if !ok {
			continue
		}

		for _, pRaw := range parts {
			p, ok := pRaw.(map[string]any)
			if !ok {
				continue
			}

			fd, ok := p["fileData"].(map[string]any)
			if !ok {
				continue
			}

			uri, ok := fd["fileUri"].(string)
			if ok && isRemoteURL(uri) {
				part := p
				refs = append(refs, imageRef{
					get:         func() string { return uri },
					set:         nil,
					part:        part,
					claudeBlock: nil,
				})
			}
		}
	}

	return refs
}

func collectClaudeBlockImageRefs(block map[string]any) []imageRef {
	if t, ok := block["type"].(string); !ok || t != "image" {
		return nil
	}

	source, ok := block["source"].(map[string]any)
	if !ok {
		return nil
	}

	if st, ok := source["type"].(string); !ok || st != "url" {
		return nil
	}

	if u, ok := source["url"].(string); ok && isRemoteURL(u) {
		b := block

		return []imageRef{{
			get:         func() string { return u },
			set:         nil,
			part:        nil,
			claudeBlock: b,
		}}
	}

	return nil
}

func collectClaudeImageRefs(messages []any) []imageRef {
	var refs []imageRef

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		for _, blockRaw := range content {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}

			refs = append(refs, collectClaudeBlockImageRefs(block)...)
		}
	}

	return refs
}

func collectGeminiFormatRefs(body map[string]any) []imageRef {
	if contents, ok := body["contents"].([]any); ok {
		return collectGeminiImageRefs(contents)
	}

	return nil
}

func collectAntigravityRefs(body map[string]any) []imageRef {
	if req, ok := body["request"].(map[string]any); ok {
		if contents, ok := req["contents"].([]any); ok {
			return collectGeminiImageRefs(contents)
		}
	}

	return nil
}

func collectImageRefs(body map[string]any, sourceFormat string) []imageRef {
	switch sourceFormat {
	case "openai", "ollama", "kiro", "cursor", "commandcode":
		if messages, ok := body["messages"].([]any); ok {
			return collectOpenAIImageRefs(messages)
		}
	case "claude":
		if messages, ok := body["messages"].([]any); ok {
			return collectClaudeImageRefs(messages)
		}
	case "gemini", "gemini-cli", "vertex":
		return collectGeminiFormatRefs(body)
	case "antigravity":
		return collectAntigravityRefs(body)
	default:
		if messages, ok := body["messages"].([]any); ok {
			return collectOpenAIImageRefs(messages)
		}
	}

	return nil
}

func applyFetchedImageRef(ref imageRef, mime, data string) {
	switch {
	case ref.set != nil:
		ref.set(fmt.Sprintf("data:%s;base64,%s", mime, data))
	case ref.part != nil:
		delete(ref.part, "fileData")
		ref.part["inlineData"] = map[string]any{"mimeType": mime, "data": data}
	case ref.claudeBlock != nil:
		ref.claudeBlock["source"] = map[string]any{
			"type":       "base64",
			"media_type": mime,
			"data":       data,
		}
	}
}

// PrefetchRemoteImages replaces remote image URLs with base64 when target needs inline data.
// Matches 9router: target-format based, multi-source format support.
func PrefetchRemoteImages(body map[string]any, sourceFormat, targetFormat string) int {
	if body == nil || !targetsNeedBase64[targetFormat] {
		return 0
	}

	refs := collectImageRefs(body, sourceFormat)
	if len(refs) == 0 {
		return 0
	}

	converted := 0

	for _, ref := range refs {
		u := ref.get()
		if _, _, err := ParseDataURI(u); err == nil {
			continue
		}

		data, mime, err := fetchImageAsBase64(u)
		if err != nil {
			continue
		}

		applyFetchedImageRef(ref, mime, data)

		converted++
	}

	return converted
}

// PrefetchRemoteImagesByProvider legacy wrapper (provider-based).
func PrefetchRemoteImagesByProvider(body map[string]any, provider string) map[string]any {
	if body == nil || !ShouldPrefetchImages(provider) {
		return body
	}

	target := provider
	if provider == "vertex-partner" {
		target = "vertex"
	}

	if provider == "ollama-local" {
		target = "ollama"
	}

	PrefetchRemoteImages(body, "openai", target)
	PrefetchRemoteImages(body, "claude", target)
	PrefetchRemoteImages(body, "gemini", target)
	PrefetchRemoteImages(body, "antigravity", target)

	return body
}

func validateImageHost(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}

	if blockedHosts[strings.ToLower(host)] {
		return fmt.Errorf("blocked host: %s", host)
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("DNS lookup failed for %s", host)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP blocked: %s", ip.String())
		}
	}

	return nil
}

func executeImageRequest(rawURL string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(FetchTimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "FlameRouter/1.0")

	client := &http.Client{
		Transport: nil,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Jar:     nil,
		Timeout: time.Duration(FetchTimeoutMS) * time.Millisecond,
	}

	return client.Do(req)
}

func readAndValidateImageBody(bodyReader io.Reader) ([]byte, string, error) {
	body, err := io.ReadAll(io.LimitReader(bodyReader, MaxImageBytes+1))
	if err != nil {
		return nil, "", err
	}

	if len(body) > MaxImageBytes {
		return nil, "", fmt.Errorf("image too large")
	}

	mime := detectImageMIME(body)
	if mime == "" {
		return nil, "", fmt.Errorf("not a recognized image")
	}

	return body, mime, nil
}

func fetchImageAsBase64(rawURL string) (string, string, error) {
	if err := validateImageHost(rawURL); err != nil {
		return "", "", err
	}

	resp, err := executeImageRequest(rawURL)
	if err != nil {
		return "", "", err
	}

	if resp == nil || resp.Body == nil {
		return "", "", fmt.Errorf("empty image response")
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, mime, err := readAndValidateImageBody(resp.Body)
	if err != nil {
		return "", "", err
	}

	return base64.StdEncoding.EncodeToString(body), mime, nil
}

func isPrivateIPv4(ip4 net.IP) bool {
	if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return true
	}

	if ip4[0] == 169 && ip4[1] == 254 {
		return true
	}

	return ip4[0] == 0
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}

	if ip4 := ip.To4(); ip4 != nil {
		return isPrivateIPv4(ip4)
	}

	// IPv6 unique-local fc/fd
	if len(ip) == net.IPv6len {
		if ip[0] == 0xfc || ip[0] == 0xfd {
			return true
		}
	}

	return false
}

func isWebp(data []byte) bool {
	if len(data) < 12 {
		return false
	}

	return data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x47 &&
		data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50
}

func isPngOrJpeg(data []byte) string {
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}

	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}

	return ""
}

func isGifOrBmp(data []byte) string {
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return "image/gif"
	}

	if data[0] == 0x42 && data[1] == 0x4D {
		return "image/bmp"
	}

	return ""
}

func isImageMagicMatch(data []byte) string {
	if mime := isPngOrJpeg(data); mime != "" {
		return mime
	}

	return isGifOrBmp(data)
}

func detectImageMIME(data []byte) string {
	if len(data) < 4 {
		return ""
	}

	if mime := isImageMagicMatch(data); mime != "" {
		return mime
	}

	if isWebp(data) {
		return "image/webp"
	}

	return ""
}

// EncodeDataURI formats data bytes into a base64 data URI string.
func EncodeDataURI(mime string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
}

// ParseDataURI parses a data URI string into mime type and byte slice.
func ParseDataURI(dataURI string) (string, []byte, error) {
	if !strings.HasPrefix(dataURI, "data:") {
		return "", nil, fmt.Errorf("not a data URI")
	}

	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid data URI")
	}

	header := parts[0]
	mime := strings.TrimPrefix(header, "data:")
	mime = strings.TrimSuffix(mime, ";base64")

	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, err
	}

	return mime, data, nil
}

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

const (
	MaxImageBytes  = 10 * 1024 * 1024
	FetchTimeoutMS = 10000
)

var blockedHosts = map[string]bool{
	"localhost":                 true,
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

func collectImageRefs(body map[string]any, sourceFormat string) []imageRef {
	var refs []imageRef
	pushOpenAI := func(messages []any) {
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
				if t, _ := block["type"].(string); t != "image_url" {
					continue
				}
				var remote string
				if s, ok := block["image_url"].(string); ok {
					remote = s
					if isRemoteURL(remote) {
						b := block
						refs = append(refs, imageRef{
							get: func() string { return remote },
							set: func(v string) { b["image_url"] = v },
						})
					}
					continue
				}
				if iu, ok := block["image_url"].(map[string]any); ok {
					u, _ := iu["url"].(string)
					if isRemoteURL(u) {
						refs = append(refs, imageRef{
							get: func() string {
								if m, ok := block["image_url"].(map[string]any); ok {
									s, _ := m["url"].(string)
									return s
								}
								return ""
							},
							set: func(v string) {
								if m, ok := block["image_url"].(map[string]any); ok {
									m["url"] = v
								}
							},
						})
					}
				}
			}
		}
	}
	pushGemini := func(contents []any) {
		for _, cRaw := range contents {
			c, ok := cRaw.(map[string]any)
			if !ok {
				continue
			}
			parts, _ := c["parts"].([]any)
			for _, pRaw := range parts {
				p, ok := pRaw.(map[string]any)
				if !ok {
					continue
				}
				fd, ok := p["fileData"].(map[string]any)
				if !ok {
					continue
				}
				uri, _ := fd["fileUri"].(string)
				if isRemoteURL(uri) {
					part := p
					refs = append(refs, imageRef{
						get:  func() string { return uri },
						part: part,
					})
				}
			}
		}
	}

	switch sourceFormat {
	case "openai", "ollama", "kiro", "cursor", "commandcode":
		if messages, ok := body["messages"].([]any); ok {
			pushOpenAI(messages)
		}
	case "claude":
		if messages, ok := body["messages"].([]any); ok {
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
					if t, _ := block["type"].(string); t != "image" {
						continue
					}
					source, ok := block["source"].(map[string]any)
					if !ok {
						continue
					}
					if st, _ := source["type"].(string); st != "url" {
						continue
					}
					u, _ := source["url"].(string)
					if isRemoteURL(u) {
						b := block
						refs = append(refs, imageRef{
							get:         func() string { return u },
							claudeBlock: b,
						})
					}
				}
			}
		}
	case "gemini", "gemini-cli", "vertex":
		if contents, ok := body["contents"].([]any); ok {
			pushGemini(contents)
		}
	case "antigravity":
		if req, ok := body["request"].(map[string]any); ok {
			if contents, ok := req["contents"].([]any); ok {
				pushGemini(contents)
			}
		}
	default:
		if messages, ok := body["messages"].([]any); ok {
			pushOpenAI(messages)
		}
	}
	return refs
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
		if _, _, err := ParseDataUri(u); err == nil {
			continue
		}
		data, mime, err := fetchImageAsBase64(u)
		if err != nil {
			continue
		}
		if ref.set != nil {
			ref.set(fmt.Sprintf("data:%s;base64,%s", mime, data))
		} else if ref.part != nil {
			delete(ref.part, "fileData")
			ref.part["inlineData"] = map[string]any{"mimeType": mime, "data": data}
		} else if ref.claudeBlock != nil {
			ref.claudeBlock["source"] = map[string]any{
				"type":       "base64",
				"media_type": mime,
				"data":       data,
			}
		}
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

func fetchImageAsBase64(rawURL string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", "", fmt.Errorf("empty host")
	}
	if blockedHosts[strings.ToLower(host)] {
		return "", "", fmt.Errorf("blocked host: %s", host)
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", "", fmt.Errorf("DNS lookup failed for %s", host)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return "", "", fmt.Errorf("private IP blocked: %s", ip.String())
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(FetchTimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "FlameRouter/1.0")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// redirect:"manual" equivalent — block all redirects (SSRF)
			return http.ErrUseLastResponse
		},
		Timeout: time.Duration(FetchTimeoutMS) * time.Millisecond,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(body) > MaxImageBytes {
		return "", "", fmt.Errorf("image too large")
	}

	mime := detectImageMIME(body)
	if mime == "" {
		return "", "", fmt.Errorf("not a recognized image")
	}
	return base64.StdEncoding.EncodeToString(body), mime, nil
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// CGNAT 100.64.0.0/10
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		// link-local / cloud metadata 169.254.0.0/16
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		if ip4[0] == 0 {
			return true
		}
	}
	// IPv6 unique-local fc/fd
	if len(ip) == net.IPv6len {
		if ip[0] == 0xfc || ip[0] == 0xfd {
			return true
		}
	}
	return false
}

func detectImageMIME(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	// PNG
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	// JPEG
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// GIF
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x38 {
		return "image/gif"
	}
	// WEBP: RIFF....WEBP
	if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
		if len(data) >= 12 && data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50 {
			return "image/webp"
		}
		return ""
	}
	// BMP
	if data[0] == 0x42 && data[1] == 0x4D {
		return "image/bmp"
	}
	return ""
}

func EncodeDataUri(mime string, data []byte) string {
	return fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
}

func ParseDataUri(dataUri string) (string, []byte, error) {
	if !strings.HasPrefix(dataUri, "data:") {
		return "", nil, fmt.Errorf("not a data URI")
	}
	parts := strings.SplitN(dataUri, ",", 2)
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

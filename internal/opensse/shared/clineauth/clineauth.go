package clineauth

import (
	"runtime"
	"strings"
)

const (
	defaultAppVersion = "3.0.0"
	refererURL        = "https://cline.bot"
	titleHeader       = "Cline"
	clientType        = "vscode"
)

// GetClineAccessToken returns a normalized WorkOS access token.
// If token is empty/whitespace, returns empty string.
// If token doesn't start with "workos:", prepends "workos:".
func GetClineAccessToken(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "workos:") {
		return trimmed
	}
	// If it already has Bearer prefix or something similar, handle or preserve format
	if strings.HasPrefix(strings.ToLower(trimmed), "bearer ") {
		raw := strings.TrimSpace(trimmed[7:])
		if strings.HasPrefix(raw, "workos:") {
			return raw
		}
		return "workos:" + raw
	}
	return "workos:" + trimmed
}

// GetClineAuthorizationHeader returns the Bearer authorization header value for Cline token.
func GetClineAuthorizationHeader(token string) string {
	accessToken := GetClineAccessToken(token)
	if accessToken == "" {
		return ""
	}
	return "Bearer " + accessToken
}

// BuildClineHeaders constructs headers for Cline API requests.
// Defaults:
// - HTTP-Referer: https://cline.bot
// - X-Title: Cline
// - User-Agent: Cline/1.0
// - X-PLATFORM: macos (or runtime OS)
// - X-CLIENT-TYPE: vscode
// - X-CORE-VERSION: 3.0.0
// - X-IS-MULTIROOT: false
// Also forwards / overrides with matching client headers if present in rawHeaders.
func BuildClineHeaders(token string, rawHeaders map[string]string) map[string]string {
	plat := runtime.GOOS
	if plat == "darwin" {
		plat = "macos"
	}

	headers := map[string]string{
		"HTTP-Referer":    refererURL,
		"X-Title":         titleHeader,
		"User-Agent":      "Cline/1.0",
		"X-PLATFORM":      plat,
		"X-CLIENT-TYPE":   clientType,
		"X-CORE-VERSION":  defaultAppVersion,
		"X-IS-MULTIROOT":  "false",
	}

	for k, v := range rawHeaders {
		headers[k] = v
	}

	authHeader := GetClineAuthorizationHeader(token)
	if authHeader != "" {
		headers["Authorization"] = authHeader
	}

	return headers
}

// Package config provides application configuration loading and environment readers.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config represents flamerouter application configuration.
type Config struct {
	SearXNGURL                string
	HeadroomURL               string
	JWTSecret                 string
	InitialPassword           string
	APIKeySecret              string
	MachineIDSalt             string
	ShutdownSecret            string
	BaseURL                   string
	DataDir                   string
	Port                      int
	StreamStallTimeoutMs      int
	StreamFirstChunkTimeoutMs int
	FetchConnectTimeoutMs     int
	VideoFetchTimeoutMs       int
	RequireAPIKey             bool
	TrustProxy                bool
	AuthCookieSecure          bool
}

func resolveDataDir() (string, error) {
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		dataDir = filepath.Join(home, ".flamerouter")
	}

	return dataDir, nil
}

func resolveJWTSecret(dataDir string) (string, error) {
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret != "" {
		return jwtSecret, nil
	}

	cleanPath := filepath.Clean(filepath.Join(dataDir, "jwt-secret"))
	if data, err := os.ReadFile(cleanPath); err == nil && len(strings.TrimSpace(string(data))) > 0 { // #nosec G304
		return strings.TrimSpace(string(data)), nil
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	jwtSecret = hex.EncodeToString(b)

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}

	if err := os.WriteFile(cleanPath, []byte(jwtSecret), 0o600); err != nil {
		return "", err
	}

	return jwtSecret, nil
}

// Load reads application configuration from environment variables and disk.
func Load() (*Config, error) {
	port := 20130

	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("PORT: %w", err)
		}

		port = p
	}

	dataDir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}

	jwtSecret, err := resolveJWTSecret(dataDir)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:                      port,
		DataDir:                   dataDir,
		JWTSecret:                 jwtSecret,
		InitialPassword:           envOr("INITIAL_PASSWORD", "123456"),
		APIKeySecret:              envOr("API_KEY_SECRET", "endpoint-proxy-api-key-secret"),
		MachineIDSalt:             envOr("MACHINE_ID_SALT", "endpoint-proxy-salt"),
		RequireAPIKey:             strings.EqualFold(os.Getenv("REQUIRE_API_KEY"), "true"),
		BaseURL:                   envOr("BASE_URL", fmt.Sprintf("http://localhost:%d", port)),
		SearXNGURL:                envOr("SEARXNG_URL", "http://localhost:8888/search"),
		HeadroomURL:               envOr("HEADROOM_URL", "http://localhost:8787"),
		ShutdownSecret:            strings.TrimSpace(os.Getenv("SHUTDOWN_SECRET")),
		StreamStallTimeoutMs:      envMs("STREAM_STALL_TIMEOUT_MS", 360*1000),
		StreamFirstChunkTimeoutMs: envMs("STREAM_FIRST_CHUNK_TIMEOUT_MS", 200*1000),
		FetchConnectTimeoutMs:     envMs("FETCH_CONNECT_TIMEOUT_MS", 60*1000),
		TrustProxy:                strings.EqualFold(os.Getenv("TRUST_PROXY"), "true"),
		AuthCookieSecure:          strings.EqualFold(os.Getenv("AUTH_COOKIE_SECURE"), "true"),
		VideoFetchTimeoutMs:       envMs("VIDEO_FETCH_TIMEOUT_MS", 120*1000),
	}

	return cfg, nil
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}

	return def
}

// envMs: positive int ms from env, else def (parity 9router runtimeConfig.envMs).
func envMs(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}

	return n
}

// Package-level readers for packages without *Config (parity runtimeConfig.js).

// SEARXNGURL returns the SearXNG search endpoint URL.
func SEARXNGURL() string {
	return envOr("SEARXNG_URL", "http://localhost:8888/search")
}

// HeadroomURL returns the Headroom endpoint URL.
func HeadroomURL() string {
	return envOr("HEADROOM_URL", "http://localhost:8787")
}

// StreamStallTimeout returns the duration before considering a stream stalled.
func StreamStallTimeout() time.Duration {
	return time.Duration(envMs("STREAM_STALL_TIMEOUT_MS", 360*1000)) * time.Millisecond
}

// StreamFirstChunkTimeout returns the duration to wait for the first stream chunk.
func StreamFirstChunkTimeout() time.Duration {
	return time.Duration(envMs("STREAM_FIRST_CHUNK_TIMEOUT_MS", 200*1000)) * time.Millisecond
}

// FetchConnectTimeout returns the timeout for outgoing fetch connections.
func FetchConnectTimeout() time.Duration {
	return time.Duration(envMs("FETCH_CONNECT_TIMEOUT_MS", 60*1000)) * time.Millisecond
}

// VideoFetchTimeout returns the timeout for video fetch operations.
func VideoFetchTimeout() time.Duration {
	return time.Duration(envMs("VIDEO_FETCH_TIMEOUT_MS", 120*1000)) * time.Millisecond
}

// TrustProxy returns true if reverse proxy headers should be trusted.
func TrustProxy() bool {
	return strings.EqualFold(os.Getenv("TRUST_PROXY"), "true")
}

// AuthCookieSecure returns true if auth cookies must be marked Secure.
func AuthCookieSecure() bool {
	return strings.EqualFold(os.Getenv("AUTH_COOKIE_SECURE"), "true")
}

// ShutdownSecret returns the optional secret required for graceful shutdown.
func ShutdownSecret() string {
	return strings.TrimSpace(os.Getenv("SHUTDOWN_SECRET"))
}

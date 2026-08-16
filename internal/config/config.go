package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                      int
	DataDir                   string
	JWTSecret                 string
	InitialPassword           string
	APIKeySecret              string
	MachineIDSalt             string
	RequireAPIKey             bool
	BaseURL                   string
	SearXNGURL                string
	HeadroomURL               string
	ShutdownSecret            string
	StreamStallTimeoutMs      int
	StreamFirstChunkTimeoutMs int
	FetchConnectTimeoutMs     int
	TrustProxy                bool
	AuthCookieSecure          bool
	VideoFetchTimeoutMs       int
}

func Load() (*Config, error) {
	port := 20130
	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("PORT: %w", err)
		}
		port = p
	}

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dataDir = filepath.Join(home, ".flamerouter")
	}

	cfg := &Config{
		Port:                      port,
		DataDir:                   dataDir,
		JWTSecret:                 os.Getenv("JWT_SECRET"),
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
	if cfg.JWTSecret == "" {
		// allow empty only in tests that set it; production should set JWT_SECRET
		cfg.JWTSecret = "change-me-to-a-long-random-secret"
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

func SEARXNGURL() string {
	return envOr("SEARXNG_URL", "http://localhost:8888/search")
}

func HeadroomURL() string {
	return envOr("HEADROOM_URL", "http://localhost:8787")
}

func StreamStallTimeout() time.Duration {
	return time.Duration(envMs("STREAM_STALL_TIMEOUT_MS", 360*1000)) * time.Millisecond
}

func StreamFirstChunkTimeout() time.Duration {
	return time.Duration(envMs("STREAM_FIRST_CHUNK_TIMEOUT_MS", 200*1000)) * time.Millisecond
}

func FetchConnectTimeout() time.Duration {
	return time.Duration(envMs("FETCH_CONNECT_TIMEOUT_MS", 60*1000)) * time.Millisecond
}

func VideoFetchTimeout() time.Duration {
	return time.Duration(envMs("VIDEO_FETCH_TIMEOUT_MS", 120*1000)) * time.Millisecond
}

func TrustProxy() bool {
	return strings.EqualFold(os.Getenv("TRUST_PROXY"), "true")
}

func AuthCookieSecure() bool {
	return strings.EqualFold(os.Getenv("AUTH_COOKIE_SECURE"), "true")
}

func ShutdownSecret() string {
	return strings.TrimSpace(os.Getenv("SHUTDOWN_SECRET"))
}

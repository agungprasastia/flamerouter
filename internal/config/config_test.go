package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"flamerouter/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATA_DIR", "")
	t.Setenv("JWT_SECRET", "test-jwt")
	t.Setenv("API_KEY_SECRET", "test-api-secret")
	// Clear others that might leak from environment
	for _, k := range []string{"INITIAL_PASSWORD", "MACHINE_ID_SALT", "REQUIRE_API_KEY", "BASE_URL"} {
		t.Setenv(k, "")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 20128 {
		t.Fatalf("Port=%d want 20128", cfg.Port)
	}
	if filepath.Base(cfg.DataDir) != ".flamerouter" {
		t.Fatalf("DataDir base=%q want .flamerouter", filepath.Base(cfg.DataDir))
	}
	if cfg.JWTSecret != "test-jwt" {
		t.Fatalf("JWTSecret=%q", cfg.JWTSecret)
	}
	if cfg.APIKeySecret != "test-api-secret" {
		t.Fatalf("APIKeySecret=%q", cfg.APIKeySecret)
	}
	if cfg.RequireAPIKey {
		t.Fatal("RequireAPIKey default false")
	}
}

func TestLoad_PortOverride(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("JWT_SECRET", "j")
	t.Setenv("API_KEY_SECRET", "a")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9999 {
		t.Fatalf("Port=%d", cfg.Port)
	}
	_ = os.Getenv // keep import if needed
}

func TestLoad_EnvParityDefaults(t *testing.T) {
	for _, k := range []string{
		"SEARXNG_URL", "HEADROOM_URL", "SHUTDOWN_SECRET",
		"STREAM_STALL_TIMEOUT_MS", "STREAM_FIRST_CHUNK_TIMEOUT_MS",
		"FETCH_CONNECT_TIMEOUT_MS", "TRUST_PROXY", "AUTH_COOKIE_SECURE",
		"VIDEO_FETCH_TIMEOUT_MS", "PORT", "JWT_SECRET", "API_KEY_SECRET",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("JWT_SECRET", "j")
	t.Setenv("API_KEY_SECRET", "a")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SearXNGURL != "http://localhost:8888/search" {
		t.Fatalf("SearXNGURL=%q", cfg.SearXNGURL)
	}
	if cfg.HeadroomURL != "http://localhost:8787" {
		t.Fatalf("HeadroomURL=%q", cfg.HeadroomURL)
	}
	if cfg.StreamStallTimeoutMs != 360000 {
		t.Fatalf("StreamStallTimeoutMs=%d", cfg.StreamStallTimeoutMs)
	}
	if cfg.StreamFirstChunkTimeoutMs != 200000 {
		t.Fatalf("StreamFirstChunkTimeoutMs=%d", cfg.StreamFirstChunkTimeoutMs)
	}
	if cfg.FetchConnectTimeoutMs != 60000 {
		t.Fatalf("FetchConnectTimeoutMs=%d", cfg.FetchConnectTimeoutMs)
	}
	if cfg.VideoFetchTimeoutMs != 120000 {
		t.Fatalf("VideoFetchTimeoutMs=%d", cfg.VideoFetchTimeoutMs)
	}
	if cfg.TrustProxy || cfg.AuthCookieSecure {
		t.Fatal("TrustProxy/AuthCookieSecure default false")
	}
	if cfg.ShutdownSecret != "" {
		t.Fatal("ShutdownSecret default empty")
	}
}

func TestLoad_EnvParityOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", "j")
	t.Setenv("API_KEY_SECRET", "a")
	t.Setenv("SEARXNG_URL", "http://sx:1/search")
	t.Setenv("HEADROOM_URL", "http://hr:9")
	t.Setenv("SHUTDOWN_SECRET", "sek")
	t.Setenv("STREAM_STALL_TIMEOUT_MS", "1000")
	t.Setenv("STREAM_FIRST_CHUNK_TIMEOUT_MS", "2000")
	t.Setenv("FETCH_CONNECT_TIMEOUT_MS", "3000")
	t.Setenv("VIDEO_FETCH_TIMEOUT_MS", "4000")
	t.Setenv("TRUST_PROXY", "true")
	t.Setenv("AUTH_COOKIE_SECURE", "true")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SearXNGURL != "http://sx:1/search" || cfg.HeadroomURL != "http://hr:9" {
		t.Fatalf("urls: %+v", cfg)
	}
	if cfg.ShutdownSecret != "sek" {
		t.Fatalf("secret=%q", cfg.ShutdownSecret)
	}
	if cfg.StreamStallTimeoutMs != 1000 || cfg.StreamFirstChunkTimeoutMs != 2000 {
		t.Fatalf("stream ms: %d %d", cfg.StreamStallTimeoutMs, cfg.StreamFirstChunkTimeoutMs)
	}
	if cfg.FetchConnectTimeoutMs != 3000 || cfg.VideoFetchTimeoutMs != 4000 {
		t.Fatalf("fetch/video: %d %d", cfg.FetchConnectTimeoutMs, cfg.VideoFetchTimeoutMs)
	}
	if !cfg.TrustProxy || !cfg.AuthCookieSecure {
		t.Fatal("bool flags")
	}
}

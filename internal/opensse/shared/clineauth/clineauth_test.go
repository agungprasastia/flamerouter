package clineauth

import (
	"runtime"
	"testing"
)

func TestGetClineAccessToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "empty token",
			token: "",
			want:  "",
		},
		{
			name:  "whitespace token",
			token: "   ",
			want:  "",
		},
		{
			name:  "already has workos prefix",
			token: "workos:token_12345",
			want:  "workos:token_12345",
		},
		{
			name:  "no workos prefix",
			token: "token_12345",
			want:  "workos:token_12345",
		},
		{
			name:  "bearer prefix with no workos",
			token: "Bearer token_12345",
			want:  "workos:token_12345",
		},
		{
			name:  "bearer prefix with workos",
			token: "Bearer workos:token_12345",
			want:  "workos:token_12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetClineAccessToken(tt.token)
			if got != tt.want {
				t.Fatalf("GetClineAccessToken(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func TestGetClineAuthorizationHeader(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "empty token",
			token: "",
			want:  "",
		},
		{
			name:  "valid token",
			token: "token_123",
			want:  "Bearer workos:token_123",
		},
		{
			name:  "workos token",
			token: "workos:token_123",
			want:  "Bearer workos:token_123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetClineAuthorizationHeader(tt.token)
			if got != tt.want {
				t.Fatalf("GetClineAuthorizationHeader(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func testDefaults(t *testing.T, plat string) {
	t.Helper()

	h := BuildClineHeaders("", nil)
	if h["HTTP-Referer"] != "https://cline.bot" {
		t.Fatalf("HTTP-Referer = %q", h["HTTP-Referer"])
	}

	if h["X-Title"] != "Cline" {
		t.Fatalf("X-Title = %q", h["X-Title"])
	}

	if h["User-Agent"] != "Cline/1.0" {
		t.Fatalf("User-Agent = %q", h["User-Agent"])
	}

	if h["X-PLATFORM"] != plat {
		t.Fatalf("X-PLATFORM = %q, want %q", h["X-PLATFORM"], plat)
	}

	if h["X-CLIENT-TYPE"] != "vscode" {
		t.Fatalf("X-CLIENT-TYPE = %q", h["X-CLIENT-TYPE"])
	}

	if h["X-CORE-VERSION"] != "3.0.0" {
		t.Fatalf("X-CORE-VERSION = %q", h["X-CORE-VERSION"])
	}

	if h["X-IS-MULTIROOT"] != "false" {
		t.Fatalf("X-IS-MULTIROOT = %q", h["X-IS-MULTIROOT"])
	}

	if _, ok := h["Authorization"]; ok {
		t.Fatalf("unexpected Authorization header")
	}
}

func TestBuildClineHeaders(t *testing.T) {
	plat := runtime.GOOS
	if plat == "darwin" {
		plat = "macos"
	}

	t.Run("defaults without rawHeaders and token", func(t *testing.T) {
		testDefaults(t, plat)
	})

	t.Run("with token", func(t *testing.T) {
		h := BuildClineHeaders("abc123xyz", nil)
		if h["Authorization"] != "Bearer workos:abc123xyz" {
			t.Fatalf("Authorization = %q", h["Authorization"])
		}
	})

	t.Run("with rawHeaders overriding and adding", func(t *testing.T) {
		raw := map[string]string{
			"User-Agent":     "CustomCline/2.0",
			"X-Custom-Extra": "value123",
		}

		h := BuildClineHeaders("workos:secret", raw)
		if h["User-Agent"] != "CustomCline/2.0" {
			t.Fatalf("User-Agent = %q, want CustomCline/2.0", h["User-Agent"])
		}

		if h["X-Custom-Extra"] != "value123" {
			t.Fatalf("X-Custom-Extra = %q", h["X-Custom-Extra"])
		}

		if h["Authorization"] != "Bearer workos:secret" {
			t.Fatalf("Authorization = %q", h["Authorization"])
		}

		if h["X-Title"] != "Cline" {
			t.Fatalf("X-Title = %q", h["X-Title"])
		}
	})
}

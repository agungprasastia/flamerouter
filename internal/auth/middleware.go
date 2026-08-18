package auth

import (
	"flamerouter/internal/store"
	"net/http"
	"strings"
)

// DashboardGuard enforces authentication middleware for protected dashboard API routes.
func DashboardGuard(jwt *JWTManager, st *store.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if isPublicPath(path, r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// If requireLogin is explicitly disabled ("false"), allow access.
		// DB export/import stays protected: the archive contains all API keys,
		// OAuth secrets and settings, so it must never be reachable without a
		// session even when dashboard auth is turned off (parity 9router DB route).
		if path != "/api/settings/database" && st != nil {
			if val, err := st.GetSetting("requireLogin"); err == nil && val == "false" {
				next.ServeHTTP(w, r)
				return
			}
		}

		cookie, err := r.Cookie(cookieName)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)

			return
		}

		if _, err = jwt.Validate(cookie.Value); err != nil {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

func isPublicPath(path, method string) bool {
	if path == "/api/health" {
		return true
	}
	// bootstrap: unauth SPA needs requireLogin without session; PATCH stays protected
	if path == "/api/settings/require-login" && method == http.MethodGet {
		return true
	}

	if strings.HasPrefix(path, "/v1/") || path == "/v1" {
		return true
	}
	// auth endpoints public (login/status/logout/oidc); reset-password still needs JWT or loopback in handler
	switch path {
	case "/api/auth/login", "/api/auth/logout", "/api/auth/status", "/api/auth/reset-password",
		"/api/auth/oidc/start", "/api/auth/oidc/callback":
		return true
	}

	return false
}

package auth

import (
	"encoding/json"
	"flamerouter/internal/config"
	"flamerouter/internal/store"
	"net"
	"net/http"
	"strconv"
	"time"
)

const (
	cookieName      = "auth_token"
	settingPassHash = "password_hash"
	settingPassSalt = "password_salt"
	sessionExpiry   = 24 * time.Hour
)

// SessionHandler manages dashboard login/logout/status.
type SessionHandler struct {
	jwt             *JWTManager
	st              *store.Store
	initialPassword string
}

// NewSessionHandler creates a new SessionHandler for dashboard session authentication.
func NewSessionHandler(jwt *JWTManager, st *store.Store, initialPassword string) *SessionHandler {
	return &SessionHandler{
		jwt:             jwt,
		st:              st,
		initialPassword: initialPassword,
	}
}

func writeJSONResponse(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(data); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
	}
}

func (sh *SessionHandler) handleBadPassword(w http.ResponseWriter, ip string) {
	RecordFail(ip)

	if locked, retry := CheckLock(ip); locked {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		http.Error(w, `{"error":"too many attempts"}`, http.StatusTooManyRequests)

		return
	}

	http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
}

// Login handles POST /api/auth/login.
func (sh *SessionHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ip := ClientIP(r)
	if locked, retry := CheckLock(ip); locked {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		http.Error(w, `{"error":"too many attempts"}`, http.StatusTooManyRequests)

		return
	}

	var req struct {
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		http.Error(w, `{"error":"password required"}`, http.StatusBadRequest)
		return
	}

	if !sh.checkPassword(req.Password) {
		sh.handleBadPassword(w, ip)
		return
	}

	RecordSuccess(ip)

	token, err := sh.jwt.Generate(map[string]any{"sub": "admin"}, sessionExpiry)
	if err != nil {
		http.Error(w, `{"error":"token"}`, http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:        cookieName,
		Value:       token,
		Path:        "/",
		Domain:      "",
		Expires:     time.Time{},
		RawExpires:  "",
		MaxAge:      int(sessionExpiry.Seconds()),
		Secure:      config.AuthCookieSecure(),
		HttpOnly:    true,
		SameSite:    http.SameSiteLaxMode,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
		Quoted:      false,
	})
	writeJSONResponse(w, []byte(`{"ok":true}`))
}

// Logout handles POST /api/auth/logout.
func (sh *SessionHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:        cookieName,
		Value:       "",
		Path:        "/",
		Domain:      "",
		Expires:     time.Time{},
		RawExpires:  "",
		MaxAge:      -1,
		Secure:      false,
		HttpOnly:    true,
		SameSite:    http.SameSiteDefaultMode,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
		Quoted:      false,
	})
	writeJSONResponse(w, []byte(`{"ok":true}`))
}

// Status handles GET /api/auth/status.
func (sh *SessionHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	authenticated := false

	if c, err := r.Cookie(cookieName); err == nil {
		if _, err := sh.jwt.Validate(c.Value); err == nil {
			authenticated = true
		}
	}

	if authenticated {
		writeJSONResponse(w, []byte(`{"authenticated":true}`))
		return
	}

	writeJSONResponse(w, []byte(`{"authenticated":false}`))
}

// ResetPassword handles POST /api/auth/reset-password (local only).
func (sh *SessionHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if !isLoopback(r) {
		http.Error(w, `{"error":"local only"}`, http.StatusForbidden)
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewPassword == "" {
		http.Error(w, `{"error":"old_password and new_password required"}`, http.StatusBadRequest)
		return
	}

	if !sh.checkPassword(req.OldPassword) {
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	hash, salt := HashPassword(req.NewPassword)
	if err := sh.st.SetSetting(settingPassHash, hash); err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}

	if err := sh.st.SetSetting(settingPassSalt, salt); err != nil {
		http.Error(w, `{"error":"db"}`, http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, []byte(`{"ok":true}`))
}

func (sh *SessionHandler) checkPassword(password string) bool {
	hash, errH := sh.st.GetSetting(settingPassHash)
	salt, errS := sh.st.GetSetting(settingPassSalt)

	if errH == nil && errS == nil && hash != "" && salt != "" {
		return VerifyPassword(password, hash, salt)
	}

	return password == sh.initialPassword
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

package oauth

import (
	"context"
	"flamerouter/internal/store"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	codexProxyPort = 1455
	xaiProxyPort   = 56121
	proxyTimeout   = 5 * time.Minute
)

type proxySession struct {
	CreatedAt    time.Time
	CodeVerifier string
	RedirectURI  string
	Status       string
	ConnectionID string
	Email        string
	Error        string
}

type fixedProxy struct {
	ln       net.Listener
	srv      *http.Server
	timer    *time.Timer
	sess     map[string]*proxySession
	h        *Handler
	st       *store.Store
	provider string
	port     int
	appPort  int
	mu       sync.Mutex
}

var (
	codexProxy = &fixedProxy{port: codexProxyPort, provider: "codex", sess: map[string]*proxySession{}}
	xaiProxy   = &fixedProxy{port: xaiProxyPort, provider: "xai", sess: map[string]*proxySession{}}
)

func proxyFor(provider string) *fixedProxy {
	switch provider {
	case "codex":
		return codexProxy
	case "xai":
		return xaiProxy
	default:
		return nil
	}
}

// StartOAuthProxy binds fixed-port loopback callback (codex:1455, xai:56121).
func StartOAuthProxy(provider string, appPort int, h *Handler, st *store.Store) (map[string]any, error) {
	p := proxyFor(provider)
	if p == nil {
		return nil, fmt.Errorf("proxy only supported for codex/xai")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.srv != nil {
		return map[string]any{"success": true}, nil
	}

	p.appPort = appPort
	p.h = h
	p.st = st

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p.port))
	if err != nil {
		if isAddrInUse(err) {
			return map[string]any{"success": false, "reason": "port_busy"}, nil
		}

		return map[string]any{"success": false, "reason": err.Error()}, nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", p.serve)
	p.ln = ln
	p.srv = &http.Server{Handler: mux}
	p.timer = time.AfterFunc(proxyTimeout, func() { _ = StopOAuthProxy(provider) })

	go func() { _ = p.srv.Serve(ln) }()

	return map[string]any{"success": true}, nil
}

func isAddrInUse(err error) bool {
	s := err.Error()

	return strings.Contains(s, "address already in use") ||
		strings.Contains(s, "Only one usage of each socket address") ||
		strings.Contains(s, "bind: An attempt was made")
}

// StopOAuthProxy closes fixed-port proxy.
func StopOAuthProxy(provider string) error {
	p := proxyFor(provider)
	if p == nil {
		return fmt.Errorf("proxy only supported for codex/xai")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}

	if p.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = p.srv.Shutdown(ctx)

		cancel()

		p.srv = nil
		p.ln = nil
	}

	return nil
}

// RegisterProxySession stores PKCE session for server-side exchange.
func RegisterProxySession(provider, state, codeVerifier, redirectURI string) bool {
	p := proxyFor(provider)
	if p == nil || state == "" || codeVerifier == "" || redirectURI == "" {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.sess[state] = &proxySession{
		CodeVerifier: codeVerifier,
		RedirectURI:  redirectURI,
		Status:       "pending",
		CreatedAt:    time.Now(),
	}

	return true
}

// GetProxySessionStatus returns session snapshot (nil if unknown).
func GetProxySessionStatus(provider, state string) map[string]any {
	p := proxyFor(provider)
	if p == nil || state == "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	s := p.sess[state]
	if s == nil {
		return nil
	}

	out := map[string]any{"status": s.Status}
	if s.ConnectionID != "" {
		out["connectionId"] = s.ConnectionID
	}

	if s.Email != "" {
		out["email"] = s.Email
	}

	if s.Error != "" {
		out["error"] = s.Error
	}

	return out
}

// CompleteXaiManualCode exchanges pasted xAI code using registered session PKCE.
func CompleteXaiManualCode(ctx context.Context, h *Handler, st *store.Store, code, state string) (map[string]any, error) {
	p := proxyFor("xai")
	if p == nil {
		return nil, fmt.Errorf("xAI OAuth session not found; restart the login flow and paste the code again")
	}

	p.mu.Lock()
	sess := p.sess[state]
	p.mu.Unlock()

	if sess == nil {
		return nil, fmt.Errorf("xAI OAuth session not found; restart the login flow and paste the code again")
	}

	if code == "" {
		return nil, fmt.Errorf("Missing xAI authorization code")
	}

	return h.ExchangeAndSave(ctx, st, "xai", code, sess.RedirectURI, sess.CodeVerifier, state, nil)
}

// ClearProxySession removes session after client consumes done/error.
func ClearProxySession(provider, state string) {
	p := proxyFor(provider)
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.sess, state)
}

func (p *fixedProxy) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/callback" && r.URL.Path != "/auth/callback" {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	code := q.Get("code")
	state := q.Get("state")
	errParam := q.Get("error")

	p.mu.Lock()
	sess := p.sess[state]
	appPort := p.appPort
	h := p.h
	st := p.st
	provider := p.provider
	p.mu.Unlock()

	if sess != nil {
		if errParam != "" {
			msg := q.Get("error_description")
			if msg == "" {
				msg = errParam
			}

			p.mu.Lock()
			sess.Status = "error"
			sess.Error = msg
			p.mu.Unlock()
			writeResultPage(w, false, msg)

			_ = StopOAuthProxy(provider)

			return
		}

		if code == "" {
			p.mu.Lock()
			sess.Status = "error"
			sess.Error = "No authorization code received"
			p.mu.Unlock()
			writeResultPage(w, false, "No authorization code received")

			_ = StopOAuthProxy(provider)

			return
		}

		if h == nil || st == nil {
			p.mu.Lock()
			sess.Status = "error"
			sess.Error = "oauth handler not wired"
			p.mu.Unlock()
			writeResultPage(w, false, "oauth handler not wired")

			_ = StopOAuthProxy(provider)

			return
		}

		conn, err := h.ExchangeAndSave(r.Context(), st, provider, code, sess.RedirectURI, sess.CodeVerifier, state, nil)
		if err != nil {
			p.mu.Lock()
			sess.Status = "error"
			sess.Error = err.Error()
			p.mu.Unlock()
			writeResultPage(w, false, err.Error())

			_ = StopOAuthProxy(provider)

			return
		}

		p.mu.Lock()

		sess.Status = "done"
		if id, _ := conn["id"].(string); id != "" {
			sess.ConnectionID = id
		}

		if email, _ := conn["email"].(string); email != "" {
			sess.Email = email
		}
		p.mu.Unlock()
		writeResultPage(w, true, "You can close this window.")

		_ = StopOAuthProxy(provider)

		return
	}

	if appPort <= 0 {
		appPort = 20128
	}

	loc := fmt.Sprintf("http://localhost:%d/callback?%s", appPort, r.URL.RawQuery)
	http.Redirect(w, r, loc, http.StatusFound)

	_ = StopOAuthProxy(provider)
}

func writeResultPage(w http.ResponseWriter, success bool, message string) {
	color := "#22c55e"
	icon := "&#10003;"
	title := "Authentication Successful"

	if !success {
		color = "#ef4444"
		icon = "&#10007;"
		title = "Authentication Failed"
	}

	msg := html.EscapeString(message)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#f5f5f5}.c{text-align:center;padding:2rem;background:#fff;border-radius:8px;box-shadow:0 2px 10px rgba(0,0,0,.1)}.i{color:%s;font-size:3rem}h1{margin:1rem 0}p{color:#666}</style>
</head><body><div class="c"><div class="i">%s</div><h1>%s</h1><p>%s</p><p>Closing in <span id="cd">3</span>s...</p>
<script>let n=3;const c=document.getElementById("cd");const t=setInterval(()=>{n--;c.textContent=n;if(n<=0){clearInterval(t);window.close();}},1000);</script>
</div></body></html>`, title, color, icon, title, msg)
}

package mitm

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// Server is an HTTPS MITM proxy that intercepts traffic to specific hosts.
type Server struct {
	ln        net.Listener
	rootCA    *x509.Certificate
	rootKey   *ecdsa.PrivateKey
	hosts     map[string]Handler
	dns       *DNSOverride
	certCache map[string]*tls.Certificate
	restarter *Restarter
	status    string
	certPath  string
	keyPath   string
	addr      string
	mu        sync.RWMutex
}

func New(certPath, keyPath string) (*Server, error) {
	cert, key, err := LoadOrCreateRootCA(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	s := &Server{
		rootCA:    cert,
		rootKey:   key,
		hosts:     make(map[string]Handler),
		status:    "stopped",
		dns:       NewDNSOverride(),
		certCache: make(map[string]*tls.Certificate),
		certPath:  certPath,
		keyPath:   keyPath,
	}
	s.restarter = NewRestarter(func(addr string) error {
		return s.Start(addr)
	})

	return s, nil
}

func (s *Server) CertPath() string { return s.certPath }
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.addr
}
func (s *Server) Restarter() *Restarter { return s.restarter }

func (s *Server) Register(host string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host] = h
}

func (s *Server) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.status
}

func (s *Server) DNS() *DNSOverride { return s.dns }

// Start listens on addr. Full live MITM skeleton: TLS with SNI host certs, dispatch to handlers.
func (s *Server) Start(addr string) error {
	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		return fmt.Errorf("already running")
	}

	cfg := &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: s.getCertificate,
	}

	ln, err := tls.Listen("tcp", addr, cfg)
	if err != nil {
		s.mu.Unlock()
		return err
	}

	s.ln = ln
	s.addr = addr
	s.status = "running"

	if s.restarter != nil {
		s.restarter.SetEnabled(true)
		s.restarter.MarkStarted(addr)
	}
	s.mu.Unlock()

	go func() {
		err := http.Serve(ln, http.HandlerFunc(s.serveHTTP))
		s.mu.Lock()
		s.ln = nil
		s.status = "stopped"
		s.mu.Unlock()

		if err != nil {
			// unexpected exit → schedule restart
			if s.restarter != nil {
				s.restarter.ScheduleRestart()
			}
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.restarter != nil {
		s.restarter.SetEnabled(false)
		s.restarter.Reset()
	}

	if s.ln == nil {
		s.status = "stopped"
		return nil
	}

	err := s.ln.Close()
	s.ln = nil
	s.status = "stopped"

	return err
}

// RegisterDefaultTools wires antigravity/copilot/kiro/cursor handlers.
func (s *Server) RegisterDefaultTools(routerBase, apiKey string) {
	for host, h := range DefaultToolHandlers(routerBase, apiKey) {
		s.Register(host, h)
	}
}

func (s *Server) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	host := hello.ServerName
	if host == "" {
		host = "localhost"
	}

	s.mu.RLock()
	if c, ok := s.certCache[host]; ok {
		s.mu.RUnlock()
		return c, nil
	}
	s.mu.RUnlock()

	c, err := GenerateHostCert(host, s.rootCA, s.rootKey)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.certCache[host] = c
	s.mu.Unlock()

	return c, nil
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	if host == "" {
		host = r.TLS.ServerName
	}

	s.mu.RLock()
	h, ok := s.hosts[host]
	s.mu.RUnlock()

	if !ok || h == nil {
		PassthroughHandler{}.HandleRequest(w, r)
		return
	}

	h.HandleRequest(w, r)
}

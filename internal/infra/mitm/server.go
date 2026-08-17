// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
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

// New creates a new MITM server using the specified root CA certificates.
func New(certPath, keyPath string) (*Server, error) {
	cert, key, err := LoadOrCreateRootCA(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	s := &Server{
		ln:        nil,
		rootCA:    cert,
		rootKey:   key,
		hosts:     make(map[string]Handler),
		dns:       NewDNSOverride(),
		certCache: make(map[string]*tls.Certificate),
		restarter: nil,
		status:    "stopped",
		certPath:  certPath,
		keyPath:   keyPath,
		addr:      "",
		mu:        sync.RWMutex{},
	}
	s.restarter = NewRestarter(func(addr string) error {
		return s.Start(addr)
	})

	return s, nil
}

// CertPath returns the path to the root CA cert.
func (s *Server) CertPath() string { return s.certPath }

// Addr returns the listening address.
func (s *Server) Addr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.addr
}

// Restarter returns the restarter instance.
func (s *Server) Restarter() *Restarter { return s.restarter }

// Register adds a handler for a target host.
func (s *Server) Register(host string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts[host] = h
}

// Status returns current server status.
func (s *Server) Status() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.status
}

// DNS returns the DNS override instance.
func (s *Server) DNS() *DNSOverride { return s.dns }

func (s *Server) buildTLSConfig() *tls.Config {
	/* #nosec G402 */
	return &tls.Config{
		Rand:                                nil,
		Time:                                nil,
		Certificates:                        nil,
		NameToCertificate:                   nil,
		GetCertificate:                      s.getCertificate,
		GetClientCertificate:                nil,
		GetConfigForClient:                  nil,
		VerifyPeerCertificate:               nil,
		VerifyConnection:                    nil,
		RootCAs:                             nil,
		NextProtos:                          nil,
		ServerName:                          "",
		ClientAuth:                          tls.NoClientCert,
		ClientCAs:                           nil,
		InsecureSkipVerify:                  false,
		CipherSuites:                        nil,
		PreferServerCipherSuites:            false,
		SessionTicketsDisabled:              false,
		SessionTicketKey:                    [32]byte{},
		ClientSessionCache:                  nil,
		UnwrapSession:                       nil,
		WrapSession:                         nil,
		MinVersion:                          tls.VersionTLS12,
		MaxVersion:                          0,
		CurvePreferences:                    nil,
		DynamicRecordSizingDisabled:         false,
		Renegotiation:                       0,
		KeyLogWriter:                        nil,
		EncryptedClientHelloConfigList:      nil,
		EncryptedClientHelloRejectionVerify: nil,
		GetEncryptedClientHelloKeys:         nil,
		EncryptedClientHelloKeys:            nil,
	}
}

// Start listens on addr. Full live MITM skeleton: TLS with SNI host certs, dispatch to handlers.
func (s *Server) Start(addr string) error {
	s.mu.Lock()
	if s.ln != nil {
		s.mu.Unlock()
		return fmt.Errorf("already running")
	}

	ln, err := tls.Listen("tcp", addr, s.buildTLSConfig())
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

	go s.serveListener(ln)

	return nil
}

func (s *Server) serveListener(ln net.Listener) {
	server := &http.Server{
		Addr:                         "",
		Handler:                      http.HandlerFunc(s.serveHTTP),
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  0,
		ReadHeaderTimeout:            0,
		WriteTimeout:                 0,
		IdleTimeout:                  0,
		MaxHeaderBytes:               0,
		TLSNextProto:                 nil,
		ConnState:                    nil,
		ErrorLog:                     nil,
		BaseContext:                  nil,
		ConnContext:                  nil,
		HTTP2:                        nil,
		Protocols:                    nil,
	}

	/* #nosec G114 */
	err := server.Serve(ln)

	s.mu.Lock()
	s.ln = nil
	s.status = "stopped"
	s.mu.Unlock()

	if err != nil && s.restarter != nil {
		s.restarter.ScheduleRestart()
	}
}

// Stop halts the running MITM server.
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

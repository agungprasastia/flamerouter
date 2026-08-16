package mitm

import (
	"crypto/x509"
	"encoding/pem"
)

// RootCAPEM returns PEM-encoded root CA certificate.
func (s *Server) RootCAPEM() []byte {
	if s == nil || s.rootCA == nil {
		return nil
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.rootCA.Raw})
}

// RootCA returns the root certificate (may be nil).
func (s *Server) RootCA() *x509.Certificate {
	if s == nil {
		return nil
	}

	return s.rootCA
}

// CertExists reports whether root CA is loaded.
func (s *Server) CertExists() bool {
	return s != nil && s.rootCA != nil
}

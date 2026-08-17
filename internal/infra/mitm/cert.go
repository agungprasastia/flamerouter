// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
package mitm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func newRootCATemplate(serial *big.Int) *x509.Certificate {
	return &x509.Certificate{
		Raw:                         nil,
		RawTBSCertificate:           nil,
		RawSubjectPublicKeyInfo:     nil,
		RawSubject:                  nil,
		RawIssuer:                   nil,
		Signature:                   nil,
		SignatureAlgorithm:          0,
		PublicKeyAlgorithm:          0,
		PublicKey:                   nil,
		Version:                     0,
		SerialNumber:                serial,
		Issuer:                      pkix.Name{Country: nil, Organization: nil, OrganizationalUnit: nil, Locality: nil, Province: nil, StreetAddress: nil, PostalCode: nil, SerialNumber: "", CommonName: "", Names: nil, ExtraNames: nil},
		Subject:                     pkix.Name{Country: nil, Organization: []string{"FlameRouter MITM CA"}, OrganizationalUnit: nil, Locality: nil, Province: nil, StreetAddress: nil, PostalCode: nil, SerialNumber: "", CommonName: "FlameRouter MITM Root CA", Names: nil, ExtraNames: nil},
		NotBefore:                   time.Now().Add(-time.Hour),
		NotAfter:                    time.Now().AddDate(10, 0, 0),
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		Extensions:                  nil,
		ExtraExtensions:             nil,
		UnhandledCriticalExtensions: nil,
		ExtKeyUsage:                 nil,
		UnknownExtKeyUsage:          nil,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLen:                  0,
		MaxPathLenZero:              true,
		SubjectKeyId:                nil,
		AuthorityKeyId:              nil,
		OCSPServer:                  nil,
		IssuingCertificateURL:       nil,
		DNSNames:                    nil,
		EmailAddresses:              nil,
		IPAddresses:                 nil,
		URIs:                        nil,
		PermittedDNSDomainsCritical: false,
		PermittedDNSDomains:         nil,
		ExcludedDNSDomains:          nil,
		PermittedIPRanges:           nil,
		ExcludedIPRanges:            nil,
		PermittedEmailAddresses:     nil,
		ExcludedEmailAddresses:      nil,
		PermittedURIDomains:         nil,
		ExcludedURIDomains:          nil,
		CRLDistributionPoints:       nil,
		PolicyIdentifiers:           nil,
		Policies:                    nil,
		InhibitAnyPolicy:            0,
		InhibitAnyPolicyZero:        false,
		InhibitPolicyMapping:        0,
		InhibitPolicyMappingZero:    false,
		RequireExplicitPolicy:       0,
		RequireExplicitPolicyZero:   false,
		PolicyMappings:              nil,
	}
}

// GenerateRootCA creates a self-signed root CA certificate.
func GenerateRootCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := newRootCATemplate(serial)

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

func newHostCertTemplate(host string, serial *big.Int) *x509.Certificate {
	return &x509.Certificate{
		Raw:                         nil,
		RawTBSCertificate:           nil,
		RawSubjectPublicKeyInfo:     nil,
		RawSubject:                  nil,
		RawIssuer:                   nil,
		Signature:                   nil,
		SignatureAlgorithm:          0,
		PublicKeyAlgorithm:          0,
		PublicKey:                   nil,
		Version:                     0,
		SerialNumber:                serial,
		Issuer:                      pkix.Name{Country: nil, Organization: nil, OrganizationalUnit: nil, Locality: nil, Province: nil, StreetAddress: nil, PostalCode: nil, SerialNumber: "", CommonName: "", Names: nil, ExtraNames: nil},
		Subject:                     pkix.Name{Country: nil, Organization: []string{"FlameRouter MITM"}, OrganizationalUnit: nil, Locality: nil, Province: nil, StreetAddress: nil, PostalCode: nil, SerialNumber: "", CommonName: host, Names: nil, ExtraNames: nil},
		NotBefore:                   time.Now().Add(-time.Hour),
		NotAfter:                    time.Now().AddDate(1, 0, 0),
		KeyUsage:                    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		Extensions:                  nil,
		ExtraExtensions:             nil,
		UnhandledCriticalExtensions: nil,
		ExtKeyUsage:                 []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		UnknownExtKeyUsage:          nil,
		BasicConstraintsValid:       false,
		IsCA:                        false,
		MaxPathLen:                  0,
		MaxPathLenZero:              false,
		SubjectKeyId:                nil,
		AuthorityKeyId:              nil,
		OCSPServer:                  nil,
		IssuingCertificateURL:       nil,
		DNSNames:                    []string{host},
		EmailAddresses:              nil,
		IPAddresses:                 nil,
		URIs:                        nil,
		PermittedDNSDomainsCritical: false,
		PermittedDNSDomains:         nil,
		ExcludedDNSDomains:          nil,
		PermittedIPRanges:           nil,
		ExcludedIPRanges:            nil,
		PermittedEmailAddresses:     nil,
		ExcludedEmailAddresses:      nil,
		PermittedURIDomains:         nil,
		ExcludedURIDomains:          nil,
		CRLDistributionPoints:       nil,
		PolicyIdentifiers:           nil,
		Policies:                    nil,
		InhibitAnyPolicy:            0,
		InhibitAnyPolicyZero:        false,
		InhibitPolicyMapping:        0,
		InhibitPolicyMappingZero:    false,
		RequireExplicitPolicy:       0,
		RequireExplicitPolicyZero:   false,
		PolicyMappings:              nil,
	}
}

// GenerateHostCert creates a certificate for a specific hostname signed by the root CA.
func GenerateHostCert(host string, rootCert *x509.Certificate, rootKey *ecdsa.PrivateKey) (*tls.Certificate, error) {
	if host == "" {
		return nil, fmt.Errorf("host required")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := newHostCertTemplate(host, serial)

	certDER, err := x509.CreateCertificate(rand.Reader, template, rootCert, &key.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate:                  [][]byte{certDER, rootCert.Raw},
		PrivateKey:                   key,
		SupportedSignatureAlgorithms: nil,
		OCSPStaple:                   nil,
		SignedCertificateTimestamps:  nil,
		Leaf:                         nil,
	}, nil
}

// LoadOrCreateRootCA loads PEM cert/key from paths, or generates and writes them.
func LoadOrCreateRootCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	if certPath != "" && keyPath != "" {
		if cert, key, err := loadRootCA(certPath, keyPath); err == nil {
			return cert, key, nil
		}
	}

	cert, key, err := GenerateRootCA()
	if err != nil {
		return nil, nil, err
	}

	if certPath != "" && keyPath != "" {
		if err := writeRootCA(certPath, keyPath, cert, key); err != nil {
			return nil, nil, err
		}
	}

	return cert, key, nil
}

func loadRootCA(certPath, keyPath string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	/* #nosec G304 */
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}

	/* #nosec G304 */
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, nil, fmt.Errorf("invalid cert pem")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}

	kblock, _ := pem.Decode(keyPEM)
	if kblock == nil {
		return nil, nil, fmt.Errorf("invalid key pem")
	}

	key, err := x509.ParseECPrivateKey(kblock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, nil
}

func writeRootCA(certPath, keyPath string, cert *x509.Certificate, key *ecdsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}

	/* #nosec G304 */
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	defer func() {
		if clErr := certOut.Close(); clErr != nil {
			_ = clErr
		}
	}()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Headers: nil, Bytes: cert.Raw}); err != nil {
		return err
	}

	keyBytes, errMarshal := x509.MarshalECPrivateKey(key)
	if errMarshal != nil {
		return errMarshal
	}

	/* #nosec G304 */
	keyOut, errKey := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if errKey != nil {
		return errKey
	}

	defer func() {
		if clErr := keyOut.Close(); clErr != nil {
			_ = clErr
		}
	}()

	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Headers: nil, Bytes: keyBytes})
}

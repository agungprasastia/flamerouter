package mitm

import (
	"crypto/x509"
	"testing"
)

func TestGenerateRootAndHostCert(t *testing.T) {
	root, key, err := GenerateRootCA()
	if err != nil {
		t.Fatal(err)
	}
	if !root.IsCA {
		t.Fatal("root must be CA")
	}
	hostCert, err := GenerateHostCert("example.com", root, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(hostCert.Certificate) < 1 {
		t.Fatal("missing host cert")
	}
	leaf, err := x509.ParseCertificate(hostCert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.CheckSignatureFrom(root); err != nil {
		t.Fatalf("host not signed by root: %v", err)
	}
	found := false
	for _, d := range leaf.DNSNames {
		if d == "example.com" {
			found = true
		}
	}
	if !found {
		t.Fatal("DNS SAN missing")
	}
}

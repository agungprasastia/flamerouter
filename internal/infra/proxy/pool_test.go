package proxy

import (
	"flamerouter/internal/store"
	"testing"
)

func TestPoolToURL(t *testing.T) {
	u := poolToURL(store.ProxyPool{
		ID:       "",
		Name:     "",
		Type:     "http",
		Host:     "127.0.0.1",
		Port:     8080,
		Username: "u",
		Password: "p",
		IsActive: false,
	})
	if u.String() != "http://u:p@127.0.0.1:8080" {
		t.Fatalf("got %s", u.String())
	}

	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal(u)
	}
}

func TestPoolEmpty(t *testing.T) {
	p := New(nil)
	if p.Next() != nil {
		t.Fatal("expected nil")
	}

	tr := p.Transport()
	if tr == nil {
		t.Fatal("transport")
	}
}

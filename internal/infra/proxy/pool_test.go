package proxy

import (
	"flamerouter/internal/store"
	"net/url"
	"testing"
)

func TestPoolToURL(t *testing.T) {
	u := poolToURL(store.ProxyPool{Type: "http", Host: "127.0.0.1", Port: 8080, Username: "u", Password: "p"})
	if u.String() != "http://u:p@127.0.0.1:8080" {
		t.Fatalf("got %s", u.String())
	}

	if u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal(u)
	}

	_ = url.URL{}
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

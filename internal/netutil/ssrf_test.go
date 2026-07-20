package netutil

import "testing"

func TestAssertPublicURL_BlocksPrivate(t *testing.T) {
	for _, u := range []string{
		"http://localhost/x",
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest",
		"http://foo.local/",
		"http://[::1]/",
	} {
		if err := AssertPublicURL(u); err == nil {
			t.Fatalf("expected block: %s", u)
		}
	}
}

func TestAssertPublicURL_AllowsPublic(t *testing.T) {
	// use literal public IP to avoid DNS flakiness in CI
	if err := AssertPublicURL("https://1.1.1.1/"); err != nil {
		t.Fatal(err)
	}
}

package netutil

import "testing"

func TestAssertPublicURL_BlocksPrivate(t *testing.T) {
	for _, u := range []string{
		"http://localhost/x",
		"http://localhost./x",
		"http://127.0.0.1/",
		"http://127.0.0.1./",
		"http://2130706433/",
		"http://017700000001/",
		"http://0x7f000001/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest",
		"http://foo.local/",
		"http://foo.local./",
		"http://foo.internal/",
		"http://foo.localhost/",
		"http://127.0.0.1.nip.io/",
		"http://[::1]/",
		"http://[::ffff:127.0.0.1]/",
		"http://[fc00::1]/",
		"http://[fe80::1]/",
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

	if err := AssertPublicURL("http://8.8.8.8/search"); err != nil {
		t.Fatal(err)
	}
}

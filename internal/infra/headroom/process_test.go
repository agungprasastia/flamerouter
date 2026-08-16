package headroom

import "testing"

func TestFilterExtras(t *testing.T) {
	got := filterExtras([]string{"code", "ml", "evil", "CODE"})
	if len(got) != 2 || got[0] != "code" || got[1] != "ml" {
		t.Fatalf("got=%v", got)
	}
}

func TestPhantomSavings(t *testing.T) {
	p := New()
	p.AddPhantomSavings(10)
	p.AddPhantomSavings(5)

	if p.PhantomSavings() != 15 {
		t.Fatalf("got=%d", p.PhantomSavings())
	}
}

func TestExtrasStatusShape(t *testing.T) {
	p := New()

	st := p.ExtrasStatus()
	if _, ok := st["available"]; !ok {
		t.Fatal("missing available")
	}
}

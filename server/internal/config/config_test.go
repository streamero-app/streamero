package config

import "testing"

func TestParseRate(t *testing.T) {
	if p, b, err := parseRate("20,10"); err != nil || p != 20 || b != 10 {
		t.Fatalf("20,10 -> %d %d %v", p, b, err)
	}
	if p, b, err := parseRate(" 0 "); err != nil || p != 0 || b != 0 {
		t.Fatalf("0 -> %d %d %v", p, b, err)
	}
	for _, bad := range []string{"", "20", "x,1", "20,0", "-1,5"} {
		if _, _, err := parseRate(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

package upgrade

import "testing"

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in       string
		maj, min, pat int
		ok       bool
	}{
		{"v1.2.3", 1, 2, 3, true},
		{"1.2.3", 1, 2, 3, true},
		{"v1.2.3-alpha", 1, 2, 3, true},
		{"v1.2.3+build5", 1, 2, 3, true},
		{"v1.2", 0, 0, 0, false},
		{"dev", 0, 0, 0, false},
		{"v1.2.x", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}
	for _, c := range cases {
		maj, min, pat, ok := parseSemver(c.in)
		if ok != c.ok || maj != c.maj || min != c.min || pat != c.pat {
			t.Errorf("parseSemver(%q) = (%d,%d,%d,%t), want (%d,%d,%d,%t)",
				c.in, maj, min, pat, ok, c.maj, c.min, c.pat, c.ok)
		}
	}
}

func TestNewerVersionAvailable(t *testing.T) {
	cases := []struct {
		cur, lat string
		want     bool
	}{
		{"v1.2.0", "v1.3.0", true},
		{"v1.3.0", "v1.2.0", false},
		{"v1.3.0", "v1.3.0", false},
		{"v1.3.0", "v1.3.1", true},
		{"v2.0.0", "v1.99.99", false},
		{"v1.2.3", "v2.0.0", true},
		{"dev", "v1.0.0", false},
		{"", "v1.0.0", false},
		{"v1.0.0", "", false},
		{"v1.0.0", "garbage", false},
		{"v1.2.3-alpha", "v1.2.3", false}, // pre-release == release → not newer
	}
	for _, c := range cases {
		got := NewerVersionAvailable(c.cur, c.lat)
		if got != c.want {
			t.Errorf("NewerVersionAvailable(%q,%q) = %v, want %v", c.cur, c.lat, got, c.want)
		}
	}
}

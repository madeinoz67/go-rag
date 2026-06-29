package upgrade

import "testing"

func TestParseSemver(t *testing.T) {
	cases := []struct {
		in                  string
		major, minor, patch int
		ok                  bool
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
		major, minor, patch, ok := parseSemver(c.in)
		if ok != c.ok || major != c.major || minor != c.minor || patch != c.patch {
			t.Errorf("parseSemver(%q) = (%d,%d,%d,%t), want (%d,%d,%d,%t)",
				c.in, major, minor, patch, ok, c.major, c.minor, c.patch, c.ok)
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

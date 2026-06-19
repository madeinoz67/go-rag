package daemon

import (
	"path/filepath"
	"testing"
)

func TestPIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WritePID(dir, 12345); err != nil {
		t.Fatal(err)
	}
	pid, err := ReadPID(dir)
	if err != nil || pid != 12345 {
		t.Fatalf("want pid 12345, got %d (%v)", pid, err)
	}
	if _, err := ReadPID(t.TempDir()); err == nil {
		t.Fatal("ReadPID on a dir with no pid file must error")
	}
	_ = filepath.Join(dir, pidFileName) // keep filepath referenced
}

func TestAddrsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAddrs(dir, Addrs{MCPAddr: ":9999"}); err != nil {
		t.Fatal(err)
	}
	a, err := ReadAddrs(dir)
	if err != nil || a.MCPAddr != ":9999" {
		t.Fatalf("want :9999, got %+v (%v)", a, err)
	}
}

func TestReadTokenAbsent(t *testing.T) {
	if tok := ReadToken(t.TempDir()); tok != "" {
		t.Fatalf("want empty token when file absent, got %q", tok)
	}
}

func TestHealthAndMCPURL(t *testing.T) {
	if got := HealthURL(":7878"); got != "http://127.0.0.1:7878/mcp/health" {
		t.Errorf("HealthURL: %q", got)
	}
	if got := MCPURL(":7878"); got != "http://127.0.0.1:7878/mcp" {
		t.Errorf("MCPURL: %q", got)
	}
}

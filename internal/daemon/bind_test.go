package daemon

import (
	"errors"
	"net"
	"strings"
	"testing"
)

func TestIsLoopbackBind(t *testing.T) {
	// Stubbed by subtests below; restore at the end so other tests are unaffected.
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })

	cases := []struct {
		addr string
		want bool
	}{
		// IPv4 loopback family (entire 127.0.0.0/8)
		{"127.0.0.1:7878", true},
		{"127.5.6.7:1", true},
		// IPv6 loopback
		{"[::1]:7878", true},
		// localhost hostname (special-cased, no DNS)
		{"localhost:7878", true},
		{"LOCALHOST:7878", true},
		// all-interfaces wildcards → external
		{":7878", false}, // bare port (empty host)
		{"0.0.0.0:7878", false},
		{"[::]:7878", false},
		// LAN / public IPs → external
		{"192.168.1.10:7878", false},
		{"10.0.0.1:7878", false},
		{"8.8.8.8:7878", false},
		// unparseable / empty → fail-safe external
		{"not-a-valid-addr", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsLoopbackBind(c.addr); got != c.want {
			t.Errorf("IsLoopbackBind(%q) = %v, want %v", c.addr, got, c.want)
		}
	}

	t.Run("hostname_resolves_to_loopback", func(t *testing.T) {
		lookupIP = func(string) ([]net.IP, error) { return []net.IP{net.IPv4(127, 0, 0, 1)}, nil }
		if !IsLoopbackBind("myhost:7878") {
			t.Fatal("hostname resolving to 127.0.0.1 should be loopback")
		}
	})
	t.Run("hostname_resolves_to_nonloopback", func(t *testing.T) {
		lookupIP = func(string) ([]net.IP, error) { return []net.IP{net.IPv4(192, 168, 1, 1)}, nil }
		if IsLoopbackBind("myhost:7878") {
			t.Fatal("hostname resolving to non-loopback should be external")
		}
	})
	t.Run("hostname_unresolvable_fail_safe", func(t *testing.T) {
		lookupIP = func(string) ([]net.IP, error) { return nil, errors.New("no such host") }
		if IsLoopbackBind("myhost:7878") {
			t.Fatal("unresolvable hostname should be external (fail-safe)")
		}
	})
}

func TestNonLoopbackBinds(t *testing.T) {
	addrs := Addrs{
		MCPAddr:  "127.0.0.1:7878", // loopback
		RESTAddr: "0.0.0.0:7879",   // external
		GRPCAddr: "",               // disabled — must be omitted
	}
	got := NonLoopbackBinds(addrs)
	if len(got) != 1 || got[0].Name != "REST" || got[0].Addr != "0.0.0.0:7879" {
		t.Fatalf("NonLoopbackBinds = %+v, want only [REST 0.0.0.0:7879]", got)
	}
}

func TestValidateBind(t *testing.T) {
	allLoopback := Addrs{MCPAddr: "127.0.0.1:7878", RESTAddr: "127.0.0.1:7879", GRPCAddr: "127.0.0.1:7880"}
	external := Addrs{MCPAddr: "0.0.0.0:7878", RESTAddr: "127.0.0.1:7879"}
	multi := Addrs{MCPAddr: "0.0.0.0:7878", GRPCAddr: "192.168.1.5:7880"}

	if err := ValidateBind(allLoopback, false); err != nil {
		t.Errorf("all-loopback, no opt-in: want nil, got %v", err)
	}
	err := ValidateBind(external, false)
	if err == nil {
		t.Fatal("external, no opt-in: want error, got nil")
	}
	if !strings.Contains(err.Error(), "0.0.0.0:7878") {
		t.Errorf("error should name the offending addr, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--bind-external") {
		t.Errorf("error should mention the opt-in flag, got: %v", err)
	}
	if strings.Contains(err.Error(), "127.0.0.1:7879") {
		t.Errorf("error must not list the loopback addr as an offender, got: %v", err)
	}

	err = ValidateBind(multi, false)
	if err == nil {
		t.Fatal("multi-external, no opt-in: want error")
	}
	if !strings.Contains(err.Error(), "0.0.0.0:7878") || !strings.Contains(err.Error(), "192.168.1.5:7880") {
		t.Errorf("error should name BOTH offenders, got: %v", err)
	}

	// opt-in permits external binding.
	if err := ValidateBind(external, true); err != nil {
		t.Errorf("external with opt-in: want nil, got %v", err)
	}
	// disabled transports are ignored.
	if err := ValidateBind(Addrs{MCPAddr: "127.0.0.1:7878"}, false); err != nil {
		t.Errorf("disabled transports ignored: want nil, got %v", err)
	}
}

func TestExternalBindWarning(t *testing.T) {
	// Nothing exposed → no warning.
	if w := ExternalBindWarning(Addrs{MCPAddr: "127.0.0.1:7878", RESTAddr: "127.0.0.1:7879"}); w != "" {
		t.Errorf("all-loopback: want empty warning, got %q", w)
	}
	// External present → prominent, single warning naming the transport and risks.
	w := ExternalBindWarning(Addrs{MCPAddr: "0.0.0.0:7878", RESTAddr: "127.0.0.1:7879"})
	for _, want := range []string{"non-loopback", "UNENCRYPTED", "no TLS", "Access control", "--bind-external", "0.0.0.0:7878", "MCP"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning missing %q\ngot:\n%s", want, w)
		}
	}
	if strings.Contains(w, "127.0.0.1:7879") {
		t.Errorf("warning must not list the loopback REST addr, got:\n%s", w)
	}
}

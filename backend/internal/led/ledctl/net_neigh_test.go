package ledctl

import (
	"strings"
	"testing"
)

func neighRig(t *testing.T, route string) *rig {
	t.Helper()
	r := newRig(t, "DXP2800")
	r.write("sys/net/eth0/operstate", "up\n")
	r.run.outputs["ip route show default"] = route
	r.c.Env.Proc = r.dir + "/proc"
	return r
}

func TestGatewayNeighbourStates(t *testing.T) {
	for _, tc := range []struct {
		state  string
		ok     bool
		reason string
	}{
		{"REACHABLE", true, "ok (gateway neighbour REACHABLE)"},
		{"STALE", true, "ok (gateway neighbour STALE)"},
		{"DELAY", true, "ok (gateway neighbour DELAY)"},
		{"PROBE", true, "ok (gateway neighbour PROBE)"},
		{"PERMANENT", true, "ok (gateway neighbour PERMANENT)"},
		{"NOARP", true, "ok (gateway neighbour NOARP)"},
		{"FAILED", true, "ok (gateway neighbour FAILED, 1 of 3)"},
		{"INCOMPLETE", true, "ok (gateway neighbour INCOMPLETE, 1 of 3)"},
		{"", true, "ok (no neighbour entry for the gateway, 1 of 3)"},
	} {
		r := neighRig(t, "default via 10.0.0.1 dev eth0\n")
		r.neigh(tc.state)
		fails := 0
		ok, why := r.c.networkCheck("eth0", &fails)
		if ok != tc.ok || why != tc.reason {
			t.Errorf("%q: ok=%v reason=%q", tc.state, ok, why)
		}
	}
}

func TestGatewayNeighbourRedAfterThree(t *testing.T) {
	for state, want := range map[string]string{"FAILED": "gateway neighbour FAILED", "": "no neighbour entry for the gateway"} {
		r := neighRig(t, "default via 10.0.0.1 dev eth0\n")
		r.neigh(state)
		fails := 0
		for i := 1; i <= 3; i++ {
			ok, why := r.c.networkCheck("eth0", &fails)
			if i < 3 && !ok || i == 3 && (ok || why != want) {
				t.Errorf("%q probe %d: ok=%v reason=%q", state, i, ok, why)
			}
		}
		r.neigh("REACHABLE")
		if ok, _ := r.c.networkCheck("eth0", &fails); !ok || fails != 0 {
			t.Errorf("%q: a live entry must reset (fails=%d)", state, fails)
		}
	}
}

func TestGatewayNeighbourIPv6(t *testing.T) {
	r := neighRig(t, "default via fe80::1 dev eth0\n")
	r.run.outputs["ip -j neigh show fe80::1 dev eth0"] = `[{"dst":"fe80::1","dev":"eth0","lladdr":"aa:bb:cc:dd:ee:ff","router":null,"state":["STALE"]}]`
	fails := 0
	if ok, why := r.c.networkCheck("eth0", &fails); !ok || why != "ok (gateway neighbour STALE)" {
		t.Errorf("ok=%v reason=%q", ok, why)
	}
	// No /proc/net/arp fallback for IPv6.
	r.run.outputs["ip -j neigh show fe80::1 dev eth0"] = ""
	r.write("proc/net/arp", "IP address       HW type     Flags       HW address            Mask     Device\nfe80::1 0x1 0x2 aa:bb:cc:dd:ee:ff * eth0\n")
	if got := r.c.gatewayNeighbour("eth0", "fe80::1"); got != "" {
		t.Errorf("IPv6 must not use the ARP table: %q", got)
	}
}

func TestGatewayNeighbourProcArpFallback(t *testing.T) {
	hdr := "IP address       HW type     Flags       HW address            Mask     Device\n"
	for _, tc := range []struct {
		name, arp, want string
		code            int
	}{
		{"complete", "10.0.0.1 0x1 0x2 aa:bb:cc:dd:ee:ff * eth0\n", "COMPLETE", 0},
		{"incomplete", "10.0.0.1 0x1 0x0 00:00:00:00:00:00 * eth0\n", "INCOMPLETE", 0},
		{"other device", "10.0.0.1 0x1 0x2 aa:bb:cc:dd:ee:ff * eth9\n", "", 0},
		{"other host", "10.0.0.2 0x1 0x2 aa:bb:cc:dd:ee:ff * eth0\n", "", 0},
		{"ip fails", "10.0.0.1 0x1 0x2 aa:bb:cc:dd:ee:ff * eth0\n", "COMPLETE", 2},
	} {
		r := neighRig(t, "default via 10.0.0.1 dev eth0\n")
		r.write("proc/net/arp", hdr+tc.arp)
		r.run.codes["ip -j neigh show 10.0.0.1 dev eth0"] = tc.code
		if got := r.c.gatewayNeighbour("eth0", "10.0.0.1"); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
	r := neighRig(t, "default via 10.0.0.1 dev eth0\n")
	r.write("proc/net/arp", hdr+"10.0.0.1 0x1 0x2 aa:bb:cc:dd:ee:ff * eth0\n")
	fails := 0
	if ok, why := r.c.networkCheck("eth0", &fails); !ok || why != "ok (gateway neighbour COMPLETE)" {
		t.Errorf("ok=%v reason=%q", ok, why)
	}
}

func TestNetworkCheckSendsNothing(t *testing.T) {
	r := neighRig(t, "default via 10.0.0.1 dev eth0\n")
	r.neigh("FAILED")
	fails := 0
	for range 4 {
		r.c.networkCheck("eth0", &fails)
	}
	if r.run.called("ping") {
		t.Errorf("ping was run: %v", r.run.calls)
	}
	for _, c := range r.run.calls {
		if c != "ip route show default" && c != "ip -j neigh show 10.0.0.1 dev eth0" {
			t.Errorf("unexpected command %q", c)
		}
	}
}

func TestNetworkReasonHasNoAddresses(t *testing.T) {
	r := neighRig(t, "default via 10.0.0.1 dev eth0\n")
	for _, s := range []string{"FAILED", "STALE", ""} {
		r.neigh(s)
		fails := 2
		_, why := r.c.networkCheck("eth0", &fails)
		for _, bad := range []string{"10.0.0.1", "aa:bb"} {
			if strings.Contains(why, bad) {
				t.Errorf("reason %q shows %q", why, bad)
			}
		}
	}
}

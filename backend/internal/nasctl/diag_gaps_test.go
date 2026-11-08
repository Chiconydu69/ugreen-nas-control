package nasctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/panel"
)

func netFixture(t *testing.T, route string) *Ctl {
	dir := t.TempDir()
	env := Env{SysNet: dir + "/net", Proc: dir + "/proc"}
	nic := func(name, oper, carrier string) {
		write(t, env.SysNet+"/"+name+"/operstate", oper+"\n")
		write(t, env.SysNet+"/"+name+"/carrier", carrier+"\n")
	}
	nic("bond0", "up", "1")
	nic("eth0", "up", "1")
	nic("eth1", "down", "0")
	nic("veth12ab", "up", "1")
	write(t, env.SysNet+"/bond0/bonding/slaves", "eth0 eth1\n")
	write(t, env.SysNet+"/br0/brif/veth12ab/x", "")
	write(t, env.SysNet+"/br0/brif/bond0/x", "")
	nic("br0", "up", "1")
	write(t, env.Proc+"/net/route", route)
	return &Ctl{Env: env}
}

const routeHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func TestNetDiagShowsWhatColoursTheLED(t *testing.T) {
	route := routeHeader +
		"shim-br0\t00000000\t0100A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"bond0\t00000000\t0100A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n" +
		"bond0\t0000A8C0\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n"
	c := netFixture(t, route)
	d := c.netDiag(`{"network_device":"bond0","network_ok":false,"network_reason":"gateway neighbour FAILED"}`)
	body := strings.Join(d.lines(), "\n")
	for _, want := range []string{
		"LED network device: bond0",
		"LED daemon verdict: not ok: gateway neighbour FAILED",
		"bond0: operstate up  carrier 1  slaves eth0 eth1",
		"eth0: operstate up  carrier 1",
		"eth1: operstate down  carrier 0",
		"default route: yes via bond0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	for _, no := range []string{"0100A8C0", "192.168", "veth"} {
		if strings.Contains(body, no) {
			t.Errorf("must not show %q:\n%s", no, body)
		}
	}

	// A healthy verdict names the neighbour state, never the address or the MAC.
	ok := c.netDiag(`{"network_device":"bond0","network_ok":true,"network_reason":"ok (gateway neighbour STALE)"}`)
	okBody := strings.Join(ok.lines(), "\n")
	if !strings.Contains(okBody, "LED daemon verdict: ok (gateway neighbour STALE)") {
		t.Errorf("neighbour state missing:\n%s", okBody)
	}
	if strings.Contains(okBody, "192.168") || regexp.MustCompile(`[0-9a-f]{2}(:[0-9a-f]{2}){5}`).MatchString(okBody) {
		t.Errorf("diag must not show the gateway address or MAC:\n%s", okBody)
	}

	// A bridge lists its ports, not the container ports.
	if body := strings.Join(c.netDiag(`{"network_device":"br0","network_ok":true}`).lines(), "\n"); !strings.Contains(body, "bond0: operstate up") || !strings.Contains(body, "verdict: ok") || strings.Contains(body, "veth") {
		t.Errorf("bridge:\n%s", body)
	}
}

func TestNetDiagWithoutStatusOrRoute(t *testing.T) {
	c := netFixture(t, routeHeader)
	body := strings.Join(c.netDiag("").lines(), "\n")
	if !strings.Contains(body, "default route: no") || !strings.Contains(body, "verdict: unknown") || !strings.Contains(body, "LED network device: -") {
		t.Errorf("\n%s", body)
	}
	c = netFixture(t, routeHeader+"eth0\t00000000\t0100A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n")
	body = strings.Join(c.netDiag(`{"network_device":"","network_ok":null,"network_reason":""}`).lines(), "\n")
	if !strings.Contains(body, "LED network device: eth0") || !strings.Contains(body, "verdict: none yet") || !strings.Contains(body, "eth0: operstate up") {
		t.Errorf("\n%s", body)
	}
}

type padMem struct {
	words  map[uint64]uint32
	reads  []uint64
	closed bool
}

func (m *padMem) Read32(p uint64) (uint32, error) {
	m.reads = append(m.reads, p)
	if v, ok := m.words[p]; ok {
		return v, nil
	}
	return 0xFFFFFFFF, nil
}
func (m *padMem) Write32(p uint64, v uint32) error { panic(fmt.Sprintf("diag wrote %#x to %#x", v, p)) }
func (m *padMem) Close() error                     { m.closed = true; return nil }

func TestPadsDiagReadsRawWordsOnly(t *testing.T) {
	m := &padMem{words: map[uint64]uint32{0xFD6A0980: 0x00000002}}
	var wrote []bool
	c := &Ctl{OpenMem: func(write bool) (panel.Mem, error) { wrote = append(wrote, write); return m, nil }}
	s := padsSection(c.padsDiag("DXP4800"))
	for _, want := range []string{"power: 0xFD6A0980 = 0x00000002", "reset: 0xFD6D0B50 = 0xFFFFFFFF  (not readable)", "0xFFFFFFFF means not readable"} {
		if !strings.Contains(s.Body, want) {
			t.Errorf("missing %q:\n%s", want, s.Body)
		}
	}
	if len(wrote) != 1 || wrote[0] || !m.closed || len(m.reads) != 2 {
		t.Errorf("opened read-only once and closed: %v closed=%t reads=%#x", wrote, m.closed, m.reads)
	}

	// The GT reads the aligned word of an unaligned pad address.
	g := &Ctl{OpenMem: func(bool) (panel.Mem, error) {
		return &padMem{words: map[uint64]uint32{0xFED81524: 1 << 16, 0xFED81520: 1 << 16}}, nil
	}}
	if s := padsSection(g.padsDiag("DXP4800 GT")); !strings.Contains(s.Body, "0x00010000") {
		t.Errorf("GT:\n%s", s.Body)
	}
	if s := padsSection(c.padsDiag("Some Other NAS")); s.Body != "no button map for this model" {
		t.Errorf("unknown model: %+v", s)
	}
	bad := &Ctl{OpenMem: func(bool) (panel.Mem, error) { return nil, errors.New("operation not permitted") }}
	if s := padsSection(bad.padsDiag("DXP4800")); !strings.Contains(s.Err, "operation not permitted") {
		t.Errorf("open error: %+v", s)
	}
}

func TestVersionVerdict(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name         string
		plg          string
		cached       []string
		tree, plgAt  time.Time
		ok, contains string
	}{
		{"in step", "5.3.1", []string{"5.3.1"}, now, now.Add(-time.Minute), "ok", ""},
		{"older archive left", "5.3.1", []string{"5.3.0", "5.3.1"}, now, now, "", "do not match the .plg 5.3.1"},
		{"archive of another version", "5.4.0", []string{"5.3.1"}, now, now, "", "5.3.1"},
		{"no archive", "5.3.1", nil, now, now, "", "no cached archive"},
		{"plg newer than the files", "5.3.1", []string{"5.3.1"}, now.Add(-time.Hour), now, "", "newer than the installed files"},
		{"install a few minutes behind", "5.3.1", []string{"5.3.1"}, now.Add(-5 * time.Minute), now, "ok", ""},
		{"no plg", "", nil, now, time.Time{}, "ok", ""},
	} {
		got := versionVerdict(tc.plg, tc.cached, tc.tree, tc.plgAt)
		switch {
		case tc.ok != "" && got != tc.ok:
			t.Errorf("%s: %q", tc.name, got)
		case tc.contains != "" && (!strings.HasPrefix(got, "MISMATCH") || !strings.Contains(got, tc.contains) || !strings.Contains(got, "update pending a reboot or reinstall")):
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}

func TestFillStateReadsCronAndInstall(t *testing.T) {
	dir := t.TempDir()
	env := Env{PluginsBoot: dir + "/boot", PluginsWeb: dir + "/web", CronD: dir + "/cron.d", LEDCron: dir + "/boot/ugreen-nas-control/ugreen-nas-control.cron"}
	write(t, env.PluginsBoot+"/ugreen-nas-control/5.3.1/archive.txz", "x")
	write(t, env.PluginsBoot+"/ugreen-nas-control/fan.cfg", "mode=auto")
	write(t, env.LEDCron, "* * * * * /usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl led-schedule\n")
	write(t, env.PluginsWeb+"/ugreen-nas-control/UGREENNASControl.page", "x")
	write(t, env.CronD+"/ugreen-nas-control", "x")
	write(t, env.CronD+"/other", "x")
	at := time.Date(2026, 11, 3, 8, 30, 0, 0, time.UTC)
	for _, f := range []string{env.PluginsWeb + "/ugreen-nas-control/UGREENNASControl.page", env.CronD + "/ugreen-nas-control"} {
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
	var st StateDiag
	(&Ctl{Env: env}).fillState(&st)
	if strings.Join(st.Cached, ",") != "5.3.1" || !st.TreeAt.Equal(at) || len(st.CronLines) != 1 || !strings.Contains(st.CronLines[0], "led-schedule") {
		t.Errorf("%+v", st)
	}
	if len(st.CronD) != 1 || st.CronD[0] != "ugreen-nas-control 2026-11-03 08:30 UTC" {
		t.Errorf("cron.d: %v", st.CronD)
	}
	st.CronPresent, st.Version = true, "5.3.1"
	body := stateSection(st).Body
	for _, want := range []string{"cron file contents:", "  * * * * * /usr/local", "/etc/cron.d/*ugreen* written: ugreen-nas-control 2026-11-03 08:30 UTC", "installed files vs .plg:"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
}

func TestPluginURLKindNeverNamesAUser(t *testing.T) {
	for in, want := range map[string]string{
		"https://raw.githubusercontent.com/ugreen-nas-control/ugreen-nas-control/main/plugin/ugreen-nas-control.plg": "official",
		"https://ugreen-nas-control.github.io/ugreen-nas-control/next/ugreen-nas-control.plg":                        "official next",
		"https://raw.githubusercontent.com/jens-fork/ugreen-nas-control/main/plugin/ugreen-nas-control.plg":          "other (host only: raw.githubusercontent.com)",
		"https://jens-fork.github.io/ugreen-nas-control/next/ugreen-nas-control.plg":                                 "other (host only: github.io)",
		"http://192.168.1.20:8080/ugreen-nas-control.plg":                                                            "other (host only: <removed>)",
		"": "",
	} {
		got := pluginURLKind(in)
		if got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
		if strings.Contains(got, "jens") {
			t.Errorf("%q leaks a user name: %q", in, got)
		}
	}
}

func TestPluginLinesAre24hAtMost40AndFiltered(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	var sb strings.Builder
	sb.WriteString("Nov  2 09:00:00 tower ugreen-nas-ctl[1]: fan daemon started\n")            // two days old
	sb.WriteString("Nov  4 08:00:00 tower root: plugin: installing: ugreen-nas-control.plg\n") // in
	sb.WriteString("Nov  4 08:00:01 tower ugreen-led-control: status daemon stopped\n")        // in
	sb.WriteString("Nov  4 08:00:02 tower sshd[3]: Accepted password for root\n")              // not ours
	sb.WriteString("Nov  4 08:00:03 tower ugreen-nas-ctl[1]: serial=WD-WCC4N7XYZ123 start from 192.168.1.5\n")
	for i := range 60 {
		fmt.Fprintf(&sb, "Nov  4 09:%02d:00 tower ugreen-nas-ctl[1]: fan daemon restart %d\n", i, i)
	}
	got := pluginLines(Anon{}, sb.String(), now)
	if len(got) != 40 || !strings.HasSuffix(got[39], "restart 59") {
		t.Fatalf("want the last 40, got %d", len(got))
	}
	small := pluginLines(Anon{}, strings.SplitN(sb.String(), "Nov  4 09:", 2)[0], now)
	joined := strings.Join(small, "\n")
	for _, no := range []string{"two days", "09:00:00 tower ugreen-nas-ctl[1]: fan daemon started", "sshd", "WD-WCC4N7XYZ123", "192.168.1.5"} {
		if strings.Contains(joined, no) {
			t.Errorf("must not show %q:\n%s", no, joined)
		}
	}
	for _, want := range []string{"plugin: installing", "status daemon stopped", "serial=<removed>"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q:\n%s", want, joined)
		}
	}
	// ISO stamps and lines without a stamp are handled too.
	iso := "2026-11-01T10:00:00Z tower ugreen-nas-ctl: start\n2026-11-04T10:00:00Z tower ugreen-nas-ctl: stop\nno stamp ugreen-nas-ctl: start\n"
	if got := pluginLines(Anon{}, iso, now); len(got) != 2 {
		t.Errorf("iso: %v", got)
	}
	// A December line seen in January belongs to last year, not next.
	jan := time.Date(2027, 1, 1, 0, 30, 0, 0, time.UTC)
	if got := pluginLines(Anon{}, "Dec 31 23:00:00 tower ugreen-nas-ctl[1]: start\n", jan); len(got) != 1 {
		t.Errorf("year wrap: %v", got)
	}
}

func TestNVMeChainRootPortFirstNoModelNoSerial(t *testing.T) {
	dir := t.TempDir()
	env := Env{SysNVMe: dir + "/class/nvme"}
	pci := dir + "/devices/pci0000:00/0000:00:1d.0/0000:03:00.0"
	write(t, pci+"/nvme/nvme0/model", "Samsung SECRET MODEL\n")
	write(t, pci+"/nvme/nvme0/serial", "S6XXNX0T123456\n")
	pci2 := dir + "/devices/pci0000:00/0000:00:06.0"
	write(t, pci2+"/nvme/nvme1/model", "x\n")
	for id, p := range map[string]string{"nvme0": pci, "nvme1": pci2} {
		if err := os.Symlink(p, filepath.Join(pci, "nvme", id, "device")); err != nil && id == "nvme0" {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(pci2, pci2+"/nvme/nvme1/device"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.SysNVMe, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, p := range map[string]string{"nvme0": pci + "/nvme/nvme0", "nvme1": pci2 + "/nvme/nvme1"} {
		if err := os.Symlink(p, env.SysNVMe+"/"+id); err != nil {
			t.Fatal(err)
		}
	}
	h := fan.Hardware{NVMe: []fan.NVMe{{ID: "nvme0", Slot: "pcie"}, {ID: "nvme1", Slot: "m2-1"}}}
	body := nvmeSection((&Ctl{Env: env}).nvmeDiag(h)).Body
	for _, want := range []string{"nvme0: 0000:00:1d.0 > 0000:03:00.0  slot pcie  (root port 0000:00:1d.0 is in no slot", "nvme1: 0000:00:06.0  slot m2-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	for _, no := range []string{"SECRET", "S6XXNX0T123456"} {
		if strings.Contains(body, no) {
			t.Errorf("must not show %q", no)
		}
	}
	if body := nvmeSection(nil).Body; !strings.Contains(body, "no NVMe controller") {
		t.Error(body)
	}
}

func TestLEDOutputGoesThroughTheLogFilter(t *testing.T) {
	d := LEDDiag{
		Status:   `{"network_device":"eth0","note":"lease 192.168.1.77","leds":[{"serial":"WD-WCC4N7XYZ123"}]}`,
		Get:      "disk1 on 255 0 0\nuser=jens-secret\n",
		Adapters: []string{"i2c-0=SMBus I801 adapter at efa0", "i2c-5=adapter 00:11:22:33:44:55"},
	}
	d.anonymize(Anon{})
	all := d.Status + d.Get + strings.Join(d.Adapters, "\n")
	for _, no := range []string{"192.168.1.77", "WD-WCC4N7XYZ123", "jens-secret", "00:11:22:33:44:55"} {
		if strings.Contains(all, no) {
			t.Errorf("still has %q:\n%s", no, all)
		}
	}
	if !strings.Contains(d.Status, `"network_device":"eth0"`) || !strings.Contains(d.Adapters[0], "SMBus I801 adapter at efa0") {
		t.Errorf("useful parts kept: %s %v", d.Status, d.Adapters)
	}
}

func TestLEDSectionShowsTheNetworkBlockEvenWhenSkipped(t *testing.T) {
	c := netFixture(t, routeHeader+"eth0\t00000000\t0100A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0\n")
	body := ledSection(LEDDiag{Skipped: "x", Net: c.netDiag("")}).Body
	if !strings.Contains(body, "network (what colours the network LED") || !strings.Contains(body, "default route: yes via eth0") {
		t.Errorf("\n%s", body)
	}
}

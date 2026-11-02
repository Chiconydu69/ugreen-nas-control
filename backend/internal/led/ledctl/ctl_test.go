package ledctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBayCount(t *testing.T) {
	for product, want := range map[string]int{
		"DXP6800 Pro": 6, "DX4600 Pro": 4, "DXP8800 Plus": 8, "DXP2800": 2, "DXP4800 GT": 4,
		"iDX6011 Pro": 6, "iDX6012B": 6, "UGREEN NAS": 8, "DXP480T Plus": 8, "DX1": 8, "DXP9000": 8, "": 8,
	} {
		if got := bayCount(product); got != want {
			t.Errorf("bayCount(%q) = %d, want %d", product, got, want)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, s string
		want       bool
	}{
		{"DXP4800*GT*", "DXP4800 GT", true}, {"DXP4800*GT*", "DXP4800 Plus", false},
		{"iDX601[12]*", "iDX6012B", true}, {"iDX601[12]*", "iDX6013", false},
		{"disk[1-8]", "disk8", true}, {"disk[1-8]", "disk9", false}, {"a.b", "axb", false}, {"?x", "ax", true},
	} {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v", c.pattern, c.s, got)
		}
	}
}

func TestTempColor(t *testing.T) {
	for temp, want := range map[int64]string{
		10: "0 120 255", 30: "0 120 255", 38: "0 200 83", 45: "255 200 0", 50: "255 120 20", 55: "255 40 40", 99: "255 40 40",
	} {
		if got := tempColor(temp, 45, 55); got != want {
			t.Errorf("tempColor(%d) = %q, want %q", temp, got, want)
		}
	}
	if got := tempColor(5, 20, 20); got != "0 120 255" {
		t.Errorf("degenerate limits: %q", got)
	}
}

func TestSmartWarnings(t *testing.T) {
	now := "5=8 187=0 197=2 198=0 failed=1 errors=3"
	if got := smartWarnings(now, ""); got != "5=8 197=2 failed=1 errors=3" {
		t.Errorf("no ack: %q", got)
	}
	if got := smartWarnings(now, "5=8 197=1 errors=3 failed=1"); got != "197=2" {
		t.Errorf("partly acked: %q", got)
	}
	if got := smartWarnings(now, "5=bad"); got != "197=2 failed=1 errors=3" {
		t.Errorf("a garbage ack never warns (the shell test errors out): %q", got)
	}
}

func TestParseWantedAndMatches(t *testing.T) {
	kind, color, b := parseWanted("breath|1 2 3|200")
	if kind != "breath" || color != "1 2 3" || b != "200" {
		t.Fatalf("parseWanted: %q %q %q", kind, color, b)
	}
	state := map[string]string{
		"disk1": "on 200 1 2 3 0 0", "disk2": "breath 200 1 2 3 1500 1500", "disk3": "blink 200 9 9 9 100 100",
		"disk4": "breath 200 1 2 3 3000 3000", "disk5": "off 0 0 0 0 0 0",
	}
	cases := []struct {
		led, wanted string
		blinking    bool
		want        bool
	}{
		{"disk1", "steady|1 2 3|200", false, true}, {"disk1", "steady|1 2 3|100", false, false},
		{"disk2", "breath|1 2 3|200", false, true}, {"disk2", "pulse|1 2 3|200", false, false},
		{"disk3", "steady|1 2 3|200", true, true}, {"disk3", "steady|1 2 3|200", false, false},
		{"disk4", "pulse|1 2 3|200", false, true}, {"disk5", "off", false, true}, {"disk1", "off", false, false},
		{"disk6", "off", false, false},
	}
	for _, c := range cases {
		if got := ledMatches(state, c.led, c.wanted, c.blinking); got != c.want {
			t.Errorf("ledMatches(%s, %q, %v) = %v", c.led, c.wanted, c.blinking, got)
		}
	}
}

func TestConfigParsing(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config("# c\na=\"1\"\na=\"2\"\nbad\nb=unquoted\n  c=\"x\"\nd=\"\"\ne=\"with space\"\n")
	if r.c.cfg["a"] != "2" || r.c.cfg["e"] != "with space" {
		t.Errorf("cfg = %v", r.c.cfg)
	}
	for _, k := range []string{"b", "c", "bad"} {
		if _, ok := r.c.cfg[k]; ok {
			t.Errorf("%q must not be parsed", k)
		}
	}
	if r.c.get("d", "x") != "x" {
		t.Error("empty value falls back to the default")
	}
	r.config("a=\"3\"\n")
	if r.c.cfg["a"] != "3" || r.c.cfg["e"] != "" {
		t.Errorf("reload: %v", r.c.cfg)
	}
}

func TestNightWindow(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config("night_enabled=\"1\"\nnight_start=\"22:00\"\nnight_end=\"07:00\"\n")
	for now, want := range map[string]bool{"2200": true, "2359": true, "0000": true, "0659": true, "0700": false, "1200": false, "2159": false} {
		r.c.Env.Now = now
		if got := r.c.isNight(); got != want {
			t.Errorf("isNight at %s = %v", now, got)
		}
	}
	r.config("night_enabled=\"1\"\nnight_start=\"09:00\"\nnight_end=\"17:00\"\n")
	r.c.Env.Now = "1200"
	if !r.c.isNight() {
		t.Error("daytime window")
	}
	r.config("night_enabled=\"1\"\nnight_start=\"09:00\"\nnight_end=\"09:00\"\n")
	if r.c.isNight() {
		t.Error("equal start and end is never night")
	}
	r.config("night_start=\"09:00\"\n")
	if r.c.isNight() {
		t.Error("not enabled")
	}
	r.c.Env.Now = ""
	r.c.Now = func() time.Time { return time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC) }
	r.config("night_enabled=\"1\"\n")
	if !r.c.isNight() {
		t.Error("clock is used without UGREEN_NOW")
	}
}

func TestEffectiveBrightness(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config("night_brightness=\"40\"\n")
	for _, c := range []struct {
		saved int64
		night bool
		want  int64
	}{{255, false, 255}, {255, true, 40}, {10, true, 10}} {
		if got := r.c.effectiveBrightness(c.saved, c.night); got != c.want {
			t.Errorf("effective(%d, %v) = %d", c.saved, c.night, got)
		}
	}
	r.config("night_action=\"off\"\n")
	if got := r.c.effectiveBrightness(255, true); got != 0 {
		t.Errorf("off at night: %d", got)
	}
}

func TestSetBrightnessSwitchesOnAnOffLED(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	if rc := r.ctl("off", "disk1"); rc != 0 {
		t.Fatal(rc)
	}
	if rc := r.ctl("brightness", "disk1", "128"); rc != 0 {
		t.Fatal(rc)
	}
	if s := r.chip.get("disk1"); s[0] != "on" || s[1] != "128" {
		t.Errorf("disk1 = %v", s)
	}
	if rc := r.ctl("brightness", "disk1", "0"); rc != 0 || r.chip.get("disk1")[0] != "off" {
		t.Errorf("brightness 0 switches off: rc %d, %v", rc, r.chip.get("disk1"))
	}
	for _, bad := range []string{"256", "-1", "x", ""} {
		if rc := r.ctl("brightness", "disk1", bad); rc != 1 || !strings.Contains(r.err.String(), "between 0 and 255") {
			t.Errorf("brightness %q: rc %d, %q", bad, rc, r.err.String())
		}
	}
}

func TestUsageErrors(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	if rc := r.ctl("get"); rc != 2 || r.err.String() != "Usage: ctl get <led>\n" {
		t.Errorf("get: %d %q", rc, r.err.String())
	}
	if rc := r.ctl("nonsense"); rc != 2 || !strings.Contains(r.err.String(), "Unknown command 'nonsense'") || !strings.HasPrefix(r.out.String(), "UGREEN LED Control") {
		t.Errorf("unknown: %d %q", rc, r.err.String())
	}
	if rc := r.ctl("get", "disk7"); rc != 1 || r.err.String() != "ERROR: LED 'disk7' not found.\n" {
		t.Errorf("6 bays have no disk7: %d %q", rc, r.err.String())
	}
}

func TestPhaseStampOnlyForSlowEffects(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.c.Now = func() time.Time { return time.UnixMilli(1700000000123) }
	r.ctl("blink", "disk1", "100", "100")
	if _, err := os.Stat(r.env.PhasePrefix + "disk1"); err == nil {
		t.Error("a fast blink leaves no stamp")
	}
	r.ctl("breath", "disk1", "1200", "900")
	data, _ := os.ReadFile(r.env.PhasePrefix + "disk1")
	if string(data) != "1700000000123 1200 900\n" {
		t.Errorf("stamp = %q", data)
	}
	r.ctl("get", "disk1")
	if !strings.Contains(r.out.String(), `"sent_ms": 1700000000123,`) || !strings.Contains(r.out.String(), `"now_ms": 1700000000123`) {
		t.Errorf("get: %s", r.out.String())
	}
	r.ctl("breath", "disk1", "1300", "900")
	r.ctl("blink", "disk1", "100", "100")
	r.ctl("get", "disk1")
	if strings.Contains(r.out.String(), "sent_ms") {
		t.Errorf("stamp of other timings must not show: %s", r.out.String())
	}
}

func TestEnsureSMBus(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	if r.c.hasSMBus() {
		t.Fatal("no adapter yet")
	}
	r.run.codes["modprobe -q i2c_i801"] = 1
	if r.c.ensureSMBus() {
		t.Error("driver does not load")
	}
	delete(r.run.codes, "modprobe -q i2c_i801")
	r.write("sys/i2c/i2c-3/name", "SMBus I801 adapter at f000\n")
	if !r.c.ensureSMBus() {
		t.Error("adapter present after load")
	}
	if !r.run.called("modprobe -q i2c-dev") {
		t.Error("i2c-dev is loaded first")
	}

	r.out.Reset()
	if rc := r.c.smbusCommand(); rc != 0 || !strings.HasPrefix(r.out.String(), "SMBus adapter: ") || !strings.HasSuffix(r.out.String(), "i2c-3/name:SMBus I801 adapter at f000\n") {
		t.Errorf("smbus: %d %q", rc, r.out.String())
	}

	gt := newRig(t, "DXP4800 GT")
	if gt.c.hasSMBus() {
		t.Error("GT without adapter")
	}
	gt.write("iomem", "fed00000-fed003ff : AMDI0010:00\n")
	if !gt.c.hasSMBus() || !gt.c.ensureSMBus() {
		t.Error("GT via /proc/iomem")
	}
	gt.out.Reset()
	gt.c.smbusCommand()
	if gt.out.String() != "SMBus adapter: AMD DesignWare I2C (AMDI0010) via /dev/mem\n" {
		t.Errorf("GT smbus: %q", gt.out.String())
	}

	miss := newRig(t, "DXP6800 Pro")
	miss.write("modules", "i2c_i801 24576 0 - Live 0x0\n")
	miss.run.outputs["dmesg"] = "boot\ni801 failed\nunrelated\nSMBus busy\n"
	if rc := miss.c.smbusCommand(); rc != 1 || !strings.Contains(miss.err.String(), "i801 failed\nSMBus busy\n") || strings.Contains(miss.err.String(), "unrelated") {
		t.Errorf("missing adapter: %d %q", rc, miss.err.String())
	}
}

func TestPreflightRunsWithoutToolEnv(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.c.Env.ToolFromEnv = false
	r.ctl("help")
	if r.run.called("modprobe") {
		t.Error("help needs no controller")
	}
	r.ctl("list")
	if !r.run.called("modprobe -q i2c-dev") {
		t.Error("list loads the SMBus driver")
	}
}

func TestLinkSpeedFollowsMembers(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.write("sys/net/bond0/speed", "20000\n")
	r.write("sys/net/eth0/speed", "1000\n")
	r.write("sys/net/eth1/speed", "2500\n")
	r.write("sys/net/eth2/speed", "-1\n")
	for _, m := range []string{"eth0", "eth1", "eth2", "veth7"} {
		if err := os.Symlink("../"+m, filepath.Join(r.dir, "sys/net/bond0/lower_"+m)); err != nil {
			t.Fatal(err)
		}
	}
	r.write("sys/net/veth7/speed", "10000\n")
	if got := r.c.linkSpeed("bond0", 0); got != "2500" {
		t.Errorf("bond = %s", got)
	}
	if got := r.c.linkSpeed("eth2", 0); got != "0" {
		t.Errorf("no link = %s", got)
	}
	r.write("sys/net/loop/speed", "100\n")
	if err := os.Symlink("../loop", filepath.Join(r.dir, "sys/net/loop/lower_loop")); err != nil {
		t.Fatal(err)
	}
	if got := r.c.linkSpeed("loop", 0); got != "100" {
		t.Errorf("a loop ends at depth 5 with the deepest speed: %s", got)
	}
}

func TestPortMaxSpeedUsesCache(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	if got := r.c.portMaxSpeed("eth0"); got != "0" {
		t.Errorf("no ethtool: %s", got)
	}
	r.run.have["ethtool"] = true
	r.run.outputs["ethtool eth0"] = "Supported link modes:   100baseT/Full\n   1000baseT/Full 10000baseT/Full\nSupported pause frame use: Symmetric\nAdvertised link modes: 40000baseT/Full\n"
	if got := r.c.portMaxSpeed("eth0"); got != "10000" {
		t.Errorf("ethtool = %s", got)
	}
	r.run.outputs["ethtool eth0"] = ""
	if got := r.c.portMaxSpeed("eth0"); got != "10000" {
		t.Errorf("cached = %s", got)
	}
}

func TestIdentifyRoundTrip(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.chip.set("disk2", [7]string{"breath", "99", "1", "2", "3", "700", "800"})
	if rc := r.ctl("identify", "disk2", "7"); rc != 0 || r.out.String() != "Identifying disk2 for 7 s.\n" {
		t.Fatalf("identify: %d %q", rc, r.out.String())
	}
	if s := r.chip.get("disk2"); s[0] != "blink" || s[2] != "255" {
		t.Errorf("blinking white: %v", s)
	}
	if len(r.run.spawned) != 1 || strings.Join(r.run.spawned[0].Args, " ")[:len("_after 7 identify-stop disk2 ")] != "_after 7 identify-stop disk2 " {
		t.Fatalf("timer = %+v", r.run.spawned)
	}
	token := r.run.spawned[0].Args[4]

	if rc := r.ctl("identify-stop", "disk2", "wrong"); rc != 0 || r.out.String() != "" {
		t.Errorf("wrong token: %d %q", rc, r.out.String())
	}
	if !strings.Contains(r.c.identifyingLEDs(), `"disk2"`) {
		t.Error("still identifying")
	}
	if rc := r.ctl("identify-stop", "disk2", token); rc != 0 || r.out.String() != "Stopped identifying disk2.\n" {
		t.Errorf("stop: %d %q", rc, r.out.String())
	}
	if s := r.chip.get("disk2"); s != [7]string{"breath", "99", "1", "2", "3", "700", "800"} {
		t.Errorf("restored = %v", s)
	}
	if r.c.identifyingLEDs() != "[]" {
		t.Error("marker removed")
	}
	for _, bad := range []string{"power", "disk9", "nope"} {
		if rc := r.ctl("identify", bad); rc != 1 {
			t.Errorf("identify %s: %d", bad, rc)
		}
	}
	if rc := r.ctl("identify", "disk1", "5x"); rc != 1 || r.err.String() != "ERROR: Seconds must be a number.\n" {
		t.Errorf("seconds: %d %q", rc, r.err.String())
	}
}

func TestAfterRunsTheCommand(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	start := time.Now()
	if rc := r.ctl("_after", "0.05", "on", "disk3"); rc != 0 || time.Since(start) < 40*time.Millisecond {
		t.Errorf("rc %d after %v", rc, time.Since(start))
	}
	if r.chip.get("disk3")[0] != "on" {
		t.Error("ran")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rc := r.c.Main(ctx, []string{"_after", "30", "off", "disk3"}); rc != 0 || r.chip.get("disk3")[0] != "on" {
		t.Error("a cancelled timer does nothing")
	}
}

func TestAlertTickExitCodes(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config("alert_enabled=\"1\"\nalert_color=\"9 8 7\"\n")
	if rc := r.c.alertTick(""); rc != 0 {
		t.Errorf("nothing to do: %d", rc)
	}
	if rc := r.c.alertTick("--force"); rc != 1 {
		t.Errorf("--force without a previous alert returns 1 like the script: %d", rc)
	}
	r.write("notify/unread/a.notify", "importance=\"alert\"\n")
	if rc := r.c.alertTick(""); rc != 0 {
		t.Errorf("alert: %d", rc)
	}
	if s := r.chip.get("power"); s[0] != "blink" || s[2] != "9" {
		t.Errorf("power = %v", s)
	}
	_ = os.Remove(filepath.Join(r.dir, "notify/unread/a.notify"))
	if rc := r.c.alertTick(""); rc != 0 || catState(r.env.AlertState) != "none" {
		t.Errorf("alerts read: %d %q", rc, catState(r.env.AlertState))
	}
}

// writeSystem lays out two disks behind ata1/ata2 and one network port.
func writeSystem(r *rig) {
	for n, dev := range map[int]string{1: "sda", 2: "sdb"} {
		leaf := filepath.Join(r.dir, "sys/dev", "ata"+string(rune('0'+n)), "host", "t")
		_ = os.MkdirAll(leaf, 0o755)
		_ = os.MkdirAll(filepath.Join(r.dir, "sys/block", dev), 0o755)
		_ = os.Symlink(leaf, filepath.Join(r.dir, "sys/block", dev, "device"))
		r.write("sys/block/"+dev+"/stat", "1 0 0 0 1 0 0 0\n")
	}
	r.write("disks.ini", "[disk1]\nname=\"disk1\"\ndevice=\"sda\"\nstatus=\"DISK_OK\"\nspundown=\"0\"\ntemp=\"30\"\nid=\"X_1\"\n[disk2]\nname=\"disk2\"\ndevice=\"sdb\"\nstatus=\"DISK_DSBL\"\n")
	r.write("sys/net/eth0/operstate", "up\n")
	r.write("sys/net/eth0/speed", "1000\n")
	r.write("sys/net/eth0/statistics/rx_bytes", "1\n")
	r.write("sys/net/eth0/statistics/tx_bytes", "1\n")
	r.run.outputs["ip route show default"] = "default via 10.0.0.1 dev eth0\n"
}

func TestDaemonLoop(t *testing.T) {
	r := newRig(t, "DXP2800")
	writeSystem(r)
	r.config("disk1_color=\"1 2 3\"\n")
	r.c.Tick = 2 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() { done <- r.c.daemonRun(ctx) }()

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timeout: %s\ncalls: %v", what, r.chip.callList())
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	waitFor("first pass", func() bool {
		return r.chip.get("disk1")[2] == "1" && r.chip.get("disk2")[2] == "255" && r.chip.get("disk2")[3] == "59" && r.chip.get("netdev")[3] == "200"
	})
	if s := r.chip.get("netdev"); s[2] != "0" || s[3] != "200" || s[4] != "83" {
		t.Errorf("netdev = %v", s)
	}

	// Traffic makes the bay blink, and it goes back to steady when the counters stand still.
	// The daemon takes its baseline counter a moment after the LEDs are set, and a change that
	// lands before it is the baseline, not traffic. So keep the counters moving until it blinks.
	reads := 1
	waitFor("activity blink", func() bool {
		reads++
		r.write("sys/block/sda/stat", fmt.Sprintf("%d 0 0 0 %d 0 0 0\n", reads, reads))
		for _, c := range r.chip.callList() {
			if c == "set disk1 blink 100 100" {
				return true
			}
		}
		return false
	})
	waitFor("back to steady", func() bool { return r.chip.get("disk1")[0] == "on" })

	// A manual test changes the LED behind the daemon's back: the 30 s compare repairs it.
	r.chip.set("disk1", [7]string{"on", "255", "9", "9", "9", "0", "0"})
	waitFor("reconcile", func() bool { return r.chip.get("disk1")[2] == "1" })

	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("rc = %d", rc)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if !r.run.called("logger -t ugreen-led-control status daemon stopped") {
		t.Error("stop is logged")
	}
}

func TestDaemonHandsOffToForeignMonitor(t *testing.T) {
	r := newRig(t, "DXP2800")
	r.write("run/pid", "123\n")
	r.run.codes["pgrep -f ^/bin/bash /usr/bin/ugreen-leds"] = 0
	if rc := r.c.daemonRun(t.Context()); rc != 0 {
		t.Errorf("rc = %d", rc)
	}
	if _, err := os.Stat(r.env.PidFile); err == nil {
		t.Error("pidfile removed")
	}
	if len(r.chip.callList()) != 0 {
		t.Errorf("the LEDs are left alone: %v", r.chip.callList())
	}
}

func TestNetworkDeviceResolvesBridge(t *testing.T) {
	r := newRig(t, "DXP2800")
	r.run.outputs["ip route show default"] = "default via 1.1.1.1 dev shim-br0\ndefault via 1.1.1.1 dev br0\n"
	for _, p := range []string{"veth1", "docker0", "eth1"} {
		r.write("sys/net/br0/brif/"+p+"/x", "")
	}
	if got := r.c.networkDevice(); got != "eth1" {
		t.Errorf("device = %q", got)
	}
	r.run.outputs["ip route show default"] = ""
	if got := r.c.networkDevice(); got != "" {
		t.Errorf("no route = %q", got)
	}
}

func TestSpawnErrorsAreReported(t *testing.T) {
	for _, args := range [][]string{{"daemon", "start"}, {"fx", "start"}, {"identify", "disk1", "5"}, {"shutdown", "--test"}} {
		r := newRig(t, "DXP6800 Pro")
		r.config("fx_name=\"rainbow\"\n")
		r.run.spawnErr = errors.New("no such file")
		if rc := r.ctl(args...); rc != 1 || !strings.Contains(r.err.String(), "ERROR:") || !strings.Contains(r.err.String(), "no such file") {
			t.Errorf("%v: rc=%d stdout=%q stderr=%q", args, rc, r.out.String(), r.err.String())
		}
		if strings.Contains(r.out.String(), "started") {
			t.Errorf("%v claims success: %q", args, r.out.String())
		}
	}
}

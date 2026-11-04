package parity

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type scenario struct {
	name      string
	product   string
	unordered bool     // daemon scenarios: the order of calls is timing-dependent, compare them as a sorted multiset (counts count)
	silent    bool     // inert and prints nothing either: the exit code is the whole result
	inert     bool     // the scenario has no side effect (help, usage errors): skip the "something happened" guard
	expect    []string // substrings the chip call log must contain on both sides (proves the scenario did its work)
	fast      bool     // part of the subset `go test` runs without LEDCTL_PARITY=1
	intended  string   // reason the Go port differs from the shell on purpose; the scenario must differ (else drop the field)
	noToolEnv bool     // UGREEN_LED_I2C unset: the SMBus preflight runs
	setup     func(r *rig)
	run       func(r *rig)
}

var (
	enabled                               bool // the harness is built and TestParity can run
	full                                  bool // LEDCTL_PARITY=1: every scenario; otherwise only the fast ones
	skipReason                            string
	total, equal, different, intendedDiff atomic.Int32
	limiter                               chan struct{}
	diffReports                           = make(chan string, 256)
)

func TestMain(m *testing.M) {
	full = os.Getenv("LEDCTL_PARITY") == "1"

	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..", "..", "..")
	testdataDir = filepath.Join(wd, "testdata")
	fakeChip = filepath.Join(root, "..", "web", "tools", "harness", "fake-led-i2c")

	var err error
	if bashPath, err = findBash(); err != nil {
		if full {
			fmt.Fprintln(os.Stderr, "parity:", err)
			os.Exit(1)
		}
		// Plain `go test` outside the dev shell (macOS has bash 3.2): the check runs in `just check`.
		skipReason = err.Error()
		os.Exit(m.Run())
	}

	tmp, err := os.MkdirTemp("", "ledctl-parity-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	goBinary = filepath.Join(tmp, "ugreen-nas")
	build := exec.Command("go", "build", "-tags", "testhooks", "-trimpath", "-buildvcs=false", "-o", goBinary, "./cmd/ugreen-nas")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}

	enabled = true
	limiter = make(chan struct{}, max(2, runtime.NumCPU()))
	code := m.Run()
	_ = os.RemoveAll(tmp)
	label := "parity"
	if !full {
		label = "parity (fast subset; LEDCTL_PARITY=1 runs all)"
	}
	fmt.Printf("%s: %d scenarios, %d equal, %d different, %d intended\n", label, total.Load(), equal.Load(), different.Load(), intendedDiff.Load())
	os.Exit(code)
}

func findBash() (string, error) {
	p, err := exec.LookPath("bash")
	if err != nil {
		return "", fmt.Errorf("bash not found")
	}
	out, err := exec.Command(p, "-c", "echo ${BASH_VERSINFO[0]}").Output()
	if err != nil || strings.TrimSpace(string(out)) < "4" {
		return "", fmt.Errorf("%s is bash %s, the script needs bash >= 4 (run inside `nix develop`)", p, strings.TrimSpace(string(out)))
	}
	return p, nil
}

func unifiedDiff(a, b string) string {
	dir, _ := os.MkdirTemp("", "ledctl-diff")
	defer func() { _ = os.RemoveAll(dir) }()
	_ = os.WriteFile(filepath.Join(dir, "shell"), []byte(a), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "go"), []byte(b), 0o644)
	out, _ := exec.Command("diff", "-u", filepath.Join(dir, "shell"), filepath.Join(dir, "go")).CombinedOutput()
	return string(out)
}

func TestParity(t *testing.T) {
	if !enabled {
		t.Skip("parity needs bash >= 4 (nix develop): " + skipReason)
	}
	for _, s := range scenarios() {
		if !full && !s.fast {
			continue
		}
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			limiter <- struct{}{}
			defer func() { <-limiter }()

			product := s.product
			if product == "" {
				product = "DXP6800 Pro"
			}
			r := newRig(t, "go", product)
			if s.noToolEnv {
				delete(r.env, "UGREEN_LED_I2C")
			}
			if s.setup != nil {
				s.setup(r)
			}
			s.run(r)
			got := r.snapshot(s.unordered).text()
			want := loadGolden(t, s.name).text()

			total.Add(1)
			if problem := s.check(got, r); problem != "" {
				t.Errorf("go side is vacuous: %s", problem)
			}
			if s.intended != "" {
				if want == got {
					different.Add(1)
					t.Errorf("marked as intended difference (%s) but Go agrees with the recorded shell: remove the mark", s.intended)
					return
				}
				intendedDiff.Add(1)
				t.Logf("intended difference: %s", s.intended)
				return
			}
			if want == got {
				equal.Add(1)
				return
			}
			different.Add(1)
			t.Errorf("Go differs from the recorded shell (- golden, + go):\n%s", unifiedDiff(want, got))
		})
	}
}

func goldenPath(name string) string { return filepath.Join(testdataDir, "golden", name+".json") }

func loadGolden(t *testing.T, name string) golden {
	t.Helper()
	data, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("no golden for %q: %v", name, err)
	}
	var g golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("golden %q: %v", name, err)
	}
	return g
}

var reOutput = regexp.MustCompile(`\[stdout\]\n([^\[\n]|\[[^s\n])|\[stderr\]\n[^\n]`)

// check guards against both sides failing the same way (a bad stub path, a crashed binary): the
// transcript matches then, but nothing was tested.
func (s scenario) check(snap string, r *rig) string {
	if strings.Contains(snap, "[rc] 126") || strings.Contains(snap, "[rc] 127") {
		return "a command could not be started"
	}
	calls := r.read("calls.log")
	for _, want := range s.expect {
		if !strings.Contains(calls, want) {
			return fmt.Sprintf("chip never got %q", want)
		}
	}
	if !s.inert && r.happened() == "" {
		return "no chip or stub call, register or state file"
	}
	if s.inert && !s.silent && !reOutput.MatchString(snap) {
		return "no command printed anything"
	}
	return ""
}

func (r *rig) runs(lines ...string) {
	for _, l := range lines {
		r.run(strings.Fields(l)...)
	}
}

// settle gives background processes started by a command time to finish their first
// pass, then ends them.
func (r *rig) settle(d time.Duration) {
	r.sleep(d)
	r.stopAll()
	r.sleep(200 * time.Millisecond)
}

// quiet waits until the chip got no call for a while: the daemon finished a pass.
func (r *rig) quiet() {
	last := -1
	stable := 0
	for i := 0; i < 150; i++ {
		n := len(r.read("calls.log"))
		if n == last {
			stable++
			if stable >= 6 {
				return
			}
		} else {
			stable = 0
		}
		last = n
		r.sleep(100 * time.Millisecond)
	}
}

func scenarios() []scenario {
	base := []scenario{
		{name: "help", inert: true, run: func(r *rig) {
			r.runs("help", "-h", "--help", "bogus")
			r.run()
			r.run("")
		}},
		{name: "usage-errors", inert: true, run: func(r *rig) {
			r.runs("get", "get a b", "on", "off", "stop", "brightness x", "color x y", "color power 1 2", "blink power 1", "breath power", "identify", "identify-stop", "daemon bogus", "fx bogus", "on power extra")
		}},
		{name: "list", run: func(r *rig) {
			r.runs("list")
			r.write("stubs/chip.fail", "")
			r.runs("list")
		}},
		{name: "get", run: func(r *rig) {
			r.runs("get power", "get disk1", "get disk9", "get nope", "blink disk1 600 600", "get disk1", "blink disk2 100 100", "get disk2", "breath power 1200 900", "get power", "off disk3", "get disk3")
			r.write("run/phase-disk1", "1700000000000 1 1\n")
			r.runs("get disk1")
			r.write("stubs/chip.fail", "")
			r.runs("get power")
		}},
		{name: "all", run: func(r *rig) {
			r.runs("all", "blink disk1 600 600", "breath disk2 700 700", "color disk3 5 6 7", "off disk4", "all")
			r.write("stubs/chip.fail", "")
			r.runs("all")
		}},
		{name: "write-basic", run: func(r *rig) {
			r.runs("on power", "off disk1", "get disk1", "brightness disk1 128", "get disk1", "brightness disk1 0", "brightness disk1 300", "brightness disk1 abc",
				"brightness disk1 -5", "brightness nope 5", "color disk2 1 2 3", "color disk2 256 0 0", "color disk2 a b c", "color nope 1 2 3", "on nope", "off nope",
				"get disk2", "stop disk2", "blink disk2 100 100", "stop disk2", "get disk2", "breath netdev 1000 1000", "stop netdev", "get netdev", "off disk5", "stop disk5", "stop nope")
		}},
		{name: "write-effects", run: func(r *rig) {
			r.runs("blink disk1 450 450", "breath disk1 1200 900", "blink disk2 250 250", "blink disk2 250 249", "breath disk3 300 199", "blink nope 1 1", "blink disk1 x y", "get disk1", "get disk2", "get disk3")
		}},
		{name: "apply-none", inert: true, run: func(r *rig) { r.runs("apply") }},
		{name: "apply-config", setup: func(r *rig) {
			r.config("power_color", "10 20 30", "power_brightness", "128", "power_effect", "breath", "power_on", "1500", "power_off", "1000",
				"disk1_color", "1 2 3", "disk1_brightness", "0", "disk1_effect", "blink",
				"disk2_effect", "none", "disk2_brightness", "200", "disk3_color", "300 1 1", "disk3_brightness", "77",
				"netdev_effect", "blink", "netdev_color", "9 9 9", "disk4_effect", "blink", "disk4_on", "800", "disk4_off", "300")
		}, run: func(r *rig) {
			r.runs("blink disk2 100 100", "apply", "all")
		}},
		{name: "config-parsing", setup: func(r *rig) {
			r.write("boot/leds.cfg", "# comment\nmode=\"status\"\nmode=\"manual\"\nbad line\nkey=unquoted\nfx_leds=\"power,disk1\"\nstandby_level=\"55\"\n  indented=\"1\"\nsync_color=\"1 2 3\"\nempty=\"\"\nnetdev_status=\"weird\"\nalert_level=\"alert\"")
		}, run: func(r *rig) { r.runs("status") }},

		{name: "status-default", run: func(r *rig) { r.runs("status") }},
		{name: "status-chip-down", run: func(r *rig) {
			r.write("stubs/chip.fail", "")
			r.runs("status")
		}},
		{name: "status-chip-down-cached", run: func(r *rig) {
			r.write("run/bus", "3\n")
			r.write("stubs/chip.fail", "")
			r.runs("status")
		}},
		{name: "status-probe-mem", run: func(r *rig) {
			r.write("stubs/probe.out", "{\n  \"bus\": \"/dev/mem\",\n  \"address\": \"0x3a\"\n}\n")
			r.runs("status")
			r.write("stubs/probe.out", "{\n  \"bus\": \"/dev/i2c-12\"\n}\n")
			r.runs("status")
		}},
		{name: "status-busy", setup: func(r *rig) {
			r.config("mode", "status", "fx_name", "rainbow", "fx_leds", "power,disk1", "sync_enabled", "0", "sync_color", "9 8 7", "standby_mode", "pulse", "standby_level", "40",
				"smart_enabled", "0", "error_enabled", "0", "activity_enabled", "0", "smart_color", "1 1 1", "disk_color_mode", "temperature", "netdev_status", "link",
				"alert_enabled", "1", "alert_level", "alert", "alert_color", "5 5 5", "shutdown_blink", "1", "shutdown_color", "4 4 4", "shutdown_effect", "breath",
				"fx_speed", "4", "fx_reverse", "1", "fx_idle", "1", "disk_error_color", "6 6 6", "power_brightness", "180", "disk1_brightness", "30", "disk2_brightness", "300",
				"netdev_speed_enabled", "1", "netdev_speed_1000", "1 2 3", "night_enabled", "1")
			r.write("var.ini", "mdResync=\"1953514552\"\nmdResyncPos=\"976757276\"\nmdResyncSize=\"1953514552\"\nmdResyncAction=\"recon D\"\n")
			r.notification("a", "alert")
			r.notification("b", "warning")
			r.notification("c", "normal")
			r.write("run/alert", "alert\n")
			r.write("run/identify-disk2", "tok\nx\n")
			r.write("run/identify-disk4", "tok\nx\n")
			r.sleeper("run/led.pid")
			r.sleeper("run/fx.pid")
			r.write("stubs/pgrep.found", "")
		}, run: func(r *rig) {
			r.runs("status", "daemon status", "fx status")
		}},
		{name: "status-net-bridge", setup: func(r *rig) {
			r.write("stubs/ip.out", "default via 10.0.0.1 dev shim-br0 metric 2\ndefault via 10.0.0.1 dev br0 proto static metric 1\n")
			r.mkdir("sys/class/net/br0/brif")
			for _, p := range []string{"veth1", "vnet0", "tap0", "docker0", "virbr0"} {
				r.mkdir("sys/class/net/br0/brif/" + p)
			}
			r.mkdir("sys/class/net/br0/brif/eth1")
			r.write("sys/class/net/br0/speed", "10000\n")
			r.write("sys/class/net/eth1/speed", "2500\n")
			r.write("sys/class/net/eth1/carrier", "1\n")
			r.write("sys/class/net/eth1/operstate", "up\n")
			r.write("sys/class/net/eth1/statistics/rx_bytes", "5\n")
			r.write("sys/class/net/eth1/statistics/tx_bytes", "5\n")
		}, run: func(r *rig) { r.runs("status") }},
		{name: "status-net-bond", setup: func(r *rig) {
			r.write("stubs/ip.out", "default via 10.0.0.1 dev bond0\n")
			r.write("sys/class/net/bond0/speed", "20000\n")
			r.write("sys/class/net/bond0/operstate", "up\n")
			r.symlink("sys/class/net/bond0/lower_eth0", "../eth0")
			r.symlink("sys/class/net/bond0/lower_eth1", "../eth1")
			r.symlink("sys/class/net/bond0/lower_veth9", "../eth1")
			r.write("sys/class/net/eth1/speed", "100\n")
			r.write("sys/class/net/eth1/carrier", "1\n")
			r.write("sys/class/net/wl0/carrier", "1\n")
			r.mkdir("sys/class/net/wl0/device")
			r.write("sys/class/net/wl0/speed", "300\n")
			r.write("run/maxspeed", "eth1 1000\n")
			r.write("stubs/ethtool.eth1", "Supported link modes: 100baseT/Full 1000baseT/Full\nSupported pause frame use: No\n")
		}, run: func(r *rig) {
			r.runs("status")
			r.write("stubs/ping.fail", "")
			r.runs("status")
		}},
		{name: "status-no-network", setup: func(r *rig) {
			r.write("stubs/ip.out", "")
			r.write("sys/class/net/eth0/carrier", "")
			r.write("sys/class/net/eth0/speed", "abc\n")
		}, run: func(r *rig) { r.runs("status") }},
		{name: "status-notify-env", setup: func(r *rig) {
			r.env["UGREEN_NOTIFY_DIR"] = r.path("alt")
			r.config("alert_enabled", "1")
			r.write("alt/unread/x.notify", "importance=\"warning\"\n")
			r.write("alt/unread/y.notify", "event=\"a\"\nimportance=alert\n")
			r.write("alt/unread/z.notify", "importance=\"alert\"")
			r.write("alt/unread/w.txt", "importance=\"alert\"\n")
		}, run: func(r *rig) { r.runs("status") }},

		{name: "bays-default", inert: true, run: func(r *rig) { r.runs("bays") }},
		{name: "bays-limits", inert: true, setup: func(r *rig) {
			r.write("dynamix.cfg", "[display]\nhot=\"40\"\nmax=\"50\"\nhot=\"99\"\n[notify]\npath=\"/x\"\n")
			r.write("disks.ini", diskSection("disk1", "sdc", "ST_A B", "DISK_OK", "0", "47", "2")+
				"[disk1b]\nname=\"disk9\"\ndevice=\"sdc\"\nstatus=\"DISK_OK\"\n"+
				"[cache]\nname=\"cache\"\ndevice=\"sdd\"\nid=\"NVME_1\"\nstatus=\"DISK_OK\"\nrotational=\"0\"\ntemp=\"31\"\nwarning=\"50\"\ncritical=\"60\"\n")
			r.config("smart_ack_ST_A_B", "5=8 errors=1")
			r.write("smart/disk1", fmt.Sprintf(smartTable, "8", "3"))
		}, run: func(r *rig) { r.runs("bays") }},
		{name: "bays-serial", inert: true, setup: func(r *rig) {
			r.config("bay1_serial", "SN6", "bay2_serial", "NOPE", "bay3_serial", "SN1")
		}, run: func(r *rig) { r.runs("bays") }},
		{name: "bays-udevadm", setup: func(r *rig) {
			r.remove("udev")
			r.mkdir("udev")
			r.write("stubs/udevadm.sdc", "ID_MODEL=X\nID_SERIAL_SHORT=FROMUDEVADM\n")
			r.config("bay5_serial", "FROMUDEVADM")
		}, run: func(r *rig) { r.runs("bays") }},
		{name: "bays-idx", product: "iDX6011 Pro", intended: "Pro bay order after noodlemctwoodle/unraid-ugreen-idx6011-pro src/monitor.sh:70-75 (Intel = bays 3,4; ASMedia = 1,2,5,6); the shell keeps the plain order", setup: idxPorts, run: func(r *rig) { r.runs("bays", "status") }},
		{name: "bays-idx-plain", product: "iDX6011", setup: idxPorts, run: func(r *rig) { r.runs("bays", "status") }},
		{name: "bays-idx-6012", product: "iDX6012", setup: idxPorts, run: func(r *rig) { r.runs("bays", "status") }},
		{name: "bays-idx-fallback", inert: true, product: "iDX6011 Pro", setup: func(r *rig) { r.remove("sys/class/ata_port") }, run: func(r *rig) { r.runs("bays") }},
		{name: "bays-no-disks", inert: true, setup: func(r *rig) {
			r.remove("disks.ini")
			r.remove("sys/block")
			r.remove("smart")
		}, run: func(r *rig) { r.runs("bays") }},
	}
	list := append(base, more()...)

	// What plain `go test` runs: no daemon, no sleeps, one scenario per area.
	fast := map[string]bool{"help": true, "usage-errors": true, "get": true, "all": true, "write-basic": true, "apply-config": true,
		"status-default": true, "status-chip-down": true, "bays-default": true, "model-DX4600 Pro": true, "model-iDX6011 Pro": true,
		"schedule-night": true, "daemon-stale-pid": true, "daemon-start-foreign": true, "smbus-missing": true}
	for i := range list {
		list[i].fast = fast[list[i].name]
	}
	return list
}

// idxPorts: ASMedia controller on ata3-6 (the rig's default disks keep ata1-2 on the Intel one).
func idxPorts(r *rig) {
	r.write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x1b21\n")
	for n := 3; n <= 6; n++ {
		r.mkdir(fmt.Sprintf("sys/devices/pci0000:00/0000:00:01.0/0000:01:00.0/ata%d", n))
		r.symlink(fmt.Sprintf("sys/class/ata_port/ata%d", n), fmt.Sprintf("../../devices/pci0000:00/0000:00:01.0/0000:01:00.0/ata%d", n))
	}
}

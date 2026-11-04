package nasctl

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
)

func TestAnonRemovesWhatIdentifies(t *testing.T) {
	a := Anon{Hostnames: []string{"Tower-Of-Jens", "tower-of-jens"}}
	for _, c := range []struct{ in, gone string }{
		{"link up aa:bb:cc:dd:ee:ff on eth0", "aa:bb:cc:dd:ee:ff"},
		{"lease 192.168.17.42 from 10.0.0.1", "192.168.17.42"},
		{"addr fe80::1234:5678:9abc:def0 scope link", "fe80::1234:5678:9abc:def0"},
		{"host Tower-Of-Jens ugreen-nas-ctl[12]: started", "Tower-Of-Jens"},
		{"ugreen-nas-ctl: connect tower-of-jens.local failed", "tower-of-jens"},
		{"Serial Number: ABC123SECRET", "ABC123SECRET"},
		{"serial=WD-WCC4N7XYZ123 smart ok", "WD-WCC4N7XYZ123"},
		{"drive WD-WCC4N7XYZ123 spun down", "WD-WCC4N7XYZ123"},
		{"drive S3Z9NB0K123456A spun down", "S3Z9NB0K123456A"},
		{"user=jens-secret logged in", "jens-secret"},
		{"share: family-photos mounted", "family-photos"},
		{"read /mnt/user/Family-Photos/x.jpg", "Family-Photos"},
		{"label=\"MY DISK\"", "MY DISK"},
	} {
		got := a.Line(c.in)
		if strings.Contains(strings.ToLower(got), strings.ToLower(c.gone)) {
			t.Errorf("%q -> %q still has %q", c.in, got, c.gone)
		}
	}
}

func TestAnonKeepsTheUsefulParts(t *testing.T) {
	a := Anon{Hostnames: []string{"nas"}}
	for _, s := range []string{
		"Nov  4 10:00:00 <host> ugreen-nas-ctl[123]: fan daemon started",
		"kernel 6.12.24-Unraid DXP4800 Plus it87 i2c-dev",
		"2026-11-04T10:00:00Z version 4.3",
	} {
		if got := a.Line(strings.Replace(s, "<host>", "nas", 1)); got != s {
			t.Errorf("%q -> %q", s, got)
		}
	}
}

func TestSlotNamesNeverShowAPoolName(t *testing.T) {
	h := fan.Hardware{Bays: []fan.Bay{{Bay: 1, Present: true, Role: "parity"}, {Bay: 2, Present: true, Role: "disk3"}, {Bay: 3, Present: true, Role: "Secret-Pool"}, {Bay: 4, Present: true}, {Bay: 5}}}
	if got := strings.Join(slotNames(h), ","); got != "parity,disk3,pool,unassigned,-" {
		t.Error(got)
	}
}

func TestLedConfigIsAllowListed(t *testing.T) {
	got := ledConfig([]byte("mode=\"status\"\nmode=\"fx\"\nmqtt_pass=\"hunter2\"\nnight_enabled=\"1\"\nbogus line\n"))
	if strings.Join(got, " ") != `mode=fx night_enabled=1` {
		t.Error(got)
	}
}

func TestLogLinesKeepOnlyThePluginsAndTheLastN(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("Nov 4 10:00:00 tower sshd[1]: login from 192.168.1.5\n")
	for i := range 40 {
		sb.WriteString("Nov 4 10:00:00 tower ugreen-nas-fan[7]: tick " + string(rune('A'+i%26)) + " mac aa:bb:cc:dd:ee:ff on tower\n")
	}
	got := logLines(Anon{Hostnames: []string{"tower"}}, sb.String(), 30)
	if len(got) != 30 {
		t.Fatalf("%d lines", len(got))
	}
	all := strings.Join(got, "\n")
	for _, s := range []string{"sshd", "192.168", "aa:bb", " tower "} {
		if strings.Contains(all, s) {
			t.Errorf("log has %q", s)
		}
	}
}

func TestFailingSectionPrintsItsErrorAndTheRestStays(t *testing.T) {
	secs := []Section{
		section("LED", func() Section { panic("boom") }),
		fanSection(FanDiag{Err: "timed out after 30s"}),
		logSection(nil, ""),
	}
	out := RenderDiag(Anon{}, time.Unix(0, 0), "", Report{}, nil, secs)
	for _, want := range []string{"ERROR: internal error: boom", "ERROR: timed out after 30s", "no ugreen-* lines"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

func TestEnvSectionNamesLeftoverFoldersAndModules(t *testing.T) {
	s := envSection(EnvDiag{
		Plugins: []string{"dynamix/", "ugreenleds-driver/", "fanctrlplus.plg", "ugreen-nas-control/", "unrelated.plg", "my-secret-share.plg"},
		Modules: []string{"it87", "it87_wdt", "i2c_i801", "e1000e", "led_ugreen"},
		Hwmon:   []string{"hwmon1=it8613"},
	})
	for _, want := range []string{"ugreenleds-driver/", "fanctrlplus.plg", "it87_wdt", "led_ugreen", "hwmon1=it8613"} {
		if !strings.Contains(s.Body, want) {
			t.Errorf("missing %q:\n%s", want, s.Body)
		}
	}
	for _, no := range []string{"unrelated", "e1000e", "my-secret-share"} {
		if strings.Contains(s.Body, no) {
			t.Errorf("has %q", no)
		}
	}
}

func goldenDiag(t *testing.T) string {
	two := 40
	h := fan.Hardware{Model: "DXP4800 Plus", Bays: []fan.Bay{{Bay: 1, Present: true, HDD: true, Role: "parity", Model: "WDC-SECRET", Temp: &two}, {Bay: 2}}}
	rep := Report{
		Schema: 1, PluginVersion: "4.3", UnraidVersion: "7.1.2",
		Machine: Machine{SysVendor: "UGREEN", Product: "DXP4800 Plus", Family: "NAS", BoardVendor: "UGREEN", Board: "UGREEN-BOARD", BIOSVendor: "AMI", BIOSVersion: "1.2", BIOSDate: "03/01/2024"},
		CPU:     "Intel(R) N100", Threads: 4,
		Fan:  FanReport{Found: true, Chip: "it8613", Revision: 5.0, Base: "0x0a00"},
		LED:  LEDReport{Supported: true, Probe: "ok", Bus: "/dev/i2c-4", Address: "0x3a", LEDCount: 6},
		Bays: []BayReport{{Bay: 1, Port: 1, Present: true, Kind: "hdd"}, {Bay: 2, Port: 2}},
		NVMe: []string{"m2-1"}, Network: []NetReport{{Name: "eth0", MaxMbit: 2500}}, Memory: []MemReport{{SizeMB: 16384, Type: "DDR5", SpeedMT: 4800}},
		Rivals: []string{},
	}
	status := `{"fan_control": true, "chip": "it8613", "fans": [{"id":"cpu","role":"cpu","rpm":1200,"pct":40,"auto":false},{"id":"case","role":"case","rpm":900,"pct":null,"auto":true}],
	 "sensors": {"cpu": 45, "board": 38}, "daemon": {"mode": "auto", "failsafe": "", "cpu_temp": 45, "case_temp": 38, "case_from": "nvme", "case_driver": "case", "targets": {"cpu": 40, "case": 30}},
	 "watchdog": {"armed": true, "timeout": 60, "interval": 20, "last_pet": 990, "time": 1000, "state": "armed"}, "conflicts": [{"kind":"module","id":"it87","severity":"warn"}]}`
	led := LEDDiag{Enabled: true, Supported: true, DaemonRunning: true, Config: []string{"mode=status"}, Bays: rep.Bays, Slots: slotNames(h),
		Modprobe: "loaded i2c-dev for the scan (modprobe i2c-dev, the only change diag makes)",
		Get:      "power on 255 255 255 255 0 0\ndisk1 on 255 0 255 0 0 0\n", Scan: "/dev/i2c-4: 0x3a\n"}
	ev, reg := evidenceFixture(t)
	modelSec, _ := modelSection(reg, ev)
	js := evidenceSection(ev)
	js.Lang = "json"
	checks := diagChecks(Facts{Report: rep, LED: led, Env: EnvDiag{Plugins: []string{"ugreenleds-driver/"}, Modules: []string{"it87"}}})
	secs := []Section{
		modelSec,
		ledSection(led),
		rivalSection(RivalDiag{Procs: []RivalProc{{Name: "FanCtrl Plus", Program: "fanctrlplus_loop.sh", PID: 7, Up: "1h0m0s", Controls: "fan"}}, FanOn: true}),
		fanSection(FanDiag{Status: []byte(status), DaemonRunning: true}),
		dmiSection(goldenDMI(t)),
		drivesSection([]byte(disksINI), nil, false),
		settingsSection([]byte(`mode="auto"\nmqtt_host="broker.lan"\n`), []byte(`mode="status"\n`), nil, nil, false),
		envSection(EnvDiag{Plugins: []string{"ugreen-nas-control/", "ugreenleds-driver/"}, OwnFiles: []string{"fan.cfg", "leds.cfg"}, Modules: []string{"it87", "i2c_dev"}, Hwmon: []string{"hwmon1=it8613"}, DevI2C: []string{"i2c-4"}}),
		logSection([]string{"Nov  4 10:00:00 <host> ugreen-nas-ctl[1]: fan daemon started"}, ""),
		js,
	}
	return RenderDiag(Anon{}, time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC), "6.12.24-Unraid", rep, checks, secs)
}

func TestDiagLayout(t *testing.T) {
	got := goldenDiag(t)
	const golden = "testdata/diag.golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("layout changed (UPDATE_GOLDEN=1 rewrites it):\n%s", got)
	}
	if strings.Contains(got, "WDC-SECRET") {
		t.Error("a drive model is in the output")
	}
}

const disksINI = `["parity"]
idx="0"
name="parity"
device="sde"
id="WDC_WD240KFGX-68CJNN0_65G6SJXL"
rotational="1"
spundown="1"
status="DISK_OK"
temp="*"
type="Parity"
transport="ata"
["disk1"]
name="disk1"
device=""
id=""
status="DISK_NP"
type="Data"
["Family-Media"]
name="Family-Media"
device="nvme0n1"
id="Samsung_SSD_980_S6B0NJ0SECRET"
rotational="0"
spundown="0"
status="DISK_OK"
temp="38"
type="Cache"
transport="nvme"
`

func TestDrivesHaveNoIdentity(t *testing.T) {
	s := drivesSection([]byte(disksINI), nil, true)
	for _, no := range []string{"WDC", "65G6SJXL", "Family-Media", "Samsung", "SECRET", "nvme0n1", "sde"} {
		if strings.Contains(s.Body, no) {
			t.Errorf("drives leak %q:\n%s", no, s.Body)
		}
	}
	for _, want := range []string{"parity: Parity ata hdd", "disk1: empty", "pool1: Cache nvme ssd", "Unassigned Devices plugin installed: true"} {
		if !strings.Contains(s.Body, want) {
			t.Errorf("missing %q:\n%s", want, s.Body)
		}
	}
}

func TestSettingsRedactSecretLookingKeys(t *testing.T) {
	cfg := `mode="custom"
mqtt_enabled="1"
mqtt_host="broker.lan"
mqtt_user="jens"
mqtt_password="hunter2"
mqtt_topic_prefix="home/nas"
api_token="abc"
ignore_drives="WDC_WD80_SECRET"
notify_email_addr="me@example.org"
min_pct="10"
`
	got := strings.Join(redactCfg([]byte(cfg), 100, fanCfgShown), "\n")
	for _, no := range []string{"broker.lan", "jens", "hunter2", "home/nas", "abc", "WDC_WD80_SECRET", "me@example.org"} {
		if strings.Contains(got, no) {
			t.Errorf("settings leak %q:\n%s", no, got)
		}
	}
	for _, want := range []string{`mode=custom`, `mqtt_enabled=1`, `min_pct=10`, `mqtt_host: <hidden>`, `api_token: <hidden>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestDMIOmitsSerialsAndUUID(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{"product_name": "DXP4800 Plus", "product_serial": "SN-SECRET", "product_uuid": "UUID-SECRET", "board_serial": "BS-SECRET", "chassis_asset_tag": "ASSET-SECRET", "bios_version": "1.2 aa:bb:cc:dd:ee:ff"} {
		write(t, dir+"/"+name, text+"\n")
	}
	s := dmiSection(dir)
	if !strings.Contains(s.Body, "product_name: DXP4800 Plus") || strings.Contains(s.Body, "SECRET") || strings.Contains(s.Body, "aa:bb") {
		t.Error(s.Body)
	}
}

func TestChecksNameTheProblems(t *testing.T) {
	no := false
	got := strings.Join(diagChecks(Facts{
		Report:        Report{LED: LEDReport{Probe: "failed"}},
		LED:           LEDDiag{Enabled: false, Untested: true, Modprobe: "loaded i2c-dev for the scan", Scan: `{"/dev/i2c-4": {"answer": []}}`},
		FanSt:         &fanStatus{FanControl: &no},
		Env:           EnvDiag{Plugins: []string{"ugreenleds-driver/", "ugreen-nas-control/"}, Modules: []string{"it87"}},
		Failed:        []string{"dmesg"},
		Fan:           FanDiag{},
		FanDaemonDown: true,
	}), "\n")
	for _, want := range []string{"LED control is off", "model untested", "i2c-dev was not loaded", "no I2C device answers at 0x3a", "fan chip not found", "fan control is off", "it87 loaded", "leftover plugin folder: ugreenleds-driver/", "section failed: dmesg"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func goldenDMI(t *testing.T) string {
	dir := t.TempDir()
	write(t, dir+"/product_name", "DXP4800 Plus\n")
	write(t, dir+"/board_name", "UGREEN-BOARD\n")
	write(t, dir+"/product_serial", "SN-SECRET\n")
	return dir
}

func rivalCtl(t *testing.T) *Ctl {
	dir := t.TempDir()
	env := Env{
		Proc: dir + "/proc", PluginsBoot: dir + "/boot", PluginsWeb: dir + "/web", ProcModules: dir + "/modules",
		Hwmon: dir + "/hwmon", CronD: dir + "/cron.d", DMIDir: dir + "/dmi", FanCfg: dir + "/none",
	}
	write(t, env.DMIDir+"/product_name", "DXP4800 Plus\n")
	// FanCtrl Plus 2 runs as a shell script; 4242 is its parent, 4300 a child. A grep that only mentions it is no rival.
	write(t, env.Proc+"/4300/cmdline", "/bin/bash\x00/usr/local/emhttp/plugins/fanctrlplus2/scripts/fanctrlplus2_loop.sh\x00")
	write(t, env.Proc+"/4242/cmdline", "/bin/bash\x00/usr/local/emhttp/plugins/fanctrlplus2/scripts/fanctrlplus2_loop.sh\x00")
	write(t, env.Proc+"/4301/cmdline", "grep\x00fanctrlplus2_loop.sh\x00")
	write(t, env.Proc+"/4242/stat", "4242 (bash) S 1 4242 4242 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 10000 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0\n")
	write(t, env.Proc+"/uptime", "1000.00 900.00\n")
	write(t, env.PluginsBoot+"/fanctrlplus2.plg", "<!ENTITY version \"2024.5\">\n")
	write(t, env.PluginsWeb+"/fanctrlplus2/x", "")
	write(t, env.PluginsBoot+"/dynamix.system.autofan.plg", "<!ENTITY version \"1.0\">\n")
	write(t, env.PluginsBoot+"/ugreenleds-driver/leftover", "")
	write(t, env.ProcModules, "it87 20480 0 - Live 0x0\ni2c_i801 1 0 - Live 0x0\n")
	write(t, env.Hwmon+"/hwmon3/name", "it8613\n")
	write(t, env.Hwmon+"/hwmon3/pwm1_enable", "1\n")
	write(t, env.CronD+"/root", "*/5 * * * * /usr/local/bin/autofan --pwm1 hunter2\n")
	write(t, env.PluginsBoot+"/user.scripts/scripts/Secret Fan Script/script", "#!/bin/bash\necho 100 > /sys/class/hwmon/hwmon3/pwm1\n")
	write(t, env.PluginsBoot+"/user.scripts/schedule.json", `{"`+env.PluginsBoot+`/user.scripts/scripts/Secret Fan Script/script": {"frequency": "custom"}}`)
	return New(env, nil, nil)
}

func TestRivalsShowWhoRunsNowAndWhoIsOnlyInstalled(t *testing.T) {
	d := rivalCtl(t).rivalDiag(nil)
	if len(d.Procs) != 1 || d.Procs[0].PID != 4242 || d.Procs[0].Program != "fanctrlplus2_loop.sh" || d.Procs[0].Up != "15m0s" {
		t.Fatalf("procs: %+v", d.Procs)
	}
	checks := strings.Join(rivalChecks(d), "\n")
	first, _, _ := strings.Cut(checks, "\n")
	if first != "CONFLICT: FanCtrl Plus 2 is running (pid 4242) and writes the same fan chip" {
		t.Errorf("first check: %q", first)
	}
	for _, want := range []string{"rival installed, not running: Dynamix Auto Fan Control", "CONFLICT? a user script calls a fan or LED tool: pwm1 (scheduled)", "pwm in manual mode: it8613 pwm1 manual"} {
		if !strings.Contains(checks, want) {
			t.Errorf("missing %q:\n%s", want, checks)
		}
	}
	if strings.Contains(checks, "rival installed, not running: FanCtrl Plus 2") {
		t.Errorf("a running rival is also listed as not running:\n%s", checks)
	}
	body := rivalSection(d).Body
	for _, want := range []string{"RUNNING: FanCtrl Plus 2 (fanctrlplus2_loop.sh) pid 4242, up 15m0s", "UGREEN LED Control", "leftover folder, no .plg", "it87 (fan)", "cron files that run such tools: 1 (1: autofan,pwm1)", "yields: no"} {
		if want == "UGREEN LED Control" {
			want = "UGREEN LEDs driver: leftover folder, no .plg"
		}
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Secret Fan Script") || strings.Contains(body, "cron.d") || strings.Contains(body, "root:") {
		t.Errorf("a user-chosen name leaked:\n%s", body)
	}
	if strings.Contains(body, "hunter2") {
		t.Errorf("a cron argument leaked:\n%s", body)
	}
}

func TestEveryRegistryModelResolves(t *testing.T) {
	models, err := loadRegistry(modelsJSON)
	if err != nil || len(models) < 10 {
		t.Fatalf("registry: %d models, %v (run just docs)", len(models), err)
	}
	for _, m := range models {
		got := matchModel(models, m.FixtureDMI)
		if got == nil || got.ID != m.ID {
			t.Errorf("%s: the DMI name %q resolves to %v (a new model in web/models needs `just docs`; a pattern that overlaps another must come after it)", m.ID, m.FixtureDMI, got)
		}
		if m.Fan != nil {
			want := m.Fan.Kind
			if want == "" {
				want = fan.KindFixed
			}
			if k := fan.FanKind(m.FixtureDMI); k != want && (want != fan.KindFixed || k != fan.KindReadOnly) {
				t.Errorf("%s: registry fan kind %q, backend says %q", m.ID, want, k)
			}
			if (m.Fan.Watchdog != nil) && !fan.WatchdogModel(m.FixtureDMI) {
				t.Errorf("%s: registry has a watchdog, backend does not feed one", m.ID)
			}
		}
	}
	if matchModel(models, "Some Other Box") != nil {
		t.Error("an unknown name matched")
	}
}

func evidenceFixture(t *testing.T) (Evidence, *RegModel) {
	models, _ := loadRegistry(modelsJSON)
	reg := matchModel(models, "DXP4800 GT")
	if reg == nil {
		t.Fatal("DXP4800 GT is not in the registry")
	}
	rep := Report{
		PluginVersion: "5.0.0", UnraidVersion: "7.1.2",
		Machine: Machine{Product: "DXP4800 GT", SysVendor: "UGREEN", BIOSVersion: scrub("1.2 aa:bb:cc:dd:ee:ff")},
		Fan:     FanReport{Found: true, Chip: "IT5571"},
		LED:     LEDReport{Supported: true, Probe: "ok"},
		Bays:    []BayReport{{Bay: 1}, {Bay: 2}},
		NVMe:    []string{"m2-1", "m2-2"}, Network: []NetReport{{Name: "eth0"}, {Name: "eth1"}},
	}
	cpu := 50
	st := &fanStatus{Fans: []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		RPM  int    `json:"rpm"`
		Pct  *int   `json:"pct"`
		Auto bool   `json:"auto"`
		Err  string `json:"error"`
	}{{ID: "fan1", Role: "cpu", RPM: 1000}, {ID: "fan3", Role: "case", RPM: 0}}}
	st.Sensors.CPU = &cpu
	ev := BuildEvidence(EvInputs{Reg: reg, Now: time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC), Report: rep, FanSt: st,
		Rivals: RivalDiag{Procs: []RivalProc{{Name: "FanCtrl Plus", Program: "fanctrlplus_loop.sh", PID: 7}}}})
	return ev, reg
}

func TestEvidenceComparesTheRegistryWithWhatWasDetected(t *testing.T) {
	ev, reg := evidenceFixture(t)
	sec, checks := modelSection(reg, ev)
	all := strings.Join(checks, "\n")
	for _, want := range []string{"MISMATCH: bays: registry expects 4, detected 2", "MISMATCH: fans_spin"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(all, "fan_chip") || strings.Contains(all, "fan_headers") || strings.Contains(all, "nvme_slots") {
		t.Errorf("a matching value is reported as a mismatch:\n%s", all)
	}
	if !strings.Contains(sec.Body, "registry: dxp4800-gt (DXP4800 GT)") || !strings.Contains(sec.Body, "leds: verified on 5.0.0") {
		t.Errorf("model section:\n%s", sec.Body)
	}
}

func TestEvidenceJSONHasNoRedactedFields(t *testing.T) {
	ev, _ := evidenceFixture(t)
	data, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if secretKeyRe.MatchString(k) {
					t.Errorf("field %q is on the redaction list", k)
				}
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(tree)
	for _, s := range secrets {
		if strings.Contains(strings.ToLower(string(data)), strings.ToLower(s)) {
			t.Errorf("the evidence contains %q", s)
		}
	}
	var got Evidence
	if err := json.Unmarshal(data, &got); err != nil || got.Schema != "diag/1" || got.Model.ID != "dxp4800-gt" || got.Rivals.Running[0] != "FanCtrl Plus (fanctrlplus_loop.sh)" {
		t.Errorf("%v %+v", err, got)
	}
}

func TestUnknownModelPointsToAModelReport(t *testing.T) {
	ev := BuildEvidence(EvInputs{Now: time.Unix(0, 0), Report: Report{Machine: Machine{Product: "DXP9999"}}})
	sec, checks := modelSection(nil, ev)
	if ev.Model.Known || ev.Model.ID != "unknown" || !strings.Contains(sec.Body, "DXP9999") || !strings.Contains(strings.Join(checks, "\n"), "model report") {
		t.Errorf("%+v\n%s\n%v", ev.Model, sec.Body, checks)
	}
}

func TestEvidenceSectionIsValidJSON(t *testing.T) {
	ev, _ := evidenceFixture(t)
	var got Evidence
	body := evidenceSection(ev).Body
	if err := json.Unmarshal([]byte(body), &got); err != nil || got.Schema != "diag/1" || len(got.Areas) != 5 {
		t.Errorf("%v\n%s", err, body)
	}
	if lines := strings.Count(body, "\n"); lines > 20 {
		t.Errorf("%d lines", lines)
	}
}

func TestSettingsAllowListHidesUnknownKeysAndInputNames(t *testing.T) {
	cfg := "mode=\"custom\"\ncase_input=\"script:mysecretscript\"\nfan.fan1.input=\"nic:eth0\"\nfan.fan1.deadband=\"3\"\nfoo_bar=\"x\"\nignore_drives=\"disk1\"\n"
	got := strings.Join(redactCfg([]byte(cfg), 100, fanCfgShown), "\n")
	for _, no := range []string{"mysecretscript", "eth0", "=x", "disk1"} {
		if strings.Contains(got, no) {
			t.Errorf("leak %q:\n%s", no, got)
		}
	}
	for _, want := range []string{"case_input=script", "fan.fan1.input=nic", "fan.fan1.deadband=3", "foo_bar: <hidden>", "ignore_drives: <hidden>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestITKunyRunningShowsOnceAsRunning(t *testing.T) {
	d := RivalDiag{
		Procs:   []RivalProc{{Name: "UGREEN fan control (IT-Kuny)", Program: "ugreen-fan-control", Plugin: "ugreen-it87", Controls: "fan", PID: 9}},
		Plugins: []RivalPlugin{{ID: "ugreen-it87", Name: "UGREEN it87 driver (IT-Kuny)", Plg: true}},
	}
	body, checks := rivalSection(d).Body, strings.Join(rivalChecks(d), "\n")
	if strings.Count(body, "IT-Kuny") != 1 || strings.Contains(body, "installed, not running") || strings.Contains(checks, "not running") {
		t.Errorf("body:\n%s\nchecks:\n%s", body, checks)
	}
}

func TestRunningRivalIsOneCheckLine(t *testing.T) {
	f := Facts{Rivals: RivalDiag{Procs: []RivalProc{{Name: "FanCtrl Plus", Program: "fanctrlplus_loop.sh", Controls: "fan", PID: 7}}},
		FanSt: &fanStatus{Conflicts: []struct {
			Kind     string `json:"kind"`
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Detail   string `json:"detail"`
		}{{Kind: "daemon", ID: "fanctrlplus_loop.sh", Severity: "block"}}}}
	if got := strings.Join(diagChecks(f), "\n"); strings.Contains(got, "rival running") {
		t.Errorf("duplicate:\n%s", got)
	}
}

func TestIPv4KeepsVersions(t *testing.T) {
	a := Anon{}
	for _, keep := range []string{"1.00.00.05", "V1.0.0.12", "5.0.0", "BIOS 1.00.00.05 (2024)"} {
		if got := a.Text(keep); got != keep {
			t.Errorf("%q became %q", keep, got)
		}
	}
	for in, want := range map[string]string{"192.168.1.10": "<removed>", "10.0.0.2/24": "<removed>/24", "at 192.168.1.10.": "at <removed>.", "ip=10.1.2.3, x": "ip=<removed>, x"} {
		if got := a.Text(in); got != want {
			t.Errorf("%q became %q, want %q", in, got, want)
		}
	}
	if got := a.Meta("1.0.0.12"); got != "1.0.0.12" {
		t.Errorf("header: %q", got)
	}
}

func TestCutIsRuneSafe(t *testing.T) {
	if got := cut("aä", 2); got != "a..." || !utf8.ValidString(got) {
		t.Errorf("%q", got)
	}
	if got := short(strings.Repeat("ü", 300), 201); !utf8.ValidString(got) {
		t.Errorf("%q", got)
	}
}

func TestPanickingSectionKeepsTheRest(t *testing.T) {
	var s sections
	s.add("a", func() Section { panic("boom") })
	s.add("b", func() Section { return Section{Title: "b", Body: "ok"} })
	if len(s.list) != 2 || s.list[1].Body != "ok" || !strings.Contains(s.list[0].Err, "boom") || len(s.failed) != 1 {
		t.Errorf("%+v", s)
	}
	if guard(func() { panic("x") }) == "" {
		t.Error("guard swallowed the panic silently")
	}
}

func TestTimeoutKillsTheProcessGroup(t *testing.T) {
	start := time.Now()
	_, err := runTimeout(300*time.Millisecond, "/bin/sh", "-c", "sleep 30 & sleep 30")
	if err == nil || time.Since(start) > 5*time.Second {
		t.Errorf("err %v after %s", err, time.Since(start))
	}
}

func TestOwnFilesKeepKnownNames(t *testing.T) {
	if got := ownFiles([]string{"fan.cfg", "my-notes.txt", "leds.cfg"}); strings.Join(got, ",") != "fan.cfg,leds.cfg" {
		t.Errorf("%v", got)
	}
}

// secretKeyRe: JSON field names of the evidence must not look like they hold a credential, host or drive.
var secretKeyRe = regexp.MustCompile(`(?i)pass|token|secret|host|user|ip|mac|topic|key|url|addr|serial|ignore|script|path|drive`)

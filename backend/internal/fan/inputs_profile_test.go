package fan

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/profile"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// The fixtures are synthetic copies of the config shapes of the rival tools (profile/testdata); the goldens are what
// the importers make of them on a NAS with a fixed set of drives.

func testEnv() profile.Env {
	disks := map[string]string{
		"ata-EXAMPLE_DISK_A_SN0001":    "disk1",
		"ata-EXAMPLE_DISK_B_SN0002":    "disk2",
		"ata-EXAMPLE_HDD_4TB_SN1111":   "disk1",
		"ata-EXAMPLE_HDD_4TB_SN2222":   "disk2",
		"nvme-EXAMPLE_NVME_1TB_SN3333": "nvme0",
		"ata-EXAMPLE_HDD_SN1":          "disk1",
		"nvme-EXAMPLE_NVME_SN2":        "cache",
	}
	devices := map[string]string{"sda": "disk1", "sdb": "disk2", "sdc": "disk3", "nvme0n1": "nvme0", "nvme0": "nvme0"}
	return profile.Env{
		ResolveDisk:   func(id string) (string, bool) { n, ok := disks[id]; return n, ok },
		ResolveDevice: func(d string) (string, bool) { n, ok := devices[d]; return n, ok },
	}
}

func readDirFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = b
	}
	return files
}

func checkGolden(t *testing.T, name string, res *profile.Result) {
	t.Helper()
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	path := filepath.Join("profile", "testdata", "golden", name+".json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (UPDATE_GOLDEN=1 writes it)", err)
	}
	// Compared as JSON: the repo formatter may lay the golden files out differently.
	var got, exp any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &exp); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("%s differs from its golden:\n%s", name, b)
	}
}

func TestImportersGolden(t *testing.T) {
	rules, env := ProfileRules{}, testEnv()
	root := filepath.Join("profile", "testdata")
	type job struct {
		kind, dir string
	}
	var jobs []job
	for _, kind := range []string{"fanctrlplus", "fanctrlplus2", "dynamix-autofan"} {
		entries, _ := os.ReadDir(filepath.Join(root, kind))
		for _, e := range entries {
			if e.IsDir() && e.Name() != "backup" {
				jobs = append(jobs, job{kind, e.Name()})
			}
		}
	}
	for _, j := range jobs {
		files := readDirFiles(t, filepath.Join(root, j.kind, j.dir))
		var res *profile.Result
		var err error
		switch j.kind {
		case "fanctrlplus":
			res, err = profile.ParseFanCtrlPlus(files, 1, env, rules)
		case "fanctrlplus2":
			res, err = profile.ParseFanCtrlPlus(files, 2, env, rules)
		default:
			res, err = profile.ParseDynamix(files, env, rules)
		}
		if err != nil {
			t.Errorf("%s/%s: %v", j.kind, j.dir, err)
			continue
		}
		checkGolden(t, j.kind+"__"+j.dir, res)
	}

	// The FanCtrl Plus 2 JSON backup gives the same blocks as the files of its folder.
	backup, err := os.ReadFile(filepath.Join(root, "fanctrlplus2", "backup", "fanctrlplus2-backup.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := profile.ParseFanCtrlPlus2Backup(backup, env, rules)
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "fanctrlplus2__backup", res)
	folder, err := profile.ParseFanCtrlPlus(readDirFiles(t, filepath.Join(root, "fanctrlplus2", "typical-multifan")), 2, env, rules)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(res.Profile.Fan) != fmt.Sprint(folder.Profile.Fan) {
		t.Errorf("backup %v != folder %v", res.Profile.Fan, folder.Profile.Fan)
	}

	itk, _ := filepath.Glob(filepath.Join(root, "itkuny-ugreen-fan", "*.env"))
	if len(itk) != 4 {
		t.Fatalf("env fixtures: %v", itk)
	}
	for _, p := range itk {
		b, _ := os.ReadFile(p)
		res, err := profile.ParseITKuny(b, rules)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		checkGolden(t, "itkuny__"+strings.TrimSuffix(filepath.Base(p), ".env"), res)
	}
}

// Every value an importer produces is one LoadConfig takes as it is.
func TestImportedSettingsLoadWithoutWarnings(t *testing.T) {
	rules, env := ProfileRules{}, testEnv()
	var results []*profile.Result
	root := filepath.Join("profile", "testdata")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || filepath.Base(filepath.Dir(p)) == "testdata" || filepath.Base(p) == "backup" || filepath.Base(p) == "golden" {
			return nil
		}
		if filepath.Dir(filepath.Dir(p)) != root {
			return nil
		}
		files := readDirFiles(t, p)
		var res *profile.Result
		switch filepath.Base(filepath.Dir(p)) {
		case "fanctrlplus":
			res, _ = profile.ParseFanCtrlPlus(files, 1, env, rules)
		case "fanctrlplus2":
			res, _ = profile.ParseFanCtrlPlus(files, 2, env, rules)
		case "dynamix-autofan":
			res, _ = profile.ParseDynamix(files, env, rules)
		}
		if res != nil {
			results = append(results, res)
		}
		return nil
	})
	envs, _ := filepath.Glob(filepath.Join(root, "itkuny-ugreen-fan", "*.env"))
	for _, p := range envs {
		b, _ := os.ReadFile(p)
		if res, err := profile.ParseITKuny(b, rules); err == nil {
			results = append(results, res)
		}
	}
	if len(results) < 10 {
		t.Fatalf("only %d results", len(results))
	}
	for _, res := range results {
		cfg := writeFanCfg(t, res.Profile.Fan)
		c := LoadConfig(cfg)
		if len(c.Warnings) > 0 {
			t.Errorf("%s: %v", res.Title, c.Warnings)
		}
		for k, v := range res.Profile.Fan {
			if got := EffectiveSettings(c)[k]; got != v && k != "mode" && !strings.HasSuffix(k, "_curve") {
				t.Errorf("%s: %s = %q is loaded as %q", res.Title, k, v, got)
			}
		}
		if _, _, err := (ProfileRules{}).Normalize("min_pct", res.Profile.Fan["min_pct"]); res.Profile.Fan["min_pct"] != "" && err != nil {
			t.Error(err)
		}
	}
}

func writeFanCfg(t *testing.T, fanSettings map[string]string) string {
	t.Helper()
	keys := make([]string, 0, len(fanSettings))
	for k := range fanSettings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%q\n", k, fanSettings[k])
	}
	for _, k := range keys {
		if !isV1Setting(k) {
			sb.WriteString("cfg_version=\"2\"\n")
			break
		}
	}
	p := filepath.Join(t.TempDir(), "fan.cfg")
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProfileRoundTrip(t *testing.T) {
	cfgText := `cfg_version="2"
mode="custom"
cpu_curve="40:20,60:50,80:100"
case_curve="30:25,45:50,55:100"
min_pct="22"
cpu_crit="88"
ignore_drives="disk3,cache"
case_input="max:disk_max:disk1+disk2|script:probe"
case_deadband="3"
case_on_missing="hold"
cpu_up_step="20"
fan.fan2.min_run="35"
fan.fan2.curve="30:20,50:60"
quiet_enabled="1"
quiet_days="mon,tue,sat"
quiet_cap="45"
job_mover="floor"
job_mover_pct="55"
`
	cfg := filepath.Join(t.TempDir(), "fan.cfg")
	if err := os.WriteFile(cfg, []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadConfig(cfg)
	if len(c.Warnings) > 0 {
		t.Fatal(c.Warnings)
	}
	p := ExportProfile(c, "DXP6800 Pro", time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC))
	b, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > profile.MaxBytes {
		t.Fatalf("an export of %d bytes is over the upload limit", len(b))
	}
	res, err := profile.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	res.Finish(ProfileRules{})
	if len(res.Warnings) > 0 || len(res.NotImported) > 0 {
		t.Fatalf("a clean export must come back clean: %v %v", res.Warnings, res.NotImported)
	}
	if fmt.Sprint(res.Profile.Fan) != fmt.Sprint(p.Fan) || res.Profile.Token() != p.Token() {
		t.Fatalf("round trip changed the settings:\n%v\n%v", p.Fan, res.Profile.Fan)
	}
	if res.Profile.Model != "DXP6800 Pro" || res.Profile.Created != "2026-03-04T05:06:07Z" {
		t.Fatalf("header: %+v", res.Profile)
	}

	// Applying it to an empty NAS reproduces the effective settings.
	c2 := LoadConfig(writeFanCfg(t, res.Profile.Fan))
	if fmt.Sprint(EffectiveSettings(c2)) != fmt.Sprint(EffectiveSettings(c)) {
		t.Fatalf("effective settings differ:\n%v\n%v", EffectiveSettings(c), EffectiveSettings(c2))
	}
}

func TestExportOfAV1ConfigHasNoV2Settings(t *testing.T) {
	c := LoadConfig(filepath.Join(t.TempDir(), "none.cfg"))
	p := ExportProfile(c, "", time.Now())
	for k := range p.Fan {
		if !isV1Setting(k) {
			t.Errorf("v1 export carries %s", k)
		}
	}
	if len(p.Fan) != 10 {
		t.Errorf("settings: %v", p.Fan)
	}
	for _, never := range []string{"fan_control", "ui_lang", "ui_theme", "cfg_version"} {
		if _, ok := p.Fan[never]; ok {
			t.Errorf("%s must not be in a profile", never)
		}
	}
}

func TestProfileClampAndRefusals(t *testing.T) {
	doc := func(schema int, kind string, fan string) []byte {
		return []byte(fmt.Sprintf(`{"schema":%d,"kind":%q,"fan":%s,"future_field":{"x":1}}`, schema, kind, fan))
	}
	res, err := profile.Decode(doc(1, profile.Kind, `{"min_pct":"99","cpu_crit":"5","disk_crit":"abc","nvme_offset":"-4","mode":"banana","bogus":"1","fan_control":"0","cpu_curve":"80:100,40:20,40:30","case_deadband":"77","quiet_cap":"3","case_input":"nic:eth0 ; rm","fan.fan9.curve":"30:20,50:60","case_up_step":"0"}`))
	if err != nil {
		t.Fatal(err)
	}
	res.Finish(ProfileRules{})
	want := map[string]string{
		"min_pct": "60", "cpu_crit": "70", "nvme_offset": "0", "cpu_curve": "40:30,80:100", "case_deadband": "10", "quiet_cap": "10", "case_up_step": "1",
	}
	if fmt.Sprint(res.Profile.Fan) != fmt.Sprint(want) {
		t.Errorf("clamped:\n got %v\nwant %v", res.Profile.Fan, want)
	}
	dropped := map[string]bool{}
	for _, s := range res.NotImported {
		dropped[s.Key] = true
	}
	for _, k := range []string{"disk_crit", "mode", "bogus", "fan_control", "case_input", "fan.fan9.curve"} {
		if !dropped[k] {
			t.Errorf("%s must be listed as not imported: %v", k, res.NotImported)
		}
	}
	if len(res.Warnings) < 6 {
		t.Errorf("clamps need a warning each: %v", res.Warnings)
	}

	if _, err := profile.Decode(doc(2, profile.Kind, `{}`)); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("a newer schema must be refused: %v", err)
	}
	if _, err := profile.Decode(doc(0, profile.Kind, `{}`)); err == nil {
		t.Error("schema 0 must be refused")
	}
	if _, err := profile.Decode(doc(1, "something-else", `{}`)); err == nil {
		t.Error("another kind must be refused")
	}
	if _, err := profile.Decode([]byte(`[1,2]`)); err == nil {
		t.Error("not an object")
	}
}

func TestUploadLimitsAndText(t *testing.T) {
	rules, env := ProfileRules{}, testEnv()
	big := bytes.Repeat([]byte("a"), profile.MaxBytes+1)
	if _, err := profile.ParseUpload("x", big, env, rules); err == nil || !strings.Contains(err.Error(), "larger than 256 KB") {
		t.Errorf("over the cap: %v", err)
	}
	if _, err := profile.ParseUpload("x", bytes.Repeat([]byte("a"), profile.MaxBytes), env, rules); err == nil || strings.Contains(err.Error(), "larger") {
		t.Errorf("exactly the cap is within the limit and fails on content: %v", err)
	}
	for name, b := range map[string][]byte{
		"binary": {0x7f, 'E', 'L', 'F', 0, 1, 2},
		"nul":    []byte("FAN_MODE=normal\x00\n"),
		"latin1": {'a', '=', 0xe4, 0xf6},
		"escape": []byte("FAN_MODE=normal\x1b[31m\n"),
	} {
		if _, err := profile.ParseUpload(name, b, env, rules); err == nil || !strings.Contains(err.Error(), "not text") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := profile.ParseUpload("x", []byte("hello world\n"), env, rules); err == nil {
		t.Error("text that is no config")
	}
	if _, err := profile.ParseUpload("x", []byte(`{"format":"other"}`), env, rules); err == nil {
		t.Error("foreign JSON")
	}
}

func TestUploadRecognisesEachSource(t *testing.T) {
	rules, env := ProfileRules{}, testEnv()
	root := filepath.Join("profile", "testdata")
	for path, source := range map[string]string{
		"fanctrlplus/minimal/fanctrlplus_Case.cfg":                    profile.SourceFanCtrlPlus,
		"fanctrlplus2/typical-multifan/fanctrlplus2_Front_Intake.cfg": profile.SourceFanCtrlPlus2,
		"fanctrlplus2/backup/fanctrlplus2-backup.json":                profile.SourceFanCtrlPlus2,
		"dynamix-autofan/minimal/dynamix.system.autofan.cfg":          profile.SourceDynamix,
		"itkuny-ugreen-fan/typical-v2.0.env":                          profile.SourceITKuny,
	} {
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		res, err := profile.ParseUpload(filepath.Base(path), b, env, rules)
		if err != nil || res.Source != source {
			t.Errorf("%s: %v %v", path, res, err)
		}
	}
	own, _ := ExportProfile(defaultConfig(), "", time.Now()).Encode()
	if res, err := profile.ParseUpload("p.json", own, env, rules); err != nil || res.Source != profile.SourceOwn {
		t.Errorf("own: %v %v", res, err)
	}
}

func dirHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			fmt.Fprintf(h, "%s\n%d\n%s", p, len(b), b)
		}
		if err == nil {
			info, _ := d.Info()
			fmt.Fprintf(h, "%s %v\n", p, info.Mode())
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// A fake /boot/config/plugins with every rival, a settings file of ours: preview and apply read them and write nothing.
func TestPreviewAndApplyWriteNothing(t *testing.T) {
	if !testenv.Enabled {
		t.Skip("UGREEN_PLUGINS_BOOT is only honoured with -tags testhooks")
	}
	boot := t.TempDir()
	root := filepath.Join("profile", "testdata")
	copyTree := func(src, dst string) {
		_ = os.MkdirAll(dst, 0o755)
		for name, b := range readDirFiles(t, src) {
			if err := os.WriteFile(filepath.Join(dst, name), b, 0o444); err != nil {
				t.Fatal(err)
			}
		}
	}
	copyTree(filepath.Join(root, "fanctrlplus2", "typical-multifan"), filepath.Join(boot, "fanctrlplus2"))
	copyTree(filepath.Join(root, "fanctrlplus", "typical-multifan"), filepath.Join(boot, "fanctrlplus"))
	copyTree(filepath.Join(root, "dynamix-autofan", "typical-multifan"), filepath.Join(boot, "dynamix.system.autofan"))
	t.Setenv("UGREEN_PLUGINS_BOOT", boot)
	old := detectConflicts
	detectConflicts = func() []conflict.Conflict { return nil }
	t.Cleanup(func() { detectConflicts = old })

	oldEnv := profileEnv
	profileEnv = testEnv
	t.Cleanup(func() { profileEnv = oldEnv })

	cfgDir := t.TempDir()
	oldCfg := configPath
	configPath = filepath.Join(cfgDir, "fan.cfg")
	t.Cleanup(func() { configPath = oldCfg })
	if err := os.WriteFile(configPath, []byte("min_pct=\"30\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := dirHash(t, boot) + dirHash(t, cfgDir)

	out, err := profileCommand([]string{"sources"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(out)
	for _, id := range []string{"fanctrlplus", "fanctrlplus2", "dynamix"} {
		if !strings.Contains(string(b), `"id":"`+id+`"`) {
			t.Errorf("source %s not found: %s", id, b)
		}
	}

	pv, err := profileCommand([]string{"preview", "--source", "fanctrlplus2"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	prev := pv.(ProfilePreview)
	if prev.Token == "" || len(prev.Diff) == 0 || prev.Source != "fanctrlplus2" {
		t.Fatalf("preview: %+v", prev)
	}
	var minRow *DiffRow
	for i := range prev.Diff {
		if prev.Diff[i].Key == "min_pct" {
			minRow = &prev.Diff[i]
		}
	}
	if minRow == nil || minRow.From != "30" {
		t.Fatalf("the diff must show the current value: %+v", prev.Diff)
	}

	// Apply needs the token of the same preview.
	if _, err := profileCommand([]string{"apply", "--source", "fanctrlplus2", "--token", "deadbeef"}, strings.NewReader("")); err == nil {
		t.Fatal("a wrong token must be refused")
	}
	if _, err := profileCommand([]string{"apply", "--source", "fanctrlplus2"}, strings.NewReader("")); err == nil {
		t.Fatal("no token must be refused")
	}
	plan, err := profileCommand([]string{"apply", "--source", "fanctrlplus2", "--token", prev.Token}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if p := plan.(ProfileApplyPlan); !p.V2 || p.Values["mode"] != "custom" {
		t.Fatalf("plan: %+v", p)
	}

	// An upload through stdin gives the same kind of preview.
	up, _ := os.ReadFile(filepath.Join(root, "itkuny-ugreen-fan", "typical-v2.0.env"))
	if _, err := profileCommand([]string{"preview", "--stdin", "--name", "x.env"}, bytes.NewReader(up)); err != nil {
		t.Fatal(err)
	}
	if _, err := profileCommand([]string{"preview", "--stdin"}, bytes.NewReader(bytes.Repeat([]byte("a"), profile.MaxBytes+5))); err == nil {
		t.Fatal("an oversize upload must be refused")
	}
	// The IT-Kuny file by path.
	if _, err := profileCommand([]string{"preview", "--source", "itkuny", "--path", "/var/lib/x.env"}, strings.NewReader("")); err == nil {
		t.Fatal("a path outside /boot, /mnt, /root, /etc and /tmp must be refused")
	}
	if _, err := profileCommand([]string{"preview", "--source", "itkuny", "--path", "/etc/../root/x"}, strings.NewReader("")); err == nil {
		t.Fatal("a path with .. must be refused")
	}

	if after := dirHash(t, boot) + dirHash(t, cfgDir); after != before {
		t.Fatal("preview or apply changed a file")
	}
}

func TestExportCommand(t *testing.T) {
	oldCfg := configPath
	configPath = filepath.Join(t.TempDir(), "fan.cfg")
	t.Cleanup(func() { configPath = oldCfg })
	out, err := profileCommand([]string{"export"}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	p := out.(profile.Profile)
	if p.Schema != 1 || p.Kind != profile.Kind || p.Fan["min_pct"] != "25" {
		t.Fatalf("%+v", p)
	}
}

// Everything the importers and the export produce passes the checks api.php applies before it writes (fan_profile.php).
func TestProfileValuesPassPHPValidation(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php is not installed")
	}
	include := filepath.Join("..", "..", "..", "web", "plugin", "include")
	api, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "plugin", "api.php"))
	if err != nil {
		t.Fatal(err)
	}
	modes := regexp.MustCompile(`(?m)^const MODES = \[[^\]]*\];`).Find(api)
	if modes == nil {
		t.Fatal("const MODES not found in api.php")
	}

	values := map[string]map[string]string{}
	goldens, _ := filepath.Glob(filepath.Join("profile", "testdata", "golden", "*.json"))
	for _, g := range goldens {
		b, _ := os.ReadFile(g)
		var res profile.Result
		if err := json.Unmarshal(b, &res); err != nil {
			t.Fatal(err)
		}
		values[filepath.Base(g)] = res.Profile.Fan
	}
	cfg := filepath.Join(t.TempDir(), "fan.cfg")
	_ = os.WriteFile(cfg, []byte("cfg_version=\"2\"\nfan.fan1.curve=\"30:20,50:60\"\nfan.fan1.input=\"cpu\"\nquiet_days=\"sat,mon\"\ncase_input=\"max:disk_max:disk1+disk2|gpu\"\nignore_drives=\"b,a\"\n"), 0o644)
	values["export"] = ExportProfile(LoadConfig(cfg), "", time.Now()).Fan
	if len(values) < 10 {
		t.Fatalf("only %d value sets", len(values))
	}

	script := "<?php\n" + string(modes) + "\nrequire $argv[1].'/fan_v2.php';\nrequire $argv[1].'/fan_profile.php';\n" +
		"$sets = json_decode(file_get_contents('php://stdin'), true); $bad = [];\n" +
		"foreach ($sets as $name => $set) { foreach ($set as $k => $v) { if (!fan_profile_valid((string)$k, (string)$v)) { $bad[] = \"$name: $k=$v\"; } } }\n" +
		"echo json_encode($bad);\n"
	scriptPath := filepath.Join(t.TempDir(), "check.php")
	_ = os.WriteFile(scriptPath, []byte(script), 0o644)
	in, _ := json.Marshal(values)
	cmd := exec.Command(php, scriptPath, include)
	cmd.Stdin = bytes.NewReader(in)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.TrimSpace(string(out)) != "[]" {
		t.Fatalf("rejected by fan_profile_valid: %s", out)
	}

	// And it still rejects what the daemon would not take.
	bad, _ := json.Marshal(map[string]map[string]string{"x": {"min_pct": "99", "cpu_crit": "5", "mode": "x", "cpu_curve": "40:30", "fan_control": "0", "case_input": "script:../x", "ignore_drives": "a/b"}})
	cmd = exec.Command(php, scriptPath, include)
	cmd.Stdin = bytes.NewReader(bad)
	out, _ = cmd.CombinedOutput()
	if n := strings.Count(string(out), "x: "); n != 7 {
		t.Fatalf("expected 7 rejections, got %d: %s", n, out)
	}
}

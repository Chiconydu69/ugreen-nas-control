package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type passRules struct{}

func (passRules) Normalize(_, value string) (string, string, error) { return value, "", nil }

func TestParseKVIsLiteral(t *testing.T) {
	kv := parseKV([]byte("# comment\n\nexport A=1\nB=\"x y\"\nC='$(touch /tmp/pwned)'\nD=\"a\\\"b\\$c\\\\d\"\nE=$(id)\n  F = 5 \nbad line\n/sys/x=y\nA=2\r\nG=\"\"\n"))
	want := map[string]string{"A": "2", "B": "x y", "C": "$(touch /tmp/pwned)", "D": `a"b$c\d`, "E": "$(id)", "F": "5", "G": ""}
	if fmt.Sprint(kv) != fmt.Sprint(want) {
		t.Fatalf("got %v\nwant %v", kv, want)
	}
}

func TestCurveText(t *testing.T) {
	r := newResult("x")
	if got := r.curveText("c", []point{{60, 50}, {40, 20}, {40, 30}, {200, 150}}); got != "40:30,60:50,120:100" {
		t.Errorf("sorted/deduped/clamped: %s", got)
	}
	if got := r.curveText("c", []point{{40, 128}}); got != "40:100,50:100" {
		// 128 is clamped to 100 percent
		t.Errorf("single point: %s", got)
	}
	if got := r.curveText("c", []point{{118, 30}}); got != "108:30,118:30" {
		t.Errorf("single point near the top: %s", got)
	}
	var many []point
	for i := range 20 {
		many = append(many, point{20 + i*4, i * 5})
	}
	got := r.curveText("c", many)
	if n := len(strings.Split(got, ",")); n != 8 || !strings.HasPrefix(got, "20:0,") || !strings.HasSuffix(got, ",96:95") {
		t.Errorf("thinned: %s", got)
	}
	if len(r.Warnings) < 4 {
		t.Errorf("each repair needs a warning: %v", r.Warnings)
	}
}

func TestPwmPct(t *testing.T) {
	for pwm, want := range map[int]int{0: 0, 1: 0, 102: 40, 128: 50, 255: 100, 300: 100, 77: 30} {
		if got := pwmPct(pwm); got != want {
			t.Errorf("pwmPct(%d) = %d, want %d", pwm, got, want)
		}
	}
	// The rivals store round(pct*255/100): whole percents come back exactly.
	for p := 0; p <= 100; p++ {
		if got := pwmPct((p*255 + 50) / 100); got != p {
			t.Errorf("%d%% -> %d", p, got)
		}
	}
}

func TestTokenIgnoresDatesAndNotes(t *testing.T) {
	a := Profile{Fan: map[string]string{"a": "1", "b": "2"}, Created: "x", Notes: "n"}
	b := Profile{Fan: map[string]string{"b": "2", "a": "1"}, Created: "y"}
	if a.Token() != b.Token() {
		t.Error("same settings, different tokens")
	}
	b.Fan["a"] = "3"
	if a.Token() == b.Token() {
		t.Error("different settings, same token")
	}
	// "ab"="c" and "a"="bc" must not collide.
	if (Profile{Fan: map[string]string{"ab": "c"}}).Token() == (Profile{Fan: map[string]string{"a": "bc"}}).Token() {
		t.Error("ambiguous token encoding")
	}
}

func TestUserPathRules(t *testing.T) {
	for _, p := range []string{"/boot/config/x.env", "/mnt/user/appdata/x.env", "/etc/ugreen/ugreen-fan-control.env", "/tmp/x"} {
		if err := allowedUserPath(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	for _, p := range []string{"", "x.env", "/etc/../root/x", "/etc//passwd", "/var/lib/x", "/proc/self/environ", "/boot/./x", "/boot/x\x00"} {
		if err := allowedUserPath(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestReadCappedRefusesLinksAndBigFiles(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.env")
	_ = os.WriteFile(ok, []byte("FAN_MODE=auto\n"), 0o644)
	if _, err := readCapped(ok); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.env")
	_ = os.Symlink(ok, link)
	if _, err := readCapped(link); err == nil {
		t.Error("a link was read")
	}
	big := filepath.Join(dir, "big.env")
	_ = os.WriteFile(big, make([]byte, MaxBytes+1), 0o644)
	if _, err := readCapped(big); err == nil {
		t.Error("a big file was read")
	}
	if _, err := readCapped(dir); err == nil {
		t.Error("a directory was read")
	}
}

func TestDiscoverIsReadOnlyAndIgnoresDrafts(t *testing.T) {
	boot := t.TempDir()
	for path, text := range map[string]string{
		"fanctrlplus/fanctrlplus_A.cfg":                     "custom=\"A\"\n",
		"fanctrlplus/fanctrlplus_temp_0.cfg":                "custom=\"\"\n",
		"fanctrlplus/order.cfg":                             "left0=\"x\"\n",
		"fanctrlplus2/fanctrlplus2_B.cfg":                   "custom=\"B\"\n",
		"dynamix.system.autofan/dynamix.system.autofan.cfg": "service=\"1\"\n",
	} {
		p := filepath.Join(boot, path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(text), 0o444)
	}
	got := Discover(boot)
	var ids []string
	for _, s := range got {
		ids = append(ids, s.ID+":"+strings.Join(s.Files, "+"))
	}
	if fmt.Sprint(ids) != "[fanctrlplus:fanctrlplus_A.cfg fanctrlplus2:fanctrlplus2_B.cfg dynamix:dynamix.system.autofan.cfg]" {
		t.Fatalf("%v", ids)
	}
}

func TestITKunyRejectsOtherFiles(t *testing.T) {
	if _, err := ParseITKuny([]byte("hello=world\n"), passRules{}); err == nil {
		t.Error("a file without IT-Kuny keys")
	}
	if _, err := ParseITKuny([]byte("FAN_MODE=banana\n"), passRules{}); err != nil {
		t.Errorf("an unknown mode falls back to normal with a warning: %v", err)
	}
	res, err := ParseITKuny([]byte("FAN_MODE=auto\nMIN_PWM=100\n"), passRules{})
	if err != nil || res.Profile.Fan["mode"] != "bios" || res.Profile.Fan["min_pct"] != "39" || res.Profile.Fan["cpu_curve"] != "" {
		t.Errorf("auto: %v %v", res, err)
	}
	res, _ = ParseITKuny([]byte("FAN_MODE=max\n"), passRules{})
	if res.Profile.Fan["mode"] != "full" {
		t.Errorf("max: %v", res.Profile.Fan)
	}
}

func TestDynamixOptionsDisagree(t *testing.T) {
	cfg := "service=\"1\"\npwm=\"60\"\nlow=\"35\"\nhigh=\"45\"\noptions=\"-c x -l 90 -t 30 -T 45\"\n"
	res, err := ParseDynamix(map[string][]byte{"dynamix.system.autofan.cfg": []byte(cfg)}, Env{}, passRules{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, w := range res.Warnings {
		if strings.Contains(w, "options line says") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("warnings: %v", res.Warnings)
	}
}

func FuzzParseUpload(f *testing.F) {
	root := filepath.Join("testdata")
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, "golden") {
			b, _ := os.ReadFile(p)
			f.Add(b)
		}
		return nil
	})
	f.Add([]byte(`{"schema":1,"kind":"ugreen-nas-fan-profile","fan":{"a":"b"}}`))
	f.Add([]byte("FAN_MODE=\"\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		res, err := ParseUpload("f", b, Env{}, passRules{})
		if err == nil {
			if res == nil || res.Profile.Fan == nil {
				t.Fatal("nil result without error")
			}
			_ = res.Profile.Token()
			if _, err := res.Profile.Encode(); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func FuzzBackup(f *testing.F) {
	f.Add([]byte(`{"format":"fanctrlplus2-config","version":1,"files":{"fanctrlplus2_a.cfg":"custom=\"a\"\ncpu_enable=\"1\"\ncpu_min_temp=\"1\"\ncpu_max_temp=\"2\"\n"}}`))
	f.Fuzz(func(_ *testing.T, b []byte) { _, _ = ParseFanCtrlPlus2Backup(b, Env{}, passRules{}) })
}

func TestFanCtrlPlus2StorcliSensorImportsAsHBA(t *testing.T) {
	cfg := "custom=\"Case\"\nlabel=\"Case\"\npwm=\"90\"\nmax=\"255\"\ncpu_enable=\"0\"\naux_enable=\"1\"\n" +
		"aux_sensor=\"storcli:c0:roc,storcli:c1:roc,nvidia:gpu0\"\naux_min_temp=\"30\"\naux_max_temp=\"60\"\n"
	res, err := ParseFanCtrlPlus(map[string][]byte{"fanctrlplus2_Case.cfg": []byte(cfg)}, 2, Env{}, passRules{})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Profile.Fan["case_input"]; got != "max:hba|gpu" {
		t.Errorf("case_input = %q, want max:hba|gpu (two controllers are one input)", got)
	}
	for _, s := range res.NotImported {
		if strings.Contains(s.Reason, "storcli") {
			t.Errorf("storcli sensor listed as not imported: %+v", s)
		}
	}
}

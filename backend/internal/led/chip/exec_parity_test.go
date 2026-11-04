package chip_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/ledctl"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

const fakeBusEnv = "CHIP_TEST_FAKE_BUS_LOG"

// TestMain lets the test binary stand in for the ugreen-led-i2c tool (ExecChip runs it) on a fake bus.
func TestMain(m *testing.M) {
	if path := os.Getenv(fakeBusEnv); path != "" {
		f, err := os.Create(path)
		if err != nil {
			os.Exit(99)
		}
		chip.UseFakeBus(f)
		code := chip.Run(os.Args[1:], os.Stdout, os.Stderr)
		_ = f.Close()
		os.Exit(code)
	}
	os.Exit(m.Run())
}

type result struct {
	stdout, stderr, frames string
	rc                     int
}

func viaExec(t *testing.T, layout string, args []string) result {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "frames.log")
	t.Setenv(fakeBusEnv, logPath)
	t.Setenv("UGREEN_LAYOUT", layout)

	var out, errw bytes.Buffer
	rc := ledctl.ExecChip{Path: os.Args[0]}.Exec(args, &out, &errw)
	frames, _ := os.ReadFile(logPath)

	return result{out.String(), errw.String(), string(frames), rc}
}

func viaInProc(t *testing.T, product string, args []string) result {
	t.Helper()
	t.Setenv("UGREEN_LAYOUT", "")
	var frames bytes.Buffer
	defer chip.UseFakeBus(&frames)()
	defer chip.SetLayout("")

	var out, errw bytes.Buffer
	rc := ledctl.InProcChip{Product: product}.Exec(args, &out, &errw)

	return result{out.String(), errw.String(), frames.String(), rc}
}

// The tool and the in-process chip must print the same bytes, return the same code and write the same frames.
func TestExecAndInProcChipAgreeOnTheBus(t *testing.T) {
	cases := []struct {
		name, product, layout string
		args                  []string
		wantFrames            int
		wantOut               string
	}{
		{name: "get-one", args: []string{"get", "disk2"}, wantOut: `"name": "disk2"`},
		{name: "get-all", args: []string{"get", "all"}, wantOut: `"disk8"`},
		{name: "get-plain", args: []string{"--plain", "get", "power,netdev"}, wantOut: "netdev "},
		{name: "probe", args: []string{"probe"}, wantOut: `"leds"`},
		{name: "color", args: []string{"set", "disk1", "color", "1", "2", "3"}, wantFrames: 1},
		{name: "brightness-all", args: []string{"set", "all", "brightness", "128"}, wantFrames: 10},
		{name: "blink", args: []string{"set", "power,disk3", "blink", "500", "250"}, wantFrames: 2},
		{name: "breath", args: []string{"set", "netdev", "breath", "1200", "900"}, wantFrames: 1},
		{name: "idx-pro", product: "iDX6011 Pro", layout: "idx-pro", args: []string{"set", "disk1", "on"}, wantFrames: 1},
		{name: "idx", product: "iDX6011", layout: "idx", args: []string{"set", "disk1", "off"}, wantFrames: 1},
		{name: "unknown-led", args: []string{"set", "disk9", "on"}},
		{name: "bad-color", args: []string{"set", "power", "color", "300", "0", "0"}},
		{name: "usage", args: []string{"set"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.layout != "" && !testenv.Enabled {
				t.Skip("the release build ignores UGREEN_LAYOUT in the tool; run with -tags testhooks")
			}
			e := viaExec(t, c.layout, c.args)
			p := viaInProc(t, c.product, c.args)
			if e != p {
				t.Fatalf("differ\nexec:   %+v\ninproc: %+v", e, p)
			}
			if got := strings.Count(e.frames, "\n"); got != c.wantFrames {
				t.Errorf("frames written = %d, want %d:\n%s", got, c.wantFrames, e.frames)
			}
			if c.wantOut != "" && !strings.Contains(e.stdout, c.wantOut) {
				t.Errorf("stdout %q lacks %q", e.stdout, c.wantOut)
			}
			if c.wantOut == "" && c.wantFrames > 0 && e.rc != 0 {
				t.Errorf("rc = %d, stderr %q", e.rc, e.stderr)
			}
		})
	}
}

// The idx-pro layout moves disk1 from register 2 to 3: the frames must say so on both paths.
func TestInProcChipLayoutFollowsTheProduct(t *testing.T) {
	plain := viaInProc(t, "DXP6800 Pro", []string{"set", "disk1", "on"})
	pro := viaInProc(t, "iDX6011 Pro", []string{"set", "disk1", "on"})
	if !strings.HasPrefix(plain.frames, "write reg=02 ") || !strings.HasPrefix(pro.frames, "write reg=03 ") {
		t.Fatalf("plain %q, pro %q", plain.frames, pro.frames)
	}
}

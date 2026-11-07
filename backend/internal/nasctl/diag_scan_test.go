package nasctl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const scanHead = "{\n  \"/dev/i2c-0\": {\"answer\":[\"0x3a\"],\"busy\":null,\"name\":\"SMBus I801 adapter at 0000:00:1f.4\"},\n" +
	"  \"/dev/i2c-1\": {\"name\":\"i915 gmbus dpa\",\"skipped\":\"display bus (no UGREEN device sits there; the vendor driver also skips it)\"}"

func TestScanTextShowsSkippedBusesAsOwnLines(t *testing.T) {
	got := scanText(scanHead + "\n}\n")
	want := "/dev/i2c-0: 0x3a\n/dev/i2c-1 (i915 gmbus dpa): skipped, display bus (no UGREEN device sits there; the vendor driver also skips it)"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestScanTextKeepsFinishedBusesOfACutOffScan(t *testing.T) {
	for _, cut := range []string{scanHead, scanHead + ",\n", scanHead + ",\n  \"/dev/i2c-2\": {\"ans"} {
		got := scanText(cut)
		if !strings.Contains(got, "/dev/i2c-0: 0x3a") || !strings.Contains(got, "/dev/i2c-1 (i915 gmbus dpa): skipped") {
			t.Errorf("lost the finished buses of %q: %q", cut, got)
		}
		if strings.Contains(got, "i2c-2") {
			t.Errorf("shows the unfinished bus: %q", got)
		}
	}
}

func TestUnscannedBusesListWhatTheScanDidNotReach(t *testing.T) {
	got := unscannedBuses([]string{"i2c-0=SMBus I801", "i2c-1=i915 gmbus dpa", "i2c-2=Other"}, scanHead)
	if len(got) != 1 || got[0] != "/dev/i2c-2 (Other)" {
		t.Errorf("got %v", got)
	}
}

// A scan that hits the timeout still reports what it finished.
func TestScanTimeoutKeepsPartialResults(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "ugreen-led-i2c")
	script := "#!/bin/sh\necho \"$@\" >" + dir + "/args\ncat <<'EOF'\n" + scanHead + ",\nEOF\nexec sleep 20\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := scanTimeout
	scanTimeout = 2 * time.Second
	t.Cleanup(func() { scanTimeout = old })

	c := &Ctl{Env: Env{LEDI2C: tool}}
	out, errMsg := c.i2cScan()
	if !strings.HasPrefix(errMsg, "timed out") {
		t.Fatalf("err %q", errMsg)
	}
	if text := scanText(out); !strings.Contains(text, "/dev/i2c-0: 0x3a") {
		t.Errorf("partial result lost: %q", text)
	}
	if args, _ := os.ReadFile(dir + "/args"); strings.TrimSpace(string(args)) != "scan all" {
		t.Errorf("args %q", args)
	}

	c.FullScan = true
	c.i2cScan()
	if args, _ := os.ReadFile(dir + "/args"); strings.TrimSpace(string(args)) != "scan all --include-display" {
		t.Errorf("--full-scan args %q", args)
	}
}

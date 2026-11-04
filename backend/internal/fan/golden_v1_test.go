package fan

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/v1/*.golden from the current code")

// sweep prints every decision of the pipeline (Decide, then Smooth from a high and a low current speed) over a grid of
// temperatures, so two versions of the config code can be compared line by line.
func sweep(c Config) string {
	var b strings.Builder
	ptr := func(n int) *int { return &n }
	fmt.Fprintf(&b, "mode=%s min=%d crit=%d/%d nvme_off=%d follows=%v fan=%v stall=%v ignore=%v\n",
		c.Mode, c.MinPct, c.CPUCrit, c.DiskCrit, c.NVMeOffset, c.CaseFollowsCPU, c.FanControl, c.StallNotify, c.IgnoreDrives)
	fmt.Fprintf(&b, "cpu=%s case=%s\n", c.CPUCurve, c.CaseCurve)
	disks := []*int{nil}
	for t := 20; t <= 62; t += 3 {
		disks = append(disks, ptr(t))
	}
	nvmes := []*int{nil, ptr(45), ptr(75), ptr(90)}
	for cpu := 25; cpu <= 100; cpu += 3 {
		for _, dt := range disks {
			for _, nt := range nvmes {
				s := Sensors{CPU: ptr(cpu)}
				if dt != nil {
					s.Disks = []Disk{{Name: "disk1", Temp: dt}, {Name: "cache", Temp: ptr(*dt + 4)}}
				}
				if nt != nil {
					s.NVMe = []Temp{{Name: "nvme1", Temp: nt}}
				}
				d := Decide(c, s)
				ct := "-"
				if d.CaseTemp != nil {
					ct = fmt.Sprint(*d.CaseTemp)
				}
				fmt.Fprintf(&b, "cpu=%d disk=%s nvme=%s -> cpu%%=%d case%%=%d fs=%q ct=%s from=%s drv=%s smooth=%d/%d/%d/%d\n",
					cpu, ptrStr(dt), ptrStr(nt), d.CPU, d.Case, d.Failsafe, ct, d.CaseFrom, d.CaseDriver,
					Smooth(100, d.CPU), Smooth(10, d.CPU), Smooth(100, d.Case), Smooth(-1, d.Case))
			}
		}
	}
	d := Decide(c, Sensors{})
	fmt.Fprintf(&b, "nocpu -> %+v\n", d)
	return b.String()
}

func ptrStr(p *int) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprint(*p)
}

// Every v1 fan.cfg (no cfg_version) must decide exactly as before the v2 data model: the .golden files (a digest of the
// whole sweep) were written by the code of that time.
func TestV1ConfigsDecideLikeBefore(t *testing.T) {
	files, _ := filepath.Glob("testdata/v1/*.cfg")
	if len(files) < 8 {
		t.Fatalf("fixtures missing: %v", files)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			full := sweep(LoadConfig(f))
			head, _, _ := strings.Cut(full, "cpu=25 ")
			got := fmt.Sprintf("%ssha256 %x lines %d\n", head, sha256.Sum256([]byte(full)), strings.Count(full, "\n"))
			golden := strings.TrimSuffix(f, ".cfg") + ".golden"
			if *updateGolden {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("decisions changed for %s (run with -update-golden only if that is intended)\nwant:\n%s\ngot:\n%s", f, want, got)
			}
		})
	}
}

package chip

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// displayPrefixes are adapter names that only graphics drivers register. No UGREEN device sits on them, and
// UGREEN's own driver skips every adapter whose name does not start with "SMBus" or "Synopsys"
// (kernel-6.12 drivers/ugreen/leds-mcu.c:765-772@c75c5ab). Only names seen in the kernel source are listed;
// an unknown name is scanned.
//
//	"i915 gmbus "   drivers/gpu/drm/i915/display/intel_gmbus.c   ("i915 gmbus dpb"; the xe driver shares it)
//	"AUX "          drivers/gpu/drm/i915/display/intel_dp_aux.c  ("AUX B/DDI B/PHY B")
//	"AMDGPU DM "    amdgpu_dm_mst_types.c ("AMDGPU DM aux hw bus 0"); "AMDGPU DM i2c hw bus 0" on a DXP4800 GT
//	"AMDGPU i2c "   drivers/gpu/drm/amd/amdgpu/amdgpu_i2c.c      ("AMDGPU i2c hw bus …", "… bit bus …")
//	"Radeon i2c "   drivers/gpu/drm/radeon/radeon_i2c.c          ("Radeon i2c hw bus …", "… bit bus …")
//	"nvkm-"         drivers/gpu/drm/nouveau/nvkm/subdev/i2c/bus.c ("nvkm-0000:01:00.0-bus-0000")
var displayPrefixes = []string{"i915 gmbus ", "AUX ", "AMDGPU DM ", "AMDGPU i2c ", "Radeon i2c ", "nvkm-"}

// isDisplayBus reports whether the adapter name belongs to a graphics driver.
func isDisplayBus(name string) bool {
	for _, p := range displayPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// sysI2CDirs hold the adapter names; Unraid has only /sys/bus/i2c/devices.
var sysI2CDirs = []string{"/sys/bus/i2c/devices", "/sys/class/i2c-adapter"}

// adapterName is the kernel's name of /dev/i2c-N, or "" when it cannot be read.
func adapterName(path string) string {
	base := filepath.Base(path)
	for _, dir := range sysI2CDirs {
		if b, err := os.ReadFile(filepath.Join(dir, base, "name")); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// scanRank orders the scan: SMBus (I801) first, then DesignWare, then the rest.
func scanRank(name string) int {
	switch {
	case strings.HasPrefix(name, "SMBus"):
		return 0
	case strings.HasPrefix(name, "Synopsys"):
		return 1
	}
	return 2
}

var busNumber = regexp.MustCompile(`\d+$`)

func busIndex(path string) int {
	n, _ := strconv.Atoi(busNumber.FindString(path))
	return n
}

type scanTarget struct {
	path, name string
	skip       bool
}

// planScan orders the buses and marks the display buses to skip. Order: SMBus, DesignWare, the skipped display
// buses (free, so they are listed even when a later scan times out), then everything else; by bus number inside a group.
func planScan(paths []string, includeDisplay bool, nameOf func(string) string) []scanTarget {
	targets := make([]scanTarget, 0, len(paths))
	for _, p := range paths {
		name := nameOf(p)
		targets = append(targets, scanTarget{path: p, name: name, skip: !includeDisplay && isDisplayBus(name)})
	}
	group := func(t scanTarget) int {
		if t.skip {
			return 2
		}
		if r := scanRank(t.name); r < 2 {
			return r
		}
		return 3
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if gi, gj := group(targets[i]), group(targets[j]); gi != gj {
			return gi < gj
		}
		return busIndex(targets[i].path) < busIndex(targets[j].path)
	})
	return targets
}

const skipReason = "display bus (no UGREEN device sits there; the vendor driver also skips it)"

// streamScan scans the targets and writes the result as one JSON object, one bus per line, flushing after each bus.
// A scan that is killed half way leaves every finished bus on stdout; the object just lacks its closing brace.
func streamScan(w *bufio.Writer, targets []scanTarget, scan func(string) (found, busy []string, err error)) {
	emit := func(i int, path string, v map[string]any) {
		key, _ := json.Marshal(path)
		val, _ := json.Marshal(v)
		sep := ",\n"
		if i == 0 {
			sep = "{\n"
		}
		_, _ = fmt.Fprintf(w, "%s  %s: %s", sep, key, val)
		_ = w.Flush()
	}
	for i, t := range targets {
		v := map[string]any{}
		if t.name != "" {
			v["name"] = t.name
		}
		switch {
		case t.skip:
			v["skipped"] = skipReason
		default:
			found, busy, err := scan(t.path)
			if err != nil {
				v["error"] = err.Error()
			} else {
				v["answer"], v["busy"] = found, busy
			}
		}
		emit(i, t.path, v)
	}
	if len(targets) == 0 {
		_, _ = w.WriteString("{")
	}
	_, _ = w.WriteString("\n}\n")
	_ = w.Flush()
}

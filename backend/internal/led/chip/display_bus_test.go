package chip

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Names as the kernel reports them: #165 (DXP8800 Plus, Alder Lake, i915), the maintainer DXP4800 GT (AMD).
var adapterNames = map[string]bool{
	"SMBus I801 adapter at 0000:00:1f.4": false,
	"SMBus PIIX4 adapter port 0 at 0b00": false,
	"SMBus PIIX4 adapter port 2 at 0b00": false,
	"SMBus PIIX4 adapter port 1 at 0b20": false,
	"Synopsys DesignWare I2C adapter":    false,
	"i915 gmbus dpa":                     true,
	"i915 gmbus dpb":                     true,
	"i915 gmbus dpc":                     true,
	"i915 gmbus tc1":                     true,
	"i915 gmbus tc6":                     true,
	"i915 gmbus misc":                    true,
	"AUX B/DDI B/PHY B":                  true,
	"AUX USBC1/DDI TC1/PHY TC1":          true,
	"AUX USBC4/DDI TC4/PHY TC4":          true,
	"AMDGPU DM i2c hw bus 0":             true,
	"AMDGPU DM aux hw bus 0":             true,
	"AMDGPU i2c hw bus DVI":              true,
	"AMDGPU i2c bit bus DVI":             true,
	"Radeon i2c hw bus DVI_DDC":          true,
	"nvkm-0000:01:00.0-bus-0000":         true,
	"":                                   false, // unreadable name: scanned
	"Something new from a vendor":        false, // unknown: scanned
	"DPDDC":                              false, // not seen in the kernel source: scanned
	"xe something":                       false,
	"SMBus nForce2 adapter at 4c00":      false,
	"i915 gmbus":                         false, // no trailing name: not the kernel's format
}

func TestDisplayBusClassification(t *testing.T) {
	for name, want := range adapterNames {
		if got := isDisplayBus(name); got != want {
			t.Errorf("isDisplayBus(%q) = %t, want %t", name, got, want)
		}
	}
}

var bus165 = map[string]string{
	"/dev/i2c-0": "SMBus I801 adapter at 0000:00:1f.4", "/dev/i2c-1": "i915 gmbus dpa", "/dev/i2c-2": "i915 gmbus dpb",
	"/dev/i2c-3": "i915 gmbus dpc", "/dev/i2c-10": "AUX B/DDI B/PHY B", "/dev/i2c-11": "AUX USBC1/DDI TC1/PHY TC1",
	"/dev/i2c-12": "mystery",
}

func names(p string) string { return bus165[p] }

func paths() []string {
	// glob order: i2c-10 before i2c-2
	return []string{"/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-10", "/dev/i2c-11", "/dev/i2c-12", "/dev/i2c-2", "/dev/i2c-3"}
}

func order(ts []scanTarget) string {
	var s []string
	for _, t := range ts {
		x := t.path
		if t.skip {
			x += "!"
		}
		s = append(s, x)
	}
	return strings.Join(s, " ")
}

func TestPlanScanOrdersSMBusDesignWareSkippedThenTheRest(t *testing.T) {
	got := order(planScan(paths(), false, names))
	want := "/dev/i2c-0 /dev/i2c-1! /dev/i2c-2! /dev/i2c-3! /dev/i2c-10! /dev/i2c-11! /dev/i2c-12"
	if got != want {
		t.Errorf("order\n got %s\nwant %s", got, want)
	}

	nm := map[string]string{"/dev/i2c-5": "Other", "/dev/i2c-7": "Synopsys DesignWare I2C adapter", "/dev/i2c-9": "SMBus I801 adapter"}
	got = order(planScan([]string{"/dev/i2c-5", "/dev/i2c-7", "/dev/i2c-9"}, false, func(p string) string { return nm[p] }))
	if got != "/dev/i2c-9 /dev/i2c-7 /dev/i2c-5" {
		t.Errorf("rank order: %s", got)
	}
}

func TestIncludeDisplayRestoresTheFullScan(t *testing.T) {
	ts := planScan(paths(), true, names)
	if len(ts) != 7 {
		t.Fatalf("targets: %d", len(ts))
	}
	for _, x := range ts {
		if x.skip {
			t.Errorf("%s skipped with --include-display", x.path)
		}
	}
}

func streamRun(t *testing.T, ts []scanTarget, scan func(string) ([]string, []string, error)) (string, map[string]map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	streamScan(bufio.NewWriter(&buf), ts, scan)
	var m map[string]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, buf.String())
	}
	return buf.String(), m
}

func TestStreamScanScansEverythingButDisplayBuses(t *testing.T) {
	var scanned []string
	_, m := streamRun(t, planScan(paths(), false, names), func(p string) ([]string, []string, error) {
		scanned = append(scanned, p)
		if p == "/dev/i2c-12" {
			return nil, nil, errors.New("boom")
		}
		return []string{"0x3a"}, nil, nil
	})
	if strings.Join(scanned, " ") != "/dev/i2c-0 /dev/i2c-12" {
		t.Errorf("scanned %v", scanned)
	}
	if len(m) != 7 {
		t.Errorf("every bus has an entry, got %d", len(m))
	}
	s := m["/dev/i2c-3"]
	if s["skipped"] == nil || s["answer"] != nil || s["name"] != "i915 gmbus dpc" {
		t.Errorf("skipped entry: %v", s)
	}
	if m["/dev/i2c-12"]["error"] != "boom" {
		t.Errorf("unknown adapter was not scanned: %v", m["/dev/i2c-12"])
	}
}

func TestStreamScanWithIncludeDisplayScansAll(t *testing.T) {
	n := 0
	streamRun(t, planScan(paths(), true, names), func(string) ([]string, []string, error) { n++; return nil, nil, nil })
	if n != 7 {
		t.Errorf("scanned %d of 7", n)
	}
}

// A killed scan has written every finished bus: each bus line is flushed on its own.
func TestStreamScanFlushesPerBus(t *testing.T) {
	var buf bytes.Buffer
	ts := planScan(paths(), false, names)
	streamScan(bufio.NewWriter(&buf), ts, func(p string) ([]string, []string, error) {
		if p == "/dev/i2c-12" && !strings.Contains(buf.String(), `"/dev/i2c-0"`) {
			t.Error("i2c-0 was not flushed before the next bus")
		}
		return nil, nil, nil
	})
}

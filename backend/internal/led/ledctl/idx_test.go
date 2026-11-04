package ledctl

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// idxSATA lays out the SATA ports of an iDX601x: ASMedia (0x1b21) and Intel (0x8086) controllers.
func idxSATA(t *testing.T, r *rig, asm, intel []int) {
	t.Helper()
	for pci, spec := range map[string]struct {
		vendor string
		ports  []int
	}{"0000:01:00.0": {"0x1b21", asm}, "0000:00:17.0": {"0x8086", intel}} {
		r.write("sys/pci/"+pci+"/vendor", spec.vendor+"\n")
		for _, n := range spec.ports {
			target := fmt.Sprintf("devices/pci0000:00/%s/ata%d", pci, n)
			if err := os.MkdirAll(filepath.Join(r.dir, "sys", target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(r.dir, "sys", target), filepath.Join(r.dir, "sys/ata", fmt.Sprintf("ata%d", n))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Bay order after noodlemctwoodle (Pro) and omgapuppy (plain), src/monitor.sh:70-75 each.
func TestIdxBayOrder(t *testing.T) {
	cases := []struct {
		product string
		want    []string
	}{
		{"iDX6011 Pro", []string{"ata1", "ata2", "ata5", "ata6", "ata3", "ata4"}},
		{"iDX6011", []string{"ata1", "ata2", "ata3", "ata4", "ata5", "ata6"}},
		{"iDX6012", []string{"ata1", "ata2", "ata3", "ata4", "ata5", "ata6"}},
	}
	for _, c := range cases {
		t.Run(c.product, func(t *testing.T) {
			r := newRig(t, c.product)
			idxSATA(t, r, []int{1, 2, 3, 4}, []int{5, 6})
			if got := r.c.bayPortsLoad(); !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// Pro: ASMedia on ata3-6, Intel on ata1-2: bays are asm0, asm1, intel0, intel1, asm2, asm3.
func TestIdxProBayOrderMixedPorts(t *testing.T) {
	r := newRig(t, "iDX6011 Pro")
	idxSATA(t, r, []int{3, 4, 5, 6}, []int{1, 2})
	want := []string{"ata3", "ata4", "ata1", "ata2", "ata5", "ata6"}
	if got := r.c.bayPortsLoad(); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	p := newRig(t, "iDX6011")
	idxSATA(t, p, []int{3, 4, 5, 6}, []int{1, 2})
	want = []string{"ata3", "ata4", "ata5", "ata6", "ata1", "ata2"}
	if got := p.c.bayPortsLoad(); !slices.Equal(got, want) {
		t.Errorf("plain: got %v, want %v", got, want)
	}
}

// A Pro with another controller split falls back to ata1..6 instead of guessing.
func TestIdxProBayOrderFallback(t *testing.T) {
	r := newRig(t, "iDX6011 Pro")
	idxSATA(t, r, []int{1, 2, 3}, []int{4, 5, 6})
	want := []string{"ata1", "ata2", "ata3", "ata4", "ata5", "ata6"}
	if got := r.c.bayPortsLoad(); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPowerBootOnIDX(t *testing.T) {
	for _, product := range []string{"iDX6011", "iDX6012", "iDX6011 Pro"} {
		r := newRig(t, product)
		r.powerBootTwice(t)
		want := []string{
			"set power reset", "set power color 255 255 255", "set power brightness 255", "set power on",
		}
		calls := r.chip.callList()
		if len(calls) != 2*len(want) || !slices.Equal(calls[:len(want)], want) || !slices.Equal(calls[len(want):], want) {
			t.Errorf("%s: calls %v", product, calls)
		}
	}
}

func (r *rig) powerBootTwice(t *testing.T) {
	t.Helper()
	r.c.powerBoot()
	r.c.powerBoot() // idempotent: same writes, same result
}

func TestPowerBootUsesSavedPower(t *testing.T) {
	r := newRig(t, "iDX6011")
	r.config("power_color=\"1 2 3\"\npower_brightness=\"40\"\n")
	r.c.cfgLoad()
	r.c.powerBoot()
	want := []string{"set power reset", "set power color 1 2 3", "set power brightness 40", "set power on"}
	if got := r.chip.callList(); !slices.Equal(got, want) {
		t.Errorf("got %v", got)
	}

	z := newRig(t, "iDX6011")
	z.config("power_brightness=\"0\"\n")
	z.c.cfgLoad()
	z.c.powerBoot()
	if got := z.chip.callList(); !slices.Equal(got, []string{"set power reset", "set power off"}) {
		t.Errorf("off: %v", got)
	}
}

func TestPowerBootSkipsOtherModels(t *testing.T) {
	for _, product := range []string{"DXP4800 Plus", "DXP4800 GT", "DXP6800 Pro", "UGREEN NAS"} {
		r := newRig(t, product)
		r.c.powerBoot()
		if got := r.chip.callList(); len(got) != 0 {
			t.Errorf("%s: %v", product, got)
		}
	}
}

// netdev2 (second LAN LED) exists on exactly the iDX6011 Pro.
func TestNetdev2OnlyOnIdxPro(t *testing.T) {
	for product, want := range map[string]bool{"iDX6011 Pro": true, "iDX6011": false, "iDX6012": false, "DXP6800 Pro": false, "DXP2800 GT": false} {
		r := newRig(t, product)
		if got := r.c.validLED("netdev2"); got != want {
			t.Errorf("%s: validLED(netdev2) = %v, want %v", product, got, want)
		}
		if got := slices.Contains(r.c.leds, "netdev2"); got != want {
			t.Errorf("%s: leds %v", product, r.c.leds)
		}
		if rc := r.ctl("on", "netdev2"); (rc == 0) != want {
			t.Errorf("%s: on netdev2 rc %d", product, rc)
		}
	}
}

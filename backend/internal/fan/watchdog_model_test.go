package fan

import (
	"os"
	"slices"
	"testing"
	"time"
)

// The EC petter writes registers 0xa2/0xa3. That is safe only where 0x98 is the watchdog enable byte: the DXP4800 GT
// and the iDX6011 Pro. On the iDX6012 (Pro or not) and the plain iDX6011 0x98/0x99 is the second system fan's
// tachometer. The IT8613E models (LDN 7 of the Super-I/O) get the other petter, nothing else gets one.
func TestWatchdogOnlyOnKnownModels(t *testing.T) {
	for model, want := range map[string]bool{
		"DXP4800 GT":   true,
		"iDX6011 Pro":  true,
		"iDX6011":      false,
		"iDX6012":      false,
		"iDX6012 Pro":  false,
		"DXP2800":      true,
		"DXP4800":      true,
		"DXP4800 Plus": true,
		"DXP4800 Pro":  true,
		"DXP6800 Pro":  true,
		"DXP8800 Plus": true,
		"DXP2800 GT":   false,
		"dxp2800":      false,
		"dxp6800 pro":  false,
		"DX4600 Pro":   false,
		"":             false,
		"IDX6011 Pro":  false,
	} {
		if got := WatchdogModel(model); got != want {
			t.Errorf("WatchdogModel(%q) = %v, want %v", model, got, want)
		}
		caps, ok := CapsFor(model)
		if ok && caps.Watchdog != want {
			t.Errorf("CapsFor(%q).Watchdog = %v, want %v", model, caps.Watchdog, want)
		}
		if !ok && want {
			t.Errorf("CapsFor(%q): no fan control", model)
		}
	}
}

// An armed fake EC that is not on the watchdog list gets no register write from the petter, whatever the chip.
func TestPetterWritesNothingOnOtherModels(t *testing.T) {
	for _, model := range []string{"iDX6012 Pro", "iDX6011", "iDX6012", "DXP6800 Pro", "DXP4800 Plus"} {
		t.Run(model, func(t *testing.T) {
			isolate(t, model)
			f := newFakeIdxEC()
			f.regs[ecRegWdtEnable] = 1
			ec, err := detectEC(f, ecSpecIdxPro)
			if err != nil {
				t.Fatal(err)
			}
			f.writes = nil
			p := &Petter{}
			now := time.Unix(1_000_000, 0)
			for i := range 5 {
				if _, ok, err := p.Tick(ec, now.Add(time.Duration(i)*time.Minute)); ok || err != nil {
					t.Fatalf("tick %d: ok %v err %v", i, ok, err)
				}
			}
			if slices.ContainsFunc(f.writes, func(r byte) bool { return r == ecRegWdtHi || r == ecRegWdtLo || r == ecRegWdtEnable }) {
				t.Fatalf("watchdog register written: %x", f.writes)
			}
			if _, err := os.Stat(watchdogStatePath); err == nil {
				t.Fatal("a watchdog state file on a model without the watchdog")
			}
		})
	}
}

func TestWatchdogState(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	for _, tc := range []struct {
		name string
		st   WatchdogStatus
		want string
	}{
		{"not armed", WatchdogStatus{Interval: 60, LastPet: now.Unix()}, WdtOff},
		{"fed", WatchdogStatus{Armed: true, Interval: 60, LastPet: now.Unix() - 60}, WdtArmed},
		{"at twice the interval", WatchdogStatus{Armed: true, Interval: 60, LastPet: now.Unix() - 120}, WdtArmed},
		{"past twice the interval", WatchdogStatus{Armed: true, Interval: 60, LastPet: now.Unix() - 121}, WdtActionNeeded},
		{"never fed", WatchdogStatus{Armed: true, Interval: 60}, WdtActionNeeded},
	} {
		if got := watchdogState(tc.st, now); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

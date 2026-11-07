//go:build linux

package chip

import (
	"os"
	"strings"
	"sync/atomic"
)

// The LPSS path is on by default (lpss_leds="0" in leds.cfg turns it off; "1" means the same as no key). findBus picks
// it only when openLPSS can plausibly succeed; the first refusal of a default run is remembered for the rest of the
// boot, so a box whose gates fail goes back to the old behaviour (SMBus path, or "no adapter found") without probing
// the PCI function, switching power/control or reading /dev/mem again on every command.
const (
	lpssStateFile = "/var/run/ugreen-nas-control.lpss-state" // "ok" or "refused: <reason>", cleared by a reboot
	lpssStateOK   = "ok"
	lpssStateNo   = "refused: "
)

// Replaced in tests.
var (
	lpssState   = lpssStateFile
	i2cSysBus   = "/sys/bus/i2c/devices"
	i2cDevDir   = "/dev"
	i801Answers = lpssI801Answers

	lpssRefusedMem atomic.Bool // in-process copy, in case the state file cannot be written
)

// lpssOff is the explicit opt-out: lpss_leds="0".
func lpssOff() bool { return cfgValue(lpssOptInKey) == "0" }

// lpssRefusal is the remembered reason of a refusal in this boot, "" when there is none.
func lpssRefusal() string {
	data, _ := os.ReadFile(lpssState)
	if s := strings.TrimSpace(string(data)); strings.HasPrefix(s, lpssStateNo) {
		return strings.TrimPrefix(s, lpssStateNo)
	}
	if lpssRefusedMem.Load() {
		return "refused earlier in this process"
	}
	return ""
}

// lpssAuto is true when findBus may choose the LPSS path: not opted out, an HT32* ACPI device with a parent, and no
// refusal remembered.
func lpssAuto() bool { return !lpssOff() && lpssHasMCU() && lpssRefusal() == "" }

// lpssNoteRefused remembers the refusal of err and reports whether it did. A box whose controller answered before
// in this boot (state "ok") is not blacklisted by one transient failure.
func lpssNoteRefused(err error) bool {
	if data, _ := os.ReadFile(lpssState); strings.TrimSpace(string(data)) == lpssStateOK {
		return false
	}
	lpssRefusedMem.Store(true)
	_ = os.WriteFile(lpssState, []byte(lpssStateNo+strings.ReplaceAll(err.Error(), "\n", " ")+"\n"), 0o644) //nolint:gosec // plain text for diag
	return true
}

// lpssNoteOK remembers that the controller answered with the chip id; written once per boot.
func lpssNoteOK() {
	if data, _ := os.ReadFile(lpssState); strings.TrimSpace(string(data)) == lpssStateOK {
		return
	}
	_ = os.WriteFile(lpssState, []byte(lpssStateOK+"\n"), 0o644) //nolint:gosec // plain text for diag
}

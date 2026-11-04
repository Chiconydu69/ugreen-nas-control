package fan

import (
	"errors"
	"fmt"
	"slices"
)

// LCD backlight of the iDX6011 Pro, the only part of the display this tool touches (the LCD itself stays parked).
// EC register 0xa5 (BKLT_PWM): range 0..198, 1 = brightest, 198 = dimmest, 0 = the default (UGREEN's comment,
// ug_it55pro_functions.c:640,675@c75c5ab; tools/it55_regs_dump.sh:25; the driver registers the device at 80,
// :676). Facts only, nothing copied. Not confirmed on a device with this plugin; this tool writes 1..198 only.
// On the plain iDX 0xa5 is the beeper (ug_sataio_beeper.c:35), hence the write list of ecSpecIdx stays empty.
const (
	ecRegBacklight  = 0xa5
	backlightFull   = 1
	backlightOff    = 198
	backlightVendor = 0 // the vendor default: not a percentage, accepted when read
)

// BacklightUnknownPct is the percentage reported while the register holds the vendor default (0).
const BacklightUnknownPct = -1

// BacklightPct is the brightness in percent (100 = full, 0 = off); raw is the register as read. Raw 0 is the
// vendor default: no error, pct is BacklightUnknownPct.
func (c *EC) BacklightPct() (pct, raw int, err error) {
	if !c.hasBacklight() {
		return 0, 0, errNoBacklight
	}
	v, err := c.read(ecRegBacklight)
	if err != nil {
		return 0, 0, err
	}
	if v == backlightVendor {
		return BacklightUnknownPct, 0, nil
	}
	if v > backlightOff {
		return 0, int(v), fmt.Errorf("backlight register holds 0x%02x, outside %d..%d", v, backlightFull, backlightOff)
	}
	return backlightToPct(v), int(v), nil
}

// hasBacklight is true for the iDX6011 Pro spec only: on the plain iDX 0xa5 is the beeper
// (ug_sataio_beeper.c:35@c75c5ab), so neither a read nor a write of it is possible there or on the GT.
func (c *EC) hasBacklight() bool { return slices.Contains(c.sp().extraWrite, ecRegBacklight) }

var errNoBacklight = errors.New("this model has no backlight register")

func (c *EC) SetBacklight(pct int) error {
	if !c.hasBacklight() {
		return errNoBacklight
	}
	if pct < 0 || pct > 100 {
		return fmt.Errorf("brightness must be 0-100")
	}
	return c.write(ecRegBacklight, pctToBacklight(pct))
}

func pctToBacklight(pct int) byte {
	return byte(backlightOff - (pct*(backlightOff-backlightFull)+50)/100)
}

func backlightToPct(raw byte) int {
	return ((backlightOff-int(raw))*100 + (backlightOff-backlightFull)/2) / (backlightOff - backlightFull)
}

package ledctl

import (
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
)

// powerBoot lights the power LED of an iDX601x front panel once, when the status daemon starts.
// After a cold boot the controller can keep a mode from before (a blink or breath state, or "off")
// and ignores a plain colour/on, so the power LED stays dark. It needs a mode reset (command 0x04
// with zero parameters) first, then brightness, colour and steady on.
//
// Facts from MIT-licensed community plugins (see docs/third-party.md): noodlemctwoodle/
// unraid-ugreen-idx6011-pro src/monitor.sh:119-127 (commit d202a7c) and omgapuppy/
// unraid-ugreen-idx6011-led-fan src/monitor.sh:164-175 (commit 6196dc0), both "required to light it
// from a cold/off state". Idempotent: the same writes again change nothing a second time.
//
// Not done on the DXP4800 GT / Pro: their plugins cycle brightness 0 -> real value only because the
// kernel driver they use skips an "on" it believes is already set (flybrys src/ugreen-gt-leds:357-361,
// EllipticSet src/ugreen-pro-leds:593-597). The chip tool here writes every command, so nothing to work around.
func (c *Ctl) powerBoot() {
	if !isIDXProduct(c.product) {
		return
	}
	saved := c.savedBrightness("power")
	b := c.effectiveBrightness(saved, c.isNight())

	c.quiet(func() {
		c.tool("set", "power", "reset")
		if b == 0 {
			c.tool("set", "power", "off")
			return
		}
		col := append(strings.Fields(c.colorCfg("power_color", "255 255 255")), "255", "255", "255")
		c.tool("set", "power", "color", col[0], col[1], col[2])
		c.tool("set", "power", "brightness", itoa(b))
		c.tool("set", "power", "on")
	})
}

func isIDXProduct(product string) bool { return chip.IdxLayout(product) != "" }

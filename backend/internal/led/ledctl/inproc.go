package ledctl

import (
	"fmt"
	"io"
	"slices"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
)

// InProcChip drives the LED controller inside this process (internal/led/chip) instead of running the
// ugreen-led-i2c tool. Effects run until SIGTERM as a background process of their own, so they go to Fallback.
// Not the default: the in-process path has not been verified on hardware.
type InProcChip struct {
	Fallback Chip
	// Product is the model name (UGREEN_PRODUCT or DMI) that picks the controller layout; empty reads the DMI lazily.
	Product string

	run func(args []string, stdout, stderr io.Writer) int // chip.Run, replaced in tests
}

// Exec implements Chip. A panic in the chip code ends the command with exit code 1, not the daemon.
func (c InProcChip) Exec(args []string, stdout, stderr io.Writer) (rc int) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if slices.Contains(args, "effect") {
		if c.Fallback == nil {
			_, _ = fmt.Fprintln(stderr, "ERROR: effects need the ugreen-led-i2c tool")
			return 1
		}
		return c.Fallback.Exec(args, stdout, stderr)
	}
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(stderr, "ERROR: LED controller: %v\n", r)
			rc = 1
		}
	}()
	if c.Product != "" {
		chip.UseProduct(c.Product)
	}
	run := c.run
	if run == nil {
		run = chip.Run
	}
	return run(args, stdout, stderr)
}

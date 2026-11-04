package chip

import (
	"errors"
	"fmt"
)

// ExitRejected is the exit code of ugreen-led-i2c when the controller acknowledges its address but rejects every
// read and write (DXP2800 board with BIOS EM_DXP2800_SUB2_V1.0.27, miskcoo/ugreen_leds_controller#133). The LED
// daemon stops writing on it and tries again at growing intervals.
const ExitRejected = 4

// RejectedMessage is the one line every layer shows for that state.
const RejectedMessage = "the LED controller at 0x3a answers a detect but rejects every read and write; " +
	"this board's firmware does not take LED commands, so the LEDs cannot be driven (miskcoo/ugreen_leds_controller#133)"

// ErrRejected marks that state; errors.Is finds it.
var ErrRejected = errors.New(RejectedMessage)

// busFailure is an error of the transfer itself (a NAK, an I/O error), as opposed to a reply that was read but is
// wrong. Only failures of the first kind can mean that the controller rejects everything.
type busFailure struct{ err error }

func (e busFailure) Error() string { return e.err.Error() }
func (e busFailure) Unwrap() error { return e.err }

// asRejected turns err into ErrRejected when every attempt failed on the bus although the address still answers a
// detect (a read of one byte, as i2cdetect does). Any other error is returned unchanged.
func asRejected(err error, detect func() error) error {
	var failure busFailure
	if err == nil || !errors.As(err, &failure) || detect == nil || detect() != nil {
		return err
	}

	return fmt.Errorf("%w (last error: %w)", ErrRejected, failure.err)
}

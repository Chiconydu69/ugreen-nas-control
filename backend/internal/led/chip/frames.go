package chip

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// SMBus transfer types of the two frame shapes (Linux i2c-dev constants).
const (
	smbusBlockData    = 5 // command, count, payload
	smbusI2CBlockData = 8 // I2C block write: the payload is sent as is
)

// retries is the number of attempts per frame. UGREEN's drivers make three, with 30 ms (28a48.c:211-222@c75c5ab)
// or 5 ms (leds-mcu.c:217-247@c75c5ab) between; five with 30 ms is the more patient choice kept from the kernel module
// the first versions were written after.
const retries = 5

// frame is one way to put a LED command on the wire.
type frame struct {
	name  string
	size  uint32
	build func(id, command byte, params [4]byte) []byte
}

var (
	frameLegacy = frame{"legacy", smbusI2CBlockData, func(id, c byte, p [4]byte) []byte { return commandPacket(id, c, p) }}
	frameGT     = frame{"gt", smbusBlockData, func(_, c byte, p [4]byte) []byte { return gtCommandPacket(c, p) }}
	frameIdx    = frame{"idx", smbusBlockData, func(_, c byte, p [4]byte) []byte { return idxCommandPacket(c, p) }}
)

// frameIO is what sending a frame needs from the bus; tests replace it with a fake.
type frameIO interface {
	writeBlock(command byte, size uint32, payload []byte) error
	readByte(command byte) (byte, error)
}

// dxp2800Pref remembers, per process, which frame of the DXP2800 last got an acknowledgement, so a board that
// only answers the new frame pays for the failed legacy attempts once.
var dxp2800Pref struct {
	mu       sync.Mutex
	idxFirst bool
}

// framesFor is the order in which a command is tried. The DXP4800 GT and the iDX layouts have one frame.
// The DXP2800 has two: UGREEN's own driver serves exactly "DXP2800" with the no-ID frame (leds-mcu.c:69-92,
// 558-559,627-633@c75c5ab), but older boards are reported working with the legacy frame (miskcoo
// ugreen_leds_controller#128: the V1.2 board, chip 0xC5B2, ignores it). The legacy frame goes first so every
// board that works today behaves as before; when it is not acknowledged the idx frame is tried, and the one
// that worked last is tried first from then on.
func framesFor(gt bool) []frame {
	switch {
	case gt:
		return []frame{frameGT}
	case idxFrames():
		return []frame{frameIdx}
	case getLayout() == layoutDXP2800:
		dxp2800Pref.mu.Lock()
		defer dxp2800Pref.mu.Unlock()

		if dxp2800Pref.idxFirst {
			return []frame{frameIdx, frameLegacy}
		}
		return []frame{frameLegacy, frameIdx}
	}

	return []frame{frameLegacy}
}

// sendFrames writes the command with each frame in turn until the controller acknowledges one.
func sendFrames(io frameIO, frames []frame, id, command byte, params [4]byte) error {
	var errs []error

	for _, f := range frames {
		err := sendFrame(io, f, id, command, params)
		if err == nil {
			if getLayout() == layoutDXP2800 && len(frames) > 1 {
				dxp2800Pref.mu.Lock()
				dxp2800Pref.idxFirst = f.name == frameIdx.name
				dxp2800Pref.mu.Unlock()
			}
			return nil
		}
		if len(frames) > 1 {
			err = fmt.Errorf("%s frame: %w", f.name, err)
		}
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// sendFrame writes one command and waits until the controller confirms it (register 0x80 reads 1), with the same
// retries as the kernel driver.
func sendFrame(io frameIO, f frame, id, command byte, params [4]byte) error {
	packet := f.build(id, command, params)
	err := errors.New("no attempt made")

	for i := range retries {
		backoff(i)

		if err = io.writeBlock(id, f.size, packet); err != nil {
			continue
		}

		time.Sleep(2 * time.Millisecond)

		var ok byte
		if ok, err = io.readByte(regLastCommandOK); err == nil && ok == 1 {
			return nil
		}
		if err == nil {
			err = errors.New("the controller did not confirm the command")
		}
	}

	return err
}

// backoff waits before attempt i: 1 ms, then 30 ms.
func backoff(i int) {
	if i == 0 {
		time.Sleep(time.Millisecond)
	} else {
		time.Sleep(30 * time.Millisecond)
	}
}

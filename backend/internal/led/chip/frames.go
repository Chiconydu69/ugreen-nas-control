package chip

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
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

// systemVersion is the DMI system version (dmidecode "Version"); tests replace it.
var systemVersion = func() string { return hw.SystemVersion(hw.DMIDir) }

// idxFirstFor says whether the idx frame goes first on this board. miskcoo's tool picks the frame by the DMI system
// version of the DXP2800: "EM_" in it (for example EM_DXP2800_SUB2_V1.0.27) means the legacy DX/DXP frame, any other
// version (for example "DXP2800 V1.2 251215") the SMBus block frame (README of miskcoo/ugreen_leds_controller after
// commit f0fcd81). Only the exact DXP2800 follows it; an unreadable or placeholder version keeps the legacy frame
// first. The other order stays the fallback in both cases.
func idxFirstFor(product, version string) bool {
	if product != "DXP2800" || version == "" || strings.Contains(version, "EM_") {
		return false
	}

	lower := strings.ToLower(version)
	for _, placeholder := range []string{"default", "not specified", "to be filled", "system version", "none", "n/a"} {
		if strings.HasPrefix(lower, placeholder) {
			return false
		}
	}

	return true
}

// seedFrameOrder sets the first choice of the DXP2800 frames from the DMI system version.
func seedFrameOrder(layout, product string) {
	if layout != layoutDXP2800 {
		return
	}

	idxFirst := idxFirstFor(product, systemVersion())
	dxp2800Pref.mu.Lock()
	dxp2800Pref.idxFirst = idxFirst
	dxp2800Pref.mu.Unlock()
}

// framesFor is the order in which a command is tried. The DXP4800 GT and the iDX layouts have one frame.
// The DXP2800 has two: UGREEN's own driver serves exactly "DXP2800" with the no-ID frame (leds-mcu.c:69-92,
// 558-559,627-633@c75c5ab), but older boards are reported working with the legacy frame (miskcoo
// ugreen_leds_controller#128: the V1.2 board, chip 0xC5B2, ignores it). The legacy frame goes first so every
// board that works today behaves as before; when it is not acknowledged the idx frame is tried, and the one
// that worked last is tried first from then on. On the exact DXP2800 the first choice comes from the DMI system
// version (idxFirstFor).
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

	// Every frame failed on the bus itself: the caller may find out that the controller rejects everything.
	for _, err := range errs {
		var failure busFailure
		if !errors.As(err, &failure) {
			return errors.Join(errs...)
		}
	}

	return busFailure{errors.Join(errs...)}
}

// sendFrame writes one command and waits until the controller confirms it (register 0x80 reads 1), with the same
// retries as the kernel driver.
func sendFrame(io frameIO, f frame, id, command byte, params [4]byte) error {
	packet := f.build(id, command, params)
	err := errors.New("no attempt made")
	busOnly := true // no attempt got a reply, only transfer errors

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
			busOnly = false
		}
	}

	if busOnly {
		return busFailure{err}
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

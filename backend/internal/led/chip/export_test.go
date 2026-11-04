package chip

import "io"

// FakeBus replaces the SMBus for the external tests: every write is logged as the frame the controller would get.
type fakeBus struct{ log io.Writer }

// UseFakeBus makes Run talk to a fake controller that logs its writes to log; the returned func restores the bus.
func UseFakeBus(log io.Writer) (restore func()) {
	saved := openLedBus
	openLedBus = func(path string, _ bool) (ledBus, error) {
		if path == "" {
			path = "/dev/i2c-fake"
		}
		return fakeBusAt{fakeBus{log}, path}, nil
	}

	return func() { openLedBus = saved }
}

type fakeBusAt struct {
	fakeBus
	path string
}

func (b fakeBusAt) busPath() string { return b.path }
func (b fakeBusAt) close()          {}

func (b fakeBusAt) change(id, command byte, params [4]byte) error {
	var frame []byte
	if idxFrames() {
		frame = idxCommandPacket(command, params)
	} else {
		frame = commandPacket(id, command, params)
	}
	_, _ = b.log.Write([]byte("write reg=" + hex(id) + " frame=" + hexs(frame) + "\n"))

	return nil
}

// status answers a different, fixed block for every LED, so a wrong ID or a swapped field shows.
func (b fakeBusAt) status(name string) (ledStatus, error) {
	id, err := ledID(name)
	if err != nil {
		return ledStatus{}, err
	}

	block := []byte{id % 4, 10 + id, id, 2 * id, 3 * id, 0x01, 0x2c, 0x01, 0x90, 0, 0}
	sum := 0
	for _, x := range block[:9] {
		sum += int(x)
	}
	block[9], block[10] = byte(sum>>8), byte(sum)

	return parseStatus(name, block)
}

func hex(b byte) string { return string("0123456789abcdef"[b>>4]) + string("0123456789abcdef"[b&15]) }

func hexs(b []byte) string {
	out := ""
	for i, x := range b {
		if i > 0 {
			out += " "
		}
		out += hex(x)
	}

	return out
}

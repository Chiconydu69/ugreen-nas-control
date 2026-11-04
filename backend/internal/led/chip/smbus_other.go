//go:build !linux

package chip

import "errors"

var errNotLinux = errors.New("I2C access is only supported on Linux")

type bus struct{ path string }

func openBus(string, bool) (*bus, error) {
	return nil, errNotLinux
}

func openBusAt(string, uintptr, bool) (*bus, error) {
	return nil, errNotLinux
}

func (b *bus) close() {}

func (b *bus) writeByteData(byte, byte) error {
	return errNotLinux
}

func scanBuses(string, bool) ([]string, error) {
	return nil, errNotLinux
}

func scanBus(string) (found, busy []string, err error) {
	return nil, nil, errNotLinux
}

func (b *bus) change(byte, byte, [4]byte) error {
	return errNotLinux
}

func (b *bus) status(string) (ledStatus, error) {
	return ledStatus{}, errNotLinux
}

func (b *bus) busPath() string { return b.path }

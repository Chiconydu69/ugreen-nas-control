// Package hw holds the hardware access shared by the fan and LED code: I/O ports and the DMI model name.
package hw

type Ports interface {
	In(port uint16) (byte, error)
	Out(port uint16, value byte) error
	Close() error
}

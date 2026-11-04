//go:build linux

package hw

import "os"

// devPort reaches I/O ports through /dev/port: the file offset is the port number.
// No ioperm and no kernel module needed, only root.
type devPort struct {
	f *os.File
}

func OpenPorts() (Ports, error) {
	f, err := os.OpenFile("/dev/port", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &devPort{f: f}, nil
}

func (p *devPort) In(port uint16) (byte, error) {
	b := []byte{0}
	if _, err := p.f.ReadAt(b, int64(port)); err != nil {
		return 0, err
	}
	return b[0], nil
}

func (p *devPort) Out(port uint16, value byte) error {
	_, err := p.f.WriteAt([]byte{value}, int64(port))
	return err
}

func (p *devPort) Close() error {
	return p.f.Close()
}

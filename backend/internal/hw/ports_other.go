//go:build !linux

package hw

import "errors"

func OpenPorts() (Ports, error) {
	return nil, errors.New("I/O ports are only available on Linux")
}

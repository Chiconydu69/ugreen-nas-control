//go:build !linux

package hw

import "errors"

func OpenMem() (Mem, error) { return nil, errors.New("/dev/mem is only available on Linux") }

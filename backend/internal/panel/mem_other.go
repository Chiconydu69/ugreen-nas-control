//go:build !linux

package panel

import "errors"

func OpenMem(bool) (Mem, error) { return nil, errors.New("/dev/mem is only available on Linux") }

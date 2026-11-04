package panel

import (
	"errors"
	"fmt"
	"os"
)

// MemReader reads 32-bit words of physical memory. The buttons only get this, so they cannot write.
type MemReader interface {
	Read32(phys uint64) (uint32, error)
}

// Mem adds the write the pad-register beeper needs.
type Mem interface {
	MemReader
	Write32(phys uint64, v uint32) error
	Close() error
}

var errUnaligned = errors.New("physical address not 4-byte aligned")

func checkAligned(phys uint64) error {
	if phys&3 != 0 {
		return fmt.Errorf("%w: 0x%X", errUnaligned, phys)
	}
	return nil
}

// pageOf splits phys into the page base and the offset inside the page.
func pageOf(phys uint64) (base uint64, off int) {
	ps := uint64(os.Getpagesize())
	return phys &^ (ps - 1), int(phys & (ps - 1))
}

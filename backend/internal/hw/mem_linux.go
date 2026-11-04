//go:build linux

package hw

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

type devMem struct{ f *os.File }

// OpenMem opens /dev/mem for reading and writing. Needs root; a kernel with STRICT_DEVMEM may refuse ranges a driver
// has claimed.
func OpenMem() (Mem, error) {
	f, err := os.OpenFile("/dev/mem", os.O_RDWR|syscall.O_SYNC, 0)
	if err != nil {
		return nil, err
	}
	return &devMem{f: f}, nil
}

// access maps the page holding addr, runs fn on the register's bytes and unmaps. The mapping is page-aligned, the
// register access inside it is a single aligned load or store.
func (m *devMem) access(addr uint64, width int, fn func(reg []byte)) error {
	if err := CheckMemAccess(addr, width); err != nil {
		return err
	}
	page := uint64(os.Getpagesize())
	base, off := addr&^(page-1), int(addr&(page-1))
	b, err := syscall.Mmap(int(m.f.Fd()), int64(base), int(page), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED) //nolint:gosec // physical addresses are far below 2^63
	if err != nil {
		return fmt.Errorf("map 0x%X: %w", addr, err)
	}
	defer func() { _ = syscall.Munmap(b) }()
	fn(b[off : off+width])
	return nil
}

func (m *devMem) Read(addr uint64, width int) (v uint32, err error) {
	err = m.access(addr, width, func(reg []byte) {
		if width == 4 {
			v = atomic.LoadUint32((*uint32)(unsafe.Pointer(&reg[0]))) //nolint:gosec // aligned, inside the mapping
			return
		}
		v = uint32(reg[0])
	})
	return v, err
}

func (m *devMem) Write(addr uint64, width int, v uint32) error {
	return m.access(addr, width, func(reg []byte) {
		if width == 4 {
			atomic.StoreUint32((*uint32)(unsafe.Pointer(&reg[0])), v) //nolint:gosec // aligned, inside the mapping
			return
		}
		reg[0] = byte(v)
	})
}

func (m *devMem) Close() error { return m.f.Close() }

//go:build linux

package panel

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// devMem maps physical pages from /dev/mem, one page-aligned mapping per page, shared with the kernel's view of the
// pad registers. Read-only unless opened with write.
type devMem struct {
	f     *os.File
	write bool
	pages map[uint64][]byte
}

// OpenMem opens /dev/mem. Needs root; a kernel with STRICT_DEVMEM may refuse ranges a driver has claimed.
func OpenMem(write bool) (Mem, error) {
	flag := os.O_RDONLY
	if write {
		flag = os.O_RDWR
	}
	f, err := os.OpenFile("/dev/mem", flag|syscall.O_SYNC, 0)
	if err != nil {
		return nil, err
	}
	return &devMem{f: f, write: write, pages: map[uint64][]byte{}}, nil
}

func (m *devMem) word(phys uint64) (*uint32, error) {
	if err := checkAligned(phys); err != nil {
		return nil, err
	}
	base, off := pageOf(phys)
	b, ok := m.pages[base]
	if !ok {
		prot := syscall.PROT_READ
		if m.write {
			prot |= syscall.PROT_WRITE
		}
		var err error
		b, err = syscall.Mmap(int(m.f.Fd()), int64(base), os.Getpagesize(), prot, syscall.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("map 0x%X: %w", base, err)
		}
		m.pages[base] = b
	}
	// The register is accessed as one 32-bit word, as the vendor driver does (readl/writel).
	return (*uint32)(unsafe.Pointer(&b[off])), nil //nolint:gosec // off is aligned and inside the mapped page
}

func (m *devMem) Read32(phys uint64) (uint32, error) {
	p, err := m.word(phys)
	if err != nil {
		return 0, err
	}
	return *p, nil
}

func (m *devMem) Write32(phys uint64, v uint32) error {
	if !m.write {
		return fmt.Errorf("/dev/mem opened read-only")
	}
	p, err := m.word(phys)
	if err != nil {
		return err
	}
	*p = v
	return nil
}

func (m *devMem) Close() error {
	for k, b := range m.pages {
		_ = syscall.Munmap(b)
		delete(m.pages, k)
	}
	return m.f.Close()
}

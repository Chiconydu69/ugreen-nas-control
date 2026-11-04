package hw

import "fmt"

// Mem is physical memory through /dev/mem. Registers are accessed whole and aligned: 1 byte or one aligned 4-byte
// dword, through a page-aligned mmap, never a byte-wise or unaligned pread/pwrite. Only the power-on-after-AC-loss
// command opens it, and only on the models whose setting is a memory-mapped register.
type Mem interface {
	Read(addr uint64, width int) (uint32, error)
	Write(addr uint64, width int, v uint32) error
	Close() error
}

// CheckMemAccess accepts width 1, or width 4 at a 4-byte aligned address.
func CheckMemAccess(addr uint64, width int) error {
	switch {
	case width == 1:
		return nil
	case width == 4 && addr%4 == 0:
		return nil
	}
	return fmt.Errorf("unsupported access of %d bytes at 0x%X (only 1 byte, or 4 bytes aligned)", width, addr)
}

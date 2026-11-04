package panel

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fakeMem is physical memory with a write log.
type fakeMem struct {
	mu     sync.Mutex
	words  map[uint64]uint32
	reads  []uint64
	writes [][2]uint64
	// onRead, when set, supplies the value instead of words (n counts reads from 0).
	onRead func(n int, phys uint64) uint32
	closed bool
}

func newFakeMem() *fakeMem { return &fakeMem{words: map[uint64]uint32{}} }

func (m *fakeMem) Read32(phys uint64) (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if phys&3 != 0 {
		return 0, fmt.Errorf("unaligned read 0x%X", phys)
	}
	n := len(m.reads)
	m.reads = append(m.reads, phys)
	if m.onRead != nil {
		return m.onRead(n, phys), nil
	}
	return m.words[phys], nil
}

func (m *fakeMem) Write32(phys uint64, v uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, [2]uint64{phys, uint64(v)})
	m.words[phys] = v
	return nil
}

func (m *fakeMem) Close() error { m.closed = true; return nil }

// readOnlyMem hides Write32 from a Mem, as the button path must see it.
type readOnlyMem struct{ MemReader }

// fakePorts is a plain I/O port space: last written value is read back, plus a write log.
type fakePorts struct {
	val    map[uint16]byte
	writes [][2]int
	failIn bool
	closed bool
}

func newFakePorts() *fakePorts { return &fakePorts{val: map[uint16]byte{}} }

func (p *fakePorts) In(port uint16) (byte, error) {
	if p.failIn {
		return 0, fmt.Errorf("in failed")
	}
	return p.val[port], nil
}

func (p *fakePorts) Out(port uint16, v byte) error {
	p.writes = append(p.writes, [2]int{int(port), int(v)})
	p.val[port] = v
	return nil
}

func (p *fakePorts) Close() error { p.closed = true; return nil }

// fakeEC speaks the ACPI EC byte protocol on ports 0x62/0x66.
type fakeEC struct {
	regs     [256]byte
	state    int // 0 idle, 1 read: want address, 2 write: want address, 3 write: want value
	addr     byte
	out      []byte
	writes   [][2]byte // register, value
	echo2    bool      // writing 1 reads back 2 (seen on the iDX6011)
	deaf     bool      // writes are dropped
	failRead bool
	// chip is the id read behind 0x4E/0x4F; zero means a genuine IT5571.
	chip   uint16
	sioReg byte
	// held, when set, reports whether the shared lock is held; accesses outside it are counted in unlocked.
	held     func() bool
	unlocked int
}

func (f *fakeEC) check() {
	if f.held != nil && !f.held() {
		f.unlocked++
	}
}

func (f *fakeEC) In(port uint16) (byte, error) {
	f.check()
	switch port {
	case ecSioData:
		id := f.chip
		if id == 0 {
			id = ecChipID
		}
		if f.sioReg == 0x21 {
			return byte(id), nil
		}
		return byte(id >> 8), nil
	case ecCmd:
		if len(f.out) > 0 {
			return ecOBF, nil
		}
		return 0, nil
	case ecData:
		if len(f.out) == 0 {
			return 0, nil
		}
		v := f.out[0]
		f.out = f.out[1:]
		return v, nil
	}
	return 0xFF, nil
}

func (f *fakeEC) Out(port uint16, v byte) error {
	f.check()
	switch port {
	case ecSioIndex:
		f.sioReg = v
	case ecCmd:
		switch v {
		case ecCmdRead:
			f.state = 1
		case ecCmdWrite:
			f.state = 2
		}
	case ecData:
		switch f.state {
		case 1:
			if !f.failRead {
				f.out = append(f.out, f.regs[v])
			}
			f.state = 0
		case 2:
			f.addr, f.state = v, 3
		case 3:
			f.writes = append(f.writes, [2]byte{f.addr, v})
			if !f.deaf {
				f.regs[f.addr] = v
				if f.echo2 && v == 1 {
					f.regs[f.addr] = 2
				}
			}
			f.state = 0
		}
	}
	return nil
}

func (f *fakeEC) Close() error { return nil }

func writeFile(dir, model string) error {
	return os.WriteFile(filepath.Join(dir, "product_name"), []byte(model+"\n"), 0o644)
}

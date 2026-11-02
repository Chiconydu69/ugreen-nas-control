package fan

import (
	"errors"
	"fmt"
	"slices"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// Power on after AC loss: what the NAS does when mains power returns. Explicit user action only (`ugreen-nas-fan
// power-on-ac`), never part of the daemon, status, restore or the probe.
//
// Facts per chip (UGREEN GPL-2.0-only drivers, kernel-6.12 drivers/ugreen @c75c5ab; facts only, nothing copied):
//   - IT8613E, LDN 4 registers 0xF2 and 0xF4: v = (F2[5] << 2) | F4[6:5]; 1 = on, 3 = off, 7 = last, anything else
//     unknown. Set = one bit at a time, F2 then F4 (ug_it86x-sio.c:153-203,260-301). An older BIOS encodes it
//     differently (dh2600/ug_it86x-sio-dh2600.c:182-255): `get` then reports unknown with the raw bytes, and `set`
//     refuses without --force; forced, it writes the three target bits from there and checks them by reading back.
//   - IT5571 EC of the DXP4800 GT: bare commands 0x88 (on) and 0x89 (off) on port 0x66, no read at all; the vendor
//     keeps a RAM cache (ug_it55_4800gt_functions.c:55-57,109-188).
//   - iDX6011/6012 and iDX6011 Pro: dword at physical 0xFE001020, bit 0 set = stay off, clear = power on. Accessed
//     as one aligned dword through a page-aligned mmap. The plain iDX6011/6012 is read only everywhere in this tool,
//     so `set` is refused there; the Pro can be set.
//     (ug_it55_functions.c:42,66-129; ug_it55pro_functions.c:53,90-160).
//   - DXP2800 GT (CS201x): AMD FCH byte at physical 0xFED8035B (one byte access), bits 1:0: 0 off, 1 on, 2 or 3 last
//     (ug_201x.c:125,409-437,539-606).
type acMode string

const (
	acOn      acMode = "on"
	acOff     acMode = "off"
	acLast    acMode = "last"
	acUnknown acMode = "unknown"
)

// ACState is what a Get reports. Readable is false where the hardware cannot tell (the GT).
type ACState struct {
	Mode     acMode         `json:"mode"`
	Readable bool           `json:"readable"`
	Raw      map[string]int `json:"raw"`
	Settable []acMode       `json:"settable"`
	Source   string         `json:"source"`
}

type acLoss interface {
	Get() (ACState, error)
	Set(acMode) error
}

var (
	errNoACControl   = errors.New("this model has no power-on setting this tool can reach")
	errACNotSettable = errors.New("this model does not offer that mode")
	errACReadOnly    = errors.New("this model's power-on setting is declared read-only here (iDX6011/6012): it can be read, not changed")
	errACVerify      = errors.New("the value read back is not the one written")
)

func parseACMode(s string) (acMode, bool) {
	switch m := acMode(s); m {
	case acOn, acOff, acLast:
		return m, true
	}
	return acUnknown, false
}

// IT8613E

// Bits in 0xF2 and 0xF4 (ug_it86x-sio.c:164-196@c75c5ab).
const (
	iteRegACF2 = 0xf2
	iteRegACF4 = 0xf4
	iteACF2Bit = 0x20 // F2[5]
	iteACF4Bit = 0x20 // F4[5]
	iteACF4Hi  = 0x40 // F4[6]
)

func decodeITEAC(f2, f4 byte) acMode {
	switch v := (f2&0x20)>>3 | (f4&0x60)>>5; v {
	case 0x01:
		return acOn
	case 0x03:
		return acOff
	case 0x07:
		return acLast
	}
	return acUnknown
}

// encodeITEAC changes only F2[5], F4[5] and F4[6].
func encodeITEAC(f2, f4 byte, m acMode) (nf2, nf4 byte) {
	nf2, nf4 = f2&^iteACF2Bit, f4|iteACF4Bit
	switch m {
	case acOn:
		nf4 &^= iteACF4Hi
	case acOff:
		nf4 |= iteACF4Hi
	case acLast:
		nf2 |= iteACF2Bit
		nf4 |= iteACF4Hi
	default:
		return f2, f4
	}
	return nf2, nf4
}

// ACRead reads F2 and F4 of logical device 4, no write.
func (c *Chip) ACRead() (f2, f4 byte, err error) {
	err = c.session(sioOpts{ldn: sioLDNMonitor}, func(s *sio) error {
		f2, f4 = s.rd(iteRegACF2), s.rd(iteRegACF4)
		return nil
	})
	return
}

// ACWrite writes the registers that differ, F2 first, as UGREEN's driver does; the write list is those two.
func (c *Chip) ACWrite(f2, f4 byte) error {
	cur2, cur4, err := c.ACRead()
	if err != nil {
		return err
	}
	return c.session(sioOpts{ldn: sioLDNMonitor, allow: func(r byte) bool { return r == iteRegACF2 || r == iteRegACF4 }}, func(s *sio) error {
		if f2 != cur2 {
			s.wr(iteRegACF2, f2)
		}
		if f4 != cur4 {
			s.wr(iteRegACF4, f4)
		}
		return nil
	})
}

type iteAC struct{ chip *Chip }

func (a iteAC) Get() (ACState, error) {
	f2, f4, err := a.chip.ACRead()
	if err != nil {
		return ACState{}, err
	}
	return ACState{
		Mode: decodeITEAC(f2, f4), Readable: true, Raw: map[string]int{"f2": int(f2), "f4": int(f4)},
		Settable: []acMode{acOn, acOff, acLast}, Source: "it8613",
	}, nil
}

func (a iteAC) Set(m acMode) error {
	if !slices.Contains([]acMode{acOn, acOff, acLast}, m) {
		return errACNotSettable
	}
	f2, f4, err := a.chip.ACRead()
	if err != nil {
		return err
	}
	nf2, nf4 := encodeITEAC(f2, f4, m)
	if err := a.chip.ACWrite(nf2, nf4); err != nil {
		return err
	}
	r2, r4, err := a.chip.ACRead()
	if err != nil {
		return err
	}
	if r2 != nf2 || r4 != nf4 {
		return fmt.Errorf("%w (F2 0x%02x, F4 0x%02x, wanted 0x%02x, 0x%02x)", errACVerify, r2, r4, nf2, nf4)
	}
	return nil
}

// IT5571 EC of the DXP4800 GT: write only.
type ecAC struct{ ec *EC }

func (a ecAC) Get() (ACState, error) {
	return ACState{Mode: acUnknown, Raw: map[string]int{}, Settable: []acMode{acOn, acOff}, Source: "ec-cmd"}, nil
}

func (a ecAC) Set(m acMode) error {
	sp := a.ec.sp()
	switch {
	case sp.acCmd == nil:
		return errNoACControl
	case m == acOn:
		return a.ec.SendCommand(sp.acCmd.on)
	case m == acOff:
		return a.ec.SendCommand(sp.acCmd.off)
	}
	return fmt.Errorf("%w: the embedded controller has no 'last state' command", errACNotSettable)
}

// Memory-mapped register field: iDX dword bit 0, FCH byte bits 1:0.
type mmioAC struct {
	mem      hw.Mem
	addr     uint64
	width    int    // bytes accessed: 4 or 1
	mask     uint32 // the field inside the value
	enc      map[acMode]uint32
	dec      func(field uint32) acMode
	source   string
	settable []acMode
	readOnly bool // read allowed, `set` refused
}

const (
	idxACAddr = 0xFE001020
	fchACAddr = 0xFED8035B
)

// newIdxAC: the iDX6011 Pro can be set; the plain iDX6011/6012 is read-only (readOnly), like everything else there.
func newIdxAC(m hw.Mem, readOnly bool) *mmioAC {
	settable := []acMode{acOn, acOff}
	if readOnly {
		settable = []acMode{}
	}
	return &mmioAC{
		mem: m, addr: idxACAddr, width: 4, mask: 1, source: "mmio-idx", settable: settable, readOnly: readOnly,
		enc: map[acMode]uint32{acOn: 0, acOff: 1},
		dec: func(f uint32) acMode {
			if f == 1 {
				return acOff
			}
			return acOn
		},
	}
}

func newFCHAC(m hw.Mem) *mmioAC {
	return &mmioAC{
		mem: m, addr: fchACAddr, width: 1, mask: 3, source: "fch-mmio", settable: []acMode{acOn, acOff, acLast},
		enc: map[acMode]uint32{acOff: 0, acOn: 1, acLast: 3},
		dec: func(f uint32) acMode {
			switch f {
			case 0:
				return acOff
			case 1:
				return acOn
			}
			return acLast // 2 is "last, after an abnormal shutdown only"; the raw value tells them apart
		},
	}
}

func (a *mmioAC) load() (uint32, error) { return a.mem.Read(a.addr, a.width) }

func (a *mmioAC) Get() (ACState, error) {
	v, err := a.load()
	if err != nil {
		return ACState{}, err
	}
	return ACState{
		Mode: a.dec(v & a.mask), Readable: true, Raw: map[string]int{"value": int(v), "field": int(v & a.mask)},
		Settable: a.settable, Source: a.source,
	}, nil
}

func (a *mmioAC) Set(m acMode) error {
	if a.readOnly {
		return errACReadOnly
	}
	field, ok := a.enc[m]
	if !ok {
		return fmt.Errorf("%w: this register has no %q value", errACNotSettable, m)
	}
	v, err := a.load()
	if err != nil {
		return err
	}
	nv := v&^a.mask | field
	if err := a.mem.Write(a.addr, a.width, nv); err != nil {
		return err
	}
	rb, err := a.load()
	if err != nil {
		return err
	}
	// Only the bits this tool owns are compared; the rest of the register may change on its own.
	if rb&a.mask != field {
		return fmt.Errorf("%w (field 0x%X, wanted 0x%X)", errACVerify, rb&a.mask, field)
	}
	return nil
}

// acEnv opens the hardware a model needs, lazily: ports for the Super-I/O and EC models, /dev/mem for the others.
type acEnv struct {
	openPorts func() (Ports, error)
	openMem   func() (hw.Mem, error)
	closers   []func()
}

func (e *acEnv) ports() (Ports, error) {
	p, err := e.openPorts()
	if err == nil {
		e.closers = append(e.closers, func() { _ = p.Close() })
	}
	return p, err
}

func (e *acEnv) mem() (hw.Mem, error) {
	m, err := e.openMem()
	if err != nil {
		return nil, fmt.Errorf("open /dev/mem: %w (needs root; a kernel with STRICT_DEVMEM may refuse the register)", err)
	}
	e.closers = append(e.closers, func() { _ = m.Close() })
	return m, nil
}

func (e *acEnv) close() {
	for _, f := range e.closers {
		f()
	}
}

// powerACFor picks the mechanism of a model; the caller holds the port lock while it detects.
func powerACFor(model string, e *acEnv) (acLoss, error) {
	switch {
	case isIT8613Model(model):
		p, err := e.ports()
		if err != nil {
			return nil, err
		}
		return iteAC{chip: &Chip{ports: p}}, nil
	case isDXP4800GT(model):
		p, err := e.ports()
		if err != nil {
			return nil, err
		}
		ec, err := DetectEC(p)
		if err != nil {
			return nil, err
		}
		return ecAC{ec: ec}, nil
	case isIdxPro(model) || isIdxPlain(model):
		m, err := e.mem() // plain iDX: read only, newIdxAC refuses the write
		if err != nil {
			return nil, err
		}
		return newIdxAC(m, isIdxPlain(model)), nil
	case isDXP2800GT(model):
		p, err := e.ports()
		if err != nil {
			return nil, err
		}
		if _, err := csReadWindow(p); err != nil { // chip id check only, the host interface stays as it is
			return nil, err
		}
		m, err := e.mem()
		if err != nil {
			return nil, err
		}
		return newFCHAC(m), nil
	}
	return nil, errNoACControl
}

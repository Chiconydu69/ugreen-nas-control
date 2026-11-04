//go:build linux

package chip

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// DXP4800 GT: the controller sits on an AMD FCH DesignWare I2C controller (ACPI AMDI0010)
// that Unraid's kernel has no driver for. This drives it through /dev/mem, so no kernel
// module is needed. It is only used when no /dev/i2c adapter exists.
const (
	dwPath    = "/dev/mem"
	dwAdapter = "Synopsys DesignWare I2C adapter" // name of the kernel driver's adapter

	aoacBase   = 0xfed81000 // page with the power registers (AOAC) of the FCH I2C controllers
	aoacOffset = 0xe4e      // controller 0 (AMDI0010:00); two bytes per controller

	dwCompType    = 0x44570140
	dwSDAHoldVers = 0x3131312a // from this version on, SDA_HOLD has an RX hold field
	dwTimeout     = time.Second

	dwCon          = 0x00
	dwTar          = 0x04
	dwDataCmd      = 0x10
	dwSSHcnt       = 0x14
	dwSSLcnt       = 0x18
	dwIntrMask     = 0x30
	dwRawIntrStat  = 0x34
	dwRxTL         = 0x38
	dwTxTL         = 0x3c
	dwClrIntr      = 0x40
	dwClrTxAbrt    = 0x54
	dwClrStopDet   = 0x60
	dwEnable       = 0x6c
	dwStatus       = 0x70
	dwTxFLR        = 0x74
	dwRxFLR        = 0x78
	dwSDAHold      = 0x7c
	dwTxAbrtSource = 0x80
	dwEnableStatus = 0x9c
	dwCompParam1   = 0xf4
	dwCompVersion  = 0xf8
	dwCompTypeReg  = 0xfc

	dwCmdRead    = 0x100
	dwCmdStop    = 0x200
	dwCmdRestart = 0x400
)

// dwMsg is one I2C message: bytes to write, or (read) a buffer to fill.
type dwMsg struct {
	read bool
	buf  []byte
}

// dwCommands turns messages into DATA_CMD words: reads are 0x100, the first byte after
// a direction change gets RESTART and the last byte of the last message STOP.
func dwCommands(msgs []dwMsg) []uint32 {
	var cmds []uint32
	for i, m := range msgs {
		for j, b := range m.buf {
			cmd := uint32(b)
			if m.read {
				cmd = dwCmdRead
			}
			if i > 0 && j == 0 {
				cmd |= dwCmdRestart
			}
			if i == len(msgs)-1 && j == len(m.buf)-1 {
				cmd |= dwCmdStop
			}
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

type dwI2C struct {
	fd      *os.File
	mem     []byte // controller registers
	aoac    []byte // power registers
	power   int    // offset of this controller's AOAC register in aoac
	addr    uint8
	txDepth int
	rxDepth int
}

// dwControllers lists the AMDI0010 controllers from /proc/iomem as name -> physical base,
// without the ones a kernel driver has bound (those show up as /dev/i2c-N).
// Only on the DXP4800 GT: the power register layout is verified on no other board.
func dwControllers() (names []string, bases []int64) {
	product, _ := os.ReadFile("/sys/class/dmi/id/product_name")
	if !strings.HasPrefix(string(product), "DXP4800") || !strings.Contains(string(product), "GT") {
		return nil, nil
	}

	content, _ := os.ReadFile("/proc/iomem")
	for _, line := range strings.Split(string(content), "\n") {
		span, name, ok := strings.Cut(line, " : ")
		if !ok || !strings.HasPrefix(name, "AMDI0010:") {
			continue
		}
		if _, err := os.Lstat("/sys/bus/platform/devices/" + name + "/driver"); err == nil { //nolint:gosec // name comes from /proc/iomem and must start with AMDI0010:
			continue
		}
		var start, end int64
		if _, err := fmt.Sscanf(strings.TrimSpace(span), "%x-%x", &start, &end); err == nil && start != 0 {
			names = append(names, name)
			bases = append(bases, start)
		}
	}
	return names, bases
}

func mapPhys(f *os.File, phys int64) ([]byte, error) {
	return syscall.Mmap(int(f.Fd()), phys, 4096, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
}

// openDW returns the controller that answers at addr.
func openDW(addr uint8) (*dwI2C, error) {
	names, bases := dwControllers()
	if len(names) == 0 {
		return nil, errors.New("no AMDI0010 I2C controller found in /proc/iomem")
	}

	f, err := os.OpenFile(dwPath, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return nil, err
	}
	aoac, err := mapPhys(f, aoacBase)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	err = errors.New("no device answered")
	for i, name := range names {
		n, _ := strconv.Atoi(name[strings.IndexByte(name, ':')+1:])
		d := &dwI2C{fd: f, aoac: aoac, power: aoacOffset + 2*n, addr: addr}
		if d.mem, err = mapPhys(f, bases[i]); err != nil {
			continue
		}
		if err = d.powerOn(); err == nil {
			if err = d.init(); err == nil {
				if err = d.xfer([]dwMsg{{read: true, buf: make([]byte, 1)}}); err == nil {
					return d, nil
				}
			}
		}
		_ = syscall.Munmap(d.mem)
	}

	_ = syscall.Munmap(aoac)
	_ = f.Close()
	return nil, fmt.Errorf("%s: %w", names[len(names)-1], err)
}

func (d *dwI2C) close() {
	_ = d.disable()
	_ = syscall.Munmap(d.mem)
	_ = syscall.Munmap(d.aoac)
	_ = d.fd.Close()
}

func (d *dwI2C) rd(reg int) uint32    { return atomic.LoadUint32((*uint32)(unsafe.Pointer(&d.mem[reg]))) } //nolint:gosec // mem is a live MMIO mapping, reg is a fixed offset
func (d *dwI2C) wr(reg int, v uint32) { atomic.StoreUint32((*uint32)(unsafe.Pointer(&d.mem[reg])), v) }    //nolint:gosec // mem is a live MMIO mapping, reg is a fixed offset

// powerOn does what the ACPI _PS0 method of the controller does: D0, power domain on,
// and for a controller that was off, a reset.
func (d *dwI2C) powerOn() error {
	ctl, state := &d.aoac[d.power], &d.aoac[d.power+1]
	if *ctl&3 == 0 && *state&7 == 7 {
		return d.checkType()
	}

	*ctl = *ctl&^3 | 1<<3
	for i := 0; i < 100 && *state&7 != 7; i++ {
		time.Sleep(time.Millisecond)
	}
	if *state&7 != 7 {
		return errors.New("I2C controller does not power on")
	}

	*ctl = *ctl&^(1<<6) | 1<<7 // isolate, hold in reset
	time.Sleep(200 * time.Microsecond)
	*ctl = *ctl&^(1<<7) | 1<<6
	time.Sleep(200 * time.Microsecond)
	return d.checkType()
}

func (d *dwI2C) checkType() error {
	if t := d.rd(dwCompTypeReg); t != dwCompType {
		return fmt.Errorf("not a DesignWare I2C controller (component type 0x%08x)", t)
	}
	return nil
}

// init sets standard mode (100 kHz) as a master, keeping the timings the firmware left.
func (d *dwI2C) init() error {
	if err := d.disable(); err != nil {
		return err
	}

	if d.rd(dwSSHcnt) == 0 || d.rd(dwSSLcnt) == 0 { // for the 150 MHz input clock
		d.wr(dwSSHcnt, 642)
		d.wr(dwSSLcnt, 749)
	}
	if hold := d.rd(dwSDAHold); d.rd(dwCompVersion) >= dwSDAHoldVers && hold>>16&0xff == 0 {
		d.wr(dwSDAHold, hold|1<<16)
	}

	params := d.rd(dwCompParam1)
	d.txDepth = int(params>>16&0xff) + 1
	d.rxDepth = int(params>>8&0xff) + 1
	d.wr(dwTxTL, uint32(d.txDepth/2))
	d.wr(dwRxTL, 0)
	d.wr(dwCon, 0x63|d.rd(dwCon)&(1<<11)) // master, standard speed, restart, no slave
	d.wr(dwIntrMask, 0)
	return nil
}

func (d *dwI2C) disable() error {
	if d.rd(dwRawIntrStat)&(1<<13) != 0 { // master holds the bus: abort, like __i2c_dw_disable
		if d.rd(dwEnable)&1 == 0 {
			d.wr(dwEnable, 1) // ABORT only works on an enabled controller
			time.Sleep(250 * time.Microsecond)
		}
		d.wr(dwEnable, 3)
		for i := 0; i < 1000 && d.rd(dwEnable)&2 != 0; i++ {
			time.Sleep(100 * time.Microsecond)
		}
	}
	for i := 0; i < 100; i++ {
		d.wr(dwEnable, 0)
		if d.rd(dwEnableStatus)&1 == 0 {
			return nil
		}
		time.Sleep(50 * time.Microsecond)
	}
	return errors.New("I2C controller does not disable")
}

// xfer runs the messages as one transfer. The deferred disable aborts a stuck one; abort
// and disable leave the configuration from init alone, so no re-init is needed.
func (d *dwI2C) xfer(msgs []dwMsg) error {
	start := time.Now()
	for d.rd(dwStatus)&1 != 0 {
		if time.Since(start) > 20*time.Millisecond {
			return errors.New("I2C bus is busy")
		}
		time.Sleep(100 * time.Microsecond)
	}

	if err := d.disable(); err != nil {
		return err
	}
	d.wr(dwTar, uint32(d.addr))
	d.wr(dwIntrMask, 0)
	d.wr(dwEnable, 1)
	d.rd(dwEnableStatus)
	d.rd(dwClrIntr)
	defer func() { _ = d.disable() }()

	cmds := dwCommands(msgs)
	want := 0
	for _, m := range msgs {
		if m.read {
			want += len(m.buf)
		}
	}

	var rx []byte
	sent, reads := 0, 0
	start = time.Now()
	for {
		for room := d.txDepth - int(d.rd(dwTxFLR)); sent < len(cmds) && room > 0; room-- {
			isRead := cmds[sent]&dwCmdRead != 0
			if isRead && reads-len(rx) >= d.rxDepth {
				break
			}
			d.wr(dwDataCmd, cmds[sent])
			sent++
			if isRead {
				reads++
			}
		}

		for d.rd(dwRxFLR) > 0 && len(rx) < want {
			rx = append(rx, byte(d.rd(dwDataCmd)))
		}

		raw := d.rd(dwRawIntrStat)
		if raw&(1<<6) != 0 { // TX_ABRT
			source := d.rd(dwTxAbrtSource)
			d.rd(dwClrTxAbrt)
			if source&(1|1<<3) != 0 {
				return fmt.Errorf("no acknowledge from 0x%02x", d.addr)
			}
			return fmt.Errorf("I2C transfer aborted (0x%x)", source)
		}
		if raw&(1<<9) != 0 && sent == len(cmds) && len(rx) == want {
			break
		}

		if time.Since(start) > dwTimeout {
			return errors.New("I2C controller timed out")
		}
		time.Sleep(20 * time.Microsecond)
	}

	for i := 0; i < 200 && d.rd(dwStatus)&(1<<5) != 0; i++ {
		time.Sleep(100 * time.Microsecond)
	}
	d.rd(dwClrStopDet)

	rest := rx
	for _, m := range msgs {
		if m.read {
			rest = rest[copy(m.buf, rest):]
		}
	}
	return nil
}

// smbus maps the SMBus requests of the /dev/i2c ioctl onto I2C messages.
func (d *dwI2C) smbus(readWrite, command byte, size uint32, data *[smbusBlockMax + 2]byte) error {
	cmd := dwMsg{buf: []byte{command}}

	if readWrite == smbusRead {
		n := 1
		if size == smbusI2CBlockData {
			n = int(data[0])
		} else if size != smbusByteData {
			return syscall.EOPNOTSUPP
		}
		if n < 1 || n > smbusBlockMax {
			return syscall.EINVAL
		}

		buf := make([]byte, n)
		if err := d.xfer([]dwMsg{cmd, {read: true, buf: buf}}); err != nil {
			return err
		}
		if size == smbusI2CBlockData {
			copy(data[1:], buf)
		} else {
			copy(data[:], buf)
		}
		return nil
	}

	switch {
	case size == smbusByteData:
		cmd.buf = append(cmd.buf, data[0])
	case (size == smbusBlockData || size == smbusI2CBlockData) && data[0] <= smbusBlockMax:
		if size == smbusBlockData { // SMBus: command, count, data
			cmd.buf = append(cmd.buf, data[0])
		}
		cmd.buf = append(cmd.buf, data[1:1+data[0]]...)
	default:
		return syscall.EOPNOTSUPP
	}
	return d.xfer([]dwMsg{cmd})
}

// scanDW is scanBus for the /dev/mem controller; the lock is held by the caller.
func scanDW() (found, busy []string, err error) {
	d, err := openDW(controllerAddr)
	if err != nil {
		return nil, nil, err
	}
	defer d.close()

	for addr := uint8(0x03); addr <= 0x77; addr++ {
		d.addr = addr
		if d.xfer([]dwMsg{{read: true, buf: make([]byte, 1)}}) == nil {
			found = append(found, fmt.Sprintf("0x%02x", addr))
		}
	}
	return found, nil, nil
}

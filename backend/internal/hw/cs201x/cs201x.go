// Package cs201x is the access to the Chipsea CS201x Super-I/O and its embedded controller (DXP2800 GT) that the
// fan driver and the LED backend share: the Super-I/O window, the host-to-EC byte access and the one lock that keeps
// the port pairs to one process at a time.
//
// Register addresses and the access sequence are facts from UGREEN's GPL-2.0-only driver ug_201x.c@c75c5ab (cited as
// ug_201x.c:LINE); no code is used.
package cs201x

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

const (
	ChipID = 0x2011 // ug_201x.c:30

	LDNHost2EC = 0x0a // ug_201x.c:48

	// Offsets from the base address of the host-to-EC interface (ug_201x.c:80-84,215-248).
	EcCtrl = 0x01 // 0 = one byte, no auto increment
	EcAddr = 0x04 // 32-bit register address, least significant byte first
	EcData = 0x08
)

// Super-I/O configuration ports and registers (ug_201x.c:39-48,155-164,257-266).
const (
	sioIndex = 0x2e
	sioData  = 0x2f

	sioRegLDN    = 0x07
	sioRegChipID = 0x20
	sioRegRev    = 0x22
	sioRegActive = 0x30
	sioRegBaseHi = 0x60
	sioRegBaseLo = 0x61
)

var enterKey = []byte{0xa5, 0x69, 0x87} // ug_201x.c:185-205
var exitKey = []byte{0x87, 0x69, 0xa5}

// Window is what the Super-I/O window of the host-to-EC device tells.
type Window struct {
	ID, Rev uint16
	Active  byte
	Base    uint16
}

// holdSignals keeps SIGTERM, SIGINT and SIGHUP from killing the process while the config window is unlocked; the
// returned func hands a caught signal back to the process once the window is closed (default action, or the handler
// of a daemon).
func holdSignals() (release func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	return func() {
		signal.Stop(ch)
		select {
		case s := <-ch:
			_ = syscall.Kill(os.Getpid(), s.(syscall.Signal))
		default:
		}
	}
}

// ReadWindow opens the Super-I/O window, reads chip id, revision, the host interface state and the base address,
// and closes it again. It writes the unlock key, the device number and, only with enableHost and only when the
// interface is off, cfg 0x30 = 1 (ug_201x.c:264-266: UGREEN does that on every load). A chip id other than
// CS201x ends it before the device number is written.
func ReadWindow(p hw.Ports, enableHost bool) (w Window, err error) {
	defer holdSignals()()
	leave := func() {
		for _, b := range exitKey {
			_ = p.Out(sioIndex, b)
		}
	}
	for _, b := range enterKey {
		if err := p.Out(sioIndex, b); err != nil {
			leave() // part of the key may be in: the exit key resets the unlock state machine
			return w, err
		}
	}
	read := func(reg byte) byte {
		if p.Out(sioIndex, reg) != nil {
			return 0xff
		}
		v, err := p.In(sioData)
		if err != nil {
			return 0xff
		}
		return v
	}

	w.ID = uint16(read(sioRegChipID))<<8 | uint16(read(sioRegChipID+1))
	w.Rev = uint16(read(sioRegRev))<<8 | uint16(read(sioRegRev+1)) // 16 bit as UGREEN (ug_201x.c:155-164)
	if w.ID != ChipID {
		leave()
		return w, fmt.Errorf("Super-I/O chip id 0x%04x, expected 0x%04x: nothing else was read", w.ID, ChipID)
	}

	_ = p.Out(sioIndex, sioRegLDN)
	_ = p.Out(sioData, LDNHost2EC)
	w.Active = read(sioRegActive)
	if enableHost && w.Active&0x01 == 0 {
		_ = p.Out(sioIndex, sioRegActive)
		_ = p.Out(sioData, 0x01)
		w.Active = read(sioRegActive)
	}
	w.Base = uint16(read(sioRegBaseHi))<<8 | uint16(read(sioRegBaseLo))
	leave()
	return w, nil
}

// EC reads and writes EC registers through the host-to-EC window at Base, outside the Super-I/O window.
type EC struct {
	P    hw.Ports
	Base uint16
}

func (e EC) latch(reg uint32) error {
	if err := e.P.Out(e.Base+EcCtrl, 0); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		if err := e.P.Out(e.Base+EcAddr+uint16(i), byte(reg>>(8*i))); err != nil {
			return err
		}
	}
	return nil
}

func (e EC) Read(reg uint32) (byte, error) {
	if err := e.latch(reg); err != nil {
		return 0, err
	}
	return e.P.In(e.Base + EcData)
}

func (e EC) Write(reg uint32, v byte) error {
	if err := e.latch(reg); err != nil {
		return err
	}
	return e.P.Out(e.Base+EcData, v)
}

func PlausibleBase(base uint16) bool { return base >= 0x100 && base != 0xffff }

// LockPath is the flock file that serialises every access to the Super-I/O ports and the EC window. It is the lock
// of the fan tool and daemon (ugreen-nas-fan), so the LED backend, which runs in other processes, cannot interleave
// its address latch with a fan access. One implementation for all users: hw.LockPath (UGREEN_FAN_LOCK).
func LockPath() string { return hw.LockPath() }

// SetLockPath points the lock at another file (tests); the returned func puts it back.
func SetLockPath(path string) (restore func()) { return hw.SetLockPath(path) }

// WithLock runs fn while holding the exclusive lock. Never nest it: a second flock on the same file from the same
// process blocks forever.
func WithLock(fn func() error) error { return hw.WithLock(fn) }

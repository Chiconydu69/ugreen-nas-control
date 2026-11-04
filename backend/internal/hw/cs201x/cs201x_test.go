package cs201x

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// fakeChip is the Super-I/O window and the host-to-EC window of a CS201x.
type fakeChip struct {
	idx    byte
	id     uint16
	active byte
	base   uint16
	ec     map[uint32]byte
	addr   [4]byte
	cfgOut []byte // values written to the config data port
}

func (f *fakeChip) In(port uint16) (byte, error) {
	switch port {
	case sioData:
		switch f.idx {
		case 0x20:
			return byte(f.id >> 8), nil
		case 0x21:
			return byte(f.id), nil
		case 0x30:
			return f.active, nil
		case 0x60:
			return byte(f.base >> 8), nil
		case 0x61:
			return byte(f.base), nil
		}
		return 0, nil
	case f.base + EcData:
		return f.ec[f.reg()], nil
	}
	return 0xff, nil
}

func (f *fakeChip) reg() uint32 {
	return uint32(f.addr[0]) | uint32(f.addr[1])<<8 | uint32(f.addr[2])<<16 | uint32(f.addr[3])<<24
}

func (f *fakeChip) Out(port uint16, v byte) error {
	switch {
	case port == sioIndex:
		f.idx = v
	case port == sioData:
		f.cfgOut = append(f.cfgOut, v)
		if f.idx == 0x30 {
			f.active = v
		}
	case port >= f.base+EcAddr && port < f.base+EcAddr+4:
		f.addr[port-f.base-EcAddr] = v
	case port == f.base+EcData:
		f.ec[f.reg()] = v
	}
	return nil
}

func (f *fakeChip) Close() error { return nil }

func newChip() *fakeChip {
	return &fakeChip{id: ChipID, active: 1, base: 0x200, ec: map[uint32]byte{}}
}

func TestEcRoundTrip(t *testing.T) {
	f := newChip()
	ec := EC{P: f, Base: f.base}
	if err := ec.Write(0x20005444, 0x7f); err != nil {
		t.Fatal(err)
	}
	if v, err := ec.Read(0x20005444); err != nil || v != 0x7f {
		t.Fatalf("read %#x %v", v, err)
	}
	if f.ec[0x20005444] != 0x7f || f.addr != [4]byte{0x44, 0x54, 0x00, 0x20} {
		t.Fatalf("address latch %v", f.addr)
	}
}

func TestReadWindowHostSwitch(t *testing.T) {
	f := newChip()
	f.active = 0
	if w, err := ReadWindow(f, false); err != nil || w.Active != 0 || w.Base != 0x200 || len(f.cfgOut) != 1 {
		t.Fatalf("read only: %+v %v writes %v", w, err, f.cfgOut)
	}
	// Only the logical device number (0x0a) was written to the data port, not cfg 0x30.
	if f.cfgOut[0] != LDNHost2EC || f.active != 0 {
		t.Fatalf("read-only window changed the chip: %v active %d", f.cfgOut, f.active)
	}
	if w, err := ReadWindow(f, true); err != nil || w.Active != 1 || f.active != 1 {
		t.Fatalf("enable: %+v %v", w, err)
	}
}

func TestReadWindowWrongChip(t *testing.T) {
	f := newChip()
	f.id = 0x5571
	if _, err := ReadWindow(f, true); err == nil || len(f.cfgOut) != 0 {
		t.Fatalf("err %v writes %v: nothing may be written for another chip", err, f.cfgOut)
	}
}

func TestWithLockExcludes(t *testing.T) {
	defer SetLockPath(filepath.Join(t.TempDir(), "lock"))()
	var inside atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- WithLock(func() error {
			inside.Add(1)
			close(entered)
			<-release
			inside.Add(-1)
			return nil
		})
	}()
	<-entered
	second := make(chan int32, 1)
	go func() {
		_ = WithLock(func() error { second <- inside.Load(); return nil })
	}()
	select {
	case <-second:
		t.Fatal("second holder got the lock while the first one held it")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := <-second; n != 0 {
		t.Fatalf("second ran with %d holders inside", n)
	}
}

func TestLockPathMatchesSharedHelper(t *testing.T) {
	for _, env := range []string{"", filepath.Join(t.TempDir(), "env.lock")} {
		t.Setenv(hw.LockEnv, env)
		if LockPath() != hw.LockPath() {
			t.Errorf("env %q: cs201x %q, hw %q", env, LockPath(), hw.LockPath())
		}
	}
	t.Setenv(hw.LockEnv, "")
	if LockPath() != hw.DefaultLockPath {
		t.Errorf("default %q", LockPath())
	}
}

// idxLog records what goes to the index port; failAt (1-based, 0 = never) fails that write; onWrite runs after a write.
type idxLog struct {
	*fakeChip
	idxOut  []byte
	failAt  int
	onWrite func(n int)
}

func (l *idxLog) Out(port uint16, v byte) error {
	if port == sioIndex {
		l.idxOut = append(l.idxOut, v)
		if n := len(l.idxOut); n == l.failAt {
			return errors.New("port write failed")
		} else if l.onWrite != nil {
			l.onWrite(n)
		}
	}
	return l.fakeChip.Out(port, v)
}

func hasSuffix(b, tail []byte) bool {
	return len(b) >= len(tail) && string(b[len(b)-len(tail):]) == string(tail)
}

func TestReadWindowLeavesAfterKeyError(t *testing.T) {
	l := &idxLog{fakeChip: newChip(), failAt: 2}
	if _, err := ReadWindow(l, false); err == nil {
		t.Fatal("want the write error")
	}
	if !hasSuffix(l.idxOut, exitKey) {
		t.Fatalf("exit key missing after a failed unlock: % x", l.idxOut)
	}
}

func TestReadWindowHoldsSignal(t *testing.T) {
	got := make(chan os.Signal, 2)
	signal.Notify(got, syscall.SIGHUP) // a handler; it stays until the re-sent signal arrived, else the default action ends the test binary
	defer signal.Stop(got)
	l := &idxLog{fakeChip: newChip()}
	l.onWrite = func(n int) {
		if n == 4 { // inside the config window
			_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
			time.Sleep(50 * time.Millisecond)
		}
	}
	if _, err := ReadWindow(l, false); err != nil {
		t.Fatal(err)
	}
	if !hasSuffix(l.idxOut, exitKey) {
		t.Fatalf("window not closed: % x", l.idxOut)
	}
	for i := 0; i < 2; i++ { // the original (also seen by the test handler) and the one handed back
		select {
		case <-got:
		case <-time.After(2 * time.Second):
			t.Fatal("signal not handed back after the window closed")
		}
	}
}

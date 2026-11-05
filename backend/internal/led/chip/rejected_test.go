package chip

import (
	"bytes"
	"errors"
	"strings"
	"syscall"
	"testing"
)

// nakMCU is the DXP2800 SUB2 board: the address answers a detect, every read and write is refused.
type nakMCU struct{ writes, reads int }

func (m *nakMCU) writeBlock(byte, uint32, []byte) error { m.writes++; return syscall.ENXIO }
func (m *nakMCU) readByte(byte) (byte, error)           { m.reads++; return 0, syscall.ENXIO }

func detectOK() error  { return nil }
func detectNAK() error { return syscall.ENXIO }

// Both frames are tried once each (bounded), and the result is the one clear error.
func TestRejectingControllerIsReportedOnce(t *testing.T) {
	withDXP2800(t)

	m := &nakMCU{}
	err := asRejected(sendFrames(m, framesFor(false), 0, cmdOnOff, [4]byte{1}), detectOK)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
	if !strings.Contains(err.Error(), "rejects every read and write") {
		t.Errorf("message %q", err)
	}
	if m.writes != 2*retries || m.reads != 0 {
		t.Errorf("writes %d reads %d, want %d 0: no endless retries", m.writes, m.reads, 2*retries)
	}

	// The frame order is not flipped by failures.
	dxp2800Pref.mu.Lock()
	flipped := dxp2800Pref.idxFirst
	dxp2800Pref.mu.Unlock()
	if flipped {
		t.Error("a failing controller must not change the preferred frame")
	}
}

// Without a detect ACK it is an ordinary bus error, and so is a controller that replies but does not confirm.
func TestRejectedNeedsDetectAndNak(t *testing.T) {
	withDXP2800(t)

	m := &nakMCU{}
	if err := asRejected(sendFrames(m, framesFor(false), 0, cmdOnOff, [4]byte{1}), detectNAK); errors.Is(err, ErrRejected) {
		t.Error("no detect answer: not the rejected state")
	}

	notConfirming := &fakeMCU{}
	if err := asRejected(sendFrames(notConfirming, framesFor(false), 0, cmdOnOff, [4]byte{1}), detectOK); errors.Is(err, ErrRejected) {
		t.Errorf("a reply that is not a confirmation is not a rejection: %v", err)
	}
}

// rejectingBus is a ledBus that classified its failures as ErrRejected.
type rejectingBus struct{ statusCalls int }

func (b *rejectingBus) change(byte, byte, [4]byte) error {
	return asRejected(busFailure{syscall.ENXIO}, detectOK)
}

func (b *rejectingBus) status(name string) (ledStatus, error) {
	b.statusCalls++
	return ledStatus{}, errors.Join(errors.New(name), asRejected(busFailure{syscall.ENXIO}, detectOK))
}
func (b *rejectingBus) close()          {}
func (b *rejectingBus) busPath() string { return "/dev/i2c-0" }

func TestCommandsExitWithRejectedCode(t *testing.T) {
	bus := &rejectingBus{}
	saved := openLedBus
	openLedBus = func(string, bool) (ledBus, error) { return bus, nil }
	t.Cleanup(func() { openLedBus = saved })

	for _, args := range [][]string{{"probe"}, {"get", "power"}, {"get", "all"}, {"set", "power", "on"}} {
		var out, errOut bytes.Buffer
		if rc := Run(args, &out, &errOut); rc != ExitRejected {
			t.Errorf("%v: exit %d, want %d (stderr %q)", args, rc, ExitRejected, errOut.String())
		}
		if !strings.Contains(errOut.String(), "rejects every read and write") {
			t.Errorf("%v: stderr %q lacks the message", args, errOut.String())
		}
	}

	if bus.statusCalls > 3 {
		t.Errorf("probe and get stop at the first LED: %d status calls", bus.statusCalls)
	}
}

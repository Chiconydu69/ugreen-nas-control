package chip

import (
	"bytes"
	"errors"
	"testing"
)

// fakeMCU is a controller that acknowledges only the frames it accepts, and logs what it was sent.
type fakeMCU struct {
	accept []byte // size of the frames it understands, 0 = none
	sent   []sentFrame
	acked  bool
}

type sentFrame struct {
	reg     byte
	size    uint32
	payload []byte
}

func (m *fakeMCU) writeBlock(command byte, size uint32, payload []byte) error {
	m.sent = append(m.sent, sentFrame{command, size, append([]byte(nil), payload...)})
	// legacy frames are 12 bytes with the ID first; idx frames 11 bytes without
	m.acked = bytes.Contains(m.accept, []byte{byte(len(payload))})

	return nil
}

func (m *fakeMCU) readByte(byte) (byte, error) {
	if m.acked {
		return 1, nil
	}
	return 0, nil
}

func withDXP2800(t *testing.T) {
	t.Helper()

	saved := getLayout()
	t.Cleanup(func() { SetLayout(saved) })
	SetLayout(layoutDXP2800)

	dxp2800Pref.mu.Lock()
	dxp2800Pref.idxFirst = false
	dxp2800Pref.mu.Unlock()
}

func sentSizes(m *fakeMCU) (legacy, idx int) {
	for _, f := range m.sent {
		if len(f.payload) == 12 {
			legacy++
		} else {
			idx++
		}
	}
	return legacy, idx
}

// A board that answers the legacy frame is written exactly as before: no idx frame is ever sent.
func TestDXP2800LegacyBoardUnchanged(t *testing.T) {
	withDXP2800(t)

	m := &fakeMCU{accept: []byte{12}}
	if err := sendFrames(m, framesFor(false), 3, cmdColor, [4]byte{255, 128, 0}); err != nil {
		t.Fatal(err)
	}

	if legacy, idx := sentSizes(m); legacy != 1 || idx != 0 {
		t.Fatalf("legacy %d idx %d, want 1 0", legacy, idx)
	}
	if want := commandPacket(3, cmdColor, [4]byte{255, 128, 0}); !bytes.Equal(m.sent[0].payload, want) || m.sent[0].size != smbusI2CBlockData || m.sent[0].reg != 3 {
		t.Fatalf("sent %+v", m.sent[0])
	}
}

// The V1.2 board ignores the legacy frame: after the legacy retries the idx frame is sent and acknowledged, and the
// next command goes to the idx frame first.
func TestDXP2800FallsBackToIdxFrame(t *testing.T) {
	withDXP2800(t)

	m := &fakeMCU{accept: []byte{11}}
	if err := sendFrames(m, framesFor(false), 3, cmdColor, [4]byte{255, 128, 0}); err != nil {
		t.Fatal(err)
	}

	if legacy, idx := sentSizes(m); legacy != retries || idx != 1 {
		t.Fatalf("legacy %d idx %d, want %d 1", legacy, idx, retries)
	}
	last := m.sent[len(m.sent)-1]
	if want := idxCommandPacket(cmdColor, [4]byte{255, 128, 0}); !bytes.Equal(last.payload, want) || last.size != smbusBlockData || last.reg != 3 {
		t.Fatalf("idx frame sent %+v", last)
	}

	m.sent = nil
	if err := sendFrames(m, framesFor(false), 0, cmdOnOff, [4]byte{1}); err != nil {
		t.Fatal(err)
	}
	if legacy, idx := sentSizes(m); legacy != 0 || idx != 1 {
		t.Fatalf("second command: legacy %d idx %d, want 0 1", legacy, idx)
	}
}

// If the chip takes neither frame, the error names both.
func TestDXP2800NoFrameAcknowledged(t *testing.T) {
	withDXP2800(t)

	m := &fakeMCU{}
	err := sendFrames(m, framesFor(false), 0, cmdOnOff, [4]byte{1})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"legacy frame", "idx frame", "did not confirm"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if legacy, idx := sentSizes(m); legacy != retries || idx != retries {
		t.Fatalf("legacy %d idx %d, want %d each", legacy, idx, retries)
	}
}

// Every other layout has exactly one frame, so a failure is not retried with a second shape.
func TestOtherLayoutsHaveOneFrame(t *testing.T) {
	saved := getLayout()
	defer SetLayout(saved)

	for layout, want := range map[string]string{"": "legacy", "idx": "idx", "idx-pro": "idx"} {
		SetLayout(layout)
		if got := framesFor(false); len(got) != 1 || got[0].name != want {
			t.Errorf("layout %q: %v, want only %s", layout, got, want)
		}
	}
	if got := framesFor(true); len(got) != 1 || got[0].name != "gt" {
		t.Errorf("gt: %v", got)
	}

	var failing failingIO
	if err := sendFrames(failing, framesFor(false), 0, cmdOnOff, [4]byte{}); err == nil || err.Error() != "boom" {
		t.Errorf("single-frame error = %v, want the plain error", err)
	}
}

type failingIO struct{}

func (failingIO) writeBlock(byte, uint32, []byte) error { return errors.New("boom") }
func (failingIO) readByte(byte) (byte, error)           { return 0, errors.New("boom") }

func TestIdxFirstFromSystemVersion(t *testing.T) {
	for _, c := range []struct {
		product, version string
		want             bool
	}{
		{"DXP2800", "EM_DXP2800_SUB2_V1.0.27", false},
		{"DXP2800", "DXP2800 V1.2 251215", true},
		{"DXP2800", "", false},
		{"DXP2800", "Default string", false},
		{"DXP2800", "To Be Filled By O.E.M.", false},
		{"DXP6800", "DXP6800 V1.2 251215", false},
		{"DXP6800S", "V1", false},
		{"DXP4800", "V1", false},
	} {
		if got := idxFirstFor(c.product, c.version); got != c.want {
			t.Errorf("idxFirstFor(%q, %q) = %v, want %v", c.product, c.version, got, c.want)
		}
	}
}

// The system version decides the first frame; the other one stays the fallback.
func TestDXP2800FirstFrameFollowsSystemVersion(t *testing.T) {
	saved, savedVersion := getLayout(), systemVersion
	t.Cleanup(func() {
		SetLayout(saved)
		systemVersion = savedVersion
		dxp2800Pref.mu.Lock()
		dxp2800Pref.idxFirst = false
		dxp2800Pref.mu.Unlock()
	})

	for version, first := range map[string]string{"EM_DXP2800_SUB2_V1.0.27": "legacy", "DXP2800 V1.2 251215": "idx"} {
		systemVersion = func() string { return version }
		UseProduct("DXP2800")

		got := framesFor(false)
		if len(got) != 2 || got[0].name != first {
			t.Errorf("version %q: first frame %v, want %s then the other", version, got, first)
		}
	}

	// DXP6800 keeps legacy first whatever the version says.
	systemVersion = func() string { return "DXP6800 V1.2 251215" }
	UseProduct("DXP6800")
	if got := framesFor(false); got[0].name != "legacy" {
		t.Errorf("DXP6800: %v", got)
	}
}

// A V1.2 board seeded to the idx frame is written with it at once, without the failed legacy attempts.
func TestDXP2800SeededIdxBoardSendsIdxFirst(t *testing.T) {
	withDXP2800(t)
	dxp2800Pref.mu.Lock()
	dxp2800Pref.idxFirst = true
	dxp2800Pref.mu.Unlock()

	m := &fakeMCU{accept: []byte{11}}
	if err := sendFrames(m, framesFor(false), 0, cmdOnOff, [4]byte{1}); err != nil {
		t.Fatal(err)
	}
	if legacy, idx := sentSizes(m); legacy != 0 || idx != 1 {
		t.Fatalf("legacy %d idx %d, want 0 1", legacy, idx)
	}

	// A board that says idx but only takes the legacy frame still works through the fallback.
	m = &fakeMCU{accept: []byte{12}}
	if err := sendFrames(m, framesFor(false), 0, cmdOnOff, [4]byte{1}); err != nil {
		t.Fatal(err)
	}
}

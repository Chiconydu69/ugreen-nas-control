package fan

import "testing"

// fakeITE answers like the IT8613E of the DXP6800 Pro (register dump from 2026-10-06).
type fakeITE struct {
	sioKey   int
	sioIndex byte
	ldn      byte
	hwmIndex byte
	hwm      [256]byte
	writes   int
	absent   bool
}

func newFakeITE() *fakeITE {
	f := &fakeITE{}
	f.hwm[0x58] = 0x90
	// fan2 0x01ec, fan3 0x03b6, fan4 0x03d7
	f.hwm[0x0e], f.hwm[0x19] = 0xec, 0x01
	f.hwm[0x0f], f.hwm[0x1a] = 0xb6, 0x03
	f.hwm[0x80], f.hwm[0x81] = 0xd7, 0x03
	f.hwm[0x6b], f.hwm[0x73], f.hwm[0x7b] = 0x50, 0x50, 0x50
	f.hwm[0x7f] = 0x50
	f.hwm[0x29], f.hwm[0x2a], f.hwm[0x2b] = 0x3c, 0x21, 0x80
	return f
}

func (f *fakeITE) In(port uint16) (byte, error) {
	if f.absent {
		return 0xff, nil
	}
	switch port {
	case sioData:
		if f.sioKey < 4 {
			return 0xff, nil
		}
		switch f.sioIndex {
		case 0x20:
			return 0x86, nil
		case 0x21:
			return 0x13, nil
		case 0x22:
			return 0x0c, nil
		}
		if f.ldn == 4 {
			switch f.sioIndex {
			case 0x30:
				return 0x01, nil
			case 0x60:
				return 0x0a, nil
			case 0x61:
				return 0x30, nil
			}
		}
		return 0, nil
	case 0x0a36:
		return f.hwm[f.hwmIndex], nil
	}
	return 0xff, nil
}

func (f *fakeITE) Out(port uint16, v byte) error {
	switch port {
	case sioIndex:
		if f.sioKey < 4 {
			if v == sioEnterKey[f.sioKey] {
				f.sioKey++
			} else {
				f.sioKey = 0
			}
			return nil
		}
		f.sioIndex = v
	case sioData:
		switch f.sioIndex {
		case sioRegLDN:
			f.ldn = v
		case sioRegExit:
			f.sioKey = 0
		}
	case 0x0a35:
		f.hwmIndex = v
	case 0x0a36:
		f.hwm[f.hwmIndex] = v
		f.writes++
	}
	return nil
}

func (f *fakeITE) Close() error { return nil }

func TestDetectAndRead(t *testing.T) {
	f := newFakeITE()
	chip, err := Detect(f)
	if err != nil {
		t.Fatal(err)
	}
	if chip.Base != 0x0a30 || chip.ID != 0x8613 || chip.Revision != 0x0c {
		t.Fatalf("chip = %+v", chip)
	}
	if f.sioKey != 0 {
		t.Fatal("config mode was not left")
	}

	want := map[string]int{"fan2": 1371, "fan3": 710, "fan4": 686}
	for _, ch := range chip.Channels {
		rpm, err := chip.RPM(ch)
		if err != nil || rpm != want[ch.ID] {
			t.Errorf("%s rpm = %d, want %d", ch.ID, rpm, want[ch.ID])
		}
		duty, auto, _ := chip.PWM(ch)
		if dutyToPercent(duty, 255) != 31 || auto {
			t.Errorf("%s pwm = %d auto=%v", ch.ID, duty, auto)
		}
	}

	if !chip.AutoDutyKnown() {
		t.Error("IT8613 duty must count in auto mode")
	}

	temps := chip.Temps()
	if temps[0] == nil || *temps[0] != 60 || temps[1] == nil || *temps[1] != 33 || temps[2] != nil {
		t.Errorf("temps = %v", temps)
	}
}

func TestDetectRejectsOtherChips(t *testing.T) {
	f := newFakeITE()
	f.absent = true // nothing answers: every register reads 0xff
	if _, err := Detect(f); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSetPWMAndRestore(t *testing.T) {
	f := newFakeITE()
	chip, _ := Detect(f)
	snap, _ := chip.Snapshot()

	ch, _ := chip.Channel("fan3")
	f.hwm[ch.Ctrl] = 0x81 // automatic mode
	if err := chip.SetPWM(ch, percentToDuty(50, 255)); err != nil {
		t.Fatal(err)
	}
	if f.hwm[ch.Duty] != 128 || f.hwm[ch.Ctrl] != 0x01 {
		t.Fatalf("duty %#x ctrl %#x", f.hwm[ch.Duty], f.hwm[ch.Ctrl])
	}

	if err := chip.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if f.hwm[ch.Duty] != 0x50 || f.hwm[ch.Ctrl] != 0x00 {
		t.Fatalf("restore: duty %#x ctrl %#x", f.hwm[ch.Duty], f.hwm[ch.Ctrl])
	}
}

func TestPercentDuty(t *testing.T) {
	for pct := 0; pct <= 100; pct++ {
		if got := dutyToPercent(percentToDuty(pct, 255), 255); got != pct {
			t.Errorf("%d%% -> %d%%", pct, got)
		}
	}
}

func TestRPMFromCountNoReading(t *testing.T) {
	for _, c := range []int{0, 0xffff, 0x0fff} {
		if r := rpmFromCount(c); r != 0 {
			t.Errorf("count 0x%x = %d rpm, want 0", c, r)
		}
	}
	if r := rpmFromCount(0x0fff - 1); r == 0 {
		t.Error("neighbouring count must still read")
	}
}

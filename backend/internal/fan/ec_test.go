package fan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeEC answers like the IT5571 of the DXP4800 GT through the ACPI EC ports
// (register dump from 2026-10-23).
type fakeEC struct {
	sioIndex byte
	regs     [256]byte
	state    int // 0 idle, 1 read: wait address, 2 write: wait address, 3 write: wait value
	addr     byte
	out      []byte
	busy     int // status polls that still show IBF after a byte was written
	absent   bool
	chipID   uint16        // 0 = the IT5571 id
	writes   []byte        // registers written, in order
	stuck    map[byte]bool // writes to these registers are logged but do not stick
}

func newFakeEC() *fakeEC {
	f := &fakeEC{}
	f.regs[0xb0], f.regs[0xb1] = 0, 38
	f.regs[0xb4], f.regs[0xb5] = 0, 82
	f.regs[0x34], f.regs[0x35] = 0x03, 0xd1 // 977
	f.regs[0x38], f.regs[0x39] = 0x02, 0xdb // 731
	f.regs[0x5f], f.regs[0x70] = 41, 57
	return f
}

func (f *fakeEC) In(port uint16) (byte, error) {
	if f.absent {
		return 0xff, nil
	}
	switch port {
	case ecSioData:
		switch f.sioIndex {
		case 0x20:
			if f.chipID != 0 {
				return byte(f.chipID >> 8), nil
			}
			return 0x55, nil
		case 0x21:
			if f.chipID != 0 {
				return byte(f.chipID), nil
			}
			return 0x71, nil
		}
		return 0, nil
	case ecCmdPort:
		var st byte
		if f.busy > 0 {
			f.busy--
			st |= ecIBF
		}
		if len(f.out) > 0 {
			st |= ecOBF
		}
		return st, nil
	case ecDataPort:
		if len(f.out) == 0 {
			return 0xff, nil
		}
		v := f.out[0]
		f.out = f.out[1:]
		return v, nil
	}
	return 0xff, nil
}

func (f *fakeEC) Out(port uint16, v byte) error {
	switch port {
	case ecSioIndex:
		f.sioIndex = v
	case ecCmdPort:
		switch v {
		case ecCmdRead:
			f.state = 1
		case ecCmdWrite:
			f.state = 2
		}
	case ecDataPort:
		f.busy = 2
		switch f.state {
		case 1:
			f.out = append(f.out, f.regs[v])
			f.state = 0
		case 2:
			f.addr, f.state = v, 3
		case 3:
			if f.addr == ecRegWdtEnable && v != 0 {
				panic("test: the code under test armed the EC watchdog")
			}
			if !f.stuck[f.addr] {
				f.regs[f.addr] = v
			}
			f.writes = append(f.writes, f.addr)
			f.state = 0
		}
	}
	return nil
}

func (f *fakeEC) Close() error { return nil }

func TestECDetectAndRead(t *testing.T) {
	f := newFakeEC()
	ec, err := detectFans(f, "DXP4800 GT")
	if err != nil {
		t.Fatal(err)
	}
	if ec.Name() != "IT5571" || ec.MaxDuty() != 198 {
		t.Fatalf("ec = %s max %d", ec.Name(), ec.MaxDuty())
	}

	want := map[string][3]int{"fan1": {977, 19, 1}, "fan3": {731, 41, 1}}
	if len(ec.Outputs()) != 2 {
		t.Fatalf("outputs = %+v", ec.Outputs())
	}
	for _, ch := range ec.Outputs() {
		rpm, err := ec.RPM(ch)
		duty, auto, _ := ec.PWM(ch)
		w := want[ch.ID]
		if err != nil || rpm != w[0] || dutyToPercent(duty, ec.MaxDuty()) != w[1] || !auto {
			t.Errorf("%s rpm %d pct %d auto %v (%v)", ch.ID, rpm, dutyToPercent(duty, ec.MaxDuty()), auto, err)
		}
	}

	temps := ec.Temps()
	if len(temps) != 1 || temps[0] == nil || *temps[0] != 57 {
		t.Errorf("temps = %v", temps)
	}
	bt, ok := Fans(ec).(BoardTemper)
	if !ok || bt.BoardTemp() == nil || *bt.BoardTemp() != 41 {
		t.Errorf("board temp not read from the controller")
	}
}

func TestECDetectRejectsOthers(t *testing.T) {
	f := newFakeEC()
	f.absent = true
	if _, err := detectFans(f, "DXP4800 GT"); err == nil {
		t.Fatal("expected an error")
	}
	g := newFakeEC()
	g.regs[0xb0] = 0x7f // not a mode value
	if _, err := DetectEC(g); err == nil {
		t.Fatal("expected an error for an odd answer")
	}
}

func TestECTimeout(t *testing.T) {
	old := ecWait
	defer func() { ecWait = old }()
	ecWait = 0
	f := newFakeEC()
	ec := &EC{ports: f}
	f.busy = 1 << 30
	if _, err := ec.read(0xb0); err == nil {
		t.Fatal("expected a timeout")
	}
}

func TestECSetPWMAndRestore(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	snap, _ := ec.Snapshot()
	if snap["fan3"] != [2]byte{0, 82} {
		t.Fatalf("snapshot = %v", snap)
	}

	ch := ecChannels[1]
	if err := ec.SetPWM(ch, percentToDuty(50, ec.MaxDuty())); err != nil {
		t.Fatal(err)
	}
	if f.regs[0xb5] != 99 || f.regs[0xb4] != 1 || f.regs[0xb0] != 0 {
		t.Fatalf("duty %d mode %d, cpu mode %d", f.regs[0xb5], f.regs[0xb4], f.regs[0xb0])
	}
	_ = ec.SetPWM(ch, 250) // capped at 0xc6
	if f.regs[0xb5] != 0xc6 {
		t.Fatalf("duty %#x", f.regs[0xb5])
	}

	// Even a snapshot taken in manual mode must give the fans back to the EC.
	f.regs[0xb0] = 1
	snap["fan1"] = [2]byte{1, 10}
	if err := ec.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if f.regs[0xb0] != 0 || f.regs[0xb4] != 0 {
		t.Fatalf("modes after restore: %d %d", f.regs[0xb0], f.regs[0xb4])
	}
}

func TestECPercentDuty(t *testing.T) {
	for pct := 0; pct <= 100; pct++ {
		if got := dutyToPercent(percentToDuty(pct, ecMaxDuty), ecMaxDuty); got != pct {
			t.Errorf("%d%% -> %d%%", pct, got)
		}
	}
	if percentToDuty(100, ecMaxDuty) != 0xc6 {
		t.Error("100 % must be 0xc6")
	}
}

func TestReadSensorsK10temp(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		full := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}
	write("hwmon/hwmon3/name", "k10temp\n")
	write("hwmon/hwmon3/temp1_input", "56750\n")
	write("hwmon/hwmon3/temp1_label", "Tctl\n")

	old, oldINI := hwmonDir, disksINI
	defer func() { hwmonDir, disksINI = old, oldINI }()
	hwmonDir, disksINI = filepath.Join(dir, "hwmon"), filepath.Join(dir, "none.ini")
	if s := ReadSensors(); s.CPU == nil || *s.CPU != 57 {
		t.Fatalf("cpu = %v", s.CPU)
	}
}

func TestFanStatusAutoDuty(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	ch := ecChannels[0]

	// Auto mode: the duty register is stale, so no percentage.
	if fs := readFanStatus(ec, ch); !fs.Auto || fs.Pct != nil || fs.RPM != 977 {
		t.Fatalf("auto: %+v", fs)
	}
	b, _ := json.Marshal(readFanStatus(ec, ch))
	if !strings.Contains(string(b), `"pct":null`) {
		t.Fatalf("json = %s", b)
	}

	// Manual mode: the duty register is the output.
	_ = ec.SetPWM(ch, percentToDuty(50, ec.MaxDuty()))
	if fs := readFanStatus(ec, ch); fs.Auto || fs.Pct == nil || *fs.Pct != 50 {
		t.Fatalf("manual: %+v", fs)
	}
}

// A dead controller costs one full timeout, not ecTries x ecWait per register: after it every call of the same
// tick fails at once.
func TestECDeadFailsFast(t *testing.T) {
	oldWait, oldDead := ecWait, ecDeadFor
	defer func() { ecWait, ecDeadFor = oldWait, oldDead }()
	ecWait, ecDeadFor = 20*time.Millisecond, 300*time.Millisecond
	f := newFakeEC()
	ec := &EC{ports: f}
	f.busy = 1 << 30
	if _, err := ec.read(0xb0); !errors.Is(err, errECTimeout) {
		t.Fatalf("first read: %v", err)
	}
	start := time.Now()
	for range 5 {
		if _, err := ec.read(0xb1); !errors.Is(err, errECUnresponsive) {
			t.Fatalf("read while dead: %v", err)
		}
		if err := ec.write(0xb1, 1); !errors.Is(err, errECUnresponsive) {
			t.Fatalf("write while dead: %v", err)
		}
	}
	if d := time.Since(start); d > 10*time.Millisecond {
		t.Errorf("dead calls took %v", d)
	}
	// Once the controller answers again and the window has passed, it is used as before.
	f.busy = 0
	time.Sleep(350 * time.Millisecond)
	if _, err := ec.read(0xb1); err != nil {
		t.Errorf("recovered read: %v", err)
	}
}

// A hand-back right after a timeout gets the full retry budget: the dead window never blocks Restore or the disarm.
func TestECRestoreIgnoresDeadWindow(t *testing.T) {
	oldWait, oldDead := ecWait, ecDeadFor
	defer func() { ecWait, ecDeadFor = oldWait, oldDead }()
	ecWait, ecDeadFor = 20*time.Millisecond, time.Hour
	f := newFakeEC()
	ec := &EC{ports: f}
	f.busy = 1 << 30
	if _, err := ec.read(0xb0); !errors.Is(err, errECTimeout) {
		t.Fatalf("first read: %v", err)
	}
	f.busy = 0 // the controller answers again, the window (an hour) is still open
	if err := ec.Restore(nil); err != nil {
		t.Fatalf("restore inside the dead window: %v", err)
	}
	f.busy = 1 << 30
	_, _ = ec.read(0xb0)
	f.busy = 0
	if err := ec.DisarmWatchdog(); errors.Is(err, errECUnresponsive) {
		t.Fatalf("disarm inside the dead window: %v", err)
	}
}

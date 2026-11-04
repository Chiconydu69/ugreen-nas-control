package fan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingFans struct {
	Fans
	restoreFails int
	restoreCalls int
	pwmErr       error
}

func (f *failingFans) Restore(s Snapshot) error {
	f.restoreCalls++
	if f.restoreCalls <= f.restoreFails {
		return errors.New("ec busy")
	}
	return f.Fans.Restore(s)
}

func (f *failingFans) PWM(ch Channel) (byte, bool, error) {
	if f.pwmErr != nil {
		return 0, false, f.pwmErr
	}
	return f.Fans.PWM(ch)
}

func TestRestoreRetry(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	f.regs[0xb0], f.regs[0xb4] = 1, 1
	var logs []string
	logf := func(format string, _ ...any) { logs = append(logs, format) }

	chip := &failingFans{Fans: ec, restoreFails: 2}
	if err := restoreRetry(chip, nil, 3, time.Millisecond, logf); err != nil {
		t.Fatal(err)
	}
	if chip.restoreCalls != 3 || len(logs) != 2 || f.regs[0xb0] != 0 || f.regs[0xb4] != 0 {
		t.Fatalf("calls %d logs %d modes %d %d", chip.restoreCalls, len(logs), f.regs[0xb0], f.regs[0xb4])
	}

	chip = &failingFans{Fans: ec, restoreFails: 99}
	if err := restoreRetry(chip, nil, 3, time.Millisecond, logf); err == nil || chip.restoreCalls != 3 {
		t.Fatalf("err %v calls %d", err, chip.restoreCalls)
	}
}

func TestFanStatusReadError(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	chip := &failingFans{Fans: ec, pwmErr: errors.New("timeout")}
	fs := readFanStatus(chip, ecChannels[0])
	if fs.Pct != nil || fs.Auto || fs.Err == "" {
		t.Fatalf("%+v", fs)
	}
}

func TestDetectECRefusesBoundKernelDriver(t *testing.T) {
	old := acpiDevicesDir
	defer func() { acpiDevicesDir = old }()
	acpiDevicesDir = t.TempDir()

	// No PNP0C09 device, or one without a driver: fine.
	if _, err := DetectEC(newFakeEC()); err != nil {
		t.Fatal(err)
	}
	dev := filepath.Join(acpiDevicesDir, "PNP0C09:00")
	os.MkdirAll(dev, 0o755)
	if _, err := DetectEC(newFakeEC()); err != nil {
		t.Fatal(err)
	}

	os.Symlink("../../../../bus/acpi/drivers/ec", filepath.Join(dev, "driver"))
	_, err := DetectEC(newFakeEC())
	if err == nil || !strings.Contains(err.Error(), "ACPI embedded controller driver") {
		t.Fatalf("err = %v", err)
	}
}

func TestECSetPWMWritesDutyBeforeMode(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	f.writes = nil
	if err := ec.SetPWM(ecChannels[0], 100); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 2 || f.writes[0] != 0xb1 || f.writes[1] != 0xb0 {
		t.Fatalf("writes = %x", f.writes)
	}
	// Already manual: only the duty.
	f.writes = nil
	_ = ec.SetPWM(ecChannels[0], 120)
	if len(f.writes) != 1 || f.writes[0] != 0xb1 {
		t.Fatalf("writes = %x", f.writes)
	}
}

func TestK10tempTdieUnreadable(t *testing.T) {
	read := func(files map[string]string) *int {
		dir := t.TempDir()
		files["name"] = "k10temp\n"
		for name, content := range files {
			full := filepath.Join(dir, "hwmon", "hwmon3", name)
			os.MkdirAll(filepath.Dir(full), 0o755)
			os.WriteFile(full, []byte(content), 0o644)
		}
		old, oldINI := hwmonDir, disksINI
		defer func() { hwmonDir, disksINI = old, oldINI }()
		hwmonDir, disksINI = filepath.Join(dir, "hwmon"), filepath.Join(dir, "none.ini")
		return ReadSensors().CPU
	}

	// Tdie comes after Tctl and cannot be read: Tctl stays.
	got := read(map[string]string{"temp1_label": "Tctl\n", "temp1_input": "70000\n", "temp2_label": "Tdie\n", "temp2_input": "garbage\n"})
	if got == nil || *got != 70 {
		t.Fatalf("cpu = %v", got)
	}
	// Both valid: Tdie wins, in either order.
	got = read(map[string]string{"temp1_label": "Tctl\n", "temp1_input": "80000\n", "temp2_label": "Tdie\n", "temp2_input": "60000\n"})
	if got == nil || *got != 60 {
		t.Fatalf("cpu = %v", got)
	}
	got = read(map[string]string{"temp1_label": "Tdie\n", "temp1_input": "60000\n", "temp2_label": "Tctl\n", "temp2_input": "80000\n"})
	if got == nil || *got != 60 {
		t.Fatalf("cpu = %v", got)
	}
}

func TestReleaseRetriesAfterFailure(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	chip := &failingFans{Fans: ec, restoreFails: 1}
	current := map[string]int{"fan1": 50}
	if err := release(chip, nil, current); err == nil || len(current) != 1 {
		t.Fatalf("err %v current %v", err, current)
	}
	if err := release(chip, nil, current); err != nil || len(current) != 0 {
		t.Fatalf("err %v current %v", err, current)
	}
	if err := release(chip, nil, current); err != nil || chip.restoreCalls != 2 {
		t.Fatalf("calls %d", chip.restoreCalls)
	}
}

type closeSpy struct {
	Fans
	closed int
}

func (c *closeSpy) Close() error { c.closed++; return nil }

func TestAddChipStatusClosesWhatItOpened(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	spy := &closeSpy{Fans: ec}
	out := map[string]any{}
	addChipStatus(out, &Sensors{}, func() (Fans, error) { return spy, nil })
	if spy.closed != 1 {
		t.Fatalf("closed %d times, want 1", spy.closed)
	}
}

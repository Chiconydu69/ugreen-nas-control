//go:build linux

package chip

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePower models runtime PM of the bound function: power/control auto suspends the controller (intel_lpss_suspend),
// on resumes it (intel_lpss_resume). The returned log has every write.
func fakePower(t *testing.T, f *fakeDW) *[]string {
	t.Helper()
	var log []string
	lpssSysWrite = func(path, value string) error {
		log = append(log, filepath.Base(path)+"="+value)
		if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
			return err
		}
		if filepath.Base(path) != "control" {
			return nil
		}
		status := "active"
		if value == "auto" {
			f.suspend()
			status = "suspended"
		} else if sysLine(filepath.Join(filepath.Dir(path), "runtime_status")) != "active" {
			f.resume()
		}
		return os.WriteFile(filepath.Join(filepath.Dir(path), "runtime_status"), []byte(status+"\n"), 0o644)
	}
	return &log
}

func suspendedBound() sysOpts {
	o := bound()
	o.pmStatus = "suspended"
	return o
}

// chipIDRead is the transfer of the LED chip id.
func chipIDRead(d *dwI2C) (int, error) {
	word := make([]byte, 2)
	err := d.xfer([]dwMsg{{buf: []byte{lpssChipReg}}, {read: true, buf: word}})
	return int(word[0]) | int(word[1])<<8, err
}

func openFake(t *testing.T) (*dwI2C, *lpssReport, error) {
	t.Helper()
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, true)
	return d, r, err
}

// Field report #214: "get" works, the next "set" (a new process, so a new open) read chip id 0x0000. After the
// previous command released power/control the function runtime suspended (RESETS 0); the next open resumes it and the
// first transfer on the cold controller returns zeros. One retry after a fresh bring-up gets the real id.
func TestLPSSSecondProcessAfterSuspend(t *testing.T) {
	f := newFakeDW()
	f.id = []byte{0xb2, 0xc5}
	f.settle = 1
	fakeSys(t, suspendedBound())
	pm := fakePower(t, f)
	fakeMem(t, f)

	for i, name := range []string{"get", "set"} {
		d, r, err := openFake(t)
		if err != nil {
			t.Fatalf("%s (process %d): %v\n%s", name, i+1, err, strings.Join(r.lines, "\n"))
		}
		if got := pmControlFile(t); got != "on" {
			t.Errorf("%s: power/control %q while open", name, got)
		}
		if id, err := chipIDRead(d); err != nil || id != lpssChipID {
			t.Errorf("%s: transfer after open: 0x%04x, %v", name, id, err)
		}
		d.close()
		if got := pmControlFile(t); got != "auto" {
			t.Errorf("%s: power/control %q after close", name, got)
		}
	}
	if got := strings.Join(*pm, ","); got != "control=on,control=auto,control=on,control=auto" {
		t.Errorf("power/control writes %s", got)
	}
}

// The controller suspends behind our back inside one handle (get, then set in one process): the next transfer
// notices the lost setup, wakes it and repeats the bring-up instead of running on a controller at reset values.
func TestLPSSContextLostInsideHandle(t *testing.T) {
	f := newFakeDW()
	f.id = []byte{0xb2, 0xc5}
	fakeSys(t, suspendedBound())
	fakePower(t, f)
	fakeMem(t, f)
	d, r, err := openFake(t)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	defer d.close()

	if id, err := chipIDRead(d); err != nil || id != lpssChipID { // "get"
		t.Fatalf("get: 0x%04x, %v", id, err)
	}
	f.suspend() // runtime suspend although power/control is on
	if err := os.WriteFile(filepath.Join(lpssSys, "bus/pci/devices/0000:00:15.0/power/runtime_status"), []byte("suspended\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if id, err := chipIDRead(d); err != nil || id != lpssChipID { // "set"
		t.Fatalf("after the context loss: 0x%04x, %v\n%s", id, err, strings.Join(r.lines, "\n"))
	}

	// Same loss with the status still "active" (the function was reset): the register check catches it.
	f.suspend()
	if id, err := chipIDRead(d); err != nil || id != lpssChipID {
		t.Fatalf("after a reset: 0x%04x, %v", id, err)
	}
	if !strings.Contains(strings.Join(r.lines, "\n"), "controller lost its setup") {
		t.Errorf("no verify line:\n%s", strings.Join(r.lines, "\n"))
	}
}

// One handle holds power/control "on" over all its transactions and restores it once on close.
func TestLPSSHoldCoversEveryTransaction(t *testing.T) {
	f := newFakeDW()
	f.id = []byte{0xb2, 0xc5}
	fakeSys(t, suspendedBound())
	pm := fakePower(t, f)
	fakeMem(t, f)
	d, r, err := openFake(t)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	for i := 0; i < 4; i++ {
		if _, err := chipIDRead(d); err != nil {
			t.Fatal(err)
		}
		if got := pmControlFile(t); got != "on" {
			t.Fatalf("transaction %d: power/control %q", i, got)
		}
	}
	if len(*pm) != 1 {
		t.Errorf("writes during the handle: %v", *pm)
	}
	d.close()
	if got := strings.Join(*pm, ","); got != "control=on,control=auto" {
		t.Errorf("writes %s", got)
	}
	if _, err := os.Stat(lpssPMState); err == nil {
		t.Error("state file left behind")
	}
}

// 0x0000 is retried once after a fresh bring-up, then reported as "no data", not as a different chip.
func TestLPSSZeroChipIDRetry(t *testing.T) {
	f := newFakeDW()
	f.id = []byte{0xb2, 0xc5}
	f.settle = 1
	fakeSys(t, suspendedBound())
	fakePower(t, f)
	fakeMem(t, f)
	d, r, err := openFake(t)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	d.close()
	if f.xfers != 2 {
		t.Errorf("%d chip id transfers, want 2", f.xfers)
	}
	if !strings.Contains(strings.Join(r.lines, "\n"), "repeating the whole bring-up once") {
		t.Errorf("no retry line:\n%s", strings.Join(r.lines, "\n"))
	}

	f = newFakeDW()
	f.id = []byte{0, 0}
	fakeSys(t, suspendedBound())
	pm := fakePower(t, f)
	fakeMem(t, f)
	_, _, err = openFake(t)
	if err == nil || !strings.Contains(err.Error(), "0x0000 twice") || !strings.Contains(err.Error(), "--debug") || strings.Contains(err.Error(), "is not 0x") {
		t.Fatalf("want the no-data error, got %v", err)
	}
	if f.xfers != 2 {
		t.Errorf("%d chip id transfers, want exactly 2", f.xfers)
	}
	if f.enable || strings.Join(*pm, ",") != "control=on,control=auto" {
		t.Errorf("enable %v, power writes %v", f.enable, *pm)
	}

	// A real other id is no retry case.
	f = newFakeDW()
	f.id = []byte{0x34, 0x12}
	fakeSys(t, suspendedBound())
	fakePower(t, f)
	fakeMem(t, f)
	if _, _, err = openFake(t); err == nil || !strings.Contains(err.Error(), "0x1234") || f.xfers != 1 {
		t.Errorf("other id: %v after %d transfers", err, f.xfers)
	}
}

// --debug: every step reaches the sink.
func TestLPSSDebugTrace(t *testing.T) {
	f := newFakeDW()
	f.id = []byte{0xb2, 0xc5}
	fakeSys(t, suspendedBound())
	fakePower(t, f)
	fakeMem(t, f)
	var buf bytes.Buffer
	setLPSSDebug(true, &buf)
	t.Cleanup(func() { setLPSSDebug(false, nil) })
	d, r, err := openFake(t)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	d.close()
	for _, want := range []string{"write.power:", "pm.runtime_status:", "write.timing:", "xfer 0x3a w[5a] r[b2 c5]", "restored to auto"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("trace lacks %q:\n%s", want, buf.String())
		}
	}
}

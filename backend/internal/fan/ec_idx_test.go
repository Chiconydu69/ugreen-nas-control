package fan

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"
)

func newFakeIdxEC() *fakeEC {
	f := newFakeEC()
	f.regs[0x36], f.regs[0x37] = 0x02, 0x58 // cpu2 600
	f.regs[0x3a], f.regs[0x3b] = 0x03, 0x20 // sys2 800
	f.regs[0xb3], f.regs[0xb7] = 30, 40
	return f
}

func TestIdxProFourFans(t *testing.T) {
	isolate(t, "iDX6011 Pro")
	f := newFakeIdxEC()
	chip, err := detectFans(f, "iDX6011 Pro")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"fan1": 977, "fan2": 600, "fan3": 731, "fan4": 800}
	if len(chip.Outputs()) != 4 {
		t.Fatalf("outputs = %v", outputIDs(chip.Outputs()))
	}
	for _, ch := range chip.Outputs() {
		if rpm, err := chip.RPM(ch); err != nil || rpm != want[ch.ID] {
			t.Errorf("%s = %d (%v), want %d", ch.ID, rpm, err, want[ch.ID])
		}
	}
	for id, role := range map[string]string{"fan1": "cpu", "fan2": "cpu", "fan3": "case", "fan4": "case"} {
		if roles[id] != role {
			t.Errorf("role of %s = %s", id, roles[id])
		}
	}

	// The GT keeps its two fans.
	gt, _ := detectFans(newFakeEC(), "DXP4800 GT")
	if len(gt.Outputs()) != 2 {
		t.Fatalf("GT outputs = %v", outputIDs(gt.Outputs()))
	}
}

func TestIdxProNeverBelowFloor(t *testing.T) {
	f := newFakeIdxEC()
	ec, _ := detectEC(f, ecSpecIdxPro)
	floor := byte((25*198 + 99) / 100) // 25 % of 198, rounded up
	for _, ch := range ec.Outputs() {
		for _, duty := range []byte{0, 1, floor - 1} {
			if err := ec.SetPWM(ch, duty); err != nil {
				t.Fatal(err)
			}
			if got := f.regs[ch.Duty]; got != floor {
				t.Errorf("%s: asked %d, wrote %d, want %d", ch.ID, duty, got, floor)
			}
		}
		_ = ec.SetPWM(ch, 255)
		if f.regs[ch.Duty] != ecMaxDuty {
			t.Errorf("%s: top duty %d", ch.ID, f.regs[ch.Duty])
		}
	}
	// The GT has no floor.
	g := newFakeEC()
	gt, _ := DetectEC(g)
	_ = gt.SetPWM(ecChannels[0], 0)
	if g.regs[0xb1] != 0 {
		t.Errorf("GT duty %d", g.regs[0xb1])
	}
}

func TestIdxProRestoreHandsBackAndDisarms(t *testing.T) {
	f := newFakeIdxEC()
	ec, _ := detectEC(f, ecSpecIdxPro)
	ec.fed = true // the petter fed the BIOS's timer
	for _, ch := range ec.Outputs() {
		_ = ec.SetPWM(ch, 100)
	}
	f.regs[ecRegWdtEnable] = 1 // the BIOS armed the watchdog
	if err := ec.Restore(nil); err != nil {
		t.Fatal(err)
	}
	for _, reg := range []byte{0xb0, 0xb2, 0xb4, 0xb6} {
		if f.regs[reg] != ecModeAuto {
			t.Errorf("register %#x = %d, want auto", reg, f.regs[reg])
		}
	}
	if f.regs[ecRegWdtEnable] != 0 {
		t.Error("watchdog still armed after the stop")
	}
}

func TestIdxProLimits(t *testing.T) {
	c := ApplyModelLimits(defaultConfig(), "iDX6011 Pro")
	if c.CPUCrit != 88 || c.DiskCrit != 55 {
		t.Fatalf("cpu %d disk %d", c.CPUCrit, c.DiskCrit)
	}
	loose := defaultConfig()
	loose.CPUCrit, loose.DiskCrit = 100, 70
	if c := ApplyModelLimits(loose, "iDX6011 Pro"); c.CPUCrit != 88 || c.DiskCrit != 60 {
		t.Fatalf("cpu %d disk %d", c.CPUCrit, c.DiskCrit)
	}
	tight := defaultConfig()
	tight.CPUCrit = 75
	if c := ApplyModelLimits(tight, "iDX6011 Pro"); c.CPUCrit != 75 {
		t.Fatal("a stricter user limit was raised")
	}
	if c := ApplyModelLimits(defaultConfig(), "DXP6800 Pro"); c.CPUCrit != 90 {
		t.Fatal("limits applied to another model")
	}
}

func TestWatchdogNeverArms(t *testing.T) {
	f := newFakeEC()
	ec, _ := DetectEC(f)
	if err := ec.write(ecRegWdtEnable, 1); err == nil || len(f.writes) != 0 {
		t.Fatalf("arming was not refused (err %v, writes %v)", err, f.writes)
	}
	if err := ec.DisarmWatchdog(); err != nil || f.regs[ecRegWdtEnable] != 0 {
		t.Fatalf("disarm: %v", err)
	}
}

func TestWatchdogDisarmedWritesNothing(t *testing.T) {
	isolate(t, "iDX6011 Pro")
	f := newFakeIdxEC()
	ec, _ := detectEC(f, ecSpecIdxPro)
	p := &Petter{}
	now := time.Unix(1_000_000, 0)
	for i := 0; i < 5; i++ {
		st, ok, err := p.Tick(ec, now.Add(time.Duration(i)*time.Minute))
		if err != nil || !ok || st.Armed || st.LastPet != 0 {
			t.Fatalf("tick %d: %+v ok %v err %v", i, st, ok, err)
		}
	}
	if slices.Contains(f.writes, ecRegWdtHi) || slices.Contains(f.writes, ecRegWdtLo) || slices.Contains(f.writes, ecRegWdtEnable) {
		t.Fatalf("a disarmed watchdog was written: %v", f.writes)
	}
}

func TestWatchdogPetCadence(t *testing.T) {
	isolate(t, "DXP4800 GT")
	f := newFakeEC()
	f.regs[ecRegWdtEnable] = 1
	ec, _ := DetectEC(f)
	p := &Petter{}
	t0 := time.Unix(1_000_000, 0)

	pets := func() int {
		n := 0
		for _, r := range f.writes {
			if r == ecRegWdtLo {
				n++
			}
		}
		return n
	}

	// Timeout 300 s: timeout/3 is 100 s, capped at 60 s.
	st, _, err := p.Tick(ec, t0)
	if err != nil || !st.Armed || st.Timeout != 300 || st.Interval != 60 || st.LastPet != t0.Unix() {
		t.Fatalf("first tick: %+v (%v)", st, err)
	}
	if f.regs[ecRegWdtHi] != 0x01 || f.regs[ecRegWdtLo] != 0x2c {
		t.Fatalf("timeout registers %#x %#x, want 300", f.regs[ecRegWdtHi], f.regs[ecRegWdtLo])
	}
	for _, s := range []int{2, 30, 59} {
		p.Tick(ec, t0.Add(time.Duration(s)*time.Second))
	}
	if pets() != 1 {
		t.Fatalf("%d pets inside the first minute", pets())
	}
	p.Tick(ec, t0.Add(60*time.Second))
	if pets() != 2 {
		t.Fatalf("%d pets after a minute", pets())
	}
	if f.regs[ecRegWdtEnable] != 1 {
		t.Error("the enable byte changed")
	}

	// The state file carries it to `status`.
	st2 := ReadWatchdogStatus()
	if st2 == nil || !st2.Armed || st2.LastPet != t0.Add(60*time.Second).Unix() {
		t.Fatalf("state = %+v", st2)
	}
	b, _ := json.Marshal(st2)
	if !json.Valid(b) {
		t.Fatal("not JSON")
	}

	// A short timeout is fed at a third of it.
	q := &Petter{Timeout: 30}
	f.writes = nil
	q.Tick(ec, t0)
	q.Tick(ec, t0.Add(9*time.Second))
	q.Tick(ec, t0.Add(10*time.Second))
	if pets() != 2 {
		t.Fatalf("%d pets with a 30 s timeout", pets())
	}
}

func TestWatchdogOtherChipsAreSkipped(t *testing.T) {
	isolate(t, "DX4600 Pro") // an IT8613E chip on a model that has no petter
	chip, _ := Detect(newFakeITE())
	if _, ok, err := (&Petter{}).Tick(chip, time.Now()); ok || err != nil {
		t.Fatalf("ok %v err %v", ok, err)
	}
	if _, err := os.Stat(watchdogStatePath); err == nil {
		t.Fatal("a state file for a chip without an EC")
	}
}

func TestBacklight(t *testing.T) {
	f := newFakeIdxEC()
	ec, _ := detectEC(f, ecSpecIdxPro)
	f.regs[ecRegBacklight] = 1
	if pct, raw, err := ec.BacklightPct(); err != nil || pct != 100 || raw != 1 {
		t.Fatalf("full: %d %d %v", pct, raw, err)
	}
	f.regs[ecRegBacklight] = 198
	if pct, _, err := ec.BacklightPct(); err != nil || pct != 0 {
		t.Fatalf("off: %d %v", pct, err)
	}
	f.regs[ecRegBacklight] = 0 // the vendor default is no error
	if pct, raw, err := ec.BacklightPct(); err != nil || pct != BacklightUnknownPct || raw != 0 {
		t.Fatalf("default: %d %d %v", pct, raw, err)
	}
	for _, bad := range []byte{199, 255} {
		f.regs[ecRegBacklight] = bad
		if _, _, err := ec.BacklightPct(); err == nil {
			t.Errorf("register %d accepted", bad)
		}
	}
	for pct := 0; pct <= 100; pct++ {
		if err := ec.SetBacklight(pct); err != nil {
			t.Fatal(err)
		}
		if raw := f.regs[ecRegBacklight]; raw < 1 || raw > 198 {
			t.Fatalf("%d %% wrote %d", pct, raw)
		}
		if got, _, _ := ec.BacklightPct(); got != pct {
			t.Errorf("%d %% read back as %d %%", pct, got)
		}
	}
	if f.regs[ecRegBacklight] != 1 {
		t.Errorf("100 %% = %d", f.regs[ecRegBacklight])
	}
	_ = ec.SetBacklight(0)
	if f.regs[ecRegBacklight] != 198 {
		t.Errorf("0 %% = %d", f.regs[ecRegBacklight])
	}
	for _, bad := range []int{-1, 101} {
		if ec.SetBacklight(bad) == nil {
			t.Errorf("%d accepted", bad)
		}
	}
	// Nothing but the backlight register was written, apart from the register itself.
	for _, r := range f.writes {
		if r != ecRegBacklight {
			t.Errorf("wrote register %#x", r)
		}
	}
}

// UGREEN reports fan3's value for a fan4 that reads 0 (ug_it55pro_functions.c:290-291@c75c5ab); a fan4 with its own
// reading, and any other channel, stays as read.
func TestIdxProFan4MirrorsFan3(t *testing.T) {
	f := newFakeIdxEC()
	ec, _ := detectEC(f, ecSpecIdxPro)
	fan3, fan4 := ecChannelsIdxPro[2], ecChannelsIdxPro[3]

	f.regs[0x3a], f.regs[0x3b] = 0, 0
	if rpm, err := ec.RPM(fan4); err != nil || rpm != 731 {
		t.Fatalf("fan4 with no tach = %d (%v), want fan3's 731", rpm, err)
	}
	f.regs[0x3a], f.regs[0x3b] = 0x03, 0x20
	if rpm, _ := ec.RPM(fan4); rpm != 800 {
		t.Fatalf("fan4 with its own tach = %d, want 800", rpm)
	}
	f.regs[0x3a], f.regs[0x3b], f.regs[0x38], f.regs[0x39] = 0, 0, 0, 0
	if rpm, _ := ec.RPM(fan4); rpm != 0 {
		t.Fatalf("both stopped: fan4 = %d", rpm)
	}
	f.regs[0x36], f.regs[0x37] = 0, 0
	f.regs[0x38], f.regs[0x39] = 0x02, 0xdb
	if rpm, _ := ec.RPM(ecChannelsIdxPro[1]); rpm != 0 {
		t.Fatalf("fan2 mirrored fan3: %d", rpm)
	}
	if rpm, _ := ec.RPM(fan3); rpm != 731 {
		t.Fatalf("fan3 = %d", rpm)
	}
}

// Fan control, the watchdog timeout and (Pro) the backlight are the only registers written. 0x99/0x9a (wake),
// 0x9c (EUP), 0x9e (Pro beeper), 0xa5 (plain beeper) and 0xa6 (G3 wake, GT SATA power) are not on any list that
// reaches them (ug_it55pro_functions.c:49-50,394-398; it55_regs_dump.sh:21-24; ug_sataio_beeper.c:31-35@c75c5ab).
func TestWriteListRefusesForeignRegisters(t *testing.T) {
	foreign := []byte{0x99, 0x9a, 0x9c, 0x9e, 0xa6, 0xa7, 0xa0, 0xa1, 0x00}
	for name, spec := range map[string]ecSpec{"GT": ecSpecGT, "Pro": ecSpecIdxPro, "plain": ecSpecIdx} {
		f := newFakeIdxEC()
		ec, err := detectEC(f, spec)
		if err != nil {
			t.Fatal(err)
		}
		for _, reg := range foreign {
			if err := ec.write(reg, 1); err == nil {
				t.Errorf("%s: register %#x was written", name, reg)
			}
		}
		if name != "Pro" {
			if err := ec.write(ecRegBacklight, 1); err == nil {
				t.Errorf("%s: backlight register written", name)
			}
		}
		if len(f.writes) != 0 {
			t.Errorf("%s: bus writes %v", name, f.writes)
		}
	}
	// What is allowed still goes through.
	f := newFakeIdxEC()
	ec, _ := detectEC(f, ecSpecIdxPro)
	for _, reg := range []byte{0xb0, 0xb1, 0xb6, 0xb7, ecRegWdtHi, ecRegWdtLo, ecRegBacklight, ecRegWdtEnable} {
		if err := ec.write(reg, 0); err != nil {
			t.Errorf("register %#x refused: %v", reg, err)
		}
	}
}

func TestBacklightOnlyOnPro(t *testing.T) {
	for name, spec := range map[string]ecSpec{"GT": ecSpecGT, "plain": ecSpecIdx} {
		f := newFakeIdxEC()
		f.regs[ecRegBacklight] = 80
		ec, _ := detectEC(f, spec)
		if _, _, err := ec.BacklightPct(); err == nil {
			t.Errorf("%s: backlight read", name)
		}
		if err := ec.SetBacklight(50); err == nil || len(f.writes) != 0 {
			t.Errorf("%s: backlight set (%v, writes %v)", name, err, f.writes)
		}
		if f.regs[ecRegBacklight] != 80 {
			t.Errorf("%s: register changed", name)
		}
	}
}

// The plain iDX6011/6012 map is read only: tach, duty, mode and the watchdog registers read, nothing is written,
// not even by the stop path.
func TestPlainIdxReadOnly(t *testing.T) {
	isolate(t, "iDX6011")
	f := newFakeEC()
	for r := range f.regs {
		f.regs[r] = 0
	}
	f.regs[0x96], f.regs[0x97] = 0x03, 0x20 // fan1 800
	f.regs[0x98], f.regs[0x99] = 0x02, 0x58 // fan2 600
	f.regs[0x9c], f.regs[0x9d] = 0, 70
	f.regs[0x9e], f.regs[0x9f] = 1, 90
	f.regs[0x55], f.regs[0x56], f.regs[0x57] = 1, 0x01, 0x2c
	ec, err := detectEC(f, ecSpecIdx)
	if err != nil {
		t.Fatal(err)
	}
	if got := outputIDs(ec.Outputs()); !slices.Equal(got, []string{"fan1", "fan2"}) {
		t.Fatalf("outputs = %v", got)
	}
	want := []struct {
		rpm  int
		duty byte
		auto bool
	}{{800, 70, true}, {600, 90, false}}
	for i, ch := range ec.Outputs() {
		rpm, err := ec.RPM(ch)
		duty, auto, err2 := ec.PWM(ch)
		if err != nil || err2 != nil || rpm != want[i].rpm || duty != want[i].duty || auto != want[i].auto {
			t.Errorf("%s: rpm %d duty %d auto %v (%v %v)", ch.ID, rpm, duty, auto, err, err2)
		}
	}
	if armed, err := ec.WatchdogArmed(); err != nil || !armed {
		t.Errorf("watchdog flag: %v %v", armed, err)
	}
	if n, err := ec.WatchdogCounter(); err != nil || n != 300 {
		t.Errorf("watchdog counter: %d %v", n, err)
	}
	if ec.BoardTemp() != nil || ec.Temps()[0] != nil {
		t.Error("temperatures reported for the plain map")
	}

	for _, ch := range ec.Outputs() {
		if err := ec.SetPWM(ch, 100); !errors.Is(err, errReadOnly) {
			t.Errorf("SetPWM: %v", err)
		}
	}
	if err := ec.WatchdogPet(300); !errors.Is(err, errReadOnly) {
		t.Errorf("pet: %v", err)
	}
	if err := ec.write(0x55, 1); !errors.Is(err, errNeverArm) && !errors.Is(err, errReadOnly) {
		t.Errorf("arm: %v", err)
	}
	if err := ec.Restore(nil); err != nil {
		t.Errorf("restore: %v", err)
	}
	if err := ec.SetBacklight(10); err == nil {
		t.Error("backlight set on the plain map")
	}
	if len(f.writes) != 0 {
		t.Fatalf("bus writes on the plain map: %v", f.writes)
	}
	// No petter, whatever the flag says.
	if _, ok, err := (&Petter{}).Tick(ec, time.Unix(1_000_000, 0)); ok || err != nil {
		t.Fatalf("petter ran on the plain map: ok %v err %v", ok, err)
	}
	if len(f.writes) != 0 {
		t.Fatalf("petter wrote: %v", f.writes)
	}
}

// UGREEN checks the model name only; another chip id on a plain unit is reported, not refused.
func TestPlainIdxChipIDIsAWarning(t *testing.T) {
	f := newFakeEC()
	f.chipID = 0x8613
	ec, err := detectEC(f, ecSpecIdx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ec.Details()["warning"]; !ok {
		t.Error("no warning in the details")
	}
	if _, err := detectEC(f, ecSpecIdxPro); err == nil {
		t.Error("the Pro map accepted a foreign chip id")
	}
}

// The watchdog status carries the counter read back from the timeout registers (read only).
func TestWatchdogStatusCounter(t *testing.T) {
	isolate(t, "DXP4800 GT")
	f := newFakeEC()
	ec, _ := DetectEC(f)
	p := &Petter{}
	t0 := time.Unix(1_000_000, 0)
	if st, _, _ := p.Tick(ec, t0); st.Counter != nil {
		t.Fatalf("counter while disarmed: %d", *st.Counter)
	}
	f.regs[ecRegWdtEnable] = 1
	f.regs[ecRegWdtHi], f.regs[ecRegWdtLo] = 0x02, 0x58 // 600, as armed by someone else
	st, _, err := p.Tick(ec, t0)
	if err != nil || st.Counter == nil || *st.Counter != 600 {
		t.Fatalf("counter = %v (%v), want 600 read before the feed", st.Counter, err)
	}
	if st, _, _ := p.Tick(ec, t0.Add(time.Second)); st.Counter == nil || *st.Counter != 300 {
		t.Fatalf("after the feed: %v", st.Counter)
	}
}

// Restore clears the enable byte only of a timer this process fed; the `restore` command and a daemon that never saw
// an armed timer leave a timer somebody else armed alone.
func TestECRestoreLeavesAForeignWatchdogArmed(t *testing.T) {
	f := newFakeEC()
	f.regs[ecRegWdtEnable] = 1
	ec, _ := DetectEC(f)
	if err := ec.Restore(nil); err != nil {
		t.Fatal(err)
	}
	if f.regs[ecRegWdtEnable] != 1 || slices.Contains(f.writes, ecRegWdtEnable) {
		t.Fatalf("enable byte %d, writes %v", f.regs[ecRegWdtEnable], f.writes)
	}
}

func TestECRestoreDisarmsWhatItFed(t *testing.T) {
	isolate(t, "DXP4800 GT")
	f := newFakeEC()
	f.regs[ecRegWdtEnable] = 1
	ec, _ := DetectEC(f)
	if _, _, err := (&Petter{}).Tick(ec, time.Unix(1_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ec.Restore(nil); err != nil {
		t.Fatal(err)
	}
	if f.regs[ecRegWdtEnable] != 0 {
		t.Fatal("the timer this process fed stays armed after the stop")
	}
}

func TestWatchdogFeedNotEffective(t *testing.T) {
	isolate(t, "DXP4800 GT")
	f := newFakeEC()
	f.regs[ecRegWdtEnable] = 1
	f.regs[ecRegWdtHi], f.regs[ecRegWdtLo] = 0x02, 0x58 // 600, the feed does not change it
	f.stuck = map[byte]bool{ecRegWdtHi: true, ecRegWdtLo: true}
	ec, _ := DetectEC(f)
	p := &Petter{}
	t0 := time.Unix(1_000_000, 0)
	st, _, err := p.Tick(ec, t0)
	if err != nil || st.State != WdtNotEffective || !st.Armed || st.LastPet != t0.Unix() {
		t.Fatalf("status %+v (%v)", st, err)
	}
	if got := ReadWatchdogStatus(); got == nil || got.State != WdtNotEffective {
		t.Fatalf("persisted %+v", got)
	}
	// Disarmed: the state clears.
	f.regs[ecRegWdtEnable] = 0
	if st, _, _ := p.Tick(ec, t0.Add(time.Second)); st.State != WdtOff {
		t.Fatalf("after disarm %+v", st)
	}

	// A feed that sticks is armed, not "not effective".
	g := newFakeEC()
	g.regs[ecRegWdtEnable] = 1
	g.regs[ecRegWdtHi], g.regs[ecRegWdtLo] = 0x02, 0x58
	ec2, _ := DetectEC(g)
	if st, _, _ := (&Petter{}).Tick(ec2, t0); st.State != WdtArmed {
		t.Fatalf("effective feed: %+v", st)
	}
}

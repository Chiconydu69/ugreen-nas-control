package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cs "github.com/ugreen-nas-control/ugreen-nas-control/internal/hw/cs201x"
)

type fakeCS struct {
	idx     byte
	id      uint16
	active  byte
	base    uint16
	ec      map[uint32]byte
	addr    [4]byte
	writes  []uint16        // ports written
	ecData  []byte          // values written to the data port
	cfgData []byte          // values written to the config data port
	ecLog   []uint32        // register addresses written, in order
	stuck   map[uint32]bool // writes to these registers are logged but do not stick
}

func init() {
	csWrotePath = filepath.Join(os.TempDir(), fmt.Sprintf("ugreen-test-cs-wrote-%d", os.Getpid()))
}

func newFakeCS() *fakeCS {
	csCounterGap = 0
	_ = os.Remove(csWrotePath)
	return &fakeCS{id: 0x2011, active: 1, base: 0x200, ec: map[uint32]byte{
		csRegFan0En: 1, csRegFan1En: 1, csRegFan0Duty: 25, csRegFan1Duty: 25,
		csRegFan0Spd: 0x07, csRegFan0Spd + 1: 0x08, // 1800
		csRegFan1Spd: 0x02, csRegFan1Spd + 1: 0x58, // 600
		csRegNTC2: 41, csRegNTC4: 38, csRegFWVer: 0x12,
		csRegADC2Cfg: 1, csRegADC4Cfg: 1, csRegWdtTimeout + 1: 30,
	}}
}

func (f *fakeCS) In(port uint16) (byte, error) {
	switch port {
	case sioData:
		switch f.idx {
		case 0x20:
			return byte(f.id >> 8), nil
		case 0x21:
			return byte(f.id), nil
		case 0x22:
			return 0x01, nil
		case 0x23:
			return 0x02, nil
		case 0x30:
			return f.active, nil
		case 0x60:
			return byte(f.base >> 8), nil
		case 0x61:
			return byte(f.base), nil
		}
		return 0, nil
	case f.base + csEcData:
		a := uint32(f.addr[0]) | uint32(f.addr[1])<<8 | uint32(f.addr[2])<<16 | uint32(f.addr[3])<<24
		return f.ec[a], nil
	}
	return 0xff, nil
}

func (f *fakeCS) Out(port uint16, v byte) error {
	f.writes = append(f.writes, port)
	switch {
	case port == sioIndex:
		f.idx = v
	case port == sioData:
		f.cfgData = append(f.cfgData, v)
		if f.idx == 0x30 {
			f.active = v
		}
	case port >= f.base+csEcAddr && port < f.base+csEcAddr+4:
		f.addr[port-f.base-csEcAddr] = v
	case port == f.base+csEcData:
		f.ecData = append(f.ecData, v)
		a := uint32(f.addr[0]) | uint32(f.addr[1])<<8 | uint32(f.addr[2])<<16 | uint32(f.addr[3])<<24
		if !f.stuck[a] {
			f.ec[a] = v
		}
		f.ecLog = append(f.ecLog, a)
	}
	return nil
}

func (f *fakeCS) Close() error { return nil }

func TestProbeCS201xReads(t *testing.T) {
	f := newFakeCS()
	r, err := ProbeCS201x(f, "DXP2800 GT")
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != "0x0102" || r.ADC == nil || !r.ADC.Configured || r.Watchdog == nil || r.Watchdog.Armed || r.Watchdog.Timeout != 30 {
		t.Fatalf("v2 fields: %+v", r)
	}
	if r.ChipID != "0x2011" || r.Base != "0x0200" || r.HostEC != "active" {
		t.Fatalf("chip: %+v", r)
	}
	if len(r.Fans) != 2 || r.Fans[0].RPM != 1800 || r.Fans[1].RPM != 600 || r.Fans[0].Duty != 25 || r.Fans[0].Role != "cpu" {
		t.Fatalf("fans: %+v", r.Fans)
	}
	if r.Temps["cpu"] != 41 || r.Temps["board"] != 38 {
		t.Fatalf("temps: %+v", r.Temps)
	}
}

// Safety: no EC data register is written; the only config data value selects device 0x0A.
func TestProbeCS201xWritesNothingToTheEC(t *testing.T) {
	f := newFakeCS()
	if _, err := ProbeCS201x(f, "DXP2800 GT"); err != nil {
		t.Fatal(err)
	}
	if len(f.ecData) != 0 {
		t.Fatalf("data register written: %v", f.ecData)
	}
	if len(f.cfgData) != 1 || f.cfgData[0] != csLDNHost2EC {
		t.Fatalf("config data written: %v", f.cfgData)
	}
}

func TestProbeCS201xStopsOnOtherChip(t *testing.T) {
	f := newFakeCS()
	f.id = 0x8613
	if _, err := ProbeCS201x(f, "DXP2800 GT"); err == nil {
		t.Fatal("other chip accepted")
	}
	if len(f.cfgData) != 0 {
		t.Fatalf("went on after the wrong id: %v", f.cfgData)
	}
}

func TestProbeCS201xReportsHostInterfaceOff(t *testing.T) {
	f := newFakeCS()
	f.active = 0
	r, err := ProbeCS201x(f, "DXP2800 GT")
	if err != nil || r.HostEC != "not active" || len(r.Notes) == 0 || r.Revision != "0x0102" {
		t.Fatalf("got %+v, %v", r, err)
	}
	if len(f.cfgData) != 1 || f.active != 0 || len(f.ecData) != 0 {
		t.Fatalf("probe wrote: cfg %v ec %v active %d", f.cfgData, f.ecData, f.active)
	}
}

func TestProbeCS201xTempsAreUnsignedAndInputsReported(t *testing.T) {
	f := newFakeCS()
	f.ec[csRegNTC2], f.ec[csRegADC2Cfg] = 200, 0
	r, err := ProbeCS201x(f, "DXP2800 GT")
	if err != nil || r.Temps["cpu"] != 200 || r.ADC.Configured || r.ADC.Ch2 != 0 {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestProbeCS201xWatchdogCounting(t *testing.T) {
	f := newFakeCS()
	f.ec[csRegWdtCfg2] = 1
	r, _ := ProbeCS201x(f, "DXP2800 GT")
	if !r.Watchdog.Armed || r.Watchdog.Counting {
		t.Fatalf("%+v", r.Watchdog)
	}
}

func confirmedMap(t *testing.T, chip string, ids ...string) {
	t.Helper()
	old := fanMapPath
	t.Cleanup(func() { fanMapPath = old })
	fanMapPath = filepath.Join(t.TempDir(), "fan_map.json")
	m := FanMap{Version: 1, Model: "DXP2800 GT", Chip: chip}
	for _, id := range ids {
		f, _ := csFanOf(id)
		m.Outputs = append(m.Outputs, FanMapOutput{ID: id, Role: f.role})
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(fanMapPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCS201xOffWithoutConfirmedMap(t *testing.T) {
	old := fanMapPath
	defer func() { fanMapPath = old }()
	fanMapPath = filepath.Join(t.TempDir(), "none.json")
	if _, err := DetectCS201x(newFakeCS(), "DXP2800 GT"); err == nil {
		t.Fatal("opened without a confirmation")
	}
	confirmedMap(t, "IT8613", "fan1")
	if _, err := DetectCS201x(newFakeCS(), "DXP2800 GT"); err == nil {
		t.Fatal("an IT8613 map was accepted")
	}
}

func TestCS201xDetectAndReadsWriteNothing(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	c, err := DetectCS201x(f, "DXP2800 GT")
	if err != nil {
		t.Fatal(err)
	}
	if rpm, err := c.RPM(c.Outputs()[0]); err != nil || rpm != 1800 {
		t.Fatalf("rpm %d %v", rpm, err)
	}
	if d, auto, err := c.PWM(c.Outputs()[1]); err != nil || auto || d != 25 {
		t.Fatalf("pwm %d %v %v", d, auto, err)
	}
	if t0 := c.Temps()[0]; t0 == nil || *t0 != 41 {
		t.Fatalf("temp %v", t0)
	}
	if b := c.BoardTemp(); b == nil || *b != 38 {
		t.Fatalf("board %v", b)
	}
	if len(f.ecData) != 0 || len(f.cfgData) != 1 {
		t.Fatalf("writes: ec %v cfg %v", f.ecData, f.cfgData)
	}
}

// With the host interface off nothing is switched on, written or snapshotted: the BIOS does not expose the controller.
func TestCS201xHostInterfaceOffRefusesEverything(t *testing.T) {
	confirmedMap(t, csChip, "fan1")
	f := newFakeCS()
	f.active = 0
	c, err := DetectCS201x(f, "DXP2800 GT")
	if err != nil {
		t.Fatal(err)
	}
	ch := c.Outputs()[0]
	if _, err := c.RPM(ch); !errors.Is(err, errCSHostOff) {
		t.Fatalf("read with the interface off: %v", err)
	}
	if _, err := c.Snapshot(); !errors.Is(err, errCSHostOff) {
		t.Fatalf("snapshot: %v", err)
	}
	if err := c.SetPWM(ch, 50); !errors.Is(err, errCSHostOff) {
		t.Fatalf("set: %v", err)
	}
	if err := c.Restore(Snapshot{"fan1": {1, 80}}); !errors.Is(err, errCSHostOff) {
		t.Fatalf("restore: %v", err)
	}
	if f.active != 0 || len(f.ecData) != 0 {
		t.Fatalf("active %d, EC writes %v", f.active, f.ecData)
	}
	for _, v := range f.cfgData {
		if v != cs.LDNHost2EC {
			t.Fatalf("config write 0x%02x: only the device number may be written", v)
		}
	}
	if _, err := os.Stat(csWrotePath); err == nil {
		t.Fatal("marked as written without a write")
	}
}

// Only bit 0 of an enable register is written, and only when it differs.
func TestCS201xEnableRegisterBitZeroOnly(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	f.ec[csRegFan0En], f.ec[csRegFan1En] = 0x02, 0x03 // fan1: bit 0 off, bit 1 set; fan3: bit 0 on
	c, _ := DetectCS201x(f, "DXP2800 GT")
	if err := c.SetPWM(c.Outputs()[0], 50); err != nil {
		t.Fatal(err)
	}
	if f.ec[csRegFan0En] != 0x03 || f.ec[csRegFan1En] != 0x03 {
		t.Fatalf("enable %#x/%#x", f.ec[csRegFan0En], f.ec[csRegFan1En])
	}
	for _, a := range f.ecLog {
		if a == csRegFan1En {
			t.Fatal("enable of fan3 written although bit 0 was set")
		}
	}
	n := len(f.ecLog)
	if err := c.Restore(Snapshot{"fan1": {0x02, 70}, "fan3": {0x03, 70}}); err != nil {
		t.Fatal(err)
	}
	if f.ec[csRegFan0En] != 0x02 {
		t.Fatalf("restored enable %#x, want 0x02", f.ec[csRegFan0En])
	}
	for _, a := range f.ecLog[n:] {
		if a == csRegFan1En {
			t.Fatal("restore wrote an enable that already matched")
		}
	}
}

// A zero, missing or oversized saved duty never ends below the safe duty or above 100.
func TestCS201xRestoreEdgeSnapshots(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	c, _ := DetectCS201x(f, "DXP2800 GT")
	for _, s := range []Snapshot{nil, {}, {"fan1": {1, 0}}, {"fan1": {1, 255}, "fan3": {1, 0}}} {
		if err := c.Restore(s); err != nil {
			t.Fatal(err)
		}
		for _, d := range []byte{f.ec[csRegFan0Duty], f.ec[csRegFan1Duty]} {
			if d < csSafePct || d > 100 {
				t.Fatalf("snapshot %v: duty %d", s, d)
			}
		}
	}
}

// The two bytes of the speed can tear at a rollover: read again until the high byte holds.
type tearCS struct {
	*fakeCS
	hiReads int
}

func (f *tearCS) In(port uint16) (byte, error) {
	v, err := f.fakeCS.In(port)
	if port == f.base+csEcData {
		a := uint32(f.addr[0]) | uint32(f.addr[1])<<8 | uint32(f.addr[2])<<16 | uint32(f.addr[3])<<24
		if a == csRegFan0Spd {
			f.hiReads++
			if f.hiReads == 2 { // the second read of the high byte: the counter rolled over meanwhile
				v = 0x08
				f.ec[csRegFan0Spd], f.ec[csRegFan0Spd+1] = 0x08, 0x00
			}
		}
	}
	return v, err
}

func TestCS201xRPMReadsAgainOnRollover(t *testing.T) {
	confirmedMap(t, csChip, "fan1")
	f := &tearCS{fakeCS: newFakeCS()}
	f.ec[csRegFan0Spd], f.ec[csRegFan0Spd+1] = 0x07, 0xff
	c, _ := DetectCS201x(f, "DXP2800 GT")
	rpm, err := c.RPM(c.Outputs()[0])
	if err != nil || rpm != 0x0800 {
		t.Fatalf("rpm %#x (%v), want 0x800", rpm, err)
	}
}

func TestConfirmCS201xRefusesImplausibleReadings(t *testing.T) {
	old := fanMapPath
	defer func() { fanMapPath = old }()
	for name, set := range map[string]func(f *fakeCS){
		"stopped fan":  func(f *fakeCS) { f.ec[csRegFan0Spd], f.ec[csRegFan0Spd+1] = 0, 0 },
		"floating bus": func(f *fakeCS) { f.ec[csRegFan0Spd], f.ec[csRegFan0Spd+1] = 0xff, 0xff },
	} {
		fanMapPath = filepath.Join(t.TempDir(), "fan_map.json")
		f := newFakeCS()
		set(f)
		if _, err := confirmCS201x(f, "DXP2800 GT", []string{"fan1"}); err == nil {
			t.Fatalf("%s: confirmed", name)
		}
		if _, err := os.Stat(fanMapPath); err == nil {
			t.Fatalf("%s: map saved", name)
		}
	}
}

func TestCS201xSetPWMFloorCeilingAndPrepare(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	f.ec[csRegFan0En], f.ec[csRegNTC4], f.ec[csRegADC4Cfg] = 0, 0, 0
	c, _ := DetectCS201x(f, "DXP2800 GT")
	ch := c.Outputs()
	for _, tc := range []struct{ in, want byte }{{0, 25}, {10, 25}, {60, 60}, {100, 100}, {200, 100}} {
		if err := c.SetPWM(ch[0], tc.in); err != nil || f.ec[csRegFan0Duty] != tc.want {
			t.Fatalf("in %d: duty %d err %v", tc.in, f.ec[csRegFan0Duty], err)
		}
	}
	if f.ec[csRegFan0En] != 1 || f.ec[csRegADC4Cfg] != 1 {
		t.Fatalf("enable %d adc4 %d", f.ec[csRegFan0En], f.ec[csRegADC4Cfg])
	}
	if f.ec[csRegADC2Cfg] != 1 || f.ec[csRegFan1Duty] != 25 {
		t.Fatal("touched what it should not")
	}
	for _, a := range f.ecLog {
		switch a {
		case csRegFan0En, csRegFan0Duty, csRegADC4Cfg:
		default:
			t.Fatalf("unexpected register write 0x%08x", a)
		}
	}
	if err := c.SetPWM(Channel{ID: "fan2"}, 50); err == nil {
		t.Fatal("unknown fan accepted")
	}
}

func TestCS201xSnapshotRestore(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	f.ec[csRegFan0Duty], f.ec[csRegFan1Duty], f.ec[csRegFan1En] = 70, 0, 0
	c, _ := DetectCS201x(f, "DXP2800 GT")
	snap, err := c.Snapshot()
	if err != nil || snap["fan1"] != [2]byte{1, 70} || snap["fan3"] != [2]byte{0, 0} || len(f.ecData) != 0 {
		t.Fatalf("snap %v %v writes %v", snap, err, f.ecData)
	}
	_ = c.SetPWM(c.Outputs()[0], 90)
	_ = c.SetPWM(c.Outputs()[1], 90)
	if f.ec[csRegFan1En] != 1 {
		t.Fatal("enable not set")
	}
	if err := c.Restore(snap); err != nil {
		t.Fatal(err)
	}
	// fan1: duty back (above the floor). fan3: saved duty 0 would stop it, so the safe duty; enable back to what it was.
	if f.ec[csRegFan0Duty] != 70 || f.ec[csRegFan1Duty] != csSafePct || f.ec[csRegFan1En] != 0 {
		t.Fatalf("duty %d/%d en %d", f.ec[csRegFan0Duty], f.ec[csRegFan1Duty], f.ec[csRegFan1En])
	}
	if err := c.Restore(nil); err != nil || f.ec[csRegFan0Duty] != csSafePct {
		t.Fatalf("no snapshot: duty %d err %v", f.ec[csRegFan0Duty], err)
	}
}

func TestConfirmCS201x(t *testing.T) {
	old := fanMapPath
	defer func() { fanMapPath = old }()
	fanMapPath = filepath.Join(t.TempDir(), "fan_map.json")
	f := newFakeCS()
	f.ec[csRegFan1Spd], f.ec[csRegFan1Spd+1] = 0, 0
	if _, err := confirmCS201x(f, "DXP2800 GT", nil); err == nil {
		t.Fatal("fan without rpm confirmed")
	}
	if _, err := os.Stat(fanMapPath); err == nil {
		t.Fatal("map written on refusal")
	}
	m, err := confirmCS201x(f, "DXP2800 GT", []string{"fan1"})
	if err != nil || len(m.Outputs) != 1 {
		t.Fatalf("%+v %v", m, err)
	}
	if got, err := loadCS201xMap("DXP2800 GT"); err != nil || got.Outputs[0].ID != "fan1" {
		t.Fatalf("%+v %v", got, err)
	}
	if len(f.ecData) != 0 {
		t.Fatal("confirm wrote to the EC")
	}
}

func TestIsDXP2800GT(t *testing.T) {
	if !isDXP2800GT("DXP2800 GT") || isDXP2800GT("DXP2800") || isDXP2800GT("DXP4800 GT") {
		t.Fatal("model match")
	}
}

// The fan tool and the LED backend (other processes) take the same flock: one blocks the other.
func TestFanLockIsTheSharedCS201xLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	oldLock := lockPath
	lockPath = path
	defer func() { lockPath = oldLock }()
	defer cs.SetLockPath(path)()

	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		_ = withLock(func() error { close(held); <-release; return nil })
	}()
	<-held
	got := make(chan struct{})
	go func() { _ = cs.WithLock(func() error { close(got); return nil }) }()
	select {
	case <-got:
		t.Fatal("the LED side got the lock while the fan tool held it")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	<-got
}

// A saved duty below the floor (a snapshot of one we wrote) is never written back: the CS201x has no automatic mode.
func TestCS201xRestoreNeverBelowSafeDuty(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	c, _ := DetectCS201x(f, "DXP2800 GT")
	if err := c.Restore(Snapshot{"fan1": {1, 24}, "fan3": {1, 85}}); err != nil {
		t.Fatal(err)
	}
	if f.ec[csRegFan0Duty] != csSafePct || f.ec[csRegFan1Duty] != 85 {
		t.Fatalf("duty %d/%d, want %d/85", f.ec[csRegFan0Duty], f.ec[csRegFan1Duty], csSafePct)
	}
}

// A snapshot taken after this tool wrote a duty (a crashed daemon, a lost snapshot file) must not keep our duty as
// the BIOS value.
func TestCS201xSnapshotAfterOwnWriteIsNotTrusted(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	c, _ := DetectCS201x(f, "DXP2800 GT")
	if err := c.SetPWM(c.Outputs()[0], 30); err != nil {
		t.Fatal(err)
	}
	c2, _ := DetectCS201x(f, "DXP2800 GT") // the restarted daemon
	snap, err := c2.Snapshot()
	if err != nil || snap["fan1"][1] != 0 || snap["fan3"][1] != 0 {
		t.Fatalf("snapshot %v (%v) kept our duty", snap, err)
	}
	if err := c2.Restore(snap); err != nil || f.ec[csRegFan0Duty] != csSafePct {
		t.Fatalf("restore: duty %d (%v)", f.ec[csRegFan0Duty], err)
	}
	if _, err := os.Stat(csWrotePath); err == nil {
		t.Fatal("marker stays after the fans were handed back")
	}
	c3, _ := DetectCS201x(f, "DXP2800 GT")
	f.ec[csRegFan0Duty] = 40 // the BIOS value of a later boot
	if snap, _ := c3.Snapshot(); snap["fan1"][1] != 40 {
		t.Fatalf("clean snapshot %v", snap)
	}
}

func TestCS201xSetPWMReadsBack(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	f := newFakeCS()
	c, _ := DetectCS201x(f, "DXP2800 GT")
	f.stuck = map[uint32]bool{csRegFan0Duty: true}
	n := len(f.ecLog)
	if err := c.SetPWM(c.Outputs()[0], 50); err == nil {
		t.Fatal("a duty the EC did not take was reported as written")
	}
	writes := 0
	for _, a := range f.ecLog[n:] {
		if a == csRegFan0Duty {
			writes++
		}
	}
	if writes != 2 {
		t.Fatalf("%d duty writes, want 1 + 1 retry", writes)
	}
	f.stuck = nil
	if err := c.SetPWM(c.Outputs()[0], 50); err != nil || f.ec[csRegFan0Duty] != 50 {
		t.Fatalf("duty %d (%v)", f.ec[csRegFan0Duty], err)
	}
}

func TestConfirmCS201xRefusesWithHostInterfaceOff(t *testing.T) {
	old := fanMapPath
	defer func() { fanMapPath = old }()
	fanMapPath = filepath.Join(t.TempDir(), "fan_map.json")
	f := newFakeCS()
	f.active = 0
	_, err := confirmCS201x(f, "DXP2800 GT", nil)
	if err == nil || !strings.Contains(err.Error(), "host interface") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(fanMapPath); err == nil {
		t.Fatal("map saved blind")
	}
}

func TestForgetCS201xHandsFansBackFirst(t *testing.T) {
	confirmedMap(t, csChip, "fan1", "fan3")
	oldBios, oldLock := biosPath, lockPath
	dir := t.TempDir()
	biosPath, lockPath = filepath.Join(dir, "bios"), filepath.Join(dir, "lock")
	defer func() { biosPath, lockPath = oldBios, oldLock }()
	f := newFakeCS()

	// Nothing was ever saved: nothing was written, the map just goes.
	if err := forgetCS201x(f, "DXP2800 GT"); err != nil || len(f.ecData) != 0 {
		t.Fatalf("no snapshot: %v, writes %v", err, f.ecData)
	}
	if _, err := os.Stat(fanMapPath); err == nil {
		t.Fatal("map kept")
	}

	confirmedMap(t, csChip, "fan1", "fan3")
	if err := os.WriteFile(biosPath, []byte(`{"fan1":[1,80],"fan3":[1,25]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f.ec[csRegFan0Duty], f.ec[csRegFan1Duty] = 30, 30 // ours
	if err := forgetCS201x(f, "DXP2800 GT"); err != nil {
		t.Fatal(err)
	}
	if f.ec[csRegFan0Duty] != 80 || f.ec[csRegFan1Duty] != csSafePct {
		t.Fatalf("duty %d/%d after forget", f.ec[csRegFan0Duty], f.ec[csRegFan1Duty])
	}
	if _, err := os.Stat(fanMapPath); err == nil {
		t.Fatal("map kept")
	}

	// A failed hand-back keeps the map: forget can be run again.
	confirmedMap(t, csChip, "fan1", "fan3")
	f.id = 0x8613
	if err := forgetCS201x(f, "DXP2800 GT"); err == nil {
		t.Fatal("forget succeeded without handing the fans back")
	}
	if _, err := os.Stat(fanMapPath); err != nil {
		t.Fatal("map removed although the fans were not handed back")
	}
}

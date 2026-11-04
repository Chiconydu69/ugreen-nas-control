package fan

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
	cs "github.com/ugreen-nas-control/ugreen-nas-control/internal/hw/cs201x"
)

// Chipsea CS201x Super-I/O with an embedded controller (DXP2800 GT). Register addresses as published by UGREEN
// (GPL-2.0-only driver ug_201x.c@c75c5ab, cited as ug_201x.c:LINE); facts only, no code used. The probe below only
// reads; the fan driver is in cs201x_fans.go.
const (
	// Shared with the LED backend: hw/cs201x.
	cs201xID     = cs.ChipID
	csLDNHost2EC = cs.LDNHost2EC

	csEcCtrl = cs.EcCtrl
	csEcAddr = cs.EcAddr
	csEcData = cs.EcData

	csRegFan0En   = 0x200053f0 // ug_201x.c:86-94
	csRegFan1En   = 0x200053f1
	csRegFan0Duty = 0x200053f4 // percent, 0-100 (ug_201x.c:311-328)
	csRegFan1Duty = 0x200053f5
	csRegFan0Spd  = 0x200053f8 // high byte, the low byte follows; already rpm
	csRegFan1Spd  = 0x200053fa
	csRegNTC2     = 0x200053e2 // cpu_temp, unsigned °C (ug_201x.c:99-100,276-291)
	csRegNTC4     = 0x200053e4 // board_temp
	csRegFWVer    = 0x200054fc // defined, never read by UGREEN (ug_201x.c:103)

	csRegADC2Cfg = 0x20005370 // UGREEN writes 1 ("temperature input") before reading temps (ug_201x.c:96-97,270-272)
	csRegADC4Cfg = 0x20005371

	csRegARGBFlow = 0x20005440 // written 1 at load, 0 at unload (ug_201x.c:102,789-790,817)
	csRegPWRSW    = 0x2000548c // written 0 at load (ug_201x.c:104)

	// Watchdog (ug_201x.c:105-111,330-374): read here, never written.
	csRegWdtCfg1    = 0x20005480
	csRegWdtCfg2    = 0x20005481 // 1 = running
	csRegWdtTimeout = 0x20005482 // high byte, the low byte follows
	csRegWdtCounter = 0x20005484 // high byte, the low byte follows
	csRegWdtFeed    = 0x20005488
)

// csCounterGap is the pause between the two watchdog counter reads; tests shorten it.
var csCounterGap = 1500 * time.Millisecond

type CS201xReading struct {
	Model     string         `json:"model"`
	ChipID    string         `json:"chip_id"`
	Revision  string         `json:"revision"`
	HostEC    string         `json:"host_ec"` // active / not active
	Base      string         `json:"base,omitempty"`
	Fans      []CS201xFan    `json:"fans,omitempty"`
	Temps     map[string]int `json:"temps,omitempty"` // cpu (NTC2), board (NTC4), unsigned °C
	ADC       *CS201xADC     `json:"temp_inputs,omitempty"`
	Watchdog  *CS201xWdt     `json:"watchdog,omitempty"`
	ARGBFlow  *int           `json:"argb_flow_control,omitempty"`
	PWRSW     *int           `json:"pwrsw_cfg,omitempty"`
	FWVersion string         `json:"ec_firmware,omitempty"`
	Notes     []string       `json:"notes,omitempty"`
}

type CS201xFan struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	Enabled int    `json:"enabled_reg"`
	Duty    int    `json:"duty_pct"`
	RPM     int    `json:"rpm"`
}

// CS201xADC: the temperature input registers UGREEN sets to 1 before it reads a temperature.
type CS201xADC struct {
	Ch2        int  `json:"ch2_cfg"`
	Ch4        int  `json:"ch4_cfg"`
	Configured bool `json:"configured"` // both read 1
}

// CS201xWdt: the watchdog registers as found. The unit is unverified (UGREEN's driver says seconds but clamps in
// minutes, ug_201x.c:659-724).
type CS201xWdt struct {
	Cfg1     int  `json:"cfg1"`
	Cfg2     int  `json:"cfg2"`
	Armed    bool `json:"armed"` // cfg2 bit 0
	Timeout  int  `json:"timeout"`
	Counter  int  `json:"counter"`
	Counter2 int  `json:"counter_later"`
	Counting bool `json:"counting"` // the counter moved between the two reads
	Feed     int  `json:"feed"`
}

func isDXP2800GT(model string) bool {
	return strings.HasPrefix(model, "DXP2800 GT")
}

type csWindow = cs.Window

type csEC = cs.EC

// csReadWindow never switches the host interface on: whether the BIOS exposes the fan controller is not ours to change.
func csReadWindow(p Ports) (csWindow, error) { return cs.ReadWindow(p, false) }

// csRPM reads a 16-bit speed register as two bytes. The controller can roll the pair over between the reads, so it
// reads again until the high byte is the same on both sides of the low byte (three tries).
func csRPM(ec csEC, reg uint32) (int, error) {
	hi, err := ec.Read(reg)
	if err != nil {
		return 0, err
	}
	for range 3 {
		lo, err := ec.Read(reg + 1)
		if err != nil {
			return 0, err
		}
		hi2, err := ec.Read(reg)
		if err != nil {
			return 0, err
		}
		if hi2 == hi {
			return int(hi)<<8 | int(lo), nil
		}
		hi = hi2
	}
	return 0, errors.New("fan speed register keeps changing between the two byte reads")
}

func plausibleBase(base uint16) bool { return cs.PlausibleBase(base) }

// ProbeCS201x writes only the unlock key, the logical device number and the EC address latch (needed for every
// read). It never writes a data register, never switches the host interface on and never touches a fan or watchdog
// setting. A host interface that is off is reported, not an error.
func ProbeCS201x(p Ports, model string) (CS201xReading, error) {
	r := CS201xReading{Model: model, Temps: map[string]int{}}

	w, err := csReadWindow(p)
	r.ChipID = fmt.Sprintf("0x%04x", w.ID)
	r.Revision = fmt.Sprintf("0x%04x", w.Rev)
	if err != nil {
		return r, err
	}

	if w.Active&0x01 == 0 {
		r.HostEC = "not active"
		r.Notes = append(r.Notes, "the host interface of the embedded controller is off (cfg 0x30 = 0): the BIOS does not expose the fan controller. Nothing switches it on, so no EC register was read and fan control stays off")
		if plausibleBase(w.Base) {
			r.Base = fmt.Sprintf("0x%04x", w.Base)
		}
		return r, nil
	}
	r.HostEC = "active"
	if !plausibleBase(w.Base) {
		return r, fmt.Errorf("implausible base address 0x%04x", w.Base)
	}
	r.Base = fmt.Sprintf("0x%04x", w.Base)

	ec := csEC{P: p, Base: w.Base}
	var firstErr error
	get := func(reg uint32) int {
		v, err := ec.Read(reg)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return int(v)
	}

	rpm := func(reg uint32) int {
		v, err := csRPM(ec, reg)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return v
	}

	for i, f := range []struct {
		role          string
		en, duty, spd uint32
	}{
		{"cpu", csRegFan0En, csRegFan0Duty, csRegFan0Spd},
		{"system", csRegFan1En, csRegFan1Duty, csRegFan1Spd},
	} {
		r.Fans = append(r.Fans, CS201xFan{
			ID:      fmt.Sprintf("fan%d", i),
			Role:    f.role,
			Enabled: get(f.en),
			Duty:    get(f.duty),
			RPM:     rpm(f.spd),
		})
	}
	// Unsigned bytes, as UGREEN reads them (ug_201x.c:276-291).
	r.Temps["cpu"] = get(csRegNTC2)
	r.Temps["board"] = get(csRegNTC4)
	r.ADC = &CS201xADC{Ch2: get(csRegADC2Cfg), Ch4: get(csRegADC4Cfg)}
	r.ADC.Configured = r.ADC.Ch2 == 1 && r.ADC.Ch4 == 1
	if !r.ADC.Configured {
		r.Notes = append(r.Notes, "the temperature input registers 0x20005370/71 are not 1; UGREEN sets them before reading temperatures, so the temperatures may read 0 or stale until the driver does")
	}

	wd := &CS201xWdt{Cfg1: get(csRegWdtCfg1), Cfg2: get(csRegWdtCfg2), Feed: get(csRegWdtFeed)}
	wd.Armed = wd.Cfg2&0x01 != 0
	wd.Timeout = get(csRegWdtTimeout)<<8 | get(csRegWdtTimeout+1)
	wd.Counter = get(csRegWdtCounter)<<8 | get(csRegWdtCounter+1)
	time.Sleep(csCounterGap)
	wd.Counter2 = get(csRegWdtCounter)<<8 | get(csRegWdtCounter+1)
	wd.Counting = wd.Counter2 != wd.Counter
	r.Watchdog = wd

	flow, pwrsw := get(csRegARGBFlow), get(csRegPWRSW)
	r.ARGBFlow, r.PWRSW = &flow, &pwrsw
	r.FWVersion = fmt.Sprintf("0x%02x", get(csRegFWVer))

	return r, firstErr
}

// cs201xCommand: no argument reads the chip (JSON); `confirm [fan1] [fan3]` switches fan control on for the named
// fans (default both) by saving the map; `forget` removes it.
func cs201xCommand() {
	model := productName()
	if !isDXP2800GT(model) {
		fail(fmt.Errorf("%q is not a DXP2800 GT", model))
	}
	args := os.Args[2:]
	p, err := hw.OpenPorts()
	if err != nil {
		fail(err)
	}
	defer func() { _ = p.Close() }()
	if len(args) > 0 && args[0] == "forget" {
		if err := forgetCS201x(p, model); err != nil {
			fail(err)
		}
		printJSON(map[string]any{"ok": true})
		return
	}
	out := map[string]any{}
	if len(args) > 0 && args[0] == "confirm" {
		var m *FanMap
		err = withLock(func() (err error) { m, err = confirmCS201x(p, model, args[1:]); return })
		out["map"] = m
	} else {
		var rd CS201xReading
		err = withLock(func() (err error) { rd, err = ProbeCS201x(p, model); return })
		out["read"] = rd
	}
	out["ok"] = err == nil
	if err != nil {
		out["error"] = err.Error()
	}
	printJSON(out)
	if err != nil {
		_ = p.Close() // os.Exit skips the defer
		os.Exit(1)
	}
}

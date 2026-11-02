package fan

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// EC watchdog, pet-only. An armed embedded-controller watchdog resets the box if nothing feeds it. This tool never
// arms it (never writes the enable byte non-zero, enforced in EC.write) and only feeds a timer that is already
// running. Arming happens elsewhere: UGREEN's driver arms only when userspace opens /dev/watchdog and registering the
// device does not arm (ug_it55_4800gt_functions.c:615-640@c75c5ab); whether the BIOS arms it at boot is not proven.
// Registers: enable 0x98, timeout 0xa2/0xa3 (big-endian seconds, 1..10000, default 600) on the DXP4800 GT
// (ug_it55_4800gt_functions.c:529-536,540-543,559-560) and the iDX6011 Pro (ug_it55pro_functions.c:521-527); the plain
// iDX6011/6012 use 0x55/0x56/0x57 (ug_it55_functions.c:466-472), where 0x98/0x99 is the second fan's tach. UGREEN has no
// ping op: its keepalive calls start again, which rewrites the timeout and sets enable = 1
// (ug_it55_4800gt_functions.c:557-584,600-606). Ours rewrites the timeout only, every timeout / 3 and only while
// the enable byte is set; that this reloads the counter on hardware is unproven. On stop clear the enable byte
// after returning both fans to EC auto. The counter (timeout registers read back, :550-556) is reported as read.
const (
	ecRegWdtEnable = 0x98 // read: armed when non-zero; written only with 0
	ecRegWdtHi     = 0xa2 // timeout in seconds, big-endian
	ecRegWdtLo     = 0xa3

	wdtDefaultTimeout = 300 // seed written by the petter
	wdtMinTimeout     = 1
	wdtMaxTimeout     = 10000
	wdtMaxInterval    = 60 * time.Second // never feed less often than this
	wdtReloadSlack    = 5                // a reloaded counter reads within this many seconds of the value written
)

var errNeverArm = errors.New("refusing to arm the EC watchdog")

type WatchdogStatus struct {
	Armed    bool   `json:"armed"`
	Timeout  int    `json:"timeout"`           // seconds
	Interval int    `json:"interval"`          // seconds between feeds
	LastPet  int64  `json:"last_pet"`          // unix time, 0 = never
	Time     int64  `json:"time"`              // when this was sampled
	State    string `json:"state"`             // a Wdt* state, computed when read (WdtYielded, WdtNotCounting and WdtNotEffective are kept as written)
	Counter  *int   `json:"counter,omitempty"` // timeout registers read back before the feed; only while armed
}

// Watchdog states for the page: it renders the word and does no clock maths.
const (
	WdtOff          = "off"
	WdtArmed        = "armed"
	WdtActionNeeded = "action_needed" // armed, but nothing fed it for more than twice the interval
	WdtYielded      = "yielded"       // IT8613E: the it87_wdt module is loaded and owns the timer, nothing is accessed
	WdtNotCounting  = "not_counting"  // IT8613E: the value is non-zero but did not count down, so nothing is written
	WdtNotEffective = "not_effective" // EC: armed and fed, but the counter did not reload after a feed
)

func watchdogState(st WatchdogStatus, now time.Time) string {
	switch {
	case st.State == WdtYielded || st.State == WdtNotCounting || st.State == WdtNotEffective:
		return st.State
	case !st.Armed:
		return WdtOff
	case st.LastPet == 0 || now.Unix()-st.LastPet > 2*int64(st.Interval):
		return WdtActionNeeded
	}
	return WdtArmed
}

var watchdogStatePath = envOr("UGREEN_FAN_WATCHDOG", "/var/run/ugreen-nas-control.watchdog.json")

func wdtInterval(timeout int) time.Duration {
	return min(time.Duration(timeout)*time.Second/3, wdtMaxInterval)
}

var errNoWatchdog = errors.New("no watchdog registers for this model")

func (c *EC) wdtRegs() (*ecWdt, error) {
	if w := c.sp().wdt; w != nil {
		return w, nil
	}
	return nil, errNoWatchdog
}

// WatchdogArmed reads the enable byte (read only, also on the plain iDX).
func (c *EC) WatchdogArmed() (bool, error) {
	w, err := c.wdtRegs()
	if err != nil {
		return false, err
	}
	v, err := c.read(w.enable)
	return v != 0, err
}

// WatchdogCounter reads the timeout registers back, read only. UGREEN reads the same pair as its counter
// (ug_it55_4800gt_functions.c:550-556@c75c5ab) without saying whether it counts down.
func (c *EC) WatchdogCounter() (int, error) {
	w, err := c.wdtRegs()
	if err != nil {
		return 0, err
	}
	hi, err := c.read(w.hi)
	if err != nil {
		return 0, err
	}
	lo, err := c.read(w.lo)
	return int(hi)<<8 | int(lo), err
}

// WatchdogPet feeds a running timer by rewriting its timeout. It never touches the enable byte.
func (c *EC) WatchdogPet(timeout int) error {
	w, err := c.wdtRegs()
	if err != nil {
		return err
	}
	t := min(max(timeout, wdtMinTimeout), wdtMaxTimeout)
	if err := c.write(w.hi, byte(t>>8)); err != nil {
		return err
	}
	return c.write(w.lo, byte(t))
}

// DisarmWatchdog clears the enable byte; the only value this tool ever writes there.
func (c *EC) DisarmWatchdog() error {
	c.revive()
	w, err := c.wdtRegs()
	if err != nil {
		return err
	}
	return c.write(w.enable, 0)
}

// Petter feeds the watchdog of an EC or an IT8613E while it is armed. One per process; Tick runs under the fan lock.
// The daemon calls it every tick, also while it yields to another fan tool: feeding is harmless and keeps the box
// from resetting. A calibration or a probe calls it as well.
type Petter struct {
	Timeout int  // seconds, 0 = wdtDefaultTimeout
	Yield   bool // IT8613E: it87_wdt is loaded, the daemon sets this before every Tick
	last    time.Time
	status  WatchdogStatus
	failed  bool // the last Tick failed (the daemon logs it once)

	notEffective bool // EC: the counter did not reload after the last feed

	// IT8613E only. counting latches once two samples on different ticks showed the value fall (at least sampleGap
	// apart), until it reads 0 or the petter yields; sample is the first of the pair; petted: this process wrote the
	// timer, so a clean stop writes 0.
	counting bool
	sample   iteSample
	petted   bool
	petCfg   byte // the config the last feed wrote, kept in the marker of a clean stop
	marked   bool // the marker of an earlier clean stop was handled
}

// iteSample is one reading of the IT8613E value with the time and the config it was read under.
type iteSample struct {
	val  uint16
	cfg  byte
	at   time.Time
	have bool
}

func (p *Petter) timeout() int {
	t := p.Timeout
	if t == 0 {
		t = wdtDefaultTimeout
	}
	return min(max(t, wdtMinTimeout), wdtMaxTimeout)
}

// Tick reads the watchdog and feeds the timer if it is armed and due. ok is false for a chip without a watchdog.
func (p *Petter) Tick(chip Fans, now time.Time) (st WatchdogStatus, ok bool, err error) {
	switch c := chip.(type) {
	case *EC:
		return p.tickEC(c, now)
	case *Chip:
		return p.tickITE(c, now)
	}
	return WatchdogStatus{}, false, nil
}

func (p *Petter) record(now time.Time) {
	p.status.Time = now.Unix()
	_ = writeJSONAtomic(watchdogStatePath, p.status)
}

func (p *Petter) tickEC(ec *EC, now time.Time) (st WatchdogStatus, ok bool, err error) {
	// Defense in depth: the plain iDX6011/6012 gets no petter (its 0x98/0x99 is a fan tach, its own map is read only),
	// and the IT8613E models never reach an EC write.
	if !ecWatchdogModel(productName()) || ec.sp().wdt == nil || ec.sp().readOnly {
		return WatchdogStatus{}, false, nil
	}
	timeout := p.timeout()

	armed, err := ec.WatchdogArmed()
	if err != nil {
		return p.status, true, err
	}
	p.status.Armed, p.status.Timeout, p.status.Interval = armed, timeout, int(wdtInterval(timeout)/time.Second)
	p.status.Counter = nil
	if armed {
		if n, err := ec.WatchdogCounter(); err == nil {
			p.status.Counter = &n
		}
	}
	if !armed {
		p.notEffective = false
	}
	if armed && (p.last.IsZero() || now.Sub(p.last) >= wdtInterval(timeout)) {
		if err = ec.WatchdogPet(timeout); err == nil {
			p.last = now
			p.status.LastPet = now.Unix()
			ec.fed = true
			// Read the counter again: after a feed it shows the value written. A timer that keeps its old value did
			// not take the feed (UGREEN's driver does not say that this write reloads the counter).
			if n, cerr := ec.WatchdogCounter(); cerr == nil {
				p.notEffective = n < timeout-wdtReloadSlack || n > timeout+wdtReloadSlack
			}
		}
	}
	p.status.State = ""
	if p.notEffective {
		p.status.State = WdtNotEffective
	}
	p.status.State = watchdogState(p.status, now)
	p.record(now)
	return p.status, true, err
}

// tickITE feeds the IT8613E watchdog. A keepalive write of a non-zero value also starts a stopped timer on this chip,
// so it writes only after proof that the timer counts (see ite_watchdog.go); a value that stands still is reported
// and left alone. While it87_wdt is loaded nothing at all is accessed.
func (p *Petter) tickITE(c *Chip, now time.Time) (st WatchdogStatus, ok bool, err error) {
	if !isIT8613Model(productName()) {
		return WatchdogStatus{}, false, nil
	}
	timeout := p.timeout()
	p.status.Timeout, p.status.Interval = timeout, int(wdtInterval(timeout)/time.Second)
	p.status.Armed, p.status.Counter = false, nil

	if p.Yield {
		// The module owns the timer: this process no longer fed it, and a marker of an earlier stop is moot.
		p.counting, p.sample, p.petted = false, iteSample{}, false
		if !p.marked {
			p.marked = true
			removeWdtMarker()
		}
		p.status.State = WdtYielded
		p.record(now)
		return p.status, true, nil
	}

	cur, err := c.WatchdogRead()
	if errors.Is(err, errNotIT8613) {
		return WatchdogStatus{}, false, nil
	}
	if err != nil {
		return p.status, true, err
	}
	if cur.val == 0 {
		p.counting, p.sample, p.petted = false, iteSample{}, false
		if m := readWdtMarker(); m != nil && !p.marked {
			// A clean stop disarmed a timer the BIOS had armed: put it back, then feed it as before.
			if err := c.WatchdogRestore(*m); err != nil {
				return p.status, true, err
			}
			removeWdtMarker()
			p.marked, p.counting, p.petted, p.petCfg, p.last = true, true, true, m.Cfg, now
			n := int(m.Value)
			p.status.Counter, p.status.Armed, p.status.LastPet = &n, true, now.Unix()
			p.status.State = watchdogState(p.status, now)
			p.record(now)
			return p.status, true, nil
		}
		p.status.State = WdtOff
		p.record(now)
		return p.status, true, nil
	}

	if !p.marked { // the timer runs on its own: an old marker is moot
		p.marked = true
		removeWdtMarker()
	}
	if !p.counting {
		p.proveCounting(cur, now)
	}
	n := int(cur.val)
	p.status.Counter = &n
	if !p.counting {
		p.status.State = WdtNotCounting
		p.record(now)
		return p.status, true, nil
	}

	p.status.Armed = true
	p.status.State = ""
	if p.last.IsZero() || now.Sub(p.last) >= wdtInterval(timeout) {
		if err = c.WatchdogPet(cur, timeout); err == nil {
			p.last, p.petted, p.petCfg = now, true, cur.cfg
			p.status.LastPet = now.Unix()
		}
	}
	p.status.State = watchdogState(p.status, now)
	p.record(now)
	return p.status, true, err
}

// proveCounting compares the value with the sample of an earlier tick: the timer counts when it fell by at least one
// unit since then. The pair must be sampleGap apart (a minutes timer cannot move faster than once a minute), so the
// proof spans ticks and never sleeps under the lock. A sample that rose, or was taken under another config, or lies in
// the future, is replaced; a value that stood still is sampled again.
func (p *Petter) proveCounting(cur ITEWdt, now time.Time) {
	s := p.sample
	if !s.have || s.cfg != cur.cfg || now.Before(s.at) || cur.val > s.val {
		p.sample = iteSample{cur.val, cur.cfg, now, true}
		return
	}
	if now.Sub(s.at) < sampleGap(cur.cfg) {
		return
	}
	if cur.val < s.val {
		p.counting = true
		return
	}
	p.sample = iteSample{cur.val, cur.cfg, now, true}
}

// Stop ends the feeding on a clean daemon stop. An IT8613E timer this process fed is stopped (value 0), so that a
// stopped daemon does not leave a timer nobody feeds, and a marker records what to put back: the next daemon start
// restores it, but only if the timer still reads non-zero right before the disarm (a timer that was stopped from
// outside stays off). The EC clears its enable byte in EC.Restore instead. Nothing is written if this process never
// fed the timer or while it87_wdt owns it. The disarm always happens; a failing marker is returned after it.
func (p *Petter) Stop(chip Fans) error {
	c, ok := chip.(*Chip)
	if !ok || !p.petted || p.Yield {
		return nil
	}
	var markErr error
	if cur, err := c.WatchdogRead(); err != nil {
		markErr = err
	} else if cur.val != 0 {
		markErr = writeJSONAtomic(iteWdtMarkerPath, wdtMarker{Cfg: p.petCfg, Value: uint16(wdtUnits(p.petCfg, p.timeout()))})
	}
	if err := c.DisarmWatchdog(); err != nil {
		removeWdtMarker()
		return err
	}
	p.petted, p.counting = false, false
	p.status.Armed, p.status.State = false, WdtOff
	p.record(time.Now())
	return markErr
}

// ReadWatchdogStatus is the last state a Petter wrote, nil if none (no EC, or nothing ticked since boot).
func ReadWatchdogStatus() *WatchdogStatus {
	b, err := os.ReadFile(watchdogStatePath)
	if err != nil {
		return nil
	}
	var st WatchdogStatus
	if json.Unmarshal(b, &st) != nil {
		return nil
	}
	st.State = watchdogState(st, time.Now())
	return &st
}

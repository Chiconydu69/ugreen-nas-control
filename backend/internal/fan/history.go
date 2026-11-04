package fan

import (
	"math"
	"sync"
	"time"
)

// Fan history: what every fan did and what drove it, kept in the daemon's RAM only (never on disk: no flash wear,
// gone with the daemon). Two rings of fixed size, filled by the daemon every tick (2 s):
//
//	fine    10 s steps, the last hour    360 points
//	coarse  60 s steps, the last 24 hours 1440 points (averages of the fine points)
//
// A point is 8 + 4 + 8 bytes per fan, at most maxFans fans: 1800 points x 56 bytes = about 100 KB, allocated once.
// Nothing is computed for the history except the averages: the daemon hands over the decision of the tick (Drive).
// The web page asks for it through a unix socket (history_sock.go) only while somebody looks at the page.

const (
	maxFans    = 6
	fineStep   = 10
	fineLen    = 360
	coarseStep = 60
	coarseLen  = 1440
	maxDrivers = 64 // distinct sources; the rest is recorded as "other"

	unknownPct  = 255
	unknownRPM  = math.MaxUint16
	noDriver    = 255
	unknownTemp = math.MinInt16
)

// fanPoint: one fan in one step. Temp is the driving input in tenths of °C.
type fanPoint struct {
	rpm  uint16
	temp int16
	pct  uint8
	drv  uint8 // index into History.drivers
	mods uint8
}

type point struct {
	t        uint32 // unix seconds, start of the step
	cpu, cas int16  // tenths of °C
	fans     [maxFans]fanPoint
}

// FanSample is what one tick knows about one fan. Pct and RPM are -1 when unknown.
type FanSample struct {
	Pct, RPM int
	Drive    Drive
}

// Sample is one tick. CPU and Case are the CPU and the case-driving temperature (hottest drive), nil when unknown.
type Sample struct {
	At       time.Time
	CPU      *int
	CaseTemp *int
	Fans     map[string]FanSample
}

type ring struct {
	pts  []point
	next int
	n    int
}

func newRing(size int) ring { return ring{pts: make([]point, size)} }

func (r *ring) add(p point) {
	r.pts[r.next] = p
	r.next = (r.next + 1) % len(r.pts)
	r.n = min(r.n+1, len(r.pts))
}

// each calls f from the oldest point to the newest.
func (r *ring) each(f func(point)) {
	for i := range r.n {
		f(r.pts[(r.next-r.n+i+len(r.pts))%len(r.pts)])
	}
}

// acc averages the points of one step. Temperatures and speeds are averaged, the modifiers are or-ed, the driver is
// the one that held the most ticks (an emergency wins over everything: it must not average away).
type acc struct {
	bucket int64
	n      int
	cpu    avg
	cas    avg
	fans   [maxFans]fanAcc
}

type avg struct{ sum, n int }

func (a *avg) add(v int, unknown int) {
	if v != unknown {
		a.sum += v
		a.n++
	}
}

func (a avg) value(unknown int) int {
	if a.n == 0 {
		return unknown
	}
	return int(math.Round(float64(a.sum) / float64(a.n)))
}

type fanAcc struct {
	pct, rpm, temp avg
	drv            [maxDrivers + 1]uint16
	emergency      uint8
	hasEmergency   bool
	mods           uint8
}

func (a *acc) add(bucket int64, p point) {
	a.bucket = bucket
	a.n++
	a.cpu.add(int(p.cpu), unknownTemp)
	a.cas.add(int(p.cas), unknownTemp)
	for i, f := range p.fans {
		fa := &a.fans[i]
		fa.pct.add(int(f.pct), unknownPct)
		fa.rpm.add(int(f.rpm), unknownRPM)
		fa.temp.add(int(f.temp), unknownTemp)
		fa.mods |= f.mods
		if f.drv != noDriver {
			fa.drv[f.drv]++
			if f.mods&ModEmergency != 0 {
				fa.emergency, fa.hasEmergency = f.drv, true
			}
		}
	}
}

// point returns the average without resetting it.
func (a *acc) point(step int64) point {
	p := point{t: uint32(a.bucket * step), cpu: int16(a.cpu.value(unknownTemp)), cas: int16(a.cas.value(unknownTemp))}
	for i := range p.fans {
		fa := &a.fans[i]
		best, bestN := uint8(noDriver), uint16(0)
		for d, n := range fa.drv {
			if n > bestN {
				best, bestN = uint8(d), n
			}
		}
		if fa.hasEmergency {
			best = fa.emergency
		}
		p.fans[i] = fanPoint{
			rpm: uint16(fa.rpm.value(unknownRPM)), temp: int16(fa.temp.value(unknownTemp)),
			pct: uint8(fa.pct.value(unknownPct)), drv: best, mods: fa.mods,
		}
	}
	return p
}

// History is safe for one writer (the daemon loop) and any number of readers.
type History struct {
	mu      sync.Mutex
	ids     []string
	drivers []string
	index   map[string]uint8
	started int64

	fine, coarse       ring
	fineAcc, coarseAcc acc
}

// NewHistory records the fans in ids (at most maxFans), in that order.
func NewHistory(ids []string, start time.Time) *History {
	if len(ids) > maxFans {
		ids = ids[:maxFans]
	}
	h := &History{
		ids: ids, index: map[string]uint8{}, started: start.Unix(),
		fine: newRing(fineLen), coarse: newRing(coarseLen),
	}
	h.driverIndex("other") // index 0: what does not fit the table
	return h
}

func (h *History) driverIndex(src string) uint8 {
	if i, ok := h.index[src]; ok {
		return i
	}
	if len(h.drivers) >= maxDrivers {
		return 0
	}
	i := uint8(len(h.drivers))
	h.drivers = append(h.drivers, src)
	h.index[src] = i
	return i
}

func tenths(t *int) int16 {
	if t == nil {
		return unknownTemp
	}
	return int16(*t * 10)
}

// Record adds one tick.
func (h *History) Record(s Sample) {
	h.mu.Lock()
	defer h.mu.Unlock()

	p := point{cpu: tenths(s.CPU), cas: tenths(s.CaseTemp)}
	for i := range p.fans {
		p.fans[i] = fanPoint{rpm: unknownRPM, temp: unknownTemp, pct: unknownPct, drv: noDriver}
	}
	for i, id := range h.ids {
		f, ok := s.Fans[id]
		if !ok {
			continue
		}
		fp := &p.fans[i]
		if f.Pct >= 0 {
			fp.pct = uint8(min(f.Pct, 100))
		}
		if f.RPM >= 0 {
			fp.rpm = uint16(min(f.RPM, unknownRPM-1))
		}
		if f.Drive.Temp != nil {
			fp.temp = int16(math.Round(*f.Drive.Temp * 10))
		}
		if f.Drive.Source != "" {
			fp.drv = h.driverIndex(f.Drive.Source)
		}
		fp.mods = f.Drive.Mods
	}

	at := s.At.Unix()
	if b := at / fineStep; h.fineAcc.n > 0 && b != h.fineAcc.bucket {
		h.flushFine()
	}
	h.fineAcc.add(at/fineStep, p)
}

// flushFine closes the fine step and feeds the coarse one.
func (h *History) flushFine() {
	fp := h.fineAcc.point(fineStep)
	h.fine.add(fp)
	h.fineAcc = acc{}

	cb := int64(fp.t) / coarseStep
	if h.coarseAcc.n > 0 && cb != h.coarseAcc.bucket {
		h.coarse.add(h.coarseAcc.point(coarseStep))
		h.coarseAcc = acc{}
	}
	h.coarseAcc.add(cb, fp)
}

// HistoryAnswer is what the web page gets (api.php action fan_history): arrays, one entry per point. A temperature
// is °C with one decimal, null when unknown; Drv indexes Drivers, -1 for none; Mods is the modifier bit set (Mod*).
type HistoryAnswer struct {
	OK      bool            `json:"ok"`
	Running bool            `json:"running"`
	Range   string          `json:"range,omitempty"`
	Step    int             `json:"step,omitempty"`
	Now     int64           `json:"now,omitempty"`
	Since   int64           `json:"since,omitempty"`
	Fans    []HistoryFan    `json:"fans,omitempty"`
	Drivers []string        `json:"drivers,omitempty"`
	T       []int64         `json:"t,omitempty"`
	CPU     []*float64      `json:"cpu,omitempty"`
	Case    []*float64      `json:"case,omitempty"`
	Series  []HistorySeries `json:"series,omitempty"`
}

type HistoryFan struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type HistorySeries struct {
	ID   string     `json:"id"`
	Pct  []*int     `json:"pct"`
	RPM  []*int     `json:"rpm"`
	Temp []*float64 `json:"temp"`
	Drv  []int      `json:"drv"`
	Mods []int      `json:"mods"`
}

// Ranges the page may ask for.
const (
	Range1h  = "1h"
	Range24h = "24h"
)

func degrees(v int16) *float64 {
	if v == unknownTemp {
		return nil
	}
	f := float64(v) / 10
	return &f
}

// Answer returns the history of a range ("1h" or "24h"), oldest first, with the running step as its last point.
func (h *History) Answer(rng string, now time.Time) HistoryAnswer {
	h.mu.Lock()
	defer h.mu.Unlock()

	r, step, cur, curStep := &h.fine, fineStep, &h.fineAcc, int64(fineStep)
	if rng == Range24h {
		r, step, cur, curStep = &h.coarse, coarseStep, &h.coarseAcc, int64(coarseStep)
	} else {
		rng = Range1h
	}
	a := HistoryAnswer{
		OK: true, Running: true, Range: rng, Step: step, Now: now.Unix(), Since: h.started,
		Drivers: append([]string(nil), h.drivers...),
		T:       []int64{}, CPU: []*float64{}, Case: []*float64{},
	}
	for _, id := range h.ids {
		a.Fans = append(a.Fans, HistoryFan{ID: id, Role: roles[id]})
		a.Series = append(a.Series, HistorySeries{ID: id, Pct: []*int{}, RPM: []*int{}, Temp: []*float64{}, Drv: []int{}, Mods: []int{}})
	}
	add := func(p point) {
		a.T = append(a.T, int64(p.t))
		a.CPU = append(a.CPU, degrees(p.cpu))
		a.Case = append(a.Case, degrees(p.cas))
		for i := range h.ids {
			f, s := p.fans[i], &a.Series[i]
			var pct, rpm *int
			if f.pct != unknownPct {
				v := int(f.pct)
				pct = &v
			}
			if f.rpm != unknownRPM {
				v := int(f.rpm)
				rpm = &v
			}
			drv := -1
			if f.drv != noDriver {
				drv = int(f.drv)
			}
			s.Pct, s.RPM, s.Temp = append(s.Pct, pct), append(s.RPM, rpm), append(s.Temp, degrees(f.temp))
			s.Drv, s.Mods = append(s.Drv, drv), append(s.Mods, int(f.mods))
		}
	}
	r.each(add)
	if cur.n > 0 {
		add(cur.point(curStep))
	}
	return a
}

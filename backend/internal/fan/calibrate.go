package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

// Calibration measures the rpm of every fan at fixed speeds. The table lives next to fan.cfg (it must survive a
// reboot), the progress in /var/run. While a measurement runs the daemon keeps running but leaves the fans alone:
// it pauses as long as the pause file is fresh, so a crashed measurement hands control back within seconds.
var (
	calibPath      = envOr("UGREEN_FAN_CALIB", filepath.Join(filepath.Dir(configPath), "fan_rpm.json"))
	calibStatePath = envOr("UGREEN_FAN_CALIB_STATE", "/var/run/ugreen-nas-control.calibrate.json")
	calibPausePath = envOr("UGREEN_FAN_CALIB_PAUSE", "/var/run/ugreen-nas-control.calibrate.pause")
	calibLockPath  = envOr("UGREEN_FAN_CALIB_LOCK", "/var/run/ugreen-nas-control.calibrate.lock")
)

const (
	// Speeds measured, in percent; the other fans hold calibHold meanwhile.
	calibHold = 50
	// The CPU may get this warm during a measurement, however high the configured emergency limit is.
	calibCPUMax = 80
	// A reading above calibCPUMax only stops the measurement after this many checks in a row (a CPU burst of a
	// second or two is normal); at calibCPUHard and above it stops at once.
	calibHotReads = 4
	calibCPUHard  = 95

	pauseFresh   = 15 * time.Second // the daemon stays off the fans this long after the last heartbeat
	progressFree = 45 * time.Second // a "running" progress older than this means the measurement died
	daemonFresh  = 10 * time.Second // the daemon writes its state every tick

	heartbeatStall = 30 * time.Second // no progress update for this long: stop pausing the daemon
	calibCheck     = time.Second      // temperatures are checked this often while a step settles
)

var calibSteps = []int{10, 15, 20, 25, 30, 40, 50, 60, 70, 80, 90, 100}

// stepsFrom drops the steps below floor: the hardware runs those at the floor, so they would record the same rpm.
func stepsFrom(floor int) []int {
	return slices.DeleteFunc(slices.Clone(calibSteps), func(p int) bool { return p < floor })
}

// CalibTable: per fan the measured (percent, rpm) pairs, rising in percent.
type CalibTable struct {
	Time int64               `json:"time"`
	Fans map[string][][2]int `json:"fans"`
}

// CalibProgress state: running, done or aborted (Reason says why: stopped, daemon_stopped, cpu_hot,
// cpu_unreadable, disk_hot, read_error, busy, fan_control_off, no_daemon, hardware).
type CalibProgress struct {
	State  string `json:"state"`
	Fan    string `json:"fan,omitempty"`
	Pct    int    `json:"pct,omitempty"`
	Step   int    `json:"step"`
	Steps  int    `json:"steps"`
	Reason string `json:"reason,omitempty"`
	Time   int64  `json:"time"`
}

type CalibStatus struct {
	Table    *CalibTable    `json:"table"`
	Progress *CalibProgress `json:"progress"`
}

func fresh(path string, within time.Duration) bool {
	fi, err := os.Stat(path)
	return err == nil && time.Since(fi.ModTime()) < within
}

// calibPaused: a measurement is driving the fans, the daemon must not write.
func calibPaused() bool { return fresh(calibPausePath, pauseFresh) }

func writeJSONAtomic(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsx.WriteFile(path, b, 0o644)
}

// startHeartbeat beats now and every interval while ok() holds; the returned stop waits until no beat is
// in flight. A failing ok() lets the pause go stale, so the daemon resumes when the measurement hangs.
func startHeartbeat(beat func(), interval time.Duration, ok func() bool) (stop func()) {
	beat()
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if ok() {
					beat()
				}
			}
		}
	}()
	return func() { close(done); <-exited }
}

type tableCache struct {
	mod  time.Time
	size int64
	tbl  *CalibTable
}

func (c *tableCache) get(path string) *CalibTable {
	fi, err := os.Stat(path)
	if err != nil {
		c.mod, c.size, c.tbl = time.Time{}, 0, nil
		return nil
	}
	if !fi.ModTime().Equal(c.mod) || fi.Size() != c.size {
		c.mod, c.size, c.tbl = fi.ModTime(), fi.Size(), nil
		if b, err := os.ReadFile(path); err == nil {
			var t CalibTable
			if json.Unmarshal(b, &t) == nil && len(t.Fans) > 0 {
				c.tbl = &t
			}
		}
	}
	return c.tbl
}

// stallStart: the lowest percent at which the fan was measured to turn, else stallDefaultPct.
func stallStart(t *CalibTable, fan string) int {
	if t != nil {
		for _, pr := range t.Fans[fan] {
			if pr[1] > 0 {
				return pr[0]
			}
		}
	}
	return stallDefaultPct
}

func readCalibStatus() CalibStatus {
	var out CalibStatus
	if b, err := os.ReadFile(calibPath); err == nil {
		var t CalibTable
		if json.Unmarshal(b, &t) == nil && len(t.Fans) > 0 {
			out.Table = &t
		}
	}
	if b, err := os.ReadFile(calibStatePath); err == nil {
		var p CalibProgress
		if json.Unmarshal(b, &p) == nil {
			if p.State == "running" && time.Since(time.Unix(p.Time, 0)) > progressFree {
				p.State, p.Reason = "aborted", "stopped"
			}
			out.Progress = &p
		}
	}
	return out
}

func median3(v [3]int) int {
	s := v[:]
	sort.Ints(s)
	return s[1]
}

type calibrator struct {
	chip        Fans
	floor       int // steps below it are skipped
	cfg         func() Config
	sensors     func() Sensors
	daemonAlive func() bool
	stop        <-chan struct{}
	progress    func(CalibProgress)
	logf        func(string, ...any)
	// pet feeds an armed EC watchdog while the daemon is paused (nil: nothing to feed).
	pet func()

	settle, settleBig, gap time.Duration
	check                  time.Duration // zero: calibCheck

	hotRuns, peak int // checks in a row above the CPU ceiling; the hottest CPU reading of the run
}

// wait sleeps for d, checking the temperatures on the way; it gives the reason to stop, or "" after d.
func (cb *calibrator) wait(d time.Duration) string {
	every := cb.check
	if every <= 0 {
		every = calibCheck
	}
	end := time.Now().Add(d)
	for {
		left := time.Until(end)
		if left <= 0 {
			return ""
		}
		select {
		case <-cb.stop:
			return "stopped"
		case <-time.After(min(left, every)):
		}
		if cb.pet != nil {
			cb.pet()
		}
		if r := cb.unsafe(); r != "" {
			return r
		}
	}
}

var errBusy = errors.New("busy")

// lockCalibration takes the lock for a whole measurement; the kernel drops it when the process dies.
func lockCalibration() (*os.File, error) {
	f, err := fsx.OpenLock(calibLockPath, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errBusy
		}
		return nil, err
	}
	return f, nil
}

// unsafe gives the reason to stop, or "" when the temperatures are fine. Emergency rules come from Decide;
// the only extra is the CPU ceiling calibCPUMax. NVMe heat reports as disk_hot (the UI has no separate key).
func (cb *calibrator) unsafe() string {
	select {
	case <-cb.stop:
		return "stopped"
	default:
	}
	if !cb.daemonAlive() {
		return "daemon_stopped"
	}
	c, s := cb.cfg(), cb.sensors()
	reason := Decide(c, s).Failsafe
	if s.CPU != nil {
		cb.peak = max(cb.peak, *s.CPU)
		if reason == "" && *s.CPU > calibCPUMax {
			reason = "cpu_hot"
		}
	}
	switch reason {
	case "":
		cb.hotRuns = 0
		return ""
	case "nvme_hot":
		return "disk_hot"
	case "cpu_hot":
		cb.hotRuns++
		if cb.hotRuns < calibHotReads && *s.CPU < calibCPUHard {
			return ""
		}
		cb.logf("calibration: CPU too hot (%d checks in a row above the ceiling, hottest reading %d °C)", cb.hotRuns, cb.peak)
	}
	return reason
}

func (cb *calibrator) setAll(pct int, only string) error {
	return withLock(func() error {
		for _, ch := range cb.chip.Outputs() {
			p := pct
			if only != "" && ch.ID != only {
				p = calibHold
			}
			if err := cb.chip.SetPWM(ch, percentToDuty(p, cb.chip.MaxDuty())); err != nil {
				return err
			}
		}
		return nil
	})
}

// run sweeps every fan; the table is only complete when reason is "".
func (cb *calibrator) run() (CalibTable, string) {
	tbl := CalibTable{Fans: map[string][][2]int{}}
	outs := cb.chip.Outputs()
	steps := stepsFrom(cb.floor)
	total, step := len(outs)*len(steps), 0

	fail := func(reason string) (CalibTable, string) {
		// A stopped daemon has handed the fans back already; do not leave them in manual mode.
		if reason != "daemon_stopped" {
			_ = cb.setAll(100, "")
		}
		return tbl, reason
	}

	for _, ch := range outs {
		prev := 100
		for _, pct := range steps {
			if r := cb.unsafe(); r != "" {
				return fail(r)
			}
			step++
			cb.progress(CalibProgress{State: "running", Fan: ch.ID, Pct: pct, Step: step, Steps: total, Time: time.Now().Unix()})

			if err := cb.setAll(pct, ch.ID); err != nil {
				cb.logf("calibration: fan write failed: %v", err)
				return fail("hardware")
			}
			d := cb.settle
			if prev-pct >= 30 {
				d = cb.settleBig
			}
			prev = pct
			if r := cb.wait(d); r != "" {
				return fail(r)
			}

			var reads [3]int
			for i := range reads {
				var rpm int
				err := withLock(func() (err error) { rpm, err = cb.chip.RPM(ch); return })
				if err != nil {
					cb.logf("calibration: rpm read failed: %v", err)
					return fail("read_error")
				}
				reads[i] = rpm
				if i < 2 {
					if r := cb.wait(cb.gap); r != "" {
						return fail(r)
					}
				}
			}
			tbl.Fans[ch.ID] = append(tbl.Fans[ch.ID], [2]int{pct, median3(reads)})
		}
	}
	tbl.Time = time.Now().Unix()
	return tbl, ""
}

// handBack leaves the fans safe. After a crash they go to 100 % first (the last step may be slow). With
// "bios" mode, a stopped daemon or no fresh daemon state nobody else would take them, so restore here.
func handBack(cb *calibrator, bios Snapshot, crashed bool, reason string, daemonUp, biosMode bool) {
	if crashed {
		if err := cb.setAll(100, ""); err != nil {
			cb.logf("calibration: cannot set the fans to 100 %%: %v", err)
		}
	}
	if reason == "daemon_stopped" || biosMode || !daemonUp {
		_ = withLockStop(func() error { return restoreRetry(cb.chip, bios, restoreTries, restoreBackoff, cb.logf) })
	}
}

// runCalibrate is the `calibrate` command: blocks until done, always hands the fans back to the daemon.
func runCalibrate(chip Fans) (err error) {
	// A second start must not touch the progress of the running one.
	lock, err := lockCalibration()
	if errors.Is(err, errBusy) {
		return fmt.Errorf("not started: busy")
	}
	if err != nil {
		return fmt.Errorf("not started: %w", err)
	}
	defer func() { _ = lock.Close() }()

	logf := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
	steps := len(chip.Outputs()) * len(stepsFrom(minPctFloor(productName())))
	report := func(p CalibProgress) { _ = writeJSONAtomic(calibStatePath, p) }
	abort := func(reason string) error {
		report(CalibProgress{State: "aborted", Reason: reason, Steps: steps, Time: time.Now().Unix()})
		return fmt.Errorf("not started: %s", reason)
	}

	c := LoadConfig(configPath)
	if !c.FanControl {
		return abort("fan_control_off")
	}
	if !fresh(statePath, daemonFresh) {
		return abort("no_daemon")
	}

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, daemonSignals...)
	go func() {
		<-sig
		close(stop)
	}()

	// Fans are still untouched here: without a BIOS snapshot there is nothing to hand back, so don't start.
	var bios Snapshot
	if err := withLock(func() (err error) { bios, err = saveBIOS(chip); return }); err != nil || bios == nil {
		logf("calibration: cannot save the BIOS fan settings: %v", err)
		return abort("hardware")
	}

	// Pause the daemon; the heartbeat keeps the pause alive and dies with this process. It stops beating
	// when the measurement made no progress for heartbeatStall, so the daemon takes over a hung run.
	var lastProgress atomic.Int64
	lastProgress.Store(time.Now().UnixNano())
	progress := func(p CalibProgress) {
		lastProgress.Store(time.Now().UnixNano())
		report(p)
	}
	beat := func() { _ = fsx.WriteFile(calibPausePath, []byte(time.Now().Format(time.RFC3339)), 0o644) }
	stopBeat := startHeartbeat(beat, 2*time.Second, func() bool {
		return time.Since(time.Unix(0, lastProgress.Load())) < heartbeatStall
	})

	cb := &calibrator{
		chip: chip, floor: minPctFloor(productName()), cfg: func() Config { return LoadConfig(configPath) }, sensors: ReadSensors,
		daemonAlive: func() bool { return fresh(statePath, daemonFresh) },
		stop:        stop, progress: progress, logf: logf,
		settle: 8 * time.Second, settleBig: 15 * time.Second, gap: time.Second,
	}
	petter := &Petter{}
	cb.pet = func() {
		_ = withLock(func() error { _, _, err := petter.Tick(chip, time.Now()); return err })
	}

	var tbl CalibTable
	reason := "hardware"
	defer func() {
		stopBeat()
		crashed := false
		if r := recover(); r != nil {
			crashed = true
			reason = "hardware"
			err = fmt.Errorf("aborted: crashed: %v", r)
			logf("calibration crashed: %v", r)
		}
		finish := CalibProgress{State: "aborted", Reason: reason, Steps: steps, Time: time.Now().Unix()}
		if reason == "" {
			finish = CalibProgress{State: "done", Step: steps, Steps: steps, Time: time.Now().Unix()}
		}
		report(finish)
		_ = os.Remove(calibPausePath)
		handBack(cb, bios, crashed, reason, fresh(statePath, daemonFresh), LoadConfig(configPath).Mode == "bios")
	}()

	tbl, reason = cb.run()
	if reason != "" {
		return fmt.Errorf("aborted: %s", reason)
	}
	if err := writeJSONAtomic(calibPath, tbl); err != nil {
		reason = "hardware"
		return fmt.Errorf("cannot save %s: %w", calibPath, err)
	}
	return nil
}

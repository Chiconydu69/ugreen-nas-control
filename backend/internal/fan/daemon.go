package fan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/syslog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

var (
	configPath = envOr("UGREEN_FAN_CONFIG", "/boot/config/plugins/ugreen-nas-control/fan.cfg")
	statePath  = envOr("UGREEN_FAN_STATE", "/var/run/ugreen-nas-control.json")
	biosPath   = envOr("UGREEN_FAN_BIOS", "/var/run/ugreen-nas-control.bios")
	notifyCmd  = "/usr/local/emhttp/webGui/scripts/notify"
)

var (
	tick = 2 * time.Second

	readSensors = ReadSensors
)

const (
	// Going up follows the curve at once; going down at most this much per tick,
	// so the fans don't hunt up and down.
	stepDown = 2

	// A fan that shows 0 rpm for this many ticks while driven counts as stalled.
	stallTicks = 8

	// Stall check applies from this speed up unless fan_rpm.json shows the fan turning lower.
	stallDefaultPct = 20
)

// Role of each fan: fan2 to fan4 on the DXP6800 Pro, fan1 and fan3 on the DXP4800 GT.
var roles = map[string]string{"fan1": "cpu", "fan2": "cpu", "fan3": "case", "fan4": "case"}

type State struct {
	Time       int64          `json:"time"`
	Mode       string         `json:"mode"`
	Failsafe   string         `json:"failsafe"`
	CPUTemp    *int           `json:"cpu_temp"`
	CaseTemp   *int           `json:"case_temp"`
	CaseFrom   string         `json:"case_from"`
	CaseDriver string         `json:"case_driver"`
	Targets    map[string]int `json:"targets"`
	Stalled    []string       `json:"stalled"`
	ChipTemps  []*int         `json:"chip_temps"`
	// Conflict: another fan controller is running, so this daemon only reads (see yield). Unapplied is the
	// emergency it would have answered with 100 %, left to the rival.
	Conflict  []string `json:"conflict,omitempty"`
	Unapplied string   `json:"unapplied,omitempty"`
	// InputsMissing: configured inputs that could not be read this tick (their group used its on_missing rule).
	InputsMissing []string `json:"inputs_missing,omitempty"`
	// QuietActive: the quiet window caps the fans; Cap is that cap in %, null otherwise. Job: "mover", "parity" or "".
	QuietActive bool   `json:"quiet_active"`
	Job         string `json:"job"`
	Cap         *int   `json:"cap"`
}

// Decision is what one tick wants from the fans, apart from smoothing.
type Decision struct {
	CPU      int
	Case     int
	Failsafe string
	CaseTemp *int
	CaseFrom string
	// CaseDriver says which curve set the case target: "case" or, with case_follows_cpu, "cpu".
	CaseDriver string
}

// Decide applies the curves, the minimum speed and the emergency rules.
func Decide(c Config, s Sensors) Decision {
	var d Decision

	disk, nvme := s.MaxDiskExcept(c.IgnoreDrives), s.MaxNVMeExcept(c.IgnoreDrives)
	switch {
	case disk != nil && (nvme == nil || *disk >= *nvme-c.NVMeOffset):
		d.CaseTemp, d.CaseFrom = disk, "disk"
	case nvme != nil:
		t := *nvme - c.NVMeOffset
		d.CaseTemp, d.CaseFrom = &t, "nvme"
	}

	switch {
	case s.CPU == nil:
		d.Failsafe = "cpu_unreadable"
	case *s.CPU >= c.CPUCrit:
		d.Failsafe = "cpu_hot"
	case disk != nil && *disk >= c.DiskCrit:
		d.Failsafe = "disk_hot"
	case nvme != nil && *nvme-c.NVMeOffset >= c.DiskCrit:
		d.Failsafe = "nvme_hot"
	}
	if d.Failsafe != "" {
		d.CPU, d.Case = 100, 100
		return d
	}

	d.CPU = c.CPUCurve.At(float64(*s.CPU))
	d.CaseDriver = "case"
	if d.CaseTemp != nil {
		d.Case = c.CaseCurve.At(float64(*d.CaseTemp))
	} else {
		// All disks asleep and no NVMe: the lowest point of the curve.
		d.Case = c.CaseCurve.At(-1)
	}

	if c.CaseFollowsCPU && d.CPU > d.Case {
		d.Case, d.CaseDriver = d.CPU, "cpu"
	}

	if d.CPU < c.MinPct {
		d.CPU = c.MinPct
	}
	if d.Case < c.MinPct {
		d.Case = c.MinPct
	}
	return d
}

func Smooth(current, target int) int {
	if current < 0 || target >= current {
		return target
	}
	if current-target > stepDown {
		return current - stepDown
	}
	return target
}

func saveBIOS(chip Fans) (Snapshot, error) {
	if b, err := os.ReadFile(biosPath); err == nil {
		var s Snapshot
		if json.Unmarshal(b, &s) == nil && len(s) > 0 {
			return s, nil
		}
	}
	s, err := chip.Snapshot()
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(s)
	return s, fsx.WriteFile(biosPath, b, 0o644)
}

// Restore attempts on exit; the backoff is a variable so that tests can shorten it.
const restoreTries = 3

var restoreBackoff = 200 * time.Millisecond

// restoreRetry hands the fans back, trying again after a failure. The caller
// holds the lock.
func restoreRetry(chip Fans, bios Snapshot, tries int, backoff time.Duration, logf func(string, ...any)) (err error) {
	for i := 1; i <= tries; i++ {
		if err = chip.Restore(bios); err == nil {
			return nil
		}
		logf("restoring the BIOS fan settings failed (attempt %d of %d): %v", i, tries, err)
		if i < tries {
			time.Sleep(time.Duration(i) * backoff)
		}
	}
	return err
}

// release hands the fans back once; current is only cleared when that worked,
// so a failed restore is retried on the next tick.
// now is the clock of the schedule; tests replace it.
var now = time.Now

func outputIDs(list []Channel) []string {
	ids := make([]string, len(list))
	for i, ch := range list {
		ids[i] = ch.ID
	}
	return ids
}

func release(chip Fans, bios Snapshot, current map[string]int) error {
	if len(current) == 0 {
		return nil
	}
	if err := chip.Restore(bios); err != nil {
		return err
	}
	clear(current)
	return nil
}

// daemonSignals end the daemons like SIGTERM: a hangup (closed terminal, supervisor) or a quit must hand the fans back too.
var daemonSignals = []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT}

// endFanControl ends fan control: the watchdog feeding stops and the BIOS values go back, unless a rival tool took the fans
// over (writing then would override it).
func endFanControl(chip Fans, bios Snapshot, pet *Petter, rivals []conflict.Conflict, logf func(string, ...any)) {
	if len(rivals) > 0 {
		_ = withLockStop(func() error { stopWatchdog(pet, chip, logf); return nil })
		_ = os.Remove(statePath)
		logf("fan control stopped, fans left to %s", rivalNames(rivals))
		return
	}
	err := withLockStop(func() error {
		stopWatchdog(pet, chip, logf)
		return restoreRetry(chip, bios, restoreTries, restoreBackoff, logf)
	})
	_ = os.Remove(statePath)
	if err != nil {
		logf("fan control stopped, BIOS fan settings NOT restored: %v", err)
		return
	}
	logf("fan control stopped, BIOS fan settings restored")
}

var daemonModel = productName

func runDaemon(chip Fans) (runErr error) {
	logger, _ := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "ugreen-nas-control")
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if logger != nil {
			_ = logger.Info(msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
	}

	model := daemonModel()
	if FanKind(model) == KindReadOnly {
		return fmt.Errorf("%s is read only: fan control is off, the fans are only shown", model)
	}
	var bios Snapshot
	if err := withLock(func() (err error) { bios, err = saveBIOS(chip); return }); err != nil {
		return fmt.Errorf("cannot save the BIOS fan settings: %w", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, daemonSignals...)
	// stopped closes on the first signal and cancels ctx, so a slow input read does not hold the shutdown up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		select {
		case <-sig:
			close(stopped)
			cancel()
		case <-ctx.Done():
		}
	}()

	current := map[string]int{}
	zero := map[string]int{}
	notified := map[string]bool{}
	var calibCache tableCache
	lastMode, lastFailsafe := "", ""
	var y yielding
	k := newController()
	pet := &Petter{}
	probe := newJobProbe()
	// A panic in the loop must not leave the fans at the last written speed.
	defer func() {
		if r := recover(); r != nil {
			logf("fan control crashed: %v", r)
			endFanControl(chip, bios, pet, conflict.Blocking(detectConflicts(), conflict.Fan), logf)
			runErr = fmt.Errorf("fan control crashed: %v", r)
		}
	}()
	lastStep := time.Time{}
	lastJob, lastQuiet := "", false
	hist := NewHistory(outputIDs(chip.Outputs()), now())
	defer serveHistory(historyPath, hist, logf)()

	logf("fan control started (%s)", chip.Describe())

	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	warned := map[string]bool{} // invalid settings are logged once
	for {
		// The model's limits cap the user's: the stricter crit value wins (ApplyModelLimits never raises one).
		c := ApplyModelLimits(LoadConfig(configPath), model)
		for _, w := range c.Warnings {
			if !warned[w] {
				warned[w] = true
				logf("%s", w)
			}
		}
		s := readSensors()
		st := State{Time: time.Now().Unix(), Mode: c.Mode, CPUTemp: s.CPU, Targets: map[string]int{}}

		if c.Mode != lastMode {
			logf("fan mode: %s", c.Mode)
			lastMode = c.Mode
		}

		found := detectConflicts()
		rivals := conflict.Blocking(found, conflict.Fan)
		pet.Yield = conflict.Present(found, conflict.Watchdog)
		drives := map[string]Drive{} // what drove each fan this tick, for the history
		driveAll := func(src string) {
			for _, ch := range chip.Outputs() {
				drives[ch.ID] = Drive{Source: src}
			}
		}

		// Everything slow happens before the lock: user scripts, nvidia-smi, storcli and pgrep never hold it. The registry keeps
		// the answers for the per-tick cache, so the locked part below only reads them back.
		var in InputSource
		var jobs JobStatus
		if len(rivals) == 0 && c.Mode != "bios" && !calibPaused() {
			in = newInputSource(c, s)
			if Decide(c, s).Failsafe == "" { // an emergency never reads an input: nothing delays its 100 %
				warmInputs(ctx, in, c, outputIDs(chip.Outputs()))
			}
			if c.Mover.Mode != "off" || c.Parity.Mode != "off" {
				jobs = probe.Status(now(), c.Mover.Mode != "off", c.Parity.Mode != "off")
			}
		}
		select {
		case <-stopped:
			endFanControl(chip, bios, pet, rivals, logf)
			return nil
		default:
		}

		err := withLock(func() error {
			// Feed the EC or IT8613E watchdog if the BIOS armed it, on every path below (also while yielding to a rival, in bios
			// mode, during a calibration and in an emergency). Never arms it; rate-limited by the petter.
			petWatchdog(pet, chip, now(), logf)
			if len(rivals) > 0 {
				yield(&y, c, s, rivals, &st, logf)
				driveAll(SrcRival)
				clear(current)
				k.reset()
				return nil
			}
			y.reset(logf)

			// "bios": hand the fans back and only watch.
			if c.Mode == "bios" {
				k.reset()
				driveAll(SrcBIOS)
				return release(chip, bios, current)
			}

			// A calibration drives the fans; after it the speeds are set from scratch.
			if calibPaused() {
				// Keep reporting the case temperature for the web page. Shaping, jobs and quiet hours rest too.
				d := Decide(c, s)
				st.CaseTemp, st.CaseFrom = d.CaseTemp, d.CaseFrom
				driveAll(SrcCalibration)
				clear(current)
				k.reset()
				lastStep = time.Time{}
				return nil
			}

			at := now()
			dt := tick
			if !lastStep.IsZero() {
				dt = min(max(at.Sub(lastStep), 0), 4*tick)
			}
			lastStep = at

			if in == nil { // the state changed since the check above: only the tick's own sensors, no slow input
				in = sensorSource{c, s}
			}
			plan := k.step(ctx, tickIn{
				Cfg: c, Sens: s, In: in, Calib: calibCache.get(calibPath),
				Now: at, Dt: dt, Jobs: jobs, IDs: outputIDs(chip.Outputs()),
			})
			d := plan.Decision
			drives = plan.Drive
			st.Failsafe, st.CaseTemp, st.CaseFrom, st.CaseDriver = d.Failsafe, d.CaseTemp, d.CaseFrom, d.CaseDriver
			st.InputsMissing, st.QuietActive, st.Job, st.Cap = plan.Missing, plan.QuietActive, plan.Job, plan.Cap
			if d.Failsafe != lastFailsafe {
				if d.Failsafe != "" {
					logf("emergency: %s, all fans at 100%%", d.Failsafe)
				} else {
					logf("emergency over, back to the curves")
				}
				lastFailsafe = d.Failsafe
			}
			if plan.Job != lastJob {
				if plan.Job != "" {
					logf("%s running, case fans raised", plan.Job)
				} else {
					logf("job over, back to the curves")
				}
				lastJob = plan.Job
			}
			if plan.QuietActive != lastQuiet {
				if plan.QuietActive {
					logf("quiet hours: fans capped at %d%%", c.Quiet.Cap)
				} else if d.Failsafe == "" {
					logf("quiet hours over")
				}
				lastQuiet = plan.QuietActive
			}

			// One failing output must not keep the others from their speed (an emergency 100 % included).
			var werrs []error
			for _, ch := range chip.Outputs() {
				next := plan.Out[ch.ID]
				if err := chip.SetPWM(ch, percentToDuty(next, chip.MaxDuty())); err != nil {
					werrs = append(werrs, fmt.Errorf("%s: %w", ch.ID, err))
					delete(current, ch.ID)
					continue
				}
				current[ch.ID] = next
				st.Targets[ch.ID] = next
			}
			if len(werrs) > 0 {
				k.reset()
			}
			return errors.Join(werrs...)
		})
		if err != nil {
			logf("fan write failed: %v", err)
		}

		calib := calibCache.get(calibPath)
		sample := Sample{At: now(), CPU: s.CPU, CaseTemp: st.CaseTemp, Fans: map[string]FanSample{}}
		if !errors.Is(err, hw.ErrLockTimeout) { // the lock is busy: no second wait in the same tick
			_ = withLock(func() error {
				for _, ch := range chip.Outputs() {
					rpm, err := chip.RPM(ch)
					sample.Fans[ch.ID] = fanSample(chip, ch, rpm, err == nil, current, drives[ch.ID])
					if err != nil {
						continue
					}
					if rpm == 0 && current[ch.ID] >= stallStart(calib, ch.ID) {
						zero[ch.ID]++
					} else {
						zero[ch.ID] = 0
						notified[ch.ID] = false
					}
					if zero[ch.ID] >= stallTicks {
						st.Stalled = append(st.Stalled, ch.ID)
						if !notified[ch.ID] {
							notified[ch.ID] = true
							logf("%s stands still", ch.ID)
							if c.StallNotify {
								notify(ch.ID)
							}
						}
					}
				}
				st.ChipTemps = chip.Temps()
				return nil
			})
		}

		hist.Record(sample)
		if b, err := json.Marshal(st); err == nil {
			_ = fsx.WriteFile(statePath, b, 0o644)
		}

		select {
		case <-stopped:
			endFanControl(chip, bios, pet, rivals, logf)
			return nil
		case <-ticker.C:
		}
	}
}

// fanSample is one fan for the history: the speed the daemon wrote, or what the chip reports while the daemon does not
// drive it (BIOS mode, a rival, a calibration); -1 where unknown.
func fanSample(chip Fans, ch Channel, rpm int, rpmOK bool, current map[string]int, d Drive) FanSample {
	f := FanSample{Pct: -1, RPM: -1, Drive: d}
	if rpmOK {
		f.RPM = rpm
	}
	if pct, ok := current[ch.ID]; ok {
		f.Pct = pct
	} else if duty, auto, err := chip.PWM(ch); err == nil && (!auto || chip.AutoDutyKnown()) {
		f.Pct = dutyToPercent(duty, chip.MaxDuty())
	}
	return f
}

func notify(id string) {
	name := map[string]string{"cpu": "CPU fan", "case": "Case fan"}[roles[id]]
	notifications.send("stall:"+id, "-e", "UGREEN NAS Control", "-s", name+" stopped",
		"-d", fmt.Sprintf("%s (%s) reports 0 rpm although it is driven. Check the fan and its cable.", name, id),
		"-i", "alert")
}

// notifyTimeout bounds one run of the Unraid notify script (a variable for tests).
var notifyTimeout = 10 * time.Second

// notifier runs the notify script in the background: never under the hardware lock, never longer than notifyTimeout,
// and at most one run per key at a time (a hung script drops the repeats instead of piling up processes).
type notifier struct {
	mu   sync.Mutex
	busy map[string]bool
	wg   sync.WaitGroup // tests wait on it
}

var notifications = &notifier{busy: map[string]bool{}}

func (n *notifier) send(key string, args ...string) {
	n.mu.Lock()
	if n.busy[key] {
		n.mu.Unlock()
		return
	}
	n.busy[key] = true
	n.mu.Unlock()
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		defer func() {
			n.mu.Lock()
			delete(n.busy, key)
			n.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		_ = exec.CommandContext(ctx, notifyCmd, args...).Run()
	}()
}

// warmInputs reads every configured input once, before the lock is taken, so that the slow ones (script, GPU, HBA) run
// outside it and the registry has their answers (or failures, kept for slowTTL) when the locked part asks.
func warmInputs(ctx context.Context, in InputSource, c Config, ids []string) {
	for _, id := range ids {
		if spec := c.ForChannel(id).Input; spec.Kind != "" {
			in.Read(ctx, spec)
		}
	}
}

// detectConflicts is a variable so that tests can stand in for the system.
var detectConflicts = func() []conflict.Conflict { return conflict.Default().Detect() }

func rivalNames(list []conflict.Conflict) string {
	names := make([]string, len(list))
	for i, c := range list {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// yielding remembers what was logged while a rival controls the fans, so each change is reported once.
type yielding struct {
	names     string
	unapplied string
}

// yield is one tick while another fan controller is running: not a single write to the chip. The temperatures
// and the case driver are still reported. A hot system is not answered with 100 % (that would fight the rival
// every 2 s, silently): the reason is logged, put in the state for the web page and sent as an Unraid alert once.
func yield(y *yielding, c Config, s Sensors, rivals []conflict.Conflict, st *State, logf func(string, ...any)) {
	d := Decide(c, s)
	st.CaseTemp, st.CaseFrom, st.CaseDriver = d.CaseTemp, d.CaseFrom, d.CaseDriver
	for _, r := range rivals {
		st.Conflict = append(st.Conflict, r.Name)
	}
	st.Unapplied = d.Failsafe

	if names := rivalNames(rivals); names != y.names {
		logf("%s controls the fans too: UGREEN NAS Control reads only until one of them is switched off", names)
		y.names = names
	}
	if d.Failsafe != y.unapplied {
		if d.Failsafe != "" {
			logf("emergency (%s) but %s controls the fans: not overridden, check the cooling", d.Failsafe, y.names)
			notifyRival(d.Failsafe, y.names)
		}
		y.unapplied = d.Failsafe
	}
}

func (y *yielding) reset(logf func(string, ...any)) {
	if y.names != "" {
		logf("no other fan controller running, back to the curves")
	}
	*y = yielding{}
}

func notifyRival(reason, rival string) {
	notifications.send("rival", "-e", "UGREEN NAS Control", "-s", "Emergency, fans not set to 100 %",
		"-d", fmt.Sprintf("%s (%s) would need all fans at 100 %%, but %s controls them. Check the cooling now.", reason, rival, rival),
		"-i", "alert")
}

// InputSource reads the temperature of one non-default input spec (cpu, disk_max, disk_avg, nvme, gpu, hba, nic, script,
// max). ok false = the source cannot be read now; the group's on_missing rule decides. The auto input (empty spec) is
// not read through it: it keeps today's logic (Decide). Replace newInputSource to plug in the input registry.
type InputSource interface {
	Read(ctx context.Context, spec InputSpec) (temp float64, ok bool)
}

var newInputSource = func(c Config, s Sensors) InputSource { return sensorSource{c, s} }

// sensorSource reads from the sensors of the tick: cpu, nvme, disk_max, disk_avg, nic and max of those. GPU and
// script are not readable here (missing).
type sensorSource struct {
	c Config
	s Sensors
}

func (r sensorSource) Read(ctx context.Context, spec InputSpec) (float64, bool) {
	switch spec.Kind {
	case "cpu":
		if r.s.CPU != nil {
			return float64(*r.s.CPU), true
		}
	case "nvme":
		if t := r.s.MaxNVMeExcept(r.c.IgnoreDrives); t != nil {
			return float64(*t), true
		}
	case "disk_max", "disk_avg":
		sum, n, hottest := 0, 0, 0
		for _, d := range r.s.Disks {
			if d.Temp == nil || r.c.IgnoreDrives[d.Name] || len(spec.Disks) > 0 && !hasName(spec.Disks, d.Name) {
				continue
			}
			sum += *d.Temp
			n++
			hottest = max(hottest, *d.Temp)
		}
		switch {
		case n == 0:
		case spec.Kind == "disk_max":
			return float64(hottest), true
		default:
			return float64(sum) / float64(n), true
		}
	case "nic":
		best, found := 0, false
		for _, n := range r.s.NIC {
			if n.Iface == spec.Name && n.Temp != nil && (!found || *n.Temp > best) {
				best, found = *n.Temp, true
			}
		}
		return float64(best), found
	case "gpu":
		if r.s.GPU != nil {
			return float64(*r.s.GPU), true
		}
	case "max":
		best, found := 0.0, false
		for _, p := range spec.Parts {
			if t, ok := r.Read(ctx, p); ok && (!found || t > best) {
				best, found = t, true
			}
		}
		return best, found
	}
	return 0, false
}

func hasName(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// petWatchdog feeds the watchdog once per tick (the petter limits the writes to its interval); a failure is
// logged when it starts, not every tick. The caller holds the lock.
func petWatchdog(p *Petter, chip Fans, at time.Time, logf func(string, ...any)) {
	_, ok, err := p.Tick(chip, at)
	if !ok {
		return
	}
	switch {
	case err != nil && !p.failed:
		p.failed = true
		logf("watchdog could not be fed: %v", err)
	case err == nil:
		p.failed = false
	}
}

// stopWatchdog ends the feeding on a clean stop: an IT8613E timer this process fed is stopped, so that nobody leaves
// it running unfed. The caller holds the lock.
func stopWatchdog(p *Petter, chip Fans, logf func(string, ...any)) {
	if err := p.Stop(chip); err != nil {
		logf("watchdog could not be stopped: %v", err)
	}
}

// openWatchdogOnly opens the chip for runWatchdogOnly: an IT8613E model whose only obstacle to the daemon is the missing
// fan map. Everything else keeps the error openChip gave.
func openWatchdogOnly() (Fans, error) {
	model := productName()
	if !isIT8613Model(model) || FanKind(model) != KindProbe {
		return nil, errNoWatchdog
	}
	if _, err := loadFanMap(model); !errors.Is(err, errNoFanMap) {
		return nil, errNoWatchdog
	}
	return openChipUnchecked()
}

// runWatchdogOnly is the daemon of an IT8613E model without a confirmed fan map: fan control stays off, the watchdog
// is still fed (the BIOS may have armed it). It never writes a fan register.
func runWatchdogOnly(chip Fans) error {
	logger, _ := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "ugreen-nas-control")
	logf := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if logger != nil {
			_ = logger.Info(msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, daemonSignals...)
	defer signal.Stop(stop)

	pet := &Petter{}
	logf("watchdog petter started without fan control (%s: no confirmed fan map)", chip.Describe())
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		pet.Yield = conflict.Present(detectConflicts(), conflict.Watchdog)
		_ = withLock(func() error { petWatchdog(pet, chip, now(), logf); return nil })
		select {
		case <-stop:
			_ = withLockStop(func() error { stopWatchdog(pet, chip, logf); return nil })
			logf("watchdog petter stopped")
			return nil
		case <-ticker.C:
		}
	}
}

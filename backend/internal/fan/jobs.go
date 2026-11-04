package fan

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Background job detection for the job rules. Every path and key below is UNVERIFIED on a real Unraid box (taken from
// the Unraid forum and docs, not captured from a tower): verify on a real box before relying on it.
//   - /proc/mdstat: the md driver's own file; Unraid's md lists mdResync=<sectors left> (0 = idle) and the classic
//     "resync|recovery|check|reshape = n%" progress line while a parity check or rebuild runs.
//   - /var/local/emhttp/var.ini: mdResync="<n>" (non-zero while a parity check/sync/rebuild is active), mdResyncAction.
//   - /var/run/mover.pid: written by /usr/local/sbin/mover; stale after a crash, so the pid must be alive and a mover.
//     Without the file `pgrep -x mover` decides.

type JobStatus struct{ Mover, Parity bool }

type jobProbe struct {
	mdstat, varIni, moverPID, procDir string
	alive                             func(pid int) bool
	pgrep                             func() bool

	at   time.Time
	last JobStatus
}

const jobTTL = 10 * time.Second

// pgrepTimeout bounds the mover lookup: it runs on the daemon tick (a variable for tests).
var pgrepTimeout = 3 * time.Second

func pgrepMover() bool {
	ctx, cancel := context.WithTimeout(context.Background(), pgrepTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "pgrep", "-x", "mover").Run() == nil
}

func newJobProbe() *jobProbe {
	return &jobProbe{
		mdstat:   envOr("UGREEN_FAN_MDSTAT", "/proc/mdstat"),
		varIni:   envOr("UGREEN_FAN_VARINI", "/var/local/emhttp/var.ini"),
		moverPID: envOr("UGREEN_FAN_MOVER_PID", "/var/run/mover.pid"),
		procDir:  "/proc",
		alive:    pidAlive,
		pgrep:    pgrepMover,
	}
}

// Status is the cached state; want says which jobs the config cares about (the others are not even looked at).
func (p *jobProbe) Status(now time.Time, wantMover, wantParity bool) JobStatus {
	if d := now.Sub(p.at); !p.at.IsZero() && d >= 0 && d < jobTTL {
		return p.last
	}
	var s JobStatus
	if wantParity {
		s.Parity = parityRunning(p.mdstat, p.varIni)
	}
	if wantMover {
		s.Mover = p.moverRunning()
	}
	p.at, p.last = now, s
	return s
}

func parityRunning(mdstat, varIni string) bool {
	if f, err := os.Open(varIni); err == nil {
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), "="); ok && k == "mdResync" {
				if n, err := strconv.ParseInt(strings.Trim(v, `" `), 10, 64); err == nil && n > 0 {
					return true
				}
			}
		}
	}
	f, err := os.Open(mdstat)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "mdResync" {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
				return true
			}
			continue
		}
		for _, word := range []string{"resync", "recovery", "check", "reshape"} {
			// "[=>...]  resync = 12.3% (...)"; "resync=PENDING/DELAYED" is waiting, not running
			if i := strings.Index(line, word); i >= 0 && strings.Contains(line[i:], "%") && strings.Contains(line[i:], "=") {
				return true
			}
		}
	}
	return false
}

func (p *jobProbe) moverRunning() bool {
	b, err := os.ReadFile(p.moverPID)
	if err != nil {
		return p.pgrep()
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 || !p.alive(pid) {
		return false // stale file
	}
	// A reused pid belongs to something else: its command line has to name the mover (when /proc can tell).
	if cmd, err := os.ReadFile(filepath.Join(p.procDir, strconv.Itoa(pid), "cmdline")); err == nil {
		return strings.Contains(string(cmd), "mover")
	}
	return true
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

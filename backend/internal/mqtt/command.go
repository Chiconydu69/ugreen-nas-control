package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type command struct {
	Cmd    string `json:"cmd"`
	Target string `json:"target"`
	Name   string `json:"name"`
	TS     int64  `json:"ts"`
	Nonce  string `json:"nonce"`
}

// cmdResult answers a command. The first answer is "started" or the reason for a refusal; while the command runs,
// more answers with Step and Total follow, one when a step starts and a last one with Done set.
type cmdResult struct {
	Nonce string `json:"nonce"`
	OK    bool   `json:"ok"`
	Text  string `json:"text"`
	Step  int    `json:"step,omitempty"`
	Total int    `json:"total,omitempty"`
	Name  string `json:"name,omitempty"`
	Done  bool   `json:"done,omitempty"`
}

// job is one program a command runs; label is what a display shows for it.
type job struct {
	label string
	prog  []string
}

const (
	cmdMaxAge   = 30 * time.Second
	cmdMinSpace = 10 * time.Second
)

// commander validates and carries out commands. run is how a program is started (replaced in tests).
type commander struct {
	cfg     Config
	updates func() []Update
	// containers returns the names of the containers that were created from an image.
	containers func(image string) ([]string, error)
	run        func(name string, args ...string) error
	// report sends the answers that follow the first one (nil in tests that do not look at them).
	report func(cmdResult)
	delay  time.Duration

	mu    sync.Mutex
	seen  map[string]time.Time
	lastC time.Time
	busy  bool // an accepted command has not finished
}

// containerJobs is one update job per container that was created from image.
func (c *commander) containerJobs(image string) ([]job, string) {
	names, err := c.containers(image)
	if err != nil {
		return nil, "docker could not be run: " + err.Error()
	}
	var jobs []job
	for _, n := range names {
		if !containerNameRe.MatchString(n) {
			return nil, "container name not allowed"
		}
		jobs = append(jobs, job{n, []string{updateContainerBin, n}})
	}
	return jobs, ""
}

// handle checks one command and returns the answer. The program is started by `after`, which the caller runs once
// the answer has been sent: a restart would otherwise take the answer down with it.
func (c *commander) handle(raw []byte, now time.Time) (res cmdResult, after func()) {
	var cmd command
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return cmdResult{Text: "unreadable command"}, nil
	}
	res.Nonce = cmd.Nonce
	reject := func(text string) (cmdResult, func()) {
		logf("command %q rejected: %s", cmd.Cmd, text)
		res.Text = text
		return res, nil
	}

	if cmd.Nonce == "" || len(cmd.Nonce) > 64 {
		return reject("nonce missing")
	}
	age := now.Sub(time.Unix(cmd.TS, 0))
	if age > cmdMaxAge || age < -cmdMaxAge {
		return reject("command too old or clock apart")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		c.seen = map[string]time.Time{}
	}
	for n, t := range c.seen {
		if now.Sub(t) > 2*cmdMaxAge {
			delete(c.seen, n)
		}
	}
	if _, dup := c.seen[cmd.Nonce]; dup {
		return reject("command already seen")
	}
	if len(c.seen) >= 256 {
		return reject("too many commands")
	}
	if now.Sub(c.lastC) < cmdMinSpace {
		return reject("too fast, wait a moment")
	}
	if c.busy {
		return reject("busy, the previous command is still running")
	}

	var jobs []job
	switch cmd.Cmd {
	case "reboot", "shutdown":
		if !c.cfg.Control {
			return reject("remote control is switched off")
		}
		prog := []string{powerdownBin}
		if cmd.Cmd == "reboot" {
			prog = append(prog, "-r")
		}
		jobs = []job{{cmd.Cmd, prog}}
	case "update":
		switch cmd.Target {
		case "plugin":
			if !c.cfg.ControlUpdates {
				return reject("updates from the display are switched off")
			}
			file := ""
			for _, u := range c.updates() {
				if u.Kind == "plugin" && u.Name == cmd.Name {
					file = u.File
				}
			}
			if strings.HasPrefix(cmd.Name, "-") || file == "" || filepath.Base(file) != file {
				return reject("no such update")
			}
			jobs = []job{{cmd.Name, []string{pluginBin, "update", file}}}
		case "container", "containers":
			if !c.cfg.ControlContainers {
				return reject("container updates from the display are switched off")
			}
			var images []string
			for _, u := range c.updates() {
				if u.Kind != "container" {
					continue
				}
				if cmd.Target == "containers" || u.Name == cmd.Name {
					images = append(images, u.Name)
				}
			}
			if len(images) == 0 {
				return reject("no such update")
			}
			seen := map[string]bool{}
			for _, img := range images {
				js, bad := c.containerJobs(img)
				if bad != "" {
					return reject(bad)
				}
				for _, j := range js {
					if !seen[j.label] {
						seen[j.label] = true
						jobs = append(jobs, j)
					}
				}
			}
			if len(jobs) == 0 {
				return reject("no container found for that image")
			}
		default:
			return reject("only plugin and container updates can be installed this way")
		}
	default:
		return reject("unknown command")
	}

	// Only an accepted command uses up its nonce: a refused one (switched off, too fast) was never carried out.
	c.seen[cmd.Nonce] = now
	c.lastC = now
	c.busy = true
	for _, j := range jobs {
		logf("command %q accepted: %s", cmd.Cmd, strings.Join(j.prog, " "))
	}
	res.OK, res.Text = true, "started"
	return res, func() {
		defer func() {
			c.mu.Lock()
			c.busy = false
			c.mu.Unlock()
		}()
		say := func(r cmdResult) {
			r.Nonce = cmd.Nonce
			if c.report != nil {
				c.report(r)
			}
		}
		time.Sleep(c.delay)
		failed := 0
		for i, j := range jobs {
			say(cmdResult{OK: true, Text: "running", Step: i + 1, Total: len(jobs), Name: j.label})
			if err := c.run(j.prog[0], j.prog[1:]...); err != nil {
				failed++
				logf("command %q failed: %s: %v", cmd.Cmd, j.label, err)
				say(cmdResult{Text: "failed: " + err.Error(), Step: i + 1, Total: len(jobs), Name: j.label})
			}
		}
		text := "done"
		if failed > 0 {
			text = fmt.Sprintf("%d of %d failed", failed, len(jobs))
		}
		say(cmdResult{OK: failed == 0, Text: text, Step: len(jobs), Total: len(jobs), Done: true})
	}
}

// containerNameRe is what Docker allows for a container name; anything else is not passed to a program.
var containerNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// dockerContainers lists the containers created from image. Unraid names an update by its image, but the
// update script wants the container's name.
func dockerContainers(image string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, dockerBin, "ps", "-a", "--format", "{{.Names}}\t{{.Image}}").Output()
	if err != nil {
		return nil, err
	}
	return matchContainers(string(out), image), nil
}

func matchContainers(listing, image string) []string {
	var names []string
	for _, l := range strings.Split(listing, "\n") {
		f := strings.SplitN(l, "\t", 2)
		if len(f) == 2 && normImage(f[1]) == normImage(image) {
			names = append(names, strings.TrimSpace(f[0]))
		}
	}
	return names
}

// normImage makes the two spellings of one image equal: Unraid lists "wordpress:latest" where docker ps shows
// the image as the container was created, "wordpress"; "library/" and "docker.io/" are the same registry paths.
func normImage(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "docker.io/")
	s = strings.TrimPrefix(s, "library/")
	if i := strings.LastIndex(s, "/"); !strings.Contains(s[i+1:], ":") && !strings.Contains(s[i+1:], "@") {
		s += ":latest"
	}
	return s
}

// cmdTimeout bounds one program: a plugin update downloads and installs, which can take minutes.
const cmdTimeout = 30 * time.Minute

func execRun(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

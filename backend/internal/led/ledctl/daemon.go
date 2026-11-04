package ledctl

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Hardware blink while a disk or the network is busy.
const (
	activityOnMS  = "100"
	activityOffMS = "100"
)

// parseWanted splits "steady|R G B|brightness" (also breath|..., pulse|...) into its parts.
func parseWanted(w string) (kind, color, brightness string) {
	kind, rest, _ := strings.Cut(w, "|")
	if i := strings.LastIndexByte(rest, '|'); i >= 0 {
		color = rest[:i]
	} else {
		color = rest
	}
	_, brightness, _ = strings.Cut(rest, "|")
	return kind, color, brightness
}

// showLED sets one LED to a wanted look: "steady|R G B|brightness", "breath|...",
// "pulse|..." (slow breathing of a disk in standby) or "off".
func (c *Ctl) showLED(led, wanted string) {
	c.quiet(func() {
		if wanted == "off" {
			c.tool("set", led, "off")
			return
		}
		kind, color, brightness := parseWanted(wanted)
		c.tool(append([]string{"set", led, "color"}, strings.Fields(color)...)...)
		c.tool("set", led, "brightness", brightness)
		switch kind {
		case "breath":
			c.tool("set", led, "breath", "1500", "1500")
		case "pulse":
			c.tool("set", led, "breath", "3000", "3000")
		default:
			c.tool("set", led, "on")
		}
	})
}

// ledState is what the controller reports per LED ("<mode> <brightness> <R> <G> <B> <on_ms> <off_ms>"),
// from one `get all`. ok is false when the controller does not answer.
func (c *Ctl) ledsRead() (state map[string]string, ok bool) {
	out, rc := c.toolOut(false, "--plain", "get", "all")
	if rc != 0 {
		return nil, false
	}
	state = map[string]string{}
	lines, _ := splitTerminated(strings.TrimRight(out, "\n") + "\n")
	for _, line := range lines {
		f := splitRead(line, 2)
		if f[0] != "" {
			state[f[0]] = f[1]
		}
	}
	return state, len(state) > 0
}

// ledMatches: does the LED look like the wanted state? A running activity blink counts as
// right: it is the daemon's own doing.
func ledMatches(state map[string]string, led, wanted string, isBlinking bool) bool {
	st := state[led]
	if st == "" {
		return false
	}
	f := splitRead(st, 7)
	mode, brightness, color, on, off := f[0], f[1], f[2]+" "+f[3]+" "+f[4], f[5], f[6]

	if wanted == "off" {
		return mode == "off"
	}
	if isBlinking && mode == "blink" {
		return true
	}
	kind, wcolor, wbright := parseWanted(wanted)
	if color != wcolor || brightness != wbright {
		return false
	}
	switch kind {
	case "steady":
		return mode == "on"
	case "breath":
		return mode == "breath" && on == "1500" && off == "1500"
	case "pulse":
		return mode == "breath" && on == "3000" && off == "3000"
	}
	return false
}

// netProbe is the last link and gateway check, written by a goroutine, read by the loop.
type netProbe struct {
	mu    sync.Mutex
	ok    bool
	speed int64
}

func (p *netProbe) set(ok bool, speed int64) {
	p.mu.Lock()
	p.ok, p.speed = ok, speed
	p.mu.Unlock()
}

func (p *netProbe) get() (bool, int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ok, p.speed
}

// daemonRun is the status daemon: disk status and activity, network and effects on the LEDs
// until the context ends.
func (c *Ctl) daemonRun(ctx context.Context) int {
	if names := c.ledRivals(); names != "" {
		c.log(names + " controls the LEDs, status daemon exits")
		_ = os.Remove(c.Env.PidFile)
		c.cs201xRestore()
		return 0
	}

	bays := c.bays
	netDev := c.networkDevice()
	probe := &netProbe{ok: true}

	applied := make([]string, bays+1)
	blinking := make([]bool, bays+1)
	counters := make([]string, bays+1)
	devices := make([]string, bays+1)
	netWanted, netCounter := "", ""
	netBlinking := false
	netStatus := "activity"
	speedOn := false
	night := false
	var errorColor, syncColor, smartOn, errorOn, activityOn, smartColor, tempMode, standbyMode string

	// Link and speed right away, so the first 5 s don't show the wrong colour.
	if netDev != "" {
		probe.set(c.networkOK(netDev), atoiOr(c.linkSpeed(netDev, 0), 0))
	}

	name := netDev
	if name == "" {
		name = "none"
	}
	c.log("status daemon started (bays: " + strconv.Itoa(bays) + ", network: " + name + ")")
	c.powerBoot()

	// Every 30 s: link and gateway. A ping can take seconds, so it runs beside the LED loop.
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	if netDev != "" {
		wg.Go(func() {
			wait := c.Tick // the shell script probes at tick 1 (tick%150 == 1), then every 150 ticks
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
				probe.set(c.networkOK(netDev), atoiOr(c.linkSpeed(netDev, 0), 0))
				wait = 150 * c.Tick
			}
		})
	}
	// Leave without waiting for a ping that is still out; it ends by itself (ping -W 2).
	defer func() {
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
		}
	}()
	defer cancel() // runs first: ends the probe loop, then the wait above

	ticker := time.NewTicker(c.Tick)
	defer ticker.Stop()

	for tick := 0; ; tick++ {
		if tick%25 == 0 {
			// Two monitors would overwrite each other forever; leave the LEDs to the other one.
			if tick%150 == 0 && c.foreignMonitorRunning() {
				c.log("ugreenleds-driver monitor is running, status daemon exits")
				_ = os.Remove(c.Env.PidFile)
				c.cs201xRestore()
				return 0
			}
			if tick%150 == 0 {
				if names := c.ledRivals(); names != "" {
					c.log(names + " controls the LEDs, status daemon exits")
					_ = os.Remove(c.Env.PidFile)
					c.cs201xRestore()
					return 0
				}
			}

			// Every 5 s: saved settings (parsed again only when the file changed), which disk is
			// where, Unraid's disk status. All of it from files and memory, no helper processes.
			c.cfgLoad()
			c.disksLoad()
			c.devpathsRefresh()
			c.dynLoaded = false

			errorColor = c.colorCfg("disk_error_color", defaultErrorColor)
			night = c.isNight()

			// Parity check / rebuild: array disks breathe in the sync colour.
			c.syncSt = syncState{}
			if c.cfg["sync_enabled"] != "0" {
				c.syncStateLoad()
			}
			syncColor = c.colorCfg("sync_color", defaultSyncColor)
			smartOn = c.get("smart_enabled", "1")
			errorOn = c.get("error_enabled", "1")
			activityOn = c.get("activity_enabled", "1")
			smartColor = c.colorCfg("smart_color", defaultSmartColor)
			tempMode = c.get("disk_color_mode", "own")
			standbyMode = c.get("standby_mode", "dim")
			netStatus = c.netdevStatus()
			speedOn = c.cfg["netdev_speed_enabled"] == "1"

			// Every 30 s the LEDs are compared with what the controller says and only the ones that
			// differ are written again (a manual Test, an effect, a controller reset). Writing
			// everything would restart the breathing of every standby bay each time.
			// A manual network LED is written once and then left alone (its effect may run).
			if tick%150 == 0 && tick > 0 {
				if state, ok := c.ledsRead(); ok {
					for bay := 1; bay <= bays; bay++ {
						if applied[bay] != "" && applied[bay] != "identify" &&
							!ledMatches(state, "disk"+strconv.Itoa(bay), applied[bay], blinking[bay]) {
							applied[bay] = ""
						}
					}
					if netWanted != "" && netWanted != "manual" && !ledMatches(state, "netdev", netWanted, netBlinking) {
						netWanted = ""
					}
				} else {
					applied = make([]string, bays+1)
					if netWanted != "manual" {
						netWanted = ""
					}
				}
			}

			for bay := 1; bay <= bays; bay++ {
				b := strconv.Itoa(bay)
				dev := c.bayDevice(bay)
				devices[bay] = dev

				// Being identified: hands off; once it ends, the next round writes it again.
				if exists(c.Env.IdentifyPrefix + "disk" + b) {
					applied[bay] = "identify"
					continue
				}

				brightness := c.effectiveBrightness(atoiOr(c.get("disk"+b+"_brightness", "255"), 255), night)
				look := "steady"
				var w []string
				if dev != "" {
					w = strings.Fields(c.disks[dev])
					if field(w, 3, "0") == "1" {
						brightness = c.standbyBrightness(brightness)
						if standbyMode == "pulse" {
							look = "pulse"
						}
					}
				}

				// Error > SMART warning > parity check > temperature or own colour.
				slot := field(w, 1, "")
				var wanted string
				switch {
				case dev == "" || brightness == 0:
					wanted = "off"
				case errorOn == "1" && isErrorStatus(field(w, 2, "")):
					wanted = "steady|" + errorColor + "|" + itoa(brightness)
				case smartOn == "1" && slot != "" && c.smartBad(slot, field(w, 5, "0"), field(w, 9, "-")):
					wanted = "steady|" + smartColor + "|" + itoa(brightness)
				case c.syncSt.active && slot != "":
					wanted = "breath|" + syncColor + "|" + itoa(brightness)
				case tempMode == "temperature" && field(w, 4, "-") != "-":
					hot, maxT := c.tempLimits(field(w, 6, "1"), field(w, 7, "-"), field(w, 8, "-"))
					wanted = look + "|" + tempColor(atoiOr(field(w, 4, "-"), 0), atoiOr(hot, 45), atoiOr(maxT, 55)) + "|" + itoa(brightness)
				default:
					wanted = look + "|" + c.colorCfg("disk"+b+"_color", defaultDiskColor) + "|" + itoa(brightness)
				}

				if applied[bay] != wanted {
					c.showLED("disk"+b, wanted)
					applied[bay] = wanted
					blinking[bay] = false
				}
			}

			if netStatus == "manual" {
				// Hand it back to the saved manual look once.
				if netWanted != "manual" {
					if netBlinking {
						c.quiet(func() { c.tool("set", "netdev", "on") })
					}
					c.quiet(func() { c.applyLED("netdev") })
					if night {
						c.applyNight("netdev")
					}
					netWanted = "manual"
					netBlinking = false
				}
			} else if netDev != "" {
				brightness := c.effectiveBrightness(atoiOr(c.get("netdev_brightness", "255"), 255), night)
				netOK, netSpeed := probe.get()

				var wanted string
				switch {
				case brightness == 0:
					wanted = "off"
				case netOK && speedOn && netSpeed > 0:
					wanted = "steady|" + c.speedColor(netSpeed) + "|" + itoa(brightness)
				case netOK:
					wanted = "steady|" + c.colorCfg("netdev_color", defaultNetdevColor) + "|" + itoa(brightness)
				default:
					wanted = "steady|" + errorColor + "|" + itoa(brightness)
				}

				if netWanted != wanted {
					c.showLED("netdev", wanted)
					netWanted = wanted
					netBlinking = false
				}

				// Switched from activity to link while it was blinking.
				if netStatus == "link" && netBlinking {
					c.quiet(func() { c.tool("set", "netdev", "on") })
					netBlinking = false
				}
			}
		}

		for bay := 1; bay <= bays; bay++ {
			dev := devices[bay]
			if dev == "" {
				continue
			}
			// Dark, breathing during a parity check or in standby, or being identified: no activity blink.
			if a := applied[bay]; a == "off" || a == "identify" || strings.HasPrefix(a, "breath|") || strings.HasPrefix(a, "pulse|") {
				continue
			}
			led := "disk" + strconv.Itoa(bay)
			if exists(c.Env.IdentifyPrefix + led) {
				continue
			}

			if activityOn == "0" {
				if blinking[bay] {
					c.quiet(func() { c.tool("set", led, "on") })
					blinking[bay] = false
				}
				continue
			}

			line, ok := readLine(c.Env.SysBlock + "/" + dev + "/stat")
			if !ok {
				continue
			}
			f := splitRead(line, 6)
			counter := f[0] + " " + f[4]

			if counters[bay] != "" && counters[bay] != counter {
				if !blinking[bay] {
					c.quiet(func() { c.tool("set", led, "blink", activityOnMS, activityOffMS) })
					blinking[bay] = true
				}
			} else if blinking[bay] {
				c.quiet(func() { c.tool("set", led, "on") })
				blinking[bay] = false
			}
			counters[bay] = counter
		}

		if netDev != "" && netStatus == "activity" && netWanted != "off" && netWanted != "manual" {
			rx, ok1 := readLine(c.Env.SysNet + "/" + netDev + "/statistics/rx_bytes")
			tx, ok2 := readLine(c.Env.SysNet + "/" + netDev + "/statistics/tx_bytes")
			if ok1 && ok2 {
				counter := rx + " " + tx
				if netCounter != "" && netCounter != counter {
					if !netBlinking {
						c.quiet(func() { c.tool("set", "netdev", "blink", activityOnMS, activityOffMS) })
						netBlinking = true
					}
				} else if netBlinking {
					c.quiet(func() { c.tool("set", "netdev", "on") })
					netBlinking = false
				}
				netCounter = counter
			}
		}

		select {
		case <-ctx.Done():
			c.log("status daemon stopped")
			c.cs201xRestore()
			return 0
		case <-ticker.C:
		}
	}
}

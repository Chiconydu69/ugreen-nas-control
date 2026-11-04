package chip

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Software effects. The controller only blinks and breathes by itself, so
// rainbow and temperature colours are written frame by frame.

type rgb struct{ r, g, b byte }

// Time per rainbow step (hueStep degrees), slowest to fastest.
var rainbowPeriods = []time.Duration{
	600 * time.Millisecond,
	400 * time.Millisecond,
	250 * time.Millisecond,
	150 * time.Millisecond,
	80 * time.Millisecond,
}

const (
	hueStep            = 10
	temperaturePeriod  = 5 * time.Second
	idleRecheckPeriod  = time.Second
	defaultHwmonRoot   = "/sys/class/hwmon"
	defaultSysBlockDir = "/sys/block"
)

func hsv(hue float64) rgb {
	hue = math.Mod(hue, 360)
	if hue < 0 {
		hue += 360
	}

	x := 1 - math.Abs(math.Mod(hue/60, 2)-1)
	var r, g, b float64

	switch {
	case hue < 60:
		r, g = 1, x
	case hue < 120:
		r, g = x, 1
	case hue < 180:
		g, b = 1, x
	case hue < 240:
		g, b = x, 1
	case hue < 300:
		r, b = x, 1
	default:
		r, b = 1, x
	}

	return rgb{byte(math.Round(r * 255)), byte(math.Round(g * 255)), byte(math.Round(b * 255))}
}

// rainbowFrame spreads one full rainbow over n LEDs and moves it by hueStep
// per step; reverse runs the wave the other way.
func rainbowFrame(step, n int, reverse bool) []rgb {
	colors := make([]rgb, n)
	for i := range colors {
		offset := float64(i) * 360 / float64(n)
		if reverse {
			offset = -offset
		}
		colors[i] = hsv(float64(step*hueStep) + offset)
	}
	return colors
}

type tempStop struct {
	celsius float64
	color   rgb
}

// Blue when cool, green, yellow, red when hot.
var tempStops = []tempStop{
	{35, rgb{0, 120, 255}},
	{45, rgb{0, 200, 83}},
	{55, rgb{255, 200, 0}},
	{65, rgb{255, 40, 40}},
}

func mix(a, b byte, t float64) byte {
	return byte(math.Round(float64(a) + (float64(b)-float64(a))*t))
}

func temperatureColor(celsius float64) rgb {
	if celsius <= tempStops[0].celsius {
		return tempStops[0].color
	}

	for i := 1; i < len(tempStops); i++ {
		lo, hi := tempStops[i-1], tempStops[i]
		if celsius <= hi.celsius {
			t := (celsius - lo.celsius) / (hi.celsius - lo.celsius)
			return rgb{mix(lo.color.r, hi.color.r, t), mix(lo.color.g, hi.color.g, t), mix(lo.color.b, hi.color.b, t)}
		}
	}

	return tempStops[len(tempStops)-1].color
}

// cpuTemperature is the hottest sensor of the CPU's hwmon device, in °C.
func cpuTemperature(root string) (float64, error) {
	names, _ := filepath.Glob(filepath.Join(root, "hwmon*", "name"))
	best := math.Inf(-1)

	for _, name := range names {
		content, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(string(content)) {
		case "coretemp", "k10temp", "zenpower", "cpu_thermal":
		default:
			continue
		}

		inputs, _ := filepath.Glob(filepath.Join(filepath.Dir(name), "temp*_input"))
		for _, input := range inputs {
			raw, err := os.ReadFile(input)
			if err != nil {
				continue
			}
			milli, err := strconv.Atoi(strings.TrimSpace(string(raw)))
			if err == nil && float64(milli)/1000 > best {
				best = float64(milli) / 1000
			}
		}
	}

	if math.IsInf(best, -1) {
		return 0, errors.New("no CPU temperature sensor found under " + root)
	}
	return best, nil
}

// diskActivity is a fingerprint of all disks' read/write counters; it changes
// whenever any disk is busy.
func diskActivity(blockDir string) string {
	stats, _ := filepath.Glob(filepath.Join(blockDir, "sd*", "stat"))
	var sb strings.Builder

	for _, stat := range stats {
		content, err := os.ReadFile(stat)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(content))
		if len(fields) >= 5 {
			sb.WriteString(fields[0] + "/" + fields[4] + " ")
		}
	}
	return sb.String()
}

type effectOptions struct {
	name     string
	leds     []string
	speed    int
	reverse  bool
	idleOnly bool
	busPath  string
	force    bool
}

func validateEffect(o effectOptions) error {
	if o.name != "rainbow" && o.name != "temperature" {
		return fmt.Errorf("unknown effect %q (rainbow, temperature)", o.name)
	}
	if o.speed < 0 || o.speed >= len(rainbowPeriods) {
		return fmt.Errorf("speed must be 0-%d", len(rainbowPeriods)-1)
	}
	return nil
}

// runEffect writes frames until SIGTERM/SIGINT. Only colours that changed are
// written, and the bus is opened (and locked) per frame so the web UI and the
// status daemon still get their turn.
func runEffect(o effectOptions) error {
	if o.name == "temperature" {
		if _, err := cpuTemperature(defaultHwmonRoot); err != nil {
			return err
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	last := make([]rgb, len(o.leds))
	written := make([]bool, len(o.leds))
	activity := diskActivity(defaultSysBlockDir)
	step := 0

	for {
		interval := rainbowPeriods[o.speed]
		if o.name == "temperature" {
			interval = temperaturePeriod
		}

		busy := false
		if o.idleOnly {
			now := diskActivity(defaultSysBlockDir)
			busy = now != activity
			activity = now
		}

		if busy {
			interval = idleRecheckPeriod
		} else {
			var colors []rgb
			if o.name == "rainbow" {
				colors = rainbowFrame(step, len(o.leds), o.reverse)
				step++
			} else if celsius, err := cpuTemperature(defaultHwmonRoot); err == nil {
				colors = make([]rgb, len(o.leds))
				for i := range colors {
					colors[i] = temperatureColor(celsius)
				}
			}

			if colors != nil {
				writeColors(o, colors, last, written)
			}
		}

		select {
		case <-stop:
			return nil
		case <-time.After(interval):
		}
	}
}

func writeColors(o effectOptions, colors, last []rgb, written []bool) {
	b, err := openBus(o.busPath, o.force)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return
	}
	defer b.close()

	for i, name := range o.leds {
		if written[i] && colors[i] == last[i] {
			continue
		}
		id, _ := ledID(name)
		c := colors[i]
		if err := b.change(id, cmdColor, [4]byte{c.r, c.g, c.b}); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", name, err)
			continue
		}
		last[i], written[i] = c, true
	}
}

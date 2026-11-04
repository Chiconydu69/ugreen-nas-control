package profile

import (
	"fmt"
	"strconv"
	"strings"
)

// IT-Kuny UGREEN fan daemon: one env file (systemd EnvironmentFile syntax) with curves in raw PWM (0..255) for the
// generic models and in percent for the iDX6011 Pro keys.

// Defaults of the daemon where a key is absent.
var itkGeneric = map[string][2]string{ // mode -> cpu, disk
	"normal":   {"35:50,50:100,65:180,75:255", "35:60,45:110,55:180,60:255"},
	"silent":   {"40:0,55:40,65:80,75:150", "38:0,45:40,55:80,62:150"},
	"powerful": {"30:120,45:180,55:220,65:255", "30:120,40:180,50:220,55:255"},
}

var itkIDX = map[string][2]string{ // mode -> cpu, disk (percent)
	"silent": {"0:25,60:25,72:50,80:75,88:100", "0:25,45:25,52:50,58:80,62:100"},
	"quiet":  {"0:30,55:30,68:55,78:80,86:100", "0:30,42:30,50:55,56:82,62:100"},
	"turbo":  {"0:45,50:45,65:75,75:95,82:100", "0:45,40:45,48:78,54:96,60:100"},
}

var itkReasons = map[string]string{
	"POLL_INTERVAL":     "the fan check interval is fixed",
	"FAILSAFE_PWM":      "the emergency speed is fixed at 100 %",
	"MAX_PWM":           "this plugin has no speed cap",
	"FAN_PWM_PATH":      "the fan output is fixed by the NAS model",
	"CPU_TEMP_PATH":     "the CPU sensor is chosen by the NAS model",
	"FAN_DEBUG":         "",
	"MIN_PWM":           "",
	"FAN_MODE":          "",
	"IDX6011_FAN_FLOOR": "",
}

// ParseITKuny reads the env file. Its location is not fixed on Unraid, so the caller reads it from a path the user
// named or from uploaded text.
func ParseITKuny(b []byte, rules Rules) (*Result, error) {
	if err := CheckText(b); err != nil {
		return nil, err
	}
	kv := parseKV(b)
	if !looksITKuny(kv) {
		return nil, fmt.Errorf("not an IT-Kuny fan env file (no FAN_MODE or *_FAN_CURVE setting)")
	}
	r := newResult(SourceITKuny)
	const file = "ugreen-fan-control.env"
	mode := strings.ToLower(kv["FAN_MODE"])
	idx := false
	for k := range kv {
		if strings.HasPrefix(k, "IDX6011_") {
			idx = true
		}
	}
	if mode == "quiet" || mode == "turbo" {
		idx = true
	}

	switch mode {
	case "auto":
		r.set("mode", "bios", "FAN_MODE=auto")
	case "max":
		r.set("mode", "full", "FAN_MODE=max")
	default:
		if mode == "" {
			mode = "normal"
		}
		if idx && mode == "normal" {
			mode = "quiet" // an alias on the iDX6011 Pro
		}
		r.set("mode", "custom", "FAN_MODE="+mode)
		cpu, disk := r.itkCurves(kv, mode, idx)
		if cpu != "" {
			r.set("cpu_curve", cpu, "the "+mode+" CPU curve")
		}
		if disk != "" {
			r.set("case_curve", disk, "the "+mode+" disk curve")
		}
	}

	if idx {
		if v, ok := num(kv, "IDX6011_FAN_FLOOR"); ok {
			r.set("min_pct", strconv.Itoa(v), "IDX6011_FAN_FLOOR")
		}
	} else if v, ok := num(kv, "MIN_PWM"); ok {
		r.set("min_pct", strconv.Itoa(pwmPct(v)), "MIN_PWM")
	}
	if v, ok := num(kv, "IDX6011_CPU_CRITICAL"); ok {
		r.set("cpu_crit", strconv.Itoa(v), "IDX6011_CPU_CRITICAL")
	}
	if v, ok := num(kv, "IDX6011_DISK_CRITICAL"); ok {
		r.set("disk_crit", strconv.Itoa(v), "IDX6011_DISK_CRITICAL")
	}
	if v, ok := num(kv, "MAX_PWM"); ok && v < 255 {
		r.warnf("MAX_PWM=%d caps the speed in IT-Kuny's daemon; this plugin has no cap", v)
	}

	for _, k := range sortedKeys(kv) {
		if reason, known := itkReasons[k]; known {
			if reason != "" {
				r.skip(file, k, reason)
			}
			continue
		}
		if !strings.HasSuffix(k, "_CURVE") && !strings.HasPrefix(k, "IDX6011_") {
			r.skip(file, k, "not a setting this plugin knows")
		}
	}
	r.Title = "IT-Kuny fan control, mode " + mode
	r.Finish(rules)
	return r, nil
}

func looksITKuny(kv map[string]string) bool {
	if _, ok := kv["FAN_MODE"]; ok {
		return true
	}
	for k := range kv {
		if strings.HasSuffix(k, "_FAN_CURVE") || strings.HasPrefix(k, "IDX6011_") {
			return true
		}
	}
	return false
}

// itkCurves picks the CPU and disk curve text of the mode and converts it to ours.
func (r *Result) itkCurves(kv map[string]string, mode string, idx bool) (cpu, disk string) {
	var cpuKey, diskKey, cpuDef, diskDef string
	pct := func(v float64) int { return clampInt(int(v+0.5), 0, 100) }
	switch {
	case idx:
		m := strings.ToUpper(mode)
		cpuKey, diskKey = "IDX6011_CPU_"+m+"_CURVE", "IDX6011_DISK_"+m+"_CURVE"
		def, ok := itkIDX[mode]
		if !ok {
			def = itkIDX["quiet"]
			r.warnf("FAN_MODE=%s is not an iDX6011 Pro mode, the quiet curves are used", clipPair(mode))
			cpuKey, diskKey = "IDX6011_CPU_QUIET_CURVE", "IDX6011_DISK_QUIET_CURVE"
		}
		cpuDef, diskDef = def[0], def[1]
	default:
		def, ok := itkGeneric[mode]
		if !ok {
			def = itkGeneric["normal"]
			r.warnf("FAN_MODE=%s is not a known mode, the daemon uses normal", clipPair(mode))
			mode = "normal"
		}
		prefix := ""
		switch mode {
		case "silent":
			prefix = "SILENT_"
		case "powerful":
			prefix = "POWERFUL_"
		}
		cpuKey, diskKey = prefix+"CPU_FAN_CURVE", prefix+"DISK_FAN_CURVE"
		cpuDef, diskDef = def[0], def[1]
		pct = func(v float64) int { return pwmPct(int(v + 0.5)) }
	}
	one := func(key, def, what string) string {
		text, set := kv[key]
		if !set {
			text = def
		}
		pts := r.parseCurvePairs(key, text, func(v float64) int { return pct(v) })
		if len(pts) == 0 {
			r.warnf("%s has no usable point, the %s curve is not imported", key, what)
			return ""
		}
		return r.curveText(key, pts)
	}
	return one(cpuKey, cpuDef, "CPU"), one(diskKey, diskDef, "disk")
}

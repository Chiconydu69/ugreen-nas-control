package profile

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Dynamix System Autofan: one dynamix.system.autofan{,N}.cfg per fan, input = the hottest drive, one temperature range.

var (
	dynFileRe = regexp.MustCompile(`^dynamix\.system\.autofan[0-9]*\.cfg$`)
	dynOptRe  = regexp.MustCompile(`-([ltTm])\s+([0-9]+)`)
)

var dynReasons = map[string]string{
	"controller": "the fan output is fixed by the NAS model",
	"fan":        "the fan output is fixed by the NAS model",
	"interval":   "the fan check interval is fixed",
	"service":    "",
	"options":    "built from the other settings",
}

type dynBlock struct {
	file      string
	kv        map[string]string
	service   bool
	pwm       int
	low, high int
}

// ParseDynamix reads the Autofan cfg files (name -> content).
func ParseDynamix(files map[string][]byte, env Env, rules Rules) (*Result, error) {
	r := newResult(SourceDynamix)
	var blocks []*dynBlock
	for _, name := range sortedFileNames(files) {
		if err := CheckText(files[name]); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		if !dynFileRe.MatchString(name) {
			continue
		}
		kv := parseKV(files[name])
		blk := &dynBlock{file: name, kv: kv, service: kv["service"] == "1"}
		var ok bool
		if blk.low, ok = num(kv, "low"); !ok {
			blk.low = 35
		}
		if blk.high, ok = num(kv, "high"); !ok {
			blk.high = 45
		}
		blk.pwm, ok = num(kv, "pwm")
		if !ok && blk.service {
			blk.pwm = 35
			r.warnf("%s: no minimum PWM, the Autofan default of 35 is used", name)
		}
		if !ok && !blk.service {
			r.skip(name, "(empty block)", "the fan is set to None")
			continue
		}
		blocks = append(blocks, blk)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no Dynamix Autofan fan block found")
	}
	// The enabled blocks first, then file order (the first fan has no number).
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].service && !blocks[j].service })
	best := blocks[0]
	for _, other := range blocks[1:] {
		r.skip(other.file, "(whole block)", "another block was imported for the case fans")
	}
	if !best.service {
		r.warnf("%s: the fan is switched off in Dynamix Autofan (a dormant config), imported anyway", best.file)
	}
	best.checkOptions(r)
	if best.low >= best.high {
		return nil, fmt.Errorf("%s: low %d is not below high %d", best.file, best.low, best.high)
	}

	floor := pwmPct(best.pwm)
	r.set("case_curve", r.curveText(best.file+" case curve", []point{{best.low, floor}, {best.high, 100}}), best.file+": low, high, pwm")
	r.set("min_pct", strconv.Itoa(floor), best.file+": pwm")
	r.set("mode", "custom", "imported curves need the custom mode")
	r.warnf("%s: below %d °C Autofan drops %d PWM lower (off step); this plugin keeps the curve's first speed", best.file, best.low, 30)

	// exclude: drives left out of the hottest-drive calculation.
	var names []string
	for _, dev := range splitCSV(best.kv["exclude"]) {
		if n, ok := env.device(dev); ok {
			names = append(names, n)
		} else {
			r.warnf("%s: the excluded drive %q was not found on this NAS", best.file, clipPair(dev))
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		r.set("ignore_drives", strings.Join(dedupe(names), ","), best.file+": exclude")
		r.warnf("%s: excluded drives apply to all fans here, not only this one", best.file)
	}
	for _, k := range sortedKeys(best.kv) {
		switch k {
		case "pwm", "low", "high", "exclude":
		default:
			if reason, known := dynReasons[k]; known {
				if reason != "" {
					r.skip(best.file, k, reason)
				}
			} else {
				r.skip(best.file, k, "not an Autofan setting this plugin knows")
			}
		}
	}
	r.Title = "Dynamix Auto Fan Control: " + strings.TrimSuffix(strings.TrimPrefix(best.file, "dynamix.system."), ".cfg")
	r.Finish(rules)
	return r, nil
}

// checkOptions: the Autofan daemon runs the options line, not the single keys; report when they differ.
func (b *dynBlock) checkOptions(r *Result) {
	opts := b.kv["options"]
	if opts == "" {
		return
	}
	for _, m := range dynOptRe.FindAllStringSubmatch(opts, -1) {
		n, _ := strconv.Atoi(m[2])
		var want int
		switch m[1] {
		case "l":
			want = b.pwm
		case "t":
			want = b.low
		case "T":
			want = b.high
		default:
			continue
		}
		if n != want {
			r.warnf("%s: the options line says -%s %d but the setting says %d; Autofan runs the options line, the setting is imported", b.file, m[1], n, want)
		}
	}
}

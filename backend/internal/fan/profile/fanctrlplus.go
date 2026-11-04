package profile

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FanCtrl Plus (fanctrlplus) and FanCtrl Plus 2 (fanctrlplus2): one cfg file per fan block, key="value" lines. FC2
// files are FC1 files with more keys, so one importer handles both.

var (
	fcNameRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	cpuHintRe  = regexp.MustCompile(`(?i)cpu`)
	cpuChipRe  = regexp.MustCompile(`coretemp|k10temp|zenpower|x86_pkg`)
	auxGPURe   = regexp.MustCompile(`^nvidia:gpu[0-9]{1,2}$`)
	auxHBARe   = regexp.MustCompile(`^storcli:c[0-9]{1,3}:roc$`)
	auxCustom  = regexp.MustCompile(`^custom:([a-z0-9_-]{1,32})$`)
	fcFileRe   = regexp.MustCompile(`^fanctrlplus2?_[A-Za-z0-9_-]+\.cfg$`)
	groupKeyRe = regexp.MustCompile(`^disk_group_([0-9]{1,3})_(name|disks|low|high)$`)
)

// Why a key of a FanCtrl Plus block is not imported (the keys that are mapped are not listed here).
var fcReasons = map[string]string{
	"controller":          "the fan output is fixed by the NAS model",
	"controller_identity": "the fan output is fixed by the NAS model",
	"fan":                 "the fan output is fixed by the NAS model",
	"idle":                "the speed when no sensor works: use \"when the input is missing\" instead",
	"idle_percent":        "the speed when no sensor works: use \"when the input is missing\" instead",
	"interval":            "the fan check interval is fixed",
	"interval_sec":        "the fan check interval is fixed",
	"syslog":              "no per-fan logging",
	"service":             "a per-block switch; fan control is one switch here",
	"custom":              "",
	"label":               "",
}

// fcBlock is one cfg file.
type fcBlock struct {
	file   string
	name   string
	kv     map[string]string
	pwm    int
	max    int
	enable bool
}

type fcGroupPick struct {
	name       string
	disks      []string
	low, high  int
	groupIndex int
}

// ParseFanCtrlPlus reads the cfg files of FanCtrl Plus ("fanctrlplus_*.cfg", name -> content). version 2 is FanCtrl Plus 2.
func ParseFanCtrlPlus(files map[string][]byte, version int, env Env, rules Rules) (*Result, error) {
	source, tool := SourceFanCtrlPlus, "FanCtrl Plus"
	if version == 2 {
		source, tool = SourceFanCtrlPlus2, "FanCtrl Plus 2"
	}
	r := newResult(source)
	var blocks []*fcBlock
	for _, name := range sortedFileNames(files) {
		b := files[name]
		if err := CheckText(b); err != nil {
			return nil, fmt.Errorf("%s: %v", name, err)
		}
		if !fcFileRe.MatchString(name) {
			continue // order.cfg, pwm_labels.cfg and the like: display state only
		}
		kv := parseKV(b)
		custom := kv["custom"]
		if custom == "" || strings.Contains(name, "_temp_") {
			r.warnf("%s: an unsaved draft block, ignored", name)
			continue
		}
		blk := &fcBlock{file: name, name: custom, kv: kv, enable: kv["service"] != "0"}
		if !fcNameRe.MatchString(custom) {
			blk.name = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(name, "fanctrlplus2_"), "fanctrlplus_"), ".cfg")
		}
		var ok bool
		if blk.pwm, ok = num(kv, "pwm"); !ok {
			blk.pwm = 102
		}
		if blk.max, ok = num(kv, "max"); !ok {
			blk.max = 255
		}
		blocks = append(blocks, blk)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no %s fan block found", tool)
	}

	cands := map[string][]*fcCand{}
	for _, blk := range blocks {
		for _, c := range blk.candidates(r, env) {
			cands[c.group] = append(cands[c.group], c)
		}
	}
	if len(cands) == 0 {
		return nil, fmt.Errorf("no temperature range in the %s fan blocks that can be imported", tool)
	}

	var picked []*fcCand
	for _, group := range []string{"cpu", "case"} {
		list := cands[group]
		if len(list) == 0 {
			continue
		}
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.block.enable != b.block.enable {
				return a.block.enable
			}
			if a.block.max != b.block.max {
				return a.block.max > b.block.max
			}
			return a.block.file < b.block.file
		})
		best := list[0]
		for _, other := range list[1:] {
			r.skip(other.block.file, other.block.name, "another block was imported for the "+group+" fans ("+best.block.name+")")
		}
		if !best.block.enable {
			r.warnf("%s: the block is switched off in %s (a dormant config), imported anyway", best.block.file, tool)
		}
		picked = append(picked, best)
	}

	minPct := 100
	seen := map[*fcBlock]bool{}
	var titles []string
	for _, c := range picked {
		r.set(c.group+"_curve", c.curve, c.block.file+": "+c.from)
		if c.input != "" {
			r.set(c.group+"_input", c.input, c.block.file+": "+c.inputFrom)
		}
		minPct = min(minPct, pwmPct(c.block.pwm))
		titles = append(titles, c.block.name)
		if !seen[c.block] {
			seen[c.block] = true
			c.block.skipUnmapped(r, tool)
		}
	}
	// Two groups from one block are flagged.
	if len(picked) == 2 && picked[0].block == picked[1].block {
		r.warnf("%s: one block gave both the CPU and the case curve", picked[0].block.file)
	}
	r.set("min_pct", strconv.Itoa(minPct), "pwm of the imported blocks")
	r.set("mode", "custom", "imported curves need the custom mode")
	r.Title = tool + ": " + strings.Join(dedupe(titles), ", ")
	r.Finish(rules)
	return r, nil
}

func dedupe(s []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func sortedFileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// fcCand: one group's curve and input found in a block.
type fcCand struct {
	block     *fcBlock
	group     string
	curve     string
	from      string
	input     string
	inputFrom string
}

// candidates finds what the block gives: a case curve (drives and auxiliary sensors) and a CPU curve.
func (b *fcBlock) candidates(r *Result, env Env) []*fcCand {
	kv := b.kv
	floor, top := pwmPct(b.pwm), pwmPct(b.max)
	var out []*fcCand

	if kv["cpu_enable"] == "1" {
		lo, ok1 := num(kv, "cpu_min_temp")
		hi, ok2 := num(kv, "cpu_max_temp")
		if !ok1 || !ok2 || lo >= hi {
			r.warnf("%s: the CPU range is not usable, the CPU curve is not imported", b.file)
		} else {
			out = append(out, &fcCand{block: b, group: "cpu", from: "cpu_min_temp, cpu_max_temp, pwm, max",
				curve: r.curveText(b.file+" CPU curve", []point{{lo, floor}, {hi, top}})})
			if s := kv["cpu_sensor"]; s != "" && !cpuChipRe.MatchString(s) {
				r.warnf("%s: cpu_sensor is not a CPU sensor, the CPU temperature is used", b.file)
			}
		}
	}

	drives := b.drives(r)
	var pts []point
	var parts []string
	curveFrom, inputFrom := "", ""
	if drives != nil {
		pts = []point{{drives.low, floor}, {drives.high, top}}
		curveFrom = "low, high, pwm, max"
		if kv["disk_group_count"] != "" && kv["disk_group_count"] != "0" {
			curveFrom = fmt.Sprintf("disk_group_%d_low, disk_group_%d_high, pwm, max", drives.groupIndex, drives.groupIndex)
		}
		var names []string
		for _, id := range drives.disks {
			if n, ok := env.disk(id); ok {
				names = append(names, n)
			} else {
				r.warnf("%s: the drive %q was not found on this NAS", b.file, clipN(id, 80))
			}
		}
		if len(names) > 0 {
			parts = append(parts, "disk_max:"+strings.Join(names, "+"))
			inputFrom = "disks"
		}
	}

	aux := b.auxParts(r)
	if len(aux) > 0 {
		lo, ok1 := num(kv, "aux_min_temp")
		hi, ok2 := num(kv, "aux_max_temp")
		switch {
		case !ok1 || !ok2 || lo >= hi:
			r.warnf("%s: the auxiliary sensors have no usable temperature range, not imported", b.file)
		case drives == nil:
			pts, curveFrom = []point{{lo, floor}, {hi, top}}, "aux_min_temp, aux_max_temp, pwm, max"
			parts, inputFrom = aux, "aux_sensor"
		default:
			if lo != drives.low || hi != drives.high {
				r.warnf("%s: the auxiliary sensors use %d-%d °C, the drives %d-%d °C; one curve serves both", b.file, lo, hi, drives.low, drives.high)
			}
			parts = append(parts, aux...)
			inputFrom += ", aux_sensor"
		}
	}

	if len(pts) > 0 {
		c := &fcCand{block: b, group: "case", from: curveFrom, inputFrom: strings.TrimPrefix(inputFrom, ", ")}
		// A block that only has an auxiliary sensor and is named like a CPU fan drives the CPU fans.
		if drives == nil && len(out) == 0 && cpuHintRe.MatchString(b.name) {
			c.group = "cpu"
		}
		c.curve = r.curveText(b.file+" "+c.group+" curve", pts)
		switch len(parts) {
		case 0:
		case 1:
			c.input = parts[0]
		default:
			c.input = "max:" + strings.Join(parts[:min(len(parts), 8)], "|")
		}
		out = append(out, c)
	}
	return out
}

// drives is the drive range to import: with FanCtrl Plus 2 drive groups the group with the highest top temperature
// (the others are listed), else the flat list. nil when there is none or its range is not usable.
func (b *fcBlock) drives(r *Result) *fcGroupPick {
	kv := b.kv
	var best *fcGroupPick
	if n, _ := num(kv, "disk_group_count"); n > 0 {
		for g := 0; g < min(n, 256); g++ {
			p := fcGroupPick{name: kv[fmt.Sprintf("disk_group_%d_name", g)], groupIndex: g,
				disks: splitCSV(kv[fmt.Sprintf("disk_group_%d_disks", g)])}
			var ok bool
			if p.low, ok = num(kv, fmt.Sprintf("disk_group_%d_low", g)); !ok {
				p.low = 40
			}
			if p.high, ok = num(kv, fmt.Sprintf("disk_group_%d_high", g)); !ok {
				p.high = 60
			}
			if len(p.disks) == 0 {
				continue
			}
			loser := fmt.Sprintf("disk_group_%d", g)
			if best == nil || p.high > best.high {
				if best != nil {
					loser = fmt.Sprintf("disk_group_%d", best.groupIndex)
				} else {
					loser = ""
				}
				cp := p
				best = &cp
			}
			if loser != "" {
				r.skip(b.file, loser, "only one drive group is imported (the one with the highest top temperature)")
			}
		}
	} else if disks := splitCSV(kv["disks"]); len(disks) > 0 {
		best = &fcGroupPick{disks: disks, low: 40, high: 60}
		if v, ok := num(kv, "low"); ok {
			best.low = v
		}
		if v, ok := num(kv, "high"); ok {
			best.high = v
		}
	}
	if best != nil && best.low >= best.high {
		r.warnf("%s: the drive range is not usable, the case curve is not imported", b.file)
		return nil
	}
	return best
}

// auxParts maps the auxiliary sensor tokens to inputs; the rest is listed.
func (b *fcBlock) auxParts(r *Result) []string {
	if b.kv["aux_enable"] != "1" {
		return nil
	}
	var parts []string
	for _, tok := range splitCSV(b.kv["aux_sensor"]) {
		switch {
		case auxGPURe.MatchString(tok):
			parts = append(parts, "gpu")
		case auxHBARe.MatchString(tok):
			parts = append(parts, "hba")
		case auxCustom.MatchString(tok):
			parts = append(parts, "script:"+auxCustom.FindStringSubmatch(tok)[1])
			r.warnf("%s: the script %s is not copied; put it into the sensors folder of this plugin", b.file, auxCustom.FindStringSubmatch(tok)[1])
		default:
			r.skip(b.file, "aux_sensor", fmt.Sprintf("%q has no input here", clipN(tok, 80)))
		}
	}
	return dedupe(parts)
}

func cpuOnly(c *fcCand) []*fcCand {
	if c == nil {
		return nil
	}
	return []*fcCand{c}
}

// skipUnmapped lists the keys of the block that nothing was taken from.
func (b *fcBlock) skipUnmapped(r *Result, tool string) {
	for _, k := range sortedKeys(b.kv) {
		if reason, known := fcReasons[k]; known {
			if reason != "" {
				r.skip(b.file, k, reason)
			}
			continue
		}
		switch k {
		case "pwm", "max", "low", "high", "disks", "cpu_enable", "cpu_sensor", "cpu_min_temp", "cpu_max_temp",
			"disk_group_count", "aux_enable", "aux_sensor", "aux_min_temp", "aux_max_temp":
		default:
			if groupKeyRe.MatchString(k) {
				continue
			}
			r.skip(b.file, k, "not a "+tool+" setting this plugin knows")
		}
	}
}

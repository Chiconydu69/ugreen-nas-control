package ledctl

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
)

var (
	importanceRe = regexp.MustCompile(`^importance="?([a-z]*)"?$`)
	dynLimitRe   = regexp.MustCompile(`^(hot|max|hotssd|maxssd)="([0-9]*)"$`)
	pciRe        = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-9]`)
	ataNameRe    = regexp.MustCompile(`^ata([0-9]+)$`)
)

// dynLoad reads dynamix.cfg: Unraid's global temperature limits and its notification folder.
func (c *Ctl) dynLoad() {
	c.dyn = map[string]string{}
	c.dynNotify = ""
	c.dynLoaded = true

	lines, ok := readLines(c.Env.DynamixCfg)
	if !ok {
		return
	}
	section := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "[") {
			section = line
		} else if m := dynLimitRe.FindStringSubmatch(line); m != nil {
			if _, set := c.dyn[m[1]]; !set {
				c.dyn[m[1]] = m[2]
			}
		} else if section == "[notify]" && c.dynNotify == "" && strings.HasPrefix(line, `path="`) {
			v := line[strings.IndexByte(line, '"')+1:]
			if i := strings.IndexByte(v, '"'); i >= 0 {
				v = v[:i]
			}
			c.dynNotify = v
		}
	}
}

// tempLimits are Unraid's temperature limits for a disk: its own warning/critical, or the
// global hot/max (hotssd/maxssd for SSDs) from dynamix.cfg.
func (c *Ctl) tempLimits(rotational, warning, critical string) (hot, maxT string) {
	if !c.dynLoaded {
		c.dynLoad()
	}
	hotKey, maxKey := "hot", "max"
	if rotational == "0" {
		hotKey, maxKey = "hotssd", "maxssd"
	}
	hot, maxT = c.dyn[hotKey], c.dyn[maxKey]
	if rotational == "0" {
		if hot == "" {
			hot = "60"
		}
		if maxT == "" {
			maxT = "70"
		}
	}
	if warning != "-" {
		hot = warning
	}
	if critical != "-" {
		maxT = critical
	}
	if hot == "" {
		hot = "45"
	}
	if maxT == "" {
		maxT = "55"
	}
	return hot, maxT
}

type stop struct{ t, r, g, b int64 }

// tempColor: blue at hot-15, green at hot-7, yellow at hot, red at max.
func tempColor(temp, hot, maxT int64) string {
	stops := []stop{{hot - 15, 0, 120, 255}, {hot - 7, 0, 200, 83}, {hot, 255, 200, 0}, {maxT, 255, 40, 40}}
	if temp <= stops[0].t {
		return fmt.Sprintf("%d %d %d", stops[0].r, stops[0].g, stops[0].b)
	}
	for i := 1; i <= 3; i++ {
		lo, hi := stops[i-1], stops[i]
		if temp <= hi.t {
			span := max(hi.t-lo.t, 1)
			t := temp - lo.t
			return fmt.Sprintf("%d %d %d",
				lo.r+(hi.r-lo.r)*t/span, lo.g+(hi.g-lo.g)*t/span, lo.b+(hi.b-lo.b)*t/span)
		}
	}
	return "255 40 40"
}

// disksLoad reads Unraid's view of every device from one pass over disks.ini. The first
// section of a device wins.
func (c *Ctl) disksLoad() {
	c.disks = map[string]string{}
	var name, device, status, spundown, temp, errs, rot, warn, crit, id string
	flush := func() {
		if device == "" || name == "" {
			return
		}
		if _, set := c.disks[device]; set {
			return
		}
		if !isDigits(temp) {
			temp = "-"
		}
		if !isDigits(warn) {
			warn = "-"
		}
		if !isDigits(crit) {
			crit = "-"
		}
		c.disks[device] = strings.Join([]string{name, status, orStr(spundown, "0"), temp, orStr(errs, "0"), orStr(rot, "1"), warn, crit, orStr(id, "-")}, " ")
	}
	reset := func() {
		name, device, status, spundown, temp, errs, rot, warn, crit, id = "", "", "", "", "", "", "", "", "", ""
	}

	lines, ok := readLines(c.Env.DisksINI)
	if !ok {
		return
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "[") {
			flush()
			reset()
			continue
		}
		if !strings.Contains(line, `="`) {
			continue
		}
		key := line[:strings.IndexByte(line, '=')]
		v := line[strings.Index(line, `="`)+2:]
		if i := strings.IndexByte(v, '"'); i >= 0 {
			v = v[:i]
		}
		switch key {
		case "rotational":
			rot = v
		case "warning":
			warn = v
		case "critical":
			crit = v
		case "id":
			id = v
		case "name":
			name = v
		case "device":
			device = v
		case "status":
			status = v
		case "spundown":
			spundown = v
		case "temp":
			temp = v
		case "numErrors":
			errs = v
		}
	}
	flush()
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func field(words []string, n int, def string) string {
	if n-1 < len(words) && words[n-1] != "" {
		return words[n-1]
	}
	return def
}

// syncStateLoad reads a running parity check/rebuild from var.ini.
func (c *Ctl) syncStateLoad() {
	c.syncSt = syncState{}
	lines, ok := readLines(c.Env.VarINI)
	if !ok {
		return
	}
	var resync, pos, size, action string
	for _, line := range lines {
		var key string
		switch {
		case strings.HasPrefix(line, `mdResync="`):
			key = "resync"
		case strings.HasPrefix(line, `mdResyncPos="`):
			key = "pos"
		case strings.HasPrefix(line, `mdResyncSize="`):
			key = "size"
		case strings.HasPrefix(line, `mdResyncAction="`):
			key = "action"
		default:
			continue
		}
		v := line[strings.IndexByte(line, '"')+1:]
		if i := strings.IndexByte(v, '"'); i >= 0 {
			v = v[:i]
		}
		switch key {
		case "resync":
			resync = v
		case "pos":
			pos = v
		case "size":
			size = v
		case "action":
			if f := strings.Fields(v); len(f) > 0 {
				action = f[0]
			} else {
				action = ""
			}
		}
	}
	if resync == "" || resync == "0" {
		return
	}
	p, s := uint64(0), uint64(0)
	if isDigits(pos) {
		p, _ = strconv.ParseUint(pos, 10, 64)
	}
	if isDigits(size) {
		s, _ = strconv.ParseUint(size, 10, 64)
	}
	percent := int64(0)
	if s > 0 {
		percent = int64(p * 100 / s)
	}
	c.syncSt = syncState{active: true, action: action, percent: percent}
}

// mappingSource says how sure the port table is: tested, reported by users, or guessed.
func (c *Ctl) mappingSource() string {
	switch {
	case globAny(c.product, "DXP6800*", "DX4600*", "DXP8800*"):
		return "tested"
	case globMatch("DXP4800*GT*", c.product):
		return "tested"
	case globAny(c.product, "DX4700*", "DXP2800*", "DXP4800*"):
		return "reported"
	}
	return "guessed"
}

// idxBayPorts: iDX6011 / iDX6012 bay order from the SATA controllers (PCI vendor), each in port order.
// Plain model: bays 1-4 on the ASMedia ASM1164 (0x1b21), bays 5-6 on the Intel PCH (0x8086).
// Pro: ASMedia ports 1-2 are bays 1-2, the Intel ports are bays 3-4, ASMedia ports 3-4 are bays 5-6.
// Nil if the controllers aren't found with 4 + 2 ports.
//
// Facts from MIT-licensed community plugins (see docs/third-party.md):
//   - Pro: noodlemctwoodle/unraid-ugreen-idx6011-pro src/monitor.sh:70-75 (commit d202a7c),
//     "Intel PCH SATA -> bays 3,4; ASMedia ASM1164 -> bays 1,2,5,6".
//   - plain: omgapuppy/unraid-ugreen-idx6011-led-fan src/monitor.sh:70-75 (commit 6196dc0),
//     "Intel -> bays 5,6; ASMedia -> bays 1-4".
func (c *Ctl) idxBayPorts() []string {
	var ports []int
	for _, n := range listDir(c.Env.SysATA) {
		if m := ataNameRe.FindStringSubmatch(n); m != nil {
			v, _ := strconv.Atoi(m[1])
			ports = append(ports, v)
		}
	}
	sort.Ints(ports)

	byVendor := map[string][]string{}
	for _, vendor := range []string{"0x1b21", "0x8086"} {
		for _, port := range ports {
			name := "ata" + strconv.Itoa(port)
			all := pciRe.FindAllString(readlinkF(c.Env.SysATA+"/"+name), -1)
			if len(all) == 0 {
				continue
			}
			data, err := os.ReadFile(c.Env.SysPCI + "/" + all[len(all)-1] + "/vendor")
			if err != nil || strings.TrimRight(string(data), "\n") != vendor {
				continue
			}
			byVendor[vendor] = append(byVendor[vendor], name)
		}
	}
	asm, intel := byVendor["0x1b21"], byVendor["0x8086"]

	if c.isIDXPro() {
		if len(asm) != 4 || len(intel) != 2 {
			return nil
		}
		return []string{asm[0], asm[1], intel[0], intel[1], asm[2], asm[3]}
	}
	out := append(append([]string{}, asm...), intel...)
	if len(out) != 6 {
		return nil
	}
	return out
}

func (c *Ctl) isIDXPro() bool { return chip.IdxLayout(c.product) == "idx-pro" }

// bayPortsLoad works out the SATA port of each bay, left to right, after
// miskcoo/ugreen_leds_controller. The table is fixed hardware: once per process.
func (c *Ctl) bayPortsLoad() []string {
	if c.bayPorts != nil {
		return c.bayPorts
	}
	switch {
	case globMatch("iDX601[12]*", c.product):
		c.bayPorts = c.idxBayPorts()
		if c.bayPorts == nil {
			c.bayPorts = []string{"ata1", "ata2", "ata3", "ata4", "ata5", "ata6"}
		}
	case globMatch("DXP6800*", c.product):
		c.bayPorts = []string{"ata3", "ata4", "ata5", "ata6", "ata1", "ata2"}
	default:
		c.bayPorts = []string{}
		for i := 1; i <= c.bays; i++ {
			c.bayPorts = append(c.bayPorts, "ata"+strconv.Itoa(i))
		}
	}
	return c.bayPorts
}

// devpathsRefresh finds where each sdX sits (resolved /sys/block/sdX/device). Redone when the
// set of disks changes and at the latest after a minute.
func (c *Ctl) devpathsRefresh() {
	var list []string
	sig := ""
	for _, n := range listDir(c.Env.SysBlock) {
		if strings.HasPrefix(n, "sd") && exists(c.Env.SysBlock+"/"+n) {
			list = append(list, n)
			sig += " " + n
		}
	}
	c.devList = list

	if c.devLoaded && sig == c.devSig && c.Now().Sub(c.devAt) < time.Minute {
		return
	}
	c.devPath = map[string]string{}
	c.serials = map[string]string{}
	for _, d := range list {
		c.devPath[d] = readlinkF(c.Env.SysBlock + "/" + d + "/device")
	}
	c.devSig = sig
	c.devAt = c.Now()
	c.devLoaded = true
}

// diskSerial is the serial of a disk, from the udev database file (what udevadm reads),
// udevadm as fallback. Remembered until devpathsRefresh redoes the device table.
func (c *Ctl) diskSerial(dev string) string {
	if v, ok := c.serials[dev]; ok {
		return v
	}
	serial := ""
	found := false
	if mm, ok := readLine(c.Env.SysBlock + "/" + dev + "/dev"); ok && isReadable(c.Env.UdevData+"/b"+mm) {
		found = true
		lines, _ := readLines(c.Env.UdevData + "/b" + mm)
		for _, line := range lines {
			if v, ok := strings.CutPrefix(line, "E:ID_SERIAL_SHORT="); ok {
				serial = v
				break
			}
		}
	}
	if !found {
		out, _ := c.Run.Output("udevadm", "info", "-q", "property", "-n", "/dev/"+dev)
		var vals []string
		for _, line := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(line, "ID_SERIAL_SHORT="); ok {
				vals = append(vals, v)
			}
		}
		serial = strings.Join(vals, "\n")
	}
	c.serials[dev] = serial
	return serial
}

// bayDevice is the block device (sdX) in a bay. A saved bay<N>_serial wins over the port table.
func (c *Ctl) bayDevice(bay int) string {
	serial := c.cfg["bay"+strconv.Itoa(bay)+"_serial"]
	ports := c.bayPortsLoad()
	port := ""
	if bay-1 < len(ports) {
		port = ports[bay-1]
	}
	for _, dev := range c.devList {
		if serial != "" {
			if c.diskSerial(dev) == serial {
				return dev
			}
		} else if port != "" && strings.Contains(c.devPath[dev], "/"+port+"/") {
			return dev
		}
	}
	return ""
}

func isErrorStatus(s string) bool {
	switch s {
	case "DISK_INVALID", "DISK_DSBL", "DISK_DSBL_NEW", "DISK_WRONG", "DISK_NP_MISSING":
		return true
	}
	return false
}

var leadingDigits = regexp.MustCompile(`^[0-9]+`)

// smartNow reads the watched SMART values from Unraid's cached smartctl output, plus Unraid's
// read/write error count: "5=0 187=0 197=0 198=0 failed=0 errors=0".
// 188 (Command_Timeout) is left out: Seagate reports raw values like "0 0 0".
func (c *Ctl) smartNow(slot, errs string) string {
	path := c.Env.SmartDir + "/" + slot
	if !isReadable(path) {
		return "5=0 187=0 197=0 198=0 failed=0 errors=" + errs
	}
	val := map[string]uint64{"5": 0, "187": 0, "197": 0, "198": 0}
	failed := 0
	data, _ := os.ReadFile(path)
	text := string(data)
	for text != "" {
		line := text
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			text = ""
			// `while read` drops an unterminated last line
			break
		}
		f := splitRead(line, 11)
		a1, a9, a10 := f[0], f[8], f[9]
		switch a1 {
		case "5", "187", "197", "198":
			raw := a10
			if i := strings.IndexByte(raw, '/'); i >= 0 {
				raw = raw[:i]
			}
			v := uint64(0)
			if m := leadingDigits.FindString(raw); m != "" {
				v, _ = strconv.ParseUint(m, 10, 64)
			}
			val[a1] = v
			if a9 != "-" {
				failed = 1
			}
		}
		if isDigits(a1) && a9 != "-" && a9 != "" {
			failed = 1
		}
	}
	return fmt.Sprintf("5=%d 187=%d 197=%d 198=%d failed=%d errors=%s", val["5"], val["187"], val["197"], val["198"], failed, errs)
}

// smartWarnings lists the values that rose above what was acknowledged ("5=8 errors=2").
func smartWarnings(now, ack string) string {
	var joined []string
	for _, pair := range strings.Fields(now) {
		name, value, _ := strings.Cut(pair, "=")
		acked := ""
		for _, word := range strings.Fields(ack) {
			if v, ok := strings.CutPrefix(word, name+"="); ok {
				acked = v
			}
		}
		if acked == "" {
			acked = "0"
		}
		v, ok1 := atoi(value)
		a, ok2 := atoi(acked)
		if ok1 && ok2 && v > a {
			joined = append(joined, pair)
		}
	}
	return strings.Join(joined, " ")
}

// smartBad: a disk's SMART values rose above what was acknowledged (slot, errors, Unraid id).
func (c *Ctl) smartBad(slot, errs, id string) bool {
	now := c.smartNow(slot, errs)
	return smartWarnings(now, c.cfg["smart_ack_"+sanitizeKey(id)]) != ""
}

// standbyBrightness is the brightness of a spun-down disk: standby_mode normal|dim|off|pulse,
// standby_level in %. "pulse" keeps the brightness; the bay breathes slowly instead.
func (c *Ctl) standbyBrightness(b int64) int64 {
	switch c.get("standby_mode", "dim") {
	case "normal", "pulse":
		return b
	case "off":
		return 0
	}
	level := atoiOr(c.get("standby_level", "30"), 0)
	b = b * level / 100
	if b < 1 {
		b = 1
	}
	return b
}

func (c *Ctl) baysJSON() {
	c.disksLoad()
	c.devpathsRefresh()

	var sb strings.Builder
	sb.WriteString("[")
	for bay := 1; bay <= c.bays; bay++ {
		dev := c.bayDevice(bay)
		serial, slot, status, spundown := "", "", "", "0"
		temp, errs := "-", "0"
		hot, maxT := "45", "55"
		now, key, warnings := "", "", ""

		if dev != "" {
			serial = c.diskSerial(dev)
			w := strings.Fields(c.disks[dev])
			slot = field(w, 1, "")
			status = field(w, 2, "")
			spundown = field(w, 3, "0")
			temp = field(w, 4, "-")
			errs = field(w, 5, "0")
			hot, maxT = c.tempLimits(field(w, 6, "1"), field(w, 7, "-"), field(w, 8, "-"))
			if slot != "" {
				now = c.smartNow(slot, errs)
				key = sanitizeKey("smart_ack_" + field(w, 9, "-"))
				warnings = smartWarnings(now, c.cfg[key])
			}
		}

		tempJSON := temp
		if temp == "-" {
			tempJSON = "null"
		}
		if bay > 1 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"bay":%d,"led":"disk%d","device":"%s","serial":"%s","slot":"%s","status":"%s","error":%t,"standby":%t,"temp":%s,"errors":%s,"temp_hot":%s,"temp_max":%s,"smart_now":"%s","smart_key":"%s","smart_warnings":"%s"}`,
			bay, bay, dev, serial, slot, status, isErrorStatus(status), spundown == "1", tempJSON, errs, hot, maxT, now, key, warnings)
	}
	sb.WriteString("]\n")
	c.out("%s", sb.String())
}

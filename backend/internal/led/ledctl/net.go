package ledctl

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	routeDevRe = regexp.MustCompile(` dev ([^ ]+)`)
	routeViaRe = regexp.MustCompile(`^default via ([^ ]+)`)
	baseSpeed  = regexp.MustCompile(`[0-9]+base`)
)

func (c *Ctl) ipRouteDefault() []string {
	out, _ := c.Run.Output("ip", "route", "show", "default")
	s := string(out)
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// networkDevice is the interface behind the default route; a bridge is resolved to its uplink
// (e.g. br0 -> bond0).
func (c *Ctl) networkDevice() string {
	dev := ""
	// Unraid adds a second default route over shim-br0 (a macvlan for Docker): not the uplink.
	for _, line := range c.ipRouteDefault() {
		if m := routeDevRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(m[1], "shim-") {
			dev = m[1]
			break
		}
	}

	if dev != "" && isDir(c.Env.SysNet+"/"+dev+"/brif") {
		for _, port := range listDir(c.Env.SysNet + "/" + dev + "/brif") {
			if globAny(port, "veth*", "vnet*", "tap*", "docker*", "virbr*") {
				continue
			}
			dev = port
			break
		}
	}
	return dev
}

func (c *Ctl) networkOK(dev string) bool {
	state, _ := readLine(c.Env.SysNet + "/" + dev + "/operstate")
	if state != "up" {
		return false
	}
	gateway := ""
	for _, line := range c.ipRouteDefault() {
		if m := routeViaRe.FindStringSubmatch(line); m != nil {
			gateway = m[1]
			break
		}
	}
	if gateway == "" {
		return true
	}
	_, code := c.Run.Output("ping", "-c", "1", "-W", "2", gateway)
	return code == 0
}

// linkSpeed is the negotiated speed in Mbit/s as printed, "0" when unknown (no link, or the
// driver doesn't say). A bond, bridge or VLAN reports an aggregate or a made-up value, which
// isn't the speed of any cable: it follows its members down to the physical ports and uses
// the fastest one with a link.
func (c *Ctl) linkSpeed(dev string, depth int) string {
	best := "0"
	members := false

	if depth < 5 {
		for _, name := range listDir(c.Env.SysNet + "/" + dev) {
			member, ok := strings.CutPrefix(name, "lower_")
			if !ok || !exists(c.Env.SysNet+"/"+dev+"/"+name) {
				continue
			}
			if globAny(member, "veth*", "vnet*", "tap*") {
				continue
			}
			members = true
			speed := c.linkSpeed(member, depth+1)
			if atoiOr(speed, 0) > atoiOr(best, 0) {
				best = speed
			}
		}
	}
	if atoiOr(best, 0) > 0 {
		return best
	}
	if members {
		return "0"
	}

	speed, _ := readLine(c.Env.SysNet + "/" + dev + "/speed")
	if !isDigits(speed) {
		return "0"
	}
	if n, err := strconv.ParseInt(speed, 10, 64); err == nil && n > 0 {
		return speed
	}
	return "0"
}

// portMaxSpeed is the highest speed (Mbit/s) a port supports according to ethtool, 0 when
// unknown. It does not change while the NAS runs: asked once, then remembered in tmpfs.
func (c *Ctl) portMaxSpeed(name string) string {
	if !c.Run.Has("ethtool") {
		return "0"
	}
	if lines, ok := readLines(c.Env.MaxspeedCache); ok {
		for _, line := range lines {
			f := splitRead(line, 2)
			if f[0] == name {
				return f[1]
			}
		}
	}

	out, _ := c.Run.Output("ethtool", name)
	best := int64(0)
	in := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Supported link modes:") {
			in = true
		}
		if strings.Contains(line, "Supported pause frame use:") {
			in = false
		}
		if !in {
			continue
		}
		for _, m := range baseSpeed.FindAllString(line, -1) {
			if n, _ := strconv.ParseInt(strings.TrimSuffix(m, "base"), 10, 64); n > best {
				best = n
			}
		}
	}
	speed := itoa(best)
	if best > 0 {
		appendFile(c.Env.MaxspeedCache, fmt.Sprintf("%s %s\n", name, speed))
	}
	return speed
}

// networkPortsJSON lists the physical network ports (they have a /device link; bonds,
// bridges and veths don't).
func (c *Ctl) networkPortsJSON() string {
	var parts []string
	for _, name := range listDir(c.Env.SysNet) {
		dir := c.Env.SysNet + "/" + name
		if !exists(dir + "/device") {
			continue
		}
		if name == "lo" || globAny(name, "veth*", "vnet*", "tap*", "docker*", "virbr*", "wl*") {
			continue
		}
		carrier, _ := readLine(dir + "/carrier")
		speed, _ := readLine(dir + "/speed")
		if !isDigits(speed) {
			speed = "0"
		}
		if carrier != "1" {
			speed = "0"
		}
		parts = append(parts, fmt.Sprintf(`{"name": "%s", "link": %t, "speed": %s, "max": %s}`,
			name, carrier == "1", speed, c.portMaxSpeed(name)))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// speedColor is "R G B" for a speed in Mbit/s from the saved colours.
func (c *Ctl) speedColor(speed int64) string {
	use := 10
	for _, step := range speedSteps {
		if speed >= int64(step) {
			use = step
		}
	}
	return c.colorCfg("netdev_speed_"+strconv.Itoa(use), defaultSpeedColor(use))
}

func (c *Ctl) speedColorsJSON() string {
	var parts []string
	for _, step := range speedSteps {
		s := strconv.Itoa(step)
		parts = append(parts, fmt.Sprintf(`"%s": "%s"`, s, c.colorCfg("netdev_speed_"+s, defaultSpeedColor(step))))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

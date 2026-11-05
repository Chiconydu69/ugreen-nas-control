package ledctl

import (
	"regexp"
	"slices"
	"strconv"
)

// The keys of leds.cfg this package reads. nasctl diag prints a key's value only when KnownKey knows it, so a key the
// code starts to read must be added here (TestKeyTableCoversTheParser fails otherwise).
var staticKeys = []string{
	"mode", "standby_mode", "standby_level", "disk_color_mode", "disk_error_color",
	"alert_enabled", "alert_level", "alert_color", "sync_enabled", "sync_color",
	"smart_enabled", "smart_color", "error_enabled", "activity_enabled",
	"shutdown_blink", "shutdown_color", "shutdown_effect",
	"night_enabled", "night_action", "night_start", "night_end", "night_brightness",
	"netdev_status", "netdev_speed_enabled",
	"fx_name", "fx_leds", "fx_speed", "fx_reverse", "fx_idle",
}

var (
	// <led>_color|brightness|effect|on|off for power, netdev, netdev2 and disk1..disk99.
	perLEDKeyRe = regexp.MustCompile(`^(power|netdev2?|disk[1-9][0-9]?)_(color|brightness|effect|on|off)$`)
	speedKeyRe  = regexp.MustCompile(`^netdev_speed_([0-9]+)$`)
	// bay<N>_serial and smart_ack_<id> carry a drive serial.
	privateKeyRe = regexp.MustCompile(`^(bay[0-9]+_serial|smart_ack_.+)$`)
)

// KnownKey says whether key is a setting of leds.cfg and whether its value identifies a drive (never print it).
func KnownKey(key string) (known, private bool) {
	switch {
	case slices.Contains(staticKeys, key), perLEDKeyRe.MatchString(key):
		return true, false
	case privateKeyRe.MatchString(key):
		return true, true
	}
	if m := speedKeyRe.FindStringSubmatch(key); m != nil {
		n, _ := strconv.Atoi(m[1])
		return slices.Contains(speedSteps, n), false
	}
	return false, false
}

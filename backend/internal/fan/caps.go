package fan

// FanCaps is what the web page may offer for this model, so that it holds no model list of its own (`fan_caps` in
// the status; absent for a model without fan control).
type FanCaps struct {
	Kind        string `json:"kind"`                    // KindFixed, KindProbe or KindReadOnly
	Channels    int    `json:"channels"`                // fans the plugin drives; 0 on a probe model until a map is confirmed
	Watchdog    bool   `json:"watchdog"`                // a watchdog petter runs (EC or IT8613E)
	Backlight   bool   `json:"backlight"`               // the LCD backlight can be read and set
	MinPctFloor int    `json:"min_pct_floor,omitempty"` // the lowest speed the hardware runs at; absent when the model has no floor
}

// WatchdogModel: a watchdog is fed on the DXP4800 GT and the iDX6011 Pro (IT5571 EC, one map: ug_it55_4800gt_functions.c:534-536,
// ug_it55pro_functions.c:525-527@c75c5ab) and on the IT8613E models (isIT8613Model; it87_wdt.c:86-96@c75c5ab). Never on
// the plain iDX6011/iDX6012: its watchdog sits at 0x55-0x57 (ug_it55_functions.c:470-472, read only here), and 0x98/0x99
// is the second system fan's tach there (:30-31), so a write would be a fan register write, not a feed.
func WatchdogModel(model string) bool {
	return ecWatchdogModel(model) || isIT8613Model(model)
}

// ecWatchdogModel: the models whose IT5571 has the enable byte at 0x98.
func ecWatchdogModel(model string) bool { return isDXP4800GT(model) || isIdxPro(model) }

// isIT8613Model: the models with an IT8613E Super-I/O chip whose watchdog the petter feeds. The chip id is checked again
// on every access (runSIO), so a variant without the chip is never written.
func isIT8613Model(model string) bool {
	switch model {
	case "DXP2800", "DXP4800", "DXP4800 Plus", "DXP4800 Pro", "DXP6800 Pro", "DXP8800 Plus":
		return true
	}
	return false
}

func CapsFor(model string) (FanCaps, bool) {
	kind := FanKind(model)
	if kind == "" {
		return FanCaps{}, false
	}
	c := FanCaps{Kind: kind, Watchdog: WatchdogModel(model), Backlight: isIdxPro(model)}
	c.MinPctFloor = minPctFloor(model)
	switch {
	case isDXP4800GT(model):
		c.Channels = len(ecChannels)
	case isIdxPro(model):
		c.Channels = len(ecChannelsIdxPro)
	case isIdxPlain(model):
		c.Channels = len(ecChannelsIdx)
	case kind == KindProbe:
		if m, err := loadFanMap(model); err == nil {
			c.Channels = len(m.Outputs)
		}
	default:
		c.Channels = len(it8613Channels)
	}
	return c, true
}

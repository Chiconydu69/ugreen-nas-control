package fan

import "context"

// What drove a fan this tick: the input that gave the temperature for its curve, and what changed the speed after
// the curve. The pipeline fills it from the decisions it makes anyway (shape_pipeline.go); the history records it.

// Sources of a speed. Besides these, a source is "disk:<name>" (hottest drive of the selection), "disk_avg", "nvme",
// "gpu", "hba", "nic:<iface>" or "script:<name>"; "emergency:<reason>" while an emergency holds all fans at 100 %.
const (
	SrcCPU         = "cpu"
	SrcIdle        = "idle"        // all drives asleep and no NVMe: the lowest point of the curve
	SrcMissing     = "missing"     // the configured input could not be read (curve_low)
	SrcHold        = "hold"        // ... and the speed is held (hold)
	SrcBIOS        = "bios"        // the BIOS has the fans, the daemon only watches
	SrcCalibration = "calibration" // the calibration drives the fans
	SrcRival       = "rival"       // another fan controller is running, the daemon only reads
)

// Modifiers (bit set) that changed the speed after the curve, or the input.
const (
	ModQuiet     uint8 = 1 << iota // the quiet hours cap lowered it
	ModMover                       // the mover job raised it
	ModParity                      // the parity check job raised it
	ModFloor                       // the minimum speed raised it (the curve asked for less)
	ModShape                       // deadband, kick-start or a ramp holds it away from the curve
	ModEmergency                   // emergency: 100 %
	ModMissing                     // the configured input was missing (fallback or hold)
)

var jobMod = map[string]uint8{"mover": ModMover, "parity": ModParity}

// Drive: Source names the input, Temp is its value in °C (nil: no temperature), Mods the modifiers.
type Drive struct {
	Source string
	Temp   *float64
	Mods   uint8
}

func tempOf(t *int) *float64 {
	if t == nil {
		return nil
	}
	v := float64(*t)
	return &v
}

// hottestDisk names the hottest drive with a reading among those allowed (empty list = all) that are not ignored.
func hottestDisk(s Sensors, ignore map[string]bool, only []string) string {
	name, best := "", 0
	for _, d := range s.Disks {
		if d.Temp == nil || ignore[d.Name] || len(only) > 0 && !hasName(only, d.Name) {
			continue
		}
		if name == "" || *d.Temp > best {
			name, best = d.Name, *d.Temp
		}
	}
	return name
}

// autoDrive: the default input. The CPU for a cpu fan; for a case fan the hottest drive (or the NVMe, minus its
// offset) of Decide. mods carries what the caller knows already.
func autoDrive(d Decision, s Sensors, c Config, role string, temp float64, hasTemp bool, mods uint8) Drive {
	switch {
	case role == "cpu":
		return Drive{Source: SrcCPU, Temp: &temp, Mods: mods}
	case !hasTemp:
		return Drive{Source: SrcIdle, Mods: mods}
	case d.CaseFrom == "nvme":
		return Drive{Source: "nvme", Temp: &temp, Mods: mods}
	}
	if name := hottestDisk(s, c.IgnoreDrives, nil); name != "" {
		return Drive{Source: "disk:" + name, Temp: &temp, Mods: mods}
	}
	return Drive{Source: "disk_max", Temp: &temp, Mods: mods}
}

// inputDrive names a configured input that was read; temp is its reading. A max: input names the part that won.
func inputDrive(in InputSource, spec InputSpec, s Sensors, c Config, temp float64) Drive {
	switch spec.Kind {
	case "max":
		var best *Drive
		for _, p := range spec.Parts {
			if t, ok := in.Read(context.Background(), p); ok && (best == nil || t > *best.Temp) {
				d := inputDrive(in, p, s, c, t)
				best = &d
			}
		}
		if best != nil {
			return *best
		}
	case "disk_max":
		if name := hottestDisk(s, c.IgnoreDrives, spec.Disks); name != "" {
			return Drive{Source: "disk:" + name, Temp: &temp}
		}
	case "nic", "script":
		return Drive{Source: spec.Kind + ":" + spec.Name, Temp: &temp}
	}
	return Drive{Source: spec.Kind, Temp: &temp}
}

// emergencyDrive: all fans at 100 %; the source is the reason, the temperature the reading that caused it.
func emergencyDrive(d Decision, s Sensors, c Config) curveResult {
	var temp *float64
	switch d.Failsafe {
	case "cpu_hot", "cpu_unreadable":
		temp = tempOf(s.CPU)
	case "disk_hot":
		temp = tempOf(s.MaxDiskExcept(c.IgnoreDrives))
	case "nvme_hot":
		if n := s.MaxNVMeExcept(c.IgnoreDrives); n != nil {
			v := float64(*n - c.NVMeOffset)
			temp = &v
		}
	}
	return curveResult{Target: 100, Drive: Drive{Source: "emergency:" + d.Failsafe, Temp: temp, Mods: ModEmergency}}
}
